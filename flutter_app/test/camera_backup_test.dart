import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/mobile/camera_backup.dart';

class _Photo {
  _Photo(this.id, this.name, this.content, this.created, {this.video = false});

  final String id;
  final String name;
  String content;
  final DateTime created;
  final bool video;
  bool local = true;
  DateTime? modified;
}

class _FakeLibrary implements CameraLibrary {
  final List<_Photo> photos = <_Photo>[];
  CameraAccessLevel level = CameraAccessLevel.full;
  final Set<String> unreadable = <String>{};
  int cloudDownloads = 0;

  /// Called on every read of a file's content.
  void Function(String id)? mutateOnRead;

  @override
  Future<CameraAccessLevel> access() async => level;

  @override
  Future<List<CameraAsset>> assets({required bool includeVideos}) async => <CameraAsset>[
    for (final photo in photos)
      if (includeVideos || !photo.video) CameraAsset(id: photo.id, created: photo.created, modified: photo.modified, isVideo: photo.video),
  ];

  @override
  Future<bool> isLocal(CameraAsset asset) async => photos.firstWhere((p) => p.id == asset.id).local;

  @override
  Future<CameraFile?> load(CameraAsset asset, {void Function(double progress)? onCloudProgress}) async {
    final photo = photos.firstWhere((p) => p.id == asset.id);
    if (unreadable.contains(photo.id)) return null;
    if (!photo.local) {
      cloudDownloads += 1;
      onCloudProgress?.call(1);
    }
    final size = utf8.encode(photo.content).length;
    return CameraFile(
      name: photo.name,
      size: size,
      openRead: () {
        mutateOnRead?.call(photo.id);
        return Stream<List<int>>.value(utf8.encode(photo.content));
      },
    );
  }
}

class _FakeServer implements CameraTarget {
  /// Files directly in the target folder.
  final List<RemoteCameraFile> files = <RemoteCameraFile>[];

  /// Files the user sorted into subfolders of the target folder.
  final List<RemoteCameraFile> below = <RemoteCameraFile>[];
  final List<String> uploads = <String>[];
  final List<String> sentHashes = <String>[];
  final Set<String> rejectNames = <String>{};
  CameraFailureKind? outage;

  /// False: a server from before the content check.
  bool canCheck = true;

  /// Resumable uploads: id → bytes that arrived. A file above
  /// [resumableFrom] bytes goes this way.
  final Map<String, int> sessions = <String, int>{};
  int resumableFrom = 1 << 30;
  final List<String?> resumedWith = <String?>[];

  /// Cut the connection after this many bytes of the next resumable upload.
  int? dropAfter;

  /// The upload arrives, but its answer is lost (the phone sees an outage).
  bool loseAnswer = false;

  @override
  Future<List<RemoteCameraFile>> list(String folderId) async {
    if (outage != null) throw CameraUploadFailure(outage!);
    return List<RemoteCameraFile>.of(files);
  }

  @override
  Future<bool> contains(String folderId, String sha256, int size) async {
    if (outage != null) throw CameraUploadFailure(outage!);
    if (!canCheck) return false;
    return <RemoteCameraFile>[...files, ...below].any((file) => file.etag == sha256 && file.size == size);
  }

  @override
  Future<void> upload(
    String folderId,
    CameraFile file, {
    required String sha256,
    String? resumeId,
    Future<void> Function(String uploadId)? onResumable,
  }) async {
    if (outage != null) throw CameraUploadFailure(outage!);
    if (rejectNames.contains(file.name)) throw const CameraUploadFailure(CameraFailureKind.file, 'HTTP 500');
    final content = await file.openRead().expand((chunk) => chunk).toList();
    if (content.length >= resumableFrom) {
      resumedWith.add(resumeId);
      var id = resumeId;
      if (id == null || !sessions.containsKey(id)) {
        id = 'up${sessions.length + 1}';
        sessions[id] = 0;
        await onResumable?.call(id);
      }
      final drop = dropAfter;
      if (drop != null) {
        dropAfter = null;
        sessions[id] = drop;
        throw const CameraUploadFailure(CameraFailureKind.serverUnreachable, 'connection lost');
      }
      sessions[id] = content.length;
      sessions.remove(id);
    }
    final hash = await sha256OfStream(Stream<List<int>>.value(content));
    // The server's checksum check.
    if (hash != sha256) throw const CameraUploadFailure(CameraFailureKind.file, 'CHECKSUM_MISMATCH');
    sentHashes.add(sha256);
    // The server's rule for a hash the client declared: the same name and
    // content is this upload again, not a second file.
    if (files.any((f) => f.name == file.name && f.etag == hash)) return;
    var name = file.name;
    var n = 1;
    while (files.any((f) => f.name == name)) {
      name = '${file.name} (${n++})';
    }
    files.add(RemoteCameraFile(name: name, size: file.size, etag: hash));
    uploads.add(name);
    if (loseAnswer) {
      loseAnswer = false;
      throw const CameraUploadFailure(CameraFailureKind.serverUnreachable, 'no answer');
    }
  }
}

