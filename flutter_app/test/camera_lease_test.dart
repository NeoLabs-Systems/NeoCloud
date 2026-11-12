import 'dart:convert';
import 'dart:io';
import 'dart:isolate';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/mobile/camera_backup.dart';
import 'package:neocloud/src/mobile/camera_lease.dart';

class _Library implements CameraLibrary {
  _Library(this.count);

  final int count;

  @override
  Future<CameraAccessLevel> access() async => CameraAccessLevel.full;

  @override
  Future<List<CameraAsset>> assets({required bool includeVideos}) async => <CameraAsset>[
    for (var i = 0; i < count; i++) CameraAsset(id: 'p$i', created: DateTime(2025, 1, 1 + i)),
  ];

  @override
  Future<bool> isLocal(CameraAsset asset) async => true;

  @override
  Future<CameraFile?> load(CameraAsset asset, {void Function(double progress)? onCloudProgress}) async {
    final bytes = utf8.encode('content of ${asset.id}');
    return CameraFile(name: '${asset.id}.jpg', size: bytes.length, openRead: () => Stream<List<int>>.value(bytes));
  }
}

/// A server that keeps every upload, duplicates included ("p1 (1).jpg"), and
/// takes its time so passes overlap.
class _Server implements CameraTarget {
  final List<String> uploads = <String>[];

  @override
  Future<List<RemoteCameraFile>> list(String folderId) async => const <RemoteCameraFile>[];

  @override
  Future<bool> contains(String folderId, String sha256, int size) async => false;

  @override
  Future<void> upload(String folderId, CameraFile file, {required String sha256}) async {
    await Future<void>.delayed(const Duration(milliseconds: 5));
    uploads.add(file.name);
  }
}

/// What camera_upload_io's pass does around the engine: take the lease or
/// leave, renew it before each file, give it back.
Future<CameraPassResult?> _pass(File leaseFile, CameraBackupEngine engine) async {
  final lease = CameraLease(leaseFile, settle: const Duration(milliseconds: 20));
  if (!await lease.acquire()) return null;
  try {
    return await engine.run(
      const CameraPassOptions(folderId: 'camera'),
      const CameraConditions(),
      interruption: () async => await lease.renew() ? null : CameraBlock.interrupted,
    );
  } finally {
    await lease.release();
  }
}

Future<bool> _acquireInIsolate(String path) => Isolate.run(() => CameraLease(File(path), settle: const Duration(milliseconds: 50)).acquire());

void main() {
  late Directory dir;
  late File leaseFile;

  setUp(() async {
    dir = await Directory.systemTemp.createTemp('camera_lease_test');
    leaseFile = File('${dir.path}/camera/pass.lease');
  });

  tearDown(() => dir.delete(recursive: true));

  test('of two passes started together only one runs; nothing is uploaded twice', () async {
    final store = MemoryCameraLedgerStore();
    final server = _Server();
    // The hourly task and the new-photo trigger, each with its own engine
    // over the same ledger and server.
    CameraBackupEngine engine() => CameraBackupEngine(store: store, library: _Library(20), target: server);
    final results = await Future.wait(<Future<CameraPassResult?>>[_pass(leaseFile, engine()), _pass(leaseFile, engine())]);
    expect(results.where((result) => result != null), hasLength(1));
    expect(server.uploads, hasLength(20));
    expect(server.uploads.toSet(), hasLength(20));
    expect(leaseFile.existsSync(), isFalse);

    // Once it is done the next pass may run, and finds nothing left.
    final next = await _pass(leaseFile, engine());
    expect(next, isNotNull);
    expect(next!.uploaded, 0);
  });

  test('only one of several isolates gets the lease', () async {
    final path = leaseFile.path;
    await leaseFile.parent.create(recursive: true);
    final won = await Future.wait(<Future<bool>>[for (var i = 0; i < 6; i++) _acquireInIsolate(path)]);
    expect(won.where((taken) => taken), hasLength(1));
  });

  test('a held lease keeps others out while it is renewed; released, it is free', () async {
    var now = DateTime(2026, 11, 12, 10);
    final first = CameraLease(leaseFile, settle: Duration.zero, clock: () => now);
    final second = CameraLease(leaseFile, settle: Duration.zero, clock: () => now);
    expect(await first.acquire(), isTrue);
    // Hours into a first full backup, renewed before each file.
    for (var i = 0; i < 30; i++) {
      now = now.add(const Duration(minutes: 4));
      expect(await first.renew(), isTrue);
      expect(await second.acquire(), isFalse);
    }
    // Releasing someone else's lease does nothing.
    await second.release();
    expect(leaseFile.existsSync(), isTrue);
    await first.release();
    expect(leaseFile.existsSync(), isFalse);
    expect(await second.acquire(), isTrue);
  });

  test('a lease no longer renewed (the pass died) is taken over, and the old holder learns it lost it', () async {
    var now = DateTime(2026, 11, 12, 10);
    final dead = CameraLease(leaseFile, settle: Duration.zero, clock: () => now);
    final next = CameraLease(leaseFile, settle: Duration.zero, clock: () => now);
    expect(await dead.acquire(), isTrue);
    now = now.add(const Duration(minutes: 4));
    expect(await next.acquire(), isFalse);
    now = now.add(dead.staleAfter);
    expect(await next.acquire(), isTrue);
    // The frozen pass wakes up: it must stop before its next file and must
    // not remove the new holder's lease.
    expect(await dead.renew(), isFalse);
    await dead.release();
    expect(await next.renew(), isTrue);
  });

  test('a lease file left empty by a crash counts by its age', () async {
    await leaseFile.parent.create(recursive: true);
    await leaseFile.writeAsString('');
    final fresh = CameraLease(leaseFile, settle: Duration.zero);
    expect(await fresh.acquire(), isFalse);
    await leaseFile.setLastModified(DateTime.now().subtract(const Duration(hours: 1)));
    expect(await fresh.acquire(), isTrue);
  });
}
