import 'dart:async';

import 'package:crypto/crypto.dart';

import 'camera_dhash.dart';
import 'camera_ledger.dart';

export 'camera_ledger.dart';

// Camera backup as a plain state machine, free of plugins so it can be
// tested: the photo library, the server and the ledger store are injected.
//
// Every asset of the library has a record (id, size, modified, SHA-256,
// status). A pass uploads everything not marked done, newest first,
// whatever its date; a failing file is recorded with its reason and attempt
// count and the pass goes on. Before uploading, the file's SHA-256 is
// compared with the server's etags (= SHA-256 of the content) in the target
// folder and every folder below it, so a re-run, a reinstall or a second
// device never uploads the same photo twice, even after it was sorted into a
// subfolder. The hash goes with the upload, and the server refuses content
// that arrives different; a file of the same name and content it already has
// is that upload sent again (its answer was lost), so a retry never leaves
// "IMG (1).jpg". A large file goes in parts whose upload id is kept in the
// record, so an interrupted one resumes. Only one pass runs at a time (camera_lease.dart);
// the pass is told before each file whether it still holds that lease.

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

  /// Whether a file with this content is in the target folder or any folder
  /// below it; false when the server can't tell. Throws
  /// [CameraUploadFailure] for what stops a pass (unreachable, signed out).
  Future<bool> contains(String folderId, String sha256, int size);

  /// Uploads [file], whose content hashes to [sha256]; a taken name gets a
  /// suffix, nothing is replaced. Throws [CameraUploadFailure].
  ///
  /// A large file goes as a resumable upload: [onResumable] gets its id as
  /// soon as it exists, and [resumeId] (one an earlier attempt left) is
  /// continued, sending only the parts still missing.
  Future<void> upload(
    String folderId,
    CameraFile file, {
    required String sha256,
    String? resumeId,
    Future<void> Function(String uploadId)? onResumable,
  });
}

/// A library that can make a photo's perceptual hash (camera_dhash.dart)
/// from a small thumbnail, without loading the original.
abstract class CameraPhotoHasher {
  /// The photo's dHash, or null when it can't be made.
  Future<String?> dHash(CameraAsset asset);
}