/// A ledger store whose process "dies" at a chosen save: that save and
/// everything after it never reach the disk.
class _DyingStore implements CameraLedgerStore {
  _DyingStore(this.inner);

  final MemoryCameraLedgerStore inner;

  /// Saves still allowed before the process dies; null never dies.
  int? savesLeft;

  @override
  Future<CameraLedger> load() => inner.load();

  @override
  Future<void> save(CameraLedger ledger) async {
    final left = savesLeft;
    if (left != null) {
      if (left <= 0) throw StateError('process killed');
      savesLeft = left - 1;
    }
    await inner.save(ledger);
  }
}

const CameraPassOptions _options = CameraPassOptions(folderId: 'camera', wifiOnly: true, includeVideos: true);

void main() {
  late _FakeLibrary library;
  late _FakeServer server;
  late MemoryCameraLedgerStore store;
  late CameraBackupEngine engine;

  setUp(() {
    library = _FakeLibrary();
    server = _FakeServer();
    store = MemoryCameraLedgerStore();
    engine = CameraBackupEngine(store: store, library: library, target: server);
    for (var i = 1; i <= 6; i++) {
      library.photos.add(_Photo('a$i', 'IMG_$i.jpg', 'photo $i', DateTime(2020 + i)));
    }
  });

  test('backs up everything regardless of date, and a second pass uploads nothing', () async {
    final first = await engine.run(_options, const CameraConditions());
    expect(first.uploaded, 6);
    expect(server.uploads, hasLength(6));
    final second = await engine.run(_options, const CameraConditions());
    expect(second.uploaded + second.skipped, 0);
    expect(server.uploads, hasLength(6));
    final counts = (await store.load()).counts;
    expect(counts.onPhone, 6);
    expect(counts.backedUp, 6);
  });

  test('one bad file is recorded and the rest continue; retries count up', () async {
    server.rejectNames.add('IMG_3.jpg');
    library.unreadable.add('a5');
    final result = await engine.run(_options, const CameraConditions());
    expect(result.uploaded, 4);
    expect(result.failed, 2);
    var ledger = await store.load();
    expect(ledger.records['a3']!.status, CameraRecordStatus.failed);
    expect(ledger.records['a3']!.reason, 'HTTP 500');
    expect(ledger.records['a3']!.attempts, 1);
    expect(ledger.records['a5']!.reason, CameraReason.notAvailable);
    final counts = ledger.counts;
    expect(counts.backedUp + counts.pending + counts.failed, counts.onPhone);
    expect(cameraStatus(enabled: true, ledger: ledger).kind, CameraStatusKind.failed);

    await engine.run(_options, const CameraConditions());
    ledger = await store.load();
    expect(ledger.records['a3']!.attempts, 2);

    server.rejectNames.clear();
    library.unreadable.clear();
    await engine.run(_options, const CameraConditions());
    ledger = await store.load();
    expect(ledger.counts.failed, 0);
    expect(ledger.counts.backedUp, 6);
    expect(cameraStatus(enabled: true, ledger: ledger).kind, CameraStatusKind.upToDate);
  });

  test('gives up on a file after the attempt limit until retried', () async {
    server.rejectNames.add('IMG_2.jpg');
    for (var i = 0; i < cameraMaxAttempts + 2; i++) {
      await engine.run(_options, const CameraConditions());
    }
    expect((await store.load()).records['a2']!.attempts, cameraMaxAttempts);
    server.rejectNames.clear();
    await engine.retryFailed();
    await engine.run(_options, const CameraConditions());
    expect((await store.load()).records['a2']!.status, CameraRecordStatus.done);
  });

  test('a reinstall (empty ledger) skips what the server already has by content', () async {
    await engine.run(_options, const CameraConditions());
    final fresh = CameraBackupEngine(store: MemoryCameraLedgerStore(), library: library, target: server);
    final result = await fresh.run(_options, const CameraConditions());
    expect(result.uploaded, 0);
    expect(result.skipped, 6);
    expect(server.uploads, hasLength(6));
  });

  test('lost upload records: photos sorted into subfolders are found by content, not sent again', () async {
    await engine.run(_options, const CameraConditions());
    // The user sorts the backed-up photos into dated folders.
    server.below.addAll(server.files);
    server.files.clear();
    final fresh = CameraBackupEngine(store: MemoryCameraLedgerStore(), library: library, target: server);
    final result = await fresh.run(_options, const CameraConditions());
    expect(result.uploaded, 0);
    expect(result.skipped, 6);
    expect(server.uploads, hasLength(6));

    // An older server can't tell: those are sent again (the old behaviour).
    server.canCheck = false;
    final old = await CameraBackupEngine(store: MemoryCameraLedgerStore(), library: library, target: server).run(_options, const CameraConditions());
    expect(old.uploaded, 6);
  });

  test('each upload carries the SHA-256 of what was read', () async {
    await engine.run(_options, const CameraConditions());
    final ledger = await store.load();
    expect(server.sentHashes, unorderedEquals(<String>[for (final record in ledger.records.values) record.sha256!]));
  });

  test('a photo that changes between hashing and sending is refused, not stored damaged', () async {
    final photo = library.photos.first;
    var reads = 0;
    library.mutateOnRead = (id) {
      if (id == photo.id && ++reads == 2) photo.content = 'edited meanwhile';
    };
    final result = await engine.run(_options, const CameraConditions());
    expect(result.failed, 1);
    expect((await store.load()).records[photo.id]!.reason, 'CHECKSUM_MISMATCH');
    expect(server.uploads, hasLength(5));
  });

  test('a different file under a taken name is uploaded beside it', () async {
    server.files.add(const RemoteCameraFile(name: 'IMG_1.jpg', size: 3, etag: 'other'));
    await engine.run(_options, const CameraConditions());
    expect(server.uploads, contains('IMG_1.jpg (1)'));
  });

  test('stops with a reason for Wi-Fi, charger, sign-in, outage and full server', () async {
    expect((await engine.run(_options, const CameraConditions(onWifi: false))).block, CameraBlock.waitingWifi);
    const charging = CameraPassOptions(folderId: 'camera', chargingOnly: true);
    expect((await engine.run(charging, const CameraConditions())).block, CameraBlock.waitingCharger);
    expect((await engine.run(charging, const CameraConditions(), manual: true)).block, isNull);
    expect((await engine.run(_options, const CameraConditions(signedIn: false))).block, CameraBlock.signedOut);
    library.photos.add(_Photo('a7', 'IMG_7.jpg', 'photo 7', DateTime(2030)));
    server.outage = CameraFailureKind.serverUnreachable;
    expect((await engine.run(_options, const CameraConditions())).block, CameraBlock.serverUnreachable);
    var ledger = await store.load();
    expect(cameraStatus(enabled: true, ledger: ledger).kind, CameraStatusKind.serverUnreachable);
    expect(ledger.records['a7']!.status, CameraRecordStatus.pending);
    server.outage = CameraFailureKind.serverFull;
    expect((await engine.run(_options, const CameraConditions())).block, CameraBlock.serverFull);
    server.outage = null;
    await engine.run(_options, const CameraConditions());
    ledger = await store.load();
    expect(ledger.lastBlock, isNull);
    expect(ledger.counts.backedUp, 7);
  });

  test('videos wait for Wi-Fi when asked, photos go over mobile data', () async {
    library.photos.add(_Photo('v1', 'VID_1.mov', 'video', DateTime(2031), video: true));
    const options = CameraPassOptions(folderId: 'camera', wifiOnly: false, includeVideos: true, videosWifiOnly: true);
    await engine.run(options, const CameraConditions(onWifi: false));
    final ledger = await store.load();
    expect(ledger.records['v1']!.status, CameraRecordStatus.pending);
    expect(ledger.records['v1']!.reason, CameraReason.wifi);
    expect(ledger.counts.backedUp, 6);
  });

  test('new-only scope ignores older photos; iCloud originals are fetched', () async {
    library.photos.last.local = false;
    final options = CameraPassOptions(folderId: 'camera', scope: CameraScope.newOnly, since: DateTime(2024, 6));
    final result = await engine.run(options, const CameraConditions());
    expect(result.uploaded, 2);
    expect(library.cloudDownloads, 1);
    expect((await store.load()).counts.onPhone, 2);
  });

  test('new-only: a photo added later with an old date (chat, AirDrop, import) is still backed up', () async {
    final options = CameraPassOptions(folderId: 'camera', scope: CameraScope.newOnly, since: DateTime(2024, 6));
    // Turned on, first pass: the library as it was stays out.
    await engine.run(options, const CameraConditions(onWifi: false));
    expect((await store.load()).baseline, hasLength(4));
    library.photos
      ..add(_Photo('w1', 'IMG-20190301-WA0001.jpg', 'from a chat', DateTime(2019, 3)))
      ..add(_Photo('n1', 'IMG_new.jpg', 'just taken', DateTime(2027)));
    final result = await engine.run(options, const CameraConditions());
    expect(result.uploaded, 4);
    expect(server.uploads, containsAll(<String>['IMG-20190301-WA0001.jpg', 'IMG_new.jpg']));
    expect(server.uploads, isNot(contains('IMG_1.jpg')));
    final ledger = CameraLedger.decode((await store.load()).encode());
    expect(ledger.baseline, <String>{'a1', 'a2', 'a3', 'a4'});
    expect(ledger.counts.onPhone, 4);

    // Turned off and on again later: a new starting point.
    final again = CameraPassOptions(folderId: 'camera', scope: CameraScope.newOnly, since: DateTime(2028));
    await engine.run(again, const CameraConditions());
    expect((await store.load()).baseline, hasLength(8));
  });

  test('Wi-Fi is checked before every file: dropping it mid-pass stops before the next one', () async {
    var asked = 0;
    final result = await engine.run(
      _options,
      const CameraConditions(),
      conditionsNow: () async => CameraConditions(onWifi: ++asked <= 2),
    );
    expect(result.block, CameraBlock.waitingWifi);
    expect(result.uploaded, 2);
    expect(server.uploads, hasLength(2));
    final ledger = await store.load();
    expect(ledger.lastBlock, CameraBlock.waitingWifi);
    expect(ledger.counts.backedUp, 2);

    // With Wi-Fi back the next pass finishes.
    final next = await engine.run(_options, const CameraConditions(), conditionsNow: () async => const CameraConditions());
    expect(next.uploaded, 4);

    // Without "Wi-Fi only" mobile data is fine.
    library.photos.add(_Photo('a7', 'IMG_7.jpg', 'photo 7', DateTime(2030)));
    const anyNetwork = CameraPassOptions(folderId: 'camera', wifiOnly: false, includeVideos: true);
    final mobile = await engine.run(anyNetwork, const CameraConditions(), conditionsNow: () async => const CameraConditions(onWifi: false));
    expect(mobile.uploaded, 1);
  });

  test('many failures in a row stop the pass', () async {
    for (var i = 1; i <= 6; i++) {
      server.rejectNames.add('IMG_$i.jpg');
    }
    final result = await engine.run(_options, const CameraConditions());
    expect(result.block, CameraBlock.tooManyFailures);
    expect(result.failed, cameraFailureStreakLimit);
  });

  test('limited access still uploads what is visible and says so', () async {
    library.level = CameraAccessLevel.limited;
    final result = await engine.run(_options, const CameraConditions());
    expect(result.limitedAccess, isTrue);
    expect(result.uploaded, 6);
    expect(cameraStatus(enabled: true, ledger: await store.load(), limitedAccess: true).kind, CameraStatusKind.limitedAccess);
  });

  test('pause and the system time limit stop between files, save, and the next pass resumes', () async {
    CameraBlock? stopWith;
    final paused = await engine.run(
      _options,
      const CameraConditions(),
      onProgress: (progress) {
        if (progress.current == 3) stopWith = CameraBlock.paused;
      },
      interruption: () async => stopWith,
    );
    expect(paused.block, CameraBlock.paused);
    expect(paused.uploaded, 3);
    final ledger = await store.load();
    expect(ledger.counts.backedUp, 3);
    expect(cameraStatus(enabled: true, ledger: ledger).kind, CameraStatusKind.paused);

    // Android 15's data sync limit: the status falls back to pending.
    stopWith = null;
    final interrupted = await engine.run(
      _options,
      const CameraConditions(),
      onProgress: (progress) {
        if (progress.current == 2) stopWith = CameraBlock.interrupted;
      },
      interruption: () async => stopWith,
    );
    expect(interrupted.block, CameraBlock.interrupted);
    expect(cameraStatus(enabled: true, ledger: await store.load()).kind, CameraStatusKind.pending);

    final resumed = await engine.run(_options, const CameraConditions());
    expect(resumed.block, isNull);
    expect(server.uploads, hasLength(6));
  });

  group('a failed or interrupted upload resumes without duplicates', () {
    test('killed after the upload succeeded but before the ledger was saved', () async {
      // Six files, saved only at the end: the process dies right there.
      final dying = _DyingStore(store)..savesLeft = 0;
      final doomed = CameraBackupEngine(store: dying, library: library, target: server);
      await expectLater(doomed.run(_options, const CameraConditions()), throwsStateError);
      expect(server.uploads, hasLength(6));
      final sentBeforeCrash = List<String>.of(server.uploads);
      expect((await store.load()).counts.backedUp, 0);

      final result = await engine.run(_options, const CameraConditions());
      expect(result.skipped, sentBeforeCrash.length);
      expect(server.uploads, hasLength(6));
      expect(server.uploads.toSet(), hasLength(6));
      expect(server.uploads.where((name) => name.contains('(')), isEmpty);
      expect((await store.load()).counts.backedUp, 6);
    });

    test('the answer to an upload is lost: the retry finds it and sends nothing again', () async {
      server.loseAnswer = true;
      final first = await engine.run(_options, const CameraConditions());
      expect(first.block, CameraBlock.serverUnreachable);
      expect(server.uploads, hasLength(1));
      // A server that can't tell by content still never stores "IMG (1)".
      server.canCheck = false;
      final second = await engine.run(_options, const CameraConditions());
      expect(second.block, isNull);
      expect(server.files.map((f) => f.name).toSet(), hasLength(6));
      expect(server.files.where((f) => f.name.contains('(')), isEmpty);
    });

    test('the connection drops mid-video: the next pass resumes the same upload', () async {
      library.photos
        ..clear()
        ..add(_Photo('v1', 'VID_1.mp4', 'a long video ' * 100, DateTime(2026), video: true));
      server
        ..resumableFrom = 1000
        ..dropAfter = 600;
      final first = await engine.run(_options, const CameraConditions());
      expect(first.block, CameraBlock.serverUnreachable);
      final saved = (await store.load()).records['v1']!;
      expect(saved.uploadId, 'up1');
      expect(saved.status, CameraRecordStatus.pending);
      expect(server.sessions['up1'], 600);

      // Kept across a restart: a new engine reads it from the ledger.
      final restarted = CameraBackupEngine(store: store, library: library, target: server);
      final second = await restarted.run(_options, const CameraConditions());
      expect(second.uploaded, 1);
      expect(server.resumedWith, <String?>[null, 'up1']);
      expect(server.uploads, <String>['VID_1.mp4']);
      expect((await store.load()).records['v1']!.uploadId, isNull);
    });

    test('an edited video does not continue the upload of its old content', () async {
      library.photos
        ..clear()
        ..add(_Photo('v1', 'VID_1.mp4', 'a long video ' * 100, DateTime(2026), video: true));
      server
        ..resumableFrom = 1000
        ..dropAfter = 600;
      await engine.run(_options, const CameraConditions());
      library.photos.single.content = 'an edited video ' * 100;
      await engine.run(_options, const CameraConditions());
      expect(server.resumedWith, <String?>[null, null]);
    });

    test('the app killed mid-pass: finished files are never sent again', () async {
      for (var i = 7; i <= 30; i++) {
        library.photos.add(_Photo('a$i', 'IMG_$i.jpg', 'photo $i', DateTime(2000 + i)));
      }
      // Saved after 10 files; the process dies at the save after 20.
      final dying = _DyingStore(store)..savesLeft = 1;
      final doomed = CameraBackupEngine(store: dying, library: library, target: server);
      await expectLater(doomed.run(_options, const CameraConditions()), throwsStateError);
      final saved = (await store.load()).counts.backedUp;
      expect(saved, 10);
      expect(server.uploads, hasLength(20));

      final result = await engine.run(_options, const CameraConditions());
      expect(result.failed, 0);
      expect(server.uploads, hasLength(30));
      expect(server.uploads.toSet(), hasLength(30));
      expect((await store.load()).counts.backedUp, 30);
    });
  });

  test('the ledger survives encoding', () async {
    await engine.run(_options, const CameraConditions());
    final ledger = await store.load();
    final decoded = CameraLedger.decode(ledger.encode());
    expect(decoded.records.length, 6);
    expect(decoded.records['a1']!.sha256, ledger.records['a1']!.sha256);
    expect(decoded.log, isNotEmpty);
  });
}
