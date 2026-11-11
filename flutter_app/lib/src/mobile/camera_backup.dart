import 'dart:async';

import 'package:crypto/crypto.dart';

import 'camera_ledger.dart';

export 'camera_ledger.dart';

// Camera backup as a plain state machine, free of plugins so it can be
// tested: the photo library, the server and the ledger store are injected.
//
// Every asset of the library has a record (id, size, modified, SHA-256,
// status). A pass uploads everything not marked done, newest first,
// whatever its date; a failing file is recorded with its reason and attempt
// count and the pass goes on. Before uploading, the file's SHA-256 is
// compared with the server's etags (= SHA-256 of the content), so a re-run,
// a reinstall or a second device never uploads the same photo twice.

abstract class CameraLedgerStore {
  Future<CameraLedger> load();

  /// Must replace the stored ledger atomically.
  Future<void> save(CameraLedger ledger);
}

class MemoryCameraLedgerStore implements CameraLedgerStore {
  String? _raw;

  @override
  Future<CameraLedger> load() async => CameraLedger.decode(_raw);

  @override
  Future<void> save(CameraLedger ledger) async => _raw = ledger.encode();
}

/// A photo or video in the library.
class CameraAsset {
  const CameraAsset({required this.id, required this.created, this.modified, this.isVideo = false});

  final String id;
  final DateTime created;
  final DateTime? modified;
  final bool isVideo;
}

/// An asset's original, ready to read.
class CameraFile {
  const CameraFile({required this.name, required this.size, required this.openRead});

  final String name;
  final int size;
  final Stream<List<int>> Function() openRead;
}

enum CameraAccessLevel { full, limited, denied }

abstract class CameraLibrary {
  Future<CameraAccessLevel> access();

  /// Every photo (and video, when [includeVideos]) the app can see.
  Future<List<CameraAsset>> assets({required bool includeVideos});

  /// False while the original is only in iCloud.
  Future<bool> isLocal(CameraAsset asset);

  /// The original, downloaded from iCloud first when needed. Null when the
  /// system can't provide it.
  Future<CameraFile?> load(CameraAsset asset, {void Function(double progress)? onCloudProgress});
}

/// A file the server already has: its name and etag (= SHA-256).
class RemoteCameraFile {
  const RemoteCameraFile({required this.name, required this.size, required this.etag});

  final String name;
  final int size;
  final String etag;
}

/// What makes a whole pass stop, not just one file.
enum CameraFailureKind { serverUnreachable, signedOut, serverFull, file }

class CameraUploadFailure implements Exception {
  const CameraUploadFailure(this.kind, [this.message = '']);

  final CameraFailureKind kind;
  final String message;

  @override
  String toString() => message.isEmpty ? kind.name : message;
}

abstract class CameraTarget {
  /// The files in the target folder. Throws [CameraUploadFailure].
  Future<List<RemoteCameraFile>> list(String folderId);

  /// Uploads [file]; a taken name gets a suffix, nothing is replaced.
  /// Throws [CameraUploadFailure].
  Future<void> upload(String folderId, CameraFile file);
}

/// Scope chosen when backup was turned on.
enum CameraScope { newOnly, everything }

class CameraPassOptions {
  const CameraPassOptions({
    required this.folderId,
    this.includeVideos = false,
    this.wifiOnly = true,
    this.chargingOnly = false,
    this.videosWifiOnly = true,
    this.scope = CameraScope.everything,
    this.since,
  });

  final String? folderId;
  final bool includeVideos;
  final bool wifiOnly;
  final bool chargingOnly;
  final bool videosWifiOnly;
  final CameraScope scope;

  /// With [CameraScope.newOnly]: assets created before this are out of scope.
  final DateTime? since;
}

/// The device right now. [charging] is null when the platform can't tell.
class CameraConditions {
  const CameraConditions({this.onWifi = true, this.charging, this.signedIn = true});

  final bool onWifi;
  final bool? charging;
  final bool signedIn;
}

class CameraPassResult {
  const CameraPassResult({this.block, this.uploaded = 0, this.skipped = 0, this.failed = 0, this.limitedAccess = false});

  final CameraBlock? block;
  final int uploaded;

