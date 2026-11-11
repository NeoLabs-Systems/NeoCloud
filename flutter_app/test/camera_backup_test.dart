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
    final bytes = utf8.encode(photo.content);
    return CameraFile(name: photo.name, size: bytes.length, openRead: () => Stream<List<int>>.value(bytes));
  }
}

class _FakeServer implements CameraTarget {
  final List<RemoteCameraFile> files = <RemoteCameraFile>[];
  final List<String> uploads = <String>[];
  final Set<String> rejectNames = <String>{};
  CameraFailureKind? outage;

  @override
  Future<List<RemoteCameraFile>> list(String folderId) async {
    if (outage != null) throw CameraUploadFailure(outage!);
    return List<RemoteCameraFile>.of(files);
  }

  @override
  Future<void> upload(String folderId, CameraFile file) async {
    if (outage != null) throw CameraUploadFailure(outage!);
    if (rejectNames.contains(file.name)) throw const CameraUploadFailure(CameraFailureKind.file, 'HTTP 500');
    final content = await file.openRead().expand((chunk) => chunk).toList();
    final hash = await sha256OfStream(Stream<List<int>>.value(content));
    var name = file.name;
    var n = 1;
    while (files.any((f) => f.name == name)) {
      name = '${file.name} (${n++})';
    }
    files.add(RemoteCameraFile(name: name, size: file.size, etag: hash));
    uploads.add(name);
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

  test('the ledger survives encoding', () async {
    await engine.run(_options, const CameraConditions());
    final ledger = await store.load();
    final decoded = CameraLedger.decode(ledger.encode());
    expect(decoded.records.length, 6);
    expect(decoded.records['a1']!.sha256, ledger.records['a1']!.sha256);
    expect(decoded.log, isNotEmpty);
  });
}
