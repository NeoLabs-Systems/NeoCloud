import 'dart:async';
import 'dart:io';
import 'dart:ui';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/widgets.dart';
import 'package:neocloud_background/neocloud_background.dart';
import 'package:path_provider/path_provider.dart';
import 'package:photo_manager/photo_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:workmanager/workmanager.dart';

import '../api/backend_client.dart';
import '../device/device_session.dart';
import '../files/upload_queue.dart';
import '../l10n/app_language.dart';
import 'background_reliability.dart';
import 'camera_backup.dart';
import 'camera_dhash.dart';
import 'camera_lease.dart';
import 'camera_upload.dart';
import 'camera_upload_stub.dart';

CameraUploads createCameraUploads() => Platform.isAndroid || Platform.isIOS ? _IoCameraUploads() : NoCameraUploads();

const String _settingsKey = 'camera.settings';
const String _limitedKey = 'camera.limited';
const String _reconfirmKey = 'camera.reconfirm';
const String _pausedKey = 'camera.pausedUntil';

/// Pause in the notification holds automatic passes back this long; "Back
/// up now" ends it sooner.
const Duration _pauseFor = Duration(hours: 1);

/// A pass that waits for Wi-Fi tries again in the background after this,
/// once Wi-Fi is there.
const Duration _wifiRecheck = Duration(minutes: 15);

/// [_onWifiSettled] asks this often, this far apart, before a pass stops
/// for Wi-Fi: about 20 seconds in all.
const int _wifiSettleChecks = 5;
const Duration _wifiSettleWait = Duration(seconds: 5);

/// How often a running pass renews its lease while one file takes long;
/// before each file it renews it anyway. Well below [CameraLease.staleAfter].
const Duration _leaseRenewal = Duration(minutes: 1);

/// Must match BGTaskSchedulerPermittedIdentifiers in ios/Runner/Info.plist
/// and the registration in AppDelegate.swift. iOS runs it when the system
/// decides (typically a few times a day). Android books its own background
/// work (neocloud_background's CameraWork) and runs [cameraBackgroundMain];
/// this name only cancels the task earlier versions booked there.
const String cameraUploadTaskId = 'systems.neolabs.neocloud.cameraUpload';

/// iOS only: a BGProcessingTask, minutes long, which iOS runs mostly
/// overnight on the charger. Registered in AppDelegate.swift and listed in
/// Info.plist like [cameraUploadTaskId]; one-shot, so every pass books the
/// next.
const String cameraProcessingTaskId = 'systems.neolabs.neocloud.cameraUpload.processing';

/// The iOS background entry point: workmanager starts a headless engine and
/// calls this for both BGTasks (and, on Android, for work an earlier
/// version booked until the app replaces it). It signs in with the stored
/// device token and runs one pass, if camera upload is on for that very
/// account. On iOS the processing task is booked again first: iOS may end
/// the task before the pass ends.
@pragma('vm:entry-point')
void cameraUploadDispatcher() {
  Workmanager().executeTask((task, _) async {
    WidgetsFlutterBinding.ensureInitialized();
    // The notification speaks the app's language.
    await ensureAppLanguageLoaded();
    final connection = await createDeviceSessions().read();
    final account = connection?.accountKey;
    if (connection == null || account == null) return true;
    final client = BackendClient(baseUrl: connection.server)..bearerToken = connection.token;
    final preferences = await SharedPreferences.getInstance();
    Future<void> arm() async {
      final settings = CameraUploadSettings.decode(preferences.getString(_settingsKey));
      if (settings.activeFor(account) && settings.folderId != null) await _armTriggers(settings).catchError((Object _) {});
    }

    // On Android, booking replaces the earlier version's work, this task
    // included: only once its pass is over.
    if (Platform.isIOS) await arm();
    try {
      // Android runs the task only while charging when that was asked for.
      await _pass(client, preferences, account: account, charging: Platform.isAndroid ? true : null);
      return true;
    } catch (_) {
      return false;
    } finally {
      if (Platform.isAndroid) await arm();
    }
  });
}