  /// Already on the server, so marked done without uploading.
  final int skipped;
  final int failed;
  final bool limitedAccess;
}

/// Streams [stream] through SHA-256 and returns its hex digest.
Future<String> sha256OfStream(Stream<List<int>> stream) async {
  final sink = _DigestSink();
  final input = sha256.startChunkedConversion(sink);
  await for (final chunk in stream) {
    input.add(chunk);
  }
  input.close();
  return sink.value.toString();
}

class _DigestSink implements Sink<Digest> {
  late Digest value;

  @override
  void add(Digest data) => value = data;

  @override
  void close() {}
}

class CameraBackupEngine {
  CameraBackupEngine({required this.store, required this.library, required this.target, DateTime Function()? clock})
    : _clock = clock ?? DateTime.now;

  final CameraLedgerStore store;
  final CameraLibrary library;
  final CameraTarget target;
  final DateTime Function() _clock;

  /// Saved every few files and at the end; a crash in between only means a
  /// few files are compared with the server again.
  static const int _saveEvery = 10;

  /// One pass. [manual] means the user asked for it, so "charging only"
  /// can't hold it back on a platform that can't tell.
  Future<CameraPassResult> run(
    CameraPassOptions options,
    CameraConditions conditions, {
    bool manual = false,
    void Function(CameraProgress progress)? onProgress,
  }) async {
    final ledger = await store.load();
    final now = _clock();
    ledger.lastRun = now;

    Future<CameraPassResult> stop(CameraBlock block, {CameraPassResult? partial, String? detail}) async {
      ledger.lastBlock = block;
      ledger.addLog('stopped: ${block.name}${detail == null || detail.isEmpty ? '' : ' ($detail)'}', _clock());
      await store.save(ledger);
      return CameraPassResult(
        block: block,
        uploaded: partial?.uploaded ?? 0,
        skipped: partial?.skipped ?? 0,
        failed: partial?.failed ?? 0,
        limitedAccess: partial?.limitedAccess ?? false,
      );
    }

    final folderId = options.folderId;
    if (folderId == null) return stop(CameraBlock.noFolder);
    if (!conditions.signedIn) return stop(CameraBlock.signedOut);
    final access = await library.access();
    if (access == CameraAccessLevel.denied) return stop(CameraBlock.noAccess);
    final limited = access == CameraAccessLevel.limited;
    if (options.wifiOnly && !conditions.onWifi) return stop(CameraBlock.waitingWifi);
    if (options.chargingOnly && conditions.charging != true && !manual) return stop(CameraBlock.waitingCharger);

    // Bring the records in line with the library.
    final assets = await library.assets(includeVideos: options.includeVideos);
    final since = options.scope == CameraScope.newOnly ? options.since : null;
    final inScope = <String>{};
    for (final asset in assets) {
      if (since != null && asset.created.isBefore(since)) continue;
      inScope.add(asset.id);
      final record = ledger.records.putIfAbsent(asset.id, () => CameraRecord(assetId: asset.id));
      record
        ..created = asset.created
        ..isVideo = asset.isVideo;
      // An edited photo has new content: back it up again.
      if (record.status == CameraRecordStatus.done && asset.modified != null && record.modified != null && asset.modified!.isAfter(record.modified!)) {
        record
          ..status = CameraRecordStatus.pending
          ..attempts = 0
          ..reason = null;
      }
      record.modified = asset.modified ?? record.modified;
    }
    for (final record in ledger.records.values) {
      record.present = inScope.contains(record.assetId);
    }

    // What the server already has, by name and by content.
    final List<RemoteCameraFile> remote;
    try {
      remote = await target.list(folderId);
    } on CameraUploadFailure catch (failure) {
      return stop(_blockFor(failure.kind), detail: failure.message);
    }
    final byName = <String, RemoteCameraFile>{for (final file in remote) file.name: file};
    final etags = <String>{for (final file in remote) file.etag};

    final queue = <CameraAsset>[
      for (final asset in assets)
        if (inScope.contains(asset.id) && _due(ledger.records[asset.id]!)) asset,
    ]..sort((a, b) => b.created.compareTo(a.created));

    var uploaded = 0;
    var skipped = 0;
    var failed = 0;
    var streak = 0;
    var sinceSave = 0;
    for (var index = 0; index < queue.length; index++) {
      final asset = queue[index];
      final record = ledger.records[asset.id]!;
      onProgress?.call(CameraProgress(current: index + 1, total: queue.length, name: record.name));
      if (asset.isVideo && options.videosWifiOnly && !conditions.onWifi) {
        record.reason = CameraReason.wifi;
        continue;
      }
      try {
        if (!await library.isLocal(asset)) {
          record.reason = CameraReason.icloud;
          onProgress?.call(CameraProgress(current: index + 1, total: queue.length, name: record.name, cloud: 0));
        }
        final file = await library.load(
          asset,
          onCloudProgress: (value) => onProgress?.call(CameraProgress(current: index + 1, total: queue.length, name: record.name, cloud: value)),
        );
        if (file == null) throw const CameraUploadFailure(CameraFailureKind.file, CameraReason.notAvailable);
        record
          ..name = file.name
          ..size = file.size;
        final hash = await sha256OfStream(file.openRead());
        record.sha256 = hash;
        final sameName = byName[file.name];
        if (etags.contains(hash)) {
          skipped += 1;
          ledger.addLog('already on server: ${file.name}', _clock());
        } else {
          // A different file under the same name is uploaded too; the
          // server picks a free name.
          await target.upload(folderId, file);
          uploaded += 1;
          etags.add(hash);
          if (sameName == null) byName[file.name] = RemoteCameraFile(name: file.name, size: file.size, etag: hash);
          ledger.addLog('uploaded: ${file.name}', _clock());
        }
        record
          ..status = CameraRecordStatus.done
          ..reason = null
          ..doneAt = _clock();
        streak = 0;
      } on CameraUploadFailure catch (failure) {
        if (failure.kind != CameraFailureKind.file) {
          record.reason = null;
          return stop(
            _blockFor(failure.kind),
            partial: CameraPassResult(uploaded: uploaded, skipped: skipped, failed: failed, limitedAccess: limited),
            detail: failure.message,
          );
        }
        _fail(ledger, record, failure.message);
        failed += 1;
        streak += 1;
      } catch (error) {
        _fail(ledger, record, '$error');
        failed += 1;
        streak += 1;
      }
      if (streak >= cameraFailureStreakLimit) {
        return stop(
          CameraBlock.tooManyFailures,
          partial: CameraPassResult(uploaded: uploaded, skipped: skipped, failed: failed, limitedAccess: limited),
        );
      }
      sinceSave += 1;
      if (sinceSave >= _saveEvery) {
        sinceSave = 0;
        await store.save(ledger);
      }
    }
    ledger.lastBlock = null;
    if (failed == 0) ledger.lastSuccess = _clock();
    ledger.addLog('pass: $uploaded uploaded, $skipped already there, $failed failed', _clock());
    await store.save(ledger);
    return CameraPassResult(uploaded: uploaded, skipped: skipped, failed: failed, limitedAccess: limited);
  }