/// A server that can tell which photos are almost identical (at least 95 %)
/// to one it already has.
abstract class CameraSimilarityTarget {
  /// The asked hashes that have such a photo, or null when the server can't
  /// tell (older server). Throws [CameraUploadFailure] for what stops a pass.
  Future<Set<String>?> similar(List<String> hashes);
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
    this.skipSimilar = true,
  });

  final String? folderId;
  final bool includeVideos;
  final bool wifiOnly;
  final bool chargingOnly;
  final bool videosWifiOnly;
  final CameraScope scope;

  /// With [CameraScope.newOnly]: when backup was turned on. What was in the
  /// library then stays out ([CameraLedger.baseline]); what arrives later is
  /// backed up, whatever date it carries.
  final DateTime? since;

  /// Photos almost identical to one already in NeoCloud are not sent.
  final bool skipSimilar;
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
  CameraBackupEngine({required this.store, required this.library, required this.target, DateTime Function()? clock}) : _clock = clock ?? DateTime.now;

  final CameraLedgerStore store;
  final CameraLibrary library;
  final CameraTarget target;
  final DateTime Function() _clock;

  /// Saved every few files (or every few seconds of a slow pass) and at the
  /// end; a crash in between only means those files are hashed and compared
  /// with the server again, never sent again.
  static const int _saveEvery = 10;
  static const Duration _saveAfter = Duration(seconds: 20);

  /// One pass. [manual] means the user asked for it, so "charging only"
  /// can't hold it back on a platform that can't tell. [interruption] is
  /// asked before each file; a block it returns ends the pass there, with
  /// the ledger saved. [conditionsNow] is asked before each file too: when
  /// Wi-Fi is gone under "Wi-Fi only", the pass stops and waits for it.
  Future<CameraPassResult> run(
    CameraPassOptions options,
    CameraConditions conditions, {
    bool manual = false,
    void Function(CameraProgress progress)? onProgress,
    Future<CameraBlock?> Function()? interruption,
    Future<CameraConditions> Function()? conditionsNow,
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

    // Bring the records in line with the library.
    final assets = await library.assets(includeVideos: options.includeVideos);
    final baseline = _baseline(ledger, options, assets);
    final inScope = <String>{};
    for (final asset in assets) {
      if (baseline != null && baseline.contains(asset.id)) continue;
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
      // Skipping similar photos was turned off: send the ones it skipped.
      if (!options.skipSimilar && record.status == CameraRecordStatus.done && record.reason == CameraReason.similar) {
        record
          ..status = CameraRecordStatus.pending
          ..attempts = 0
          ..reason = null;
      }
    }
    for (final record in ledger.records.values) {
      record.present = inScope.contains(record.assetId);
    }
    if (options.wifiOnly && !conditions.onWifi) return stop(CameraBlock.waitingWifi);
    if (options.chargingOnly && conditions.charging != true && !manual) return stop(CameraBlock.waitingCharger);

    // The pass is going: the previous stop ("waiting for Wi-Fi") no longer
    // holds. Cleared now, not at the end, because a first backup runs for
    // hours and the status (read by the app while a background pass runs)
    // would show the old reason all that time.
    if (ledger.lastBlock != null) {
      ledger.lastBlock = null;
      await store.save(ledger);
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
    var savedAt = _clock();
    var current = conditions;
    final similarity = _SimilarPhotos(
      enabled: options.skipSimilar,
      hasher: library is CameraPhotoHasher ? library as CameraPhotoHasher : null,
      target: target is CameraSimilarityTarget ? target as CameraSimilarityTarget : null,
    );
    for (var index = 0; index < queue.length; index++) {
      CameraPassResult partial() => CameraPassResult(uploaded: uploaded, skipped: skipped, failed: failed, limitedAccess: limited);
      final interrupted = await interruption?.call();
      if (interrupted != null) return stop(interrupted, partial: partial());
      if (conditionsNow != null) {
        current = await conditionsNow();
        if (options.wifiOnly && !current.onWifi) return stop(CameraBlock.waitingWifi, partial: partial());
      }
      final asset = queue[index];
      final record = ledger.records[asset.id]!;
      onProgress?.call(CameraProgress(current: index + 1, total: queue.length, name: record.name));
      if (asset.isVideo && options.videosWifiOnly && !current.onWifi) {
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
        // A resumable upload left for other content (the photo was edited)
        // can't be continued.
        if (record.sha256 != hash) record.uploadId = null;
        record.sha256 = hash;
        final sameName = byName[file.name];
        String? doneReason;
        if (etags.contains(hash) || await target.contains(folderId, hash, file.size)) {
          skipped += 1;
          ledger.addLog('already on server: ${file.name}', _clock());
        } else if (!asset.isVideo && await similarity.matches(queue, index)) {
          skipped += 1;
          doneReason = CameraReason.similar;
          ledger.addLog('similar to a photo on the server: ${file.name}', _clock());
        } else {
          // A different file under the same name is uploaded too; the
          // server picks a free name.
          await target.upload(
            folderId,
            file,
            sha256: hash,
            resumeId: record.uploadId,
            onResumable: (uploadId) async {
              // Kept at once: a pass killed mid-file resumes it.
              record.uploadId = uploadId;
              await store.save(ledger);
            },
          );
          uploaded += 1;
          etags.add(hash);
          if (sameName == null) byName[file.name] = RemoteCameraFile(name: file.name, size: file.size, etag: hash);
          similarity.uploaded(asset);
          ledger.addLog('uploaded: ${file.name}', _clock());
        }
        record
          ..status = CameraRecordStatus.done
          ..reason = doneReason
          ..uploadId = null
          ..doneAt = _clock();
        streak = 0;
      } on CameraUploadFailure catch (failure) {
        if (failure.kind != CameraFailureKind.file) {
          record.reason = null;
          return stop(_blockFor(failure.kind), partial: partial(), detail: failure.message);
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
        return stop(CameraBlock.tooManyFailures, partial: partial());
      }
      sinceSave += 1;
      if (sinceSave >= _saveEvery || _clock().difference(savedAt) >= _saveAfter) {
        sinceSave = 0;
        savedAt = _clock();
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

  /// The assets that count as already there with "new photos only", or
  /// null for everything. Set on the first pass after backup was turned on
  /// (normally right away): what was in the library and dated before then.
  /// From that pass on, being new means not being in that set, so a photo
  /// that arrives later with an older date is still backed up.
  static Set<String>? _baseline(CameraLedger ledger, CameraPassOptions options, List<CameraAsset> assets) {
    final since = options.scope == CameraScope.newOnly ? options.since : null;
    if (since == null) return null;
    if (ledger.baseline == null || ledger.baselineSince != since) {
      ledger
        ..baseline = <String>{
          for (final asset in assets)
            if (asset.created.isBefore(since)) asset.id,
        }
        ..baselineSince = since;
    }
    return ledger.baseline;
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
      ..reason = reason
      // The server's resumable upload went with the failure.
      ..uploadId = null;
    ledger.addLog('failed (${record.attempts}×): ${record.name.isEmpty ? record.assetId : record.name}: $reason', _clock());
  }

  static CameraBlock _blockFor(CameraFailureKind kind) => switch (kind) {
    CameraFailureKind.serverUnreachable => CameraBlock.serverUnreachable,
    CameraFailureKind.signedOut => CameraBlock.signedOut,
    CameraFailureKind.serverFull => CameraBlock.serverFull,
    CameraFailureKind.file => CameraBlock.tooManyFailures,
  };
}

/// Which photos of a pass are almost identical (dHashes at most 3 bits
/// apart) to one already in NeoCloud: asked of the server for a batch of
/// coming photos at a time, and compared with what this pass uploaded.
class _SimilarPhotos {
  _SimilarPhotos({required bool enabled, this.hasher, this.target}) : _active = enabled && hasher != null && target != null;

  static const int batch = 50;
  static const int maxDistance = 3;

  final CameraPhotoHasher? hasher;
  final CameraSimilarityTarget? target;
  bool _active;
  final Map<String, String?> _hashes = <String, String?>{};
  final Set<String> _matched = <String>{};
  final List<String> _sent = <String>[];

  /// Whether the photo at [index] of [queue] is like one already there.
  Future<bool> matches(List<CameraAsset> queue, int index) async {
    if (!_active) return false;
    final asset = queue[index];
    if (!_hashes.containsKey(asset.id)) await _ask(queue, index);
    if (!_active) return false;
    final hash = _hashes[asset.id];
    if (hash == null) return false;
    if (_matched.contains(hash)) return true;
    return _sent.any((sent) => dHashDistance(sent, hash) <= maxDistance);
  }

  /// [asset] was uploaded: later photos like it are skipped too.
  void uploaded(CameraAsset asset) {
    final hash = _hashes[asset.id];
    if (_active && hash != null) _sent.add(hash);
  }

  Future<void> _ask(List<CameraAsset> queue, int from) async {
    final asked = <String>[];
    var photos = 0;
    for (var i = from; i < queue.length && photos < batch; i++) {
      final asset = queue[i];
      if (asset.isVideo || _hashes.containsKey(asset.id)) continue;
      photos += 1;
      String? hash;
      try {
        hash = await hasher!.dHash(asset);
      } catch (_) {
        hash = null;
      }
      _hashes[asset.id] = hash;
      if (hash != null) asked.add(hash);
    }
    if (asked.isEmpty) return;
    final found = await target!.similar(asked);
    // A server without the check: upload as before.
    if (found == null) {
      _active = false;
      return;
    }
    _matched.addAll(found);
  }
}