/// The Android background entry point: CameraUploadWorker starts an engine
/// on it for every background pass (hourly, after new photos, to continue
/// a stopped pass) and ends when it reports back. The worker books the next
/// new-photo trigger itself, and a pass that stops with files left books
/// its continuation (see [_pass]).
@pragma('vm:entry-point')
Future<void> cameraBackgroundMain() async {
  WidgetsFlutterBinding.ensureInitialized();
  DartPluginRegistrant.ensureInitialized();
  try {
    await ensureAppLanguageLoaded();
    final connection = await createDeviceSessions().read();
    final account = connection?.accountKey;
    if (connection != null && account != null) {
      final client = BackendClient(baseUrl: connection.server)..bearerToken = connection.token;
      // The work runs only while charging when that was asked for.
      await _pass(client, await SharedPreferences.getInstance(), account: account, charging: true);
    }
  } catch (error) {
    debugPrint('camera background pass: $error');
  } finally {
    await NeoCloudBackground.cameraWorkerDone();
  }
}

/// Books the background work for [settings]: on Android all of it (native,
/// see [cameraBackgroundMain]), on iOS the one-shot processing task; iOS's
/// periodic task is registered once, in `update`.
Future<void> _armTriggers(CameraUploadSettings settings) async {
  if (Platform.isAndroid) {
    final callback = PluginUtilities.getCallbackHandle(cameraBackgroundMain);
    if (callback == null) return;
    await NeoCloudBackground.configureCameraWork(
      callback: callback.toRawHandle(),
      unmeteredOnly: settings.wifiOnly,
      chargingOnly: settings.chargingOnly,
      channelName: appStrings.backgroundNotificationChannel,
      title: appStrings.backgroundNotificationChecking,
      pauseLabel: appStrings.backgroundNotificationPause,
    );
  } else if (Platform.isIOS) {
    await Workmanager().registerProcessingTask(
      cameraProcessingTaskId,
      cameraProcessingTaskId,
      constraints: Constraints(
        networkType: settings.wifiOnly ? NetworkType.unmetered : NetworkType.connected,
        requiresCharging: true,
      ),
    );
  }
}

Future<void> _disarmTriggers(Workmanager workmanager) async {
  await NeoCloudBackground.disableCameraWork();
  if (Platform.isIOS) await workmanager.cancelByUniqueName(cameraProcessingTaskId);
}

/// Holds the platform's background allowance while a pass uploads: on
/// Android the transfer service (progress notification, partial wakelock),
/// on iOS the extra time an app leaving the screen gets. Pause in the
/// notification and the system's time limits become the pass's
/// [CameraBackupEngine.run] interruption.
class _CameraTransfer {
  static const String _owner = 'camera';

  _CameraTransfer() {
    // Before anything shows: a background worker can be stopped while the
    // pass still compares the library with the server.
    NeoCloudBackground.watchStop(_owner, _stop);
  }

  bool _held = false;
  DateTime _shown = DateTime(0);
  CameraBlock? stopWith;

  void _stop(BackgroundStopReason reason) => stopWith = reason == BackgroundStopReason.pause ? CameraBlock.paused : CameraBlock.interrupted;

  void show(CameraProgress progress) {
    final now = DateTime.now();
    final title = appStrings.backgroundNotificationProgress(progress.current, progress.total);
    if (!_held) {
      _held = true;
      _shown = now;
      unawaited(NeoCloudBackground.startTransfer(
        owner: _owner,
        channelName: appStrings.backgroundNotificationChannel,
        title: title,
        body: progress.name,
        pauseLabel: appStrings.backgroundNotificationPause,
        current: progress.current,
        total: progress.total,
        onStop: _stop,
      ));
      return;
    }
    // iCloud downloads report many times a second.
    if (now.difference(_shown) < const Duration(seconds: 1)) return;
    _shown = now;
    unawaited(NeoCloudBackground.updateTransfer(
      owner: _owner,
      title: title,
      body: progress.name,
      current: progress.current,
      total: progress.total,
    ));
  }