  /// Failed records again get [cameraMaxAttempts] automatic tries.
  Future<void> retryFailed() async {
    final ledger = await store.load();
    for (final record in ledger.records.values) {
      if (record.status == CameraRecordStatus.failed) {
        record
          ..status = CameraRecordStatus.pending
          ..attempts = 0;
      }
    }
    await store.save(ledger);
  }

  static bool _due(CameraRecord record) => switch (record.status) {
    CameraRecordStatus.done => false,
    CameraRecordStatus.pending => true,
    CameraRecordStatus.failed => record.attempts < cameraMaxAttempts,
  };

  void _fail(CameraLedger ledger, CameraRecord record, String reason) {
    record
      ..status = CameraRecordStatus.failed
      ..attempts += 1
      ..reason = reason;
    ledger.addLog('failed (${record.attempts}×): ${record.name.isEmpty ? record.assetId : record.name}: $reason', _clock());
  }

  static CameraBlock _blockFor(CameraFailureKind kind) => switch (kind) {
    CameraFailureKind.serverUnreachable => CameraBlock.serverUnreachable,
    CameraFailureKind.signedOut => CameraBlock.signedOut,
    CameraFailureKind.serverFull => CameraBlock.serverFull,
    CameraFailureKind.file => CameraBlock.tooManyFailures,
  };
}
