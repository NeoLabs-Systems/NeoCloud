import 'dart:async';
import 'dart:io';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/widgets.dart';
import 'package:path_provider/path_provider.dart';
import 'package:photo_manager/photo_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:workmanager/workmanager.dart';

import '../api/backend_client.dart';
import '../device/device_session.dart';
import '../files/upload_queue.dart';
import 'camera_backup.dart';
import 'camera_upload.dart';
import 'camera_upload_stub.dart';

CameraUploads createCameraUploads() => Platform.isAndroid || Platform.isIOS ? _IoCameraUploads() : NoCameraUploads();

const String _settingsKey = 'camera.settings';
const String _leaseKey = 'camera.lease';
const String _limitedKey = 'camera.limited';
const String _reconfirmKey = 'camera.reconfirm';

/// Two passes (foreground and background task) must not run at once; a
/// lease older than this is taken to be from a pass that died.
const Duration _leaseTimeout = Duration(minutes: 20);

/// Must match BGTaskSchedulerPermittedIdentifiers in ios/Runner/Info.plist
/// and the registration in AppDelegate.swift. iOS runs it when the system
/// decides (typically a few times a day); Android every ~hour at the earliest.
const String cameraUploadTaskId = 'systems.neolabs.neocloud.cameraUpload';

/// The background entry point: workmanager starts a headless engine and
/// calls this. It signs in with the stored device token and runs one pass,
/// if camera upload is on for that very account.
@pragma('vm:entry-point')
void cameraUploadDispatcher() {
  Workmanager().executeTask((task, _) async {
    WidgetsFlutterBinding.ensureInitialized();
    final connection = await createDeviceSessions().read();
    final account = connection?.accountKey;
    if (connection == null || account == null) return true;
    final client = BackendClient(baseUrl: connection.server)..bearerToken = connection.token;
    try {
      // Android runs the task only while charging when that was asked for.
      await _pass(client, await SharedPreferences.getInstance(), account: account, charging: Platform.isAndroid ? true : null);
      return true;
    } catch (_) {
      return false;
    }
  });
}

/// The ledger as `<app support>/camera/ledger.json`, replaced atomically
/// (write a temp file, then rename over the old one). A ledger of another
/// account than [account] reads as empty.
class _FileLedgerStore implements CameraLedgerStore {
  _FileLedgerStore(this.account);

  final String? account;

  static Future<File> _file() async {
    final dir = Directory('${(await getApplicationSupportDirectory()).path}${Platform.pathSeparator}camera');
    await dir.create(recursive: true);
    return File('${dir.path}${Platform.pathSeparator}ledger.json');
  }

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

class _PhotoLibrary implements CameraLibrary {
  final Map<String, AssetEntity> _entities = <String, AssetEntity>{};

  @override
  Future<CameraAccessLevel> access() async {
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
      );
    } finally {
      await subscription?.cancel();
    }
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

class _ServerTarget implements CameraTarget {
  _ServerTarget(this._client) : _queue = UploadQueue(client: _client, onFileUploaded: (_) {})..defaultConflict = 'rename';

  final BackendClient _client;
  final UploadQueue _queue;

  static CameraUploadFailure _classify(Object error) {
    if (error is ApiException) {
      if (error.code == 'NETWORK' || error.code == 'SERVER_STOPPING' || error.statusCode == 503) {
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
  Future<void> upload(String folderId, CameraFile file) async {
    final task = await _queue.send(folderId, UploadSource(name: file.name, size: file.size, openRead: file.openRead));
    if (task.status == UploadStatus.done) return;
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
  final lease = DateTime.tryParse(preferences.getString(_leaseKey) ?? '');
  final now = DateTime.now();
  if (lease != null && now.difference(lease) < _leaseTimeout) return null;
  await preferences.setString(_leaseKey, now.toIso8601String());
  try {
    final network = await Connectivity().checkConnectivity();
    final onWifi = network.contains(ConnectivityResult.wifi) || network.contains(ConnectivityResult.ethernet);
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
      ),
      CameraConditions(onWifi: onWifi, charging: charging, signedIn: true),
      manual: manual,
      onProgress: onProgress,
    );
    await preferences.setBool(_limitedKey, result.limitedAccess);
    return result;
  } finally {
    await preferences.remove(_leaseKey);
  }
}

/// For [CameraBackupEngine.retryFailed], which uploads nothing.
class _NoTarget implements CameraTarget {
  const _NoTarget();

  @override
  Future<List<RemoteCameraFile>> list(String folderId) => throw const CameraUploadFailure(CameraFailureKind.signedOut);

  @override
  Future<void> upload(String folderId, CameraFile file) => throw const CameraUploadFailure(CameraFailureKind.signedOut);
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

  @override
  bool get supported => true;

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
      await (await _workmanager()).cancelByUniqueName(cameraUploadTaskId);
    }
    await load();
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
    await (await _workmanager()).cancelByUniqueName(cameraUploadTaskId);
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
    final state = await PhotoManager.requestPermissionExtend();
    if (state == PermissionState.authorized) return CameraAccess.granted;
    if (state == PermissionState.limited) return CameraAccess.limited;
    return CameraAccess.denied;
  }

  @override
  Future<void> openSystemSettings() => PhotoManager.openSetting();

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
    } else {
      await workmanager.cancelByUniqueName(cameraUploadTaskId);
    }
  }

  @override
  Future<void> runNow(BackendClient client, {bool manual = false}) async {
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