  Future<void> release() => NeoCloudBackground.stopTransfer(_owner);
}

Future<Directory> _cameraDir() async {
  final dir = Directory('${(await getApplicationSupportDirectory()).path}${Platform.pathSeparator}camera');
  await dir.create(recursive: true);
  return dir;
}

/// The ledger as `<app support>/camera/ledger.json`, replaced atomically
/// (write a temp file, then rename over the old one). A ledger of another
/// account than [account] reads as empty.
class _FileLedgerStore implements CameraLedgerStore {
  _FileLedgerStore(this.account);

  final String? account;

  static Future<File> _file() async => File('${(await _cameraDir()).path}${Platform.pathSeparator}ledger.json');

  static Future<void> clear() async {
    final file = await _file();
    if (await file.exists()) await file.delete();
  }

  @override
  Future<CameraLedger> load() async {
    final file = await _file();
    if (!await file.exists()) return CameraLedger(account: account);
    final ledger = CameraLedger.decode(await file.readAsString());
    return ledger.account == account ? ledger : CameraLedger(account: account);
  }

  @override
  Future<void> save(CameraLedger ledger) async {
    ledger.account = account;
    final file = await _file();
    final temp = File('${file.path}.tmp');
    final sink = temp.openWrite();
    sink.write(ledger.encode());
    await sink.flush();
    await sink.close();
    await temp.rename(file.path);
  }
}

class _PhotoLibrary implements CameraLibrary, CameraPhotoHasher {
  final Map<String, AssetEntity> _entities = <String, AssetEntity>{};

  @override
  Future<CameraAccessLevel> access() async {
    // photo_manager tells only with an activity: a background pass's
    // engine has none, and asking threw there.
    switch (await NeoCloudBackground.photoAccess()) {
      case 'full':
        return CameraAccessLevel.full;
      case 'limited':
        return CameraAccessLevel.limited;
      case 'denied':
        return CameraAccessLevel.denied;
    }
    final state = await PhotoManager.getPermissionState(requestOption: const PermissionRequestOption());
    if (state == PermissionState.authorized) return CameraAccessLevel.full;
    if (state == PermissionState.limited) return CameraAccessLevel.limited;
    return CameraAccessLevel.denied;
  }

  @override
  Future<List<CameraAsset>> assets({required bool includeVideos}) async {
    final paths = await PhotoManager.getAssetPathList(onlyAll: true, type: includeVideos ? RequestType.common : RequestType.image);
    if (paths.isEmpty) return const <CameraAsset>[];
    final all = paths.first;
    final total = await all.assetCountAsync;
    final result = <CameraAsset>[];
    const pageSize = 500;
    for (var page = 0; page * pageSize < total; page++) {
      for (final entity in await all.getAssetListPaged(page: page, size: pageSize)) {
        _entities[entity.id] = entity;
        result.add(CameraAsset(
          id: entity.id,
          created: entity.createDateTime,
          modified: entity.modifiedDateTime,
          isVideo: entity.type == AssetType.video,
        ));
      }
    }
    return result;
  }

  @override
  Future<bool> isLocal(CameraAsset asset) async {
    final entity = _entities[asset.id];
    if (entity == null || !Platform.isIOS) return true;
    return entity.isLocallyAvailable(isOrigin: true);
  }

  @override
  Future<CameraFile?> load(CameraAsset asset, {void Function(double progress)? onCloudProgress}) async {
    final entity = _entities[asset.id];
    if (entity == null) return null;
    PMProgressHandler? handler;
    StreamSubscription<PMProgressState>? subscription;
    if (Platform.isIOS && onCloudProgress != null) {
      handler = PMProgressHandler();
      subscription = handler.stream.listen((state) => onCloudProgress(state.progress));
    }
    try {
      final file = await entity.loadFile(isOrigin: true, progressHandler: handler);
      if (file == null) return null;
      final title = await entity.titleAsync;
      return CameraFile(
        name: title.isEmpty ? file.uri.pathSegments.last : title,
        size: await file.length(),
        openRead: file.openRead,
        created: entity.createDateTime,
        modified: entity.modifiedDateTime,
      );
    } finally {
      await subscription?.cancel();
    }
  }

  /// From a thumbnail of about 256 pixels, the photo's shape kept so
  /// nothing is cropped. Never pulls the original from iCloud.
  @override
  Future<String?> dHash(CameraAsset asset) async {
    final entity = _entities[asset.id];
    if (entity == null || entity.type != AssetType.image) return null;
    final width = entity.orientatedWidth;
    final height = entity.orientatedHeight;
    if (width <= 0 || height <= 0) return null;
    final scale = 256 / (width > height ? width : height);
    final size = ThumbnailSize((width * scale).round().clamp(9, 256), (height * scale).round().clamp(8, 256));
    final bytes = await entity.thumbnailDataWithSize(size, quality: 95);
    if (bytes == null) return null;
    return dHashOfEncoded(bytes);
  }

  /// Count of the library and a size estimate from up to 120 local files.
  Future<CameraLibrarySize> size({required bool includeVideos}) async {
    final list = await assets(includeVideos: includeVideos);
    if (list.isEmpty) return const CameraLibrarySize(count: 0);
    final step = (list.length / 120).ceil().clamp(1, list.length);
    var measured = 0;
    var bytes = 0;
    for (var i = 0; i < list.length; i += step) {
      final entity = _entities[list[i].id];
      if (entity == null) continue;
      // Never pull from iCloud just to count.
      if (Platform.isIOS && !await entity.isLocallyAvailable(isOrigin: true)) continue;
      final file = await entity.originFile;
      if (file == null) continue;
      bytes += await file.length();
      measured += 1;
    }
    return CameraLibrarySize(count: list.length, bytes: measured == 0 ? null : (bytes / measured * list.length).round());
  }
}

class _ServerTarget implements CameraTarget, CameraSimilarityTarget {
  _ServerTarget(this._client) : _queue = UploadQueue(client: _client, onFileUploaded: (_) {})..defaultConflict = 'rename';

  final BackendClient _client;
  final UploadQueue _queue;

  static CameraUploadFailure _classify(Object error) {
    if (error is ApiException) {
      if (error.code == 'NETWORK' || error.code == 'SERVER_STOPPING' || error.statusCode == 502 || error.statusCode == 503 || error.statusCode == 504) {
        return CameraUploadFailure(CameraFailureKind.serverUnreachable, error.message);
      }
      if (error.statusCode == 401) return CameraUploadFailure(CameraFailureKind.signedOut, error.message);
      if (error.statusCode == 507 || error.code == 'DISK_FULL' || error.code == 'QUOTA_EXCEEDED') {
        return CameraUploadFailure(CameraFailureKind.serverFull, error.code ?? '');
      }
      return CameraUploadFailure(CameraFailureKind.file, error.message);
    }
    return CameraUploadFailure(CameraFailureKind.file, '$error');
  }

  @override
  Future<List<RemoteCameraFile>> list(String folderId) async {
    final files = <RemoteCameraFile>[];
    String? cursor;
    try {
      do {
        final page = await _client.listFolder(folderId, limit: 2000, cursor: cursor);
        for (final node in page.items) {
          if (!node.isFolder) files.add(RemoteCameraFile(name: node.name, size: node.size, etag: node.etag));
        }
        cursor = page.nextCursor;
      } while (cursor != null);
    } catch (error) {
      throw _classify(error);
    }
    return files;
  }

  @override
  Future<bool> contains(String folderId, String sha256, int size) async {
    try {
      return await _client.uploadContentExists(folderId, sha256, size);
    } catch (error) {
      final failure = _classify(error);
      // A server from before the check answers 404: it can't tell.
      if (failure.kind == CameraFailureKind.file) return false;
      throw failure;
    }
  }

  @override
  Future<Set<String>?> similar(List<String> hashes) async {
    try {
      return await _client.similarUploads(hashes);
    } catch (error) {
      // A server from before the check answers 404: upload as before.
      if (error is ApiException && error.statusCode == 404) return null;
      final failure = _classify(error);
      if (failure.kind == CameraFailureKind.file) return const <String>{};
      throw failure;
    }
  }

  @override
  Future<void> upload(
    String folderId,
    CameraFile file, {
    required String sha256,
    String? resumeId,
    Future<void> Function(String uploadId)? onResumable,
  }) async {
    final task = await _queue.send(
      folderId,
      UploadSource(
        name: file.name,
        size: file.size,
        openRead: file.openRead,
        sha256: sha256,
        createdAt: file.created,
        modifiedAt: file.modified,
      ),
      resumeUploadId: resumeId,
      onSession: onResumable,
    );
    if (task.status == UploadStatus.done) return;
    final failure = task.failure;
    // A lost connection or a server going down stops the pass; what was
    // sent of a large file stays on the server for the next attempt.
    if (failure != null && _classify(failure).kind == CameraFailureKind.serverUnreachable) throw _classify(failure);
    // The queue reports text only; ask the server what is wrong with it as
    // a whole before blaming the file.
    try {
      final me = await _client.deviceMe();
      final quota = (me['quota'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
      final limit = (quota['limit'] as num?)?.toInt() ?? -1;
      final used = (quota['used'] as num?)?.toInt() ?? 0;
      if (limit >= 0 && used + file.size > limit) throw const CameraUploadFailure(CameraFailureKind.serverFull, 'QUOTA_EXCEEDED');
    } on CameraUploadFailure {
      rethrow;
    } catch (error) {
      throw _classify(error);
    }
    throw CameraUploadFailure(CameraFailureKind.file, task.error ?? 'Upload failed');
  }
}

/// One pass with what is stored on the device, for [account] only. Returns
/// the progress-free result; the ledger holds the details.
Future<CameraPassResult?> _pass(
  BackendClient client,
  SharedPreferences preferences, {
  required String account,
  bool? charging,
  bool manual = false,
  void Function(CameraProgress progress)? onProgress,
}) async {
  await preferences.reload();
  final settings = CameraUploadSettings.decode(preferences.getString(_settingsKey));
  if (!settings.activeFor(account)) return null;
  if (manual) {
    await preferences.remove(_pausedKey);
  } else {
    final paused = DateTime.tryParse(preferences.getString(_pausedKey) ?? '');
    if (paused != null && DateTime.now().isBefore(paused)) return null;
  }
  final lease = CameraLease(File('${(await _cameraDir()).path}${Platform.pathSeparator}pass.lease'));
  await NeoCloudBackground.clearDeadCameraLease();
  if (!await lease.acquire()) return null;
  await NeoCloudBackground.cameraPassStarted();
  var leaseLost = false;
  final renewal = Timer.periodic(_leaseRenewal, (_) async {
    try {
      if (!await lease.renew()) leaseLost = true;
    } on FileSystemException {
      // Tried again before the next file.
    }
  });
  final transfer = _CameraTransfer();
  // Only a pass that stops without Wi-Fi waits out a dropout.
  final onWifi = settings.wifiOnly ? _onWifiSettled : _onWifi;
  try {
    final engine = CameraBackupEngine(
      store: _FileLedgerStore(account),
      library: _PhotoLibrary(),
      target: _ServerTarget(client),
    );
    final result = await engine.run(
      CameraPassOptions(
        folderId: settings.folderId,
        includeVideos: settings.includeVideos,
        wifiOnly: settings.wifiOnly,
        chargingOnly: settings.chargingOnly,
        videosWifiOnly: settings.videosWifiOnly,
        scope: settings.scope,
        since: settings.enabledAt,
        skipSimilar: settings.skipSimilar,
      ),
      CameraConditions(onWifi: await onWifi(), charging: charging, signedIn: true),
      manual: manual,
      onProgress: (progress) {
        transfer.show(progress);
        onProgress?.call(progress);
      },
      interruption: () async {
        final stop = transfer.stopWith;
        if (stop != null) return stop;
        // Another pass took the lease over (this one was frozen past its
        // timeout): it goes on, this one ends here. So does one that can't
        // tell.
        try {
          if (leaseLost || !await lease.renew()) return CameraBlock.interrupted;
        } on FileSystemException {
          return CameraBlock.interrupted;
        }
        return null;
      },
      conditionsNow: () async => CameraConditions(onWifi: await onWifi(), charging: charging, signedIn: true),
    );
    await preferences.setBool(_limitedKey, result.limitedAccess);
    switch (result.block) {
      case CameraBlock.paused:
        await preferences.setString(_pausedKey, DateTime.now().add(_pauseFor).toIso8601String());
      // Stopped by the system with files left (its time limit, the app
      // closed): the next pass follows right away instead of at the next
      // trigger, so a first backup goes on in steps.
      case CameraBlock.interrupted:
        await NeoCloudBackground.continueCameraWork();
      // Booked under "Wi-Fi only", so it runs once Wi-Fi is back.
      case CameraBlock.waitingWifi:
        await NeoCloudBackground.continueCameraWork(delay: _wifiRecheck);
      default:
        break;
    }
    return result;
  } finally {
    renewal.cancel();
    await transfer.release();
    await lease.release();
    await NeoCloudBackground.cameraPassEnded();
  }
}

/// "Wi-Fi only" means an unmetered connection. Android answers that
/// directly (as WorkManager's UNMETERED constraint does); the transport
/// list is the fallback where it can't, or on iOS.
Future<bool> _onWifi() async {
  final unmetered = Platform.isAndroid ? await NeoCloudBackground.isUnmetered() : null;
  if (unmetered != null) return unmetered;
  final network = await Connectivity().checkConnectivity();
  return network.contains(ConnectivityResult.wifi) || network.contains(ConnectivityResult.ethernet);
}

/// [_onWifi], asked again for a while before saying no: on a weak signal
/// Wi-Fi drops for a few seconds and comes back, and a pass that stopped
/// for it would wait [_wifiRecheck] for its continuation.
Future<bool> _onWifiSettled() async {
  for (var attempt = 0; attempt < _wifiSettleChecks; attempt++) {
    if (await _onWifi()) return true;
    if (attempt < _wifiSettleChecks - 1) await Future<void>.delayed(_wifiSettleWait);
  }
  return false;
}

/// For [CameraBackupEngine.retryFailed], which uploads nothing.
class _NoTarget implements CameraTarget {
  const _NoTarget();

  @override
  Future<List<RemoteCameraFile>> list(String folderId) => throw const CameraUploadFailure(CameraFailureKind.signedOut);

  @override
  Future<bool> contains(String folderId, String sha256, int size) => throw const CameraUploadFailure(CameraFailureKind.signedOut);

  @override
  Future<void> upload(
    String folderId,
    CameraFile file, {
    required String sha256,
    String? resumeId,
    Future<void> Function(String uploadId)? onResumable,
  }) => throw const CameraUploadFailure(CameraFailureKind.signedOut);
}

/// Photos and videos together with their GPS position (Android's
/// ACCESS_MEDIA_LOCATION): without it the originals are read with the
/// position zeroed.
const PermissionRequestOption _withLocation = PermissionRequestOption(
  androidPermission: AndroidPermission(type: RequestType.common, mediaLocation: true),
);

/// Android: an install that was given the photos before NeoCloud asked for
/// their position too asks for that now. It comes with the photos, so
/// Android shows nothing; with only selected photos (or none) asking would
/// open the picker, so it waits for [_IoCameraUploads.requestAccess].
Future<void> _askForPhotoLocation() async {
  if (!Platform.isAndroid || await NeoCloudBackground.photoLocationGranted() != false) return;
  if (await NeoCloudBackground.photoAccess() != 'full') return;
  await PhotoManager.requestPermissionExtend(requestOption: _withLocation);
}

class _IoCameraUploads extends CameraUploads {
  CameraUploadSettings _settings = const CameraUploadSettings();
  CameraLedger _ledger = CameraLedger();
  CameraProgress? _progress;
  bool _running = false;
  bool _limited = false;
  bool _reconfirm = false;
  bool _initialized = false;

  /// The signed-in account (`<server>#<account id>`); null when signed out.
  String? _account;

  /// The client of the last pass, for the pass a returning Wi-Fi starts.
  BackendClient? _client;
  StreamSubscription<List<ConnectivityResult>>? _networkWatch;

  @override
  bool get supported => true;

  /// A pass that stopped for Wi-Fi only runs again on the next trigger, so
  /// its "waiting for Wi-Fi" stays up after Wi-Fi is back. While the app
  /// runs, Wi-Fi (or Ethernet) coming back starts that pass right away.
  void _watchNetwork() {
    _networkWatch ??= Connectivity().onConnectivityChanged.listen((network) {
      final client = _client;
      if (network.contains(ConnectivityResult.none) || client == null || _running || _ledger.lastBlock != CameraBlock.waitingWifi) return;
      unawaited(() async {
        if (await _onWifi()) await runNow(client);
      }().catchError((Object _) {}));
    });
  }

  /// Settings of another account (or with nobody signed in) show as off.
  @override
  CameraUploadSettings get settings => _settings.activeFor(_account) ? _settings : _settings.copyWith(enabled: false);

  @override
  bool get running => _running;

  @override
  CameraProgress? get progress => _progress;

  @override
  CameraStatus get status =>
      cameraStatus(enabled: _settings.activeFor(_account), ledger: _ledger, progress: _progress, limitedAccess: _limited);

  @override
  CameraCounts get counts => _ledger.counts;

  @override
  List<CameraRecord> records(CameraRecordStatus? status) {
    final list = _ledger.present.where((record) => status == null || record.status == status).toList();
    list.sort((a, b) => (b.created ?? DateTime(0)).compareTo(a.created ?? DateTime(0)));
    return list;
  }

  @override
  DateTime? get lastRun => _ledger.lastRun;

  @override
  DateTime? get lastSuccess => _ledger.lastSuccess;

  @override
  bool get limitedAccess => _limited;

  @override
  bool get needsConfirmation => _reconfirm;

  @override
  List<String> get log => List<String>.unmodifiable(_ledger.log);

  Future<Workmanager> _workmanager() async {
    if (!_initialized) {
      await Workmanager().initialize(cameraUploadDispatcher);
      _initialized = true;
    }
    return Workmanager();
  }

  @override
  Future<void> load() async {
    final preferences = await SharedPreferences.getInstance();
    await preferences.reload();
    _settings = CameraUploadSettings.decode(preferences.getString(_settingsKey));
    _limited = preferences.getBool(_limitedKey) ?? false;
    _reconfirm = preferences.getBool(_reconfirmKey) ?? false;
    _ledger = await _FileLedgerStore(_account).load();
    await _workmanager();
    notifyListeners();
  }

  @override
  Future<void> signIn(String accountKey) async {
    _account = accountKey;
    final preferences = await SharedPreferences.getInstance();
    await preferences.reload();
    final stored = CameraUploadSettings.decode(preferences.getString(_settingsKey));
    if (stored.account != accountKey) {
      if (stored.enabled) await preferences.setBool(_reconfirmKey, true);
      await preferences.setString(_settingsKey, stored.rebound(accountKey).encode());
      await _FileLedgerStore.clear();
      final workmanager = await _workmanager();
      await workmanager.cancelByUniqueName(cameraUploadTaskId);
      await _disarmTriggers(workmanager);
    }
    await load();
    // Booked again on every start: a force-stop or an iOS update drops them.
    if (_settings.activeFor(accountKey) && _settings.folderId != null) {
      unawaited(_armTriggers(_settings).catchError((Object _) {}));
      unawaited(_askForPhotoLocation().catchError((Object _) {}));
    }
  }

  @override
  Future<void> signOut() async {
    _account = null;
    final preferences = await SharedPreferences.getInstance();
    await preferences.reload();
    final stored = CameraUploadSettings.decode(preferences.getString(_settingsKey));
    if (stored.enabled) {
      await preferences.setBool(_reconfirmKey, true);
      await preferences.setString(_settingsKey, stored.copyWith(enabled: false).encode());
    }
    final workmanager = await _workmanager();
    await workmanager.cancelByUniqueName(cameraUploadTaskId);
    await _disarmTriggers(workmanager);
    await load();
  }

  @override
  Future<void> dismissConfirmation() async {
    final preferences = await SharedPreferences.getInstance();
    await preferences.remove(_reconfirmKey);
    _reconfirm = false;
    notifyListeners();
  }

  @override
  Future<CameraAccess> requestAccess() async {
    final state = await PhotoManager.requestPermissionExtend(requestOption: _withLocation);
    if (state == PermissionState.authorized) return CameraAccess.granted;
    if (state == PermissionState.limited) return CameraAccess.limited;
    return CameraAccess.denied;
  }

  @override
  Future<void> openSystemSettings() => PhotoManager.openSetting();

  @override
  Future<CameraAccess> photoAccess() async => switch (await _PhotoLibrary().access()) {
    CameraAccessLevel.full => CameraAccess.granted,
    CameraAccessLevel.limited => CameraAccess.limited,
    CameraAccessLevel.denied => CameraAccess.denied,
  };

  @override
  Future<BackgroundReliability> backgroundReliability() async => BackgroundReliability.evaluate(
    status: BackgroundPlatformStatus.fromMap(await NeoCloudBackground.status()),
    photos: await photoAccess(),
  );

  @override
  Future<CameraLibrarySize> librarySize({required bool includeVideos}) => _PhotoLibrary().size(includeVideos: includeVideos);

  @override
  Future<void> update(CameraUploadSettings next) async {
    final account = _account;
    if (account == null) throw StateError('Camera upload needs a signed-in account.');
    final preferences = await SharedPreferences.getInstance();
    next = next.copyWith(account: account);
    if (next.enabled && !_settings.activeFor(account)) next = next.copyWith(enabledAt: DateTime.now());
    _settings = next;
    await preferences.setString(_settingsKey, next.encode());
    if (next.enabled) {
      await preferences.remove(_reconfirmKey);
      _reconfirm = false;
    }
    notifyListeners();
    final workmanager = await _workmanager();
    if (next.enabled && next.folderId != null) {
      if (Platform.isIOS) {
        await workmanager.registerPeriodicTask(
          cameraUploadTaskId,
          cameraUploadTaskId,
          frequency: const Duration(hours: 1),
          existingWorkPolicy: ExistingPeriodicWorkPolicy.replace,
          constraints: Constraints(
            networkType: next.wifiOnly ? NetworkType.unmetered : NetworkType.connected,
            requiresCharging: next.chargingOnly,
          ),
        );
      }
      await _armTriggers(next);
    } else {
      await workmanager.cancelByUniqueName(cameraUploadTaskId);
      await _disarmTriggers(workmanager);
    }
  }

  @override
  Future<void> runNow(BackendClient client, {bool manual = false}) async {
    _client = client;
    _watchNetwork();
    final account = _account;
    if (_running || account == null || !_settings.activeFor(account)) return;
    _running = true;
    notifyListeners();
    try {
      await _pass(client, await SharedPreferences.getInstance(), account: account, manual: manual, onProgress: (progress) {
        _progress = progress;
        notifyListeners();
      });
    } finally {
      _running = false;
      _progress = null;
      await load();
    }
  }

  @override
  Future<void> retryFailed() async {
    await CameraBackupEngine(store: _FileLedgerStore(_account), library: _PhotoLibrary(), target: const _NoTarget()).retryFailed();
    await load();
  }
}
