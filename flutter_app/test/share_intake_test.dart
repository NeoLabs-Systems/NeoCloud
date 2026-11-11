import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/files/upload_queue.dart';
import 'package:neocloud/src/mobile/share_intake.dart';
import 'package:neocloud/src/mobile/share_intake_io.dart';

class _RecordingIntake implements ShareIntake {
  final List<UploadSource> released = <UploadSource>[];

  @override
  ValueListenable<List<SharedBatch>> get pending => ValueNotifier<List<SharedBatch>>(const <SharedBatch>[]);

  @override
  Future<void> start() async {}

  @override
  void remove(SharedBatch batch) {}

  @override
  Future<void> release(Iterable<UploadSource> files) async => released.addAll(files);
}

void main() {
  late Directory temp;
  late String root;

  setUp(() {
    temp = Directory(Directory.systemTemp.createTempSync('share-intake-test-').resolveSymbolicLinksSync());
    root = (Directory('${temp.path}/share-intake')..createSync()).path;
  });

  tearDown(() => temp.deleteSync(recursive: true));

  File put(String relative, [String content = 'x']) {
    final file = File('$root/$relative')..createSync(recursive: true);
    file.writeAsStringSync(content);
    return file;
  }

  group('intake paths', () {
    test('a file in its own folder inside the intake folder is taken', () {
      final file = put('8d3c/photo.jpg');
      expect(intakeFile(root, file.path)?.path, file.path);
    });

    test('anything outside, deeper, shallower or not canonical is refused', () {
      final outside = File('${temp.path}/prefs.xml')..writeAsStringSync('secret');
      put('a/b/deep.txt');
      put('flat.txt');
      put('8d3c/photo.jpg');
      expect(intakeFile(root, outside.path), isNull);
      expect(intakeFile(root, '$root/a/b/deep.txt'), isNull);
      expect(intakeFile(root, '$root/flat.txt'), isNull);
      expect(intakeFile(root, '$root/8d3c/../8d3c/photo.jpg'), isNull);
      expect(intakeFile(root, '$root/8d3c/../../prefs.xml'), isNull);
      expect(intakeFile(root, '$root/missing/file'), isNull);
      expect(intakeFile(root, '$root/8d3c'), isNull);
    });

    test('symlinks are refused, in the file and in its folder', () {
      final outside = File('${temp.path}/prefs.xml')..writeAsStringSync('secret');
      Directory('$root/link').createSync();
      Link('$root/link/prefs.xml').createSync(outside.path);
      Link('$root/folder').createSync(temp.path);
      expect(intakeFile(root, '$root/link/prefs.xml'), isNull);
      expect(intakeFile(root, '$root/folder/prefs.xml'), isNull);
    });

    test('a batch keeps only intake files and counts the rest as rejected', () {
      final good = put('1/a.txt', 'hello');
      final batch = sharedBatchFrom(<String, Object?>{
        'files': <Object?>[
          <String, Object?>{'path': good.path, 'name': 'a.txt', 'size': 5},
          <String, Object?>{'path': '/etc/hosts', 'name': 'hosts', 'size': 1},
          'garbage',
        ],
        'rejected': 2,
        'tooLarge': false,
      }, root);
      expect(batch.files.map((file) => file.name), <String>['a.txt']);
      expect(batch.files.single.size, 5);
      expect(batch.rejected, 4);
      expect(batch.tooLarge, isFalse);
    });

    test('an oversized share arrives as rejected and empty', () {
      final batch = sharedBatchFrom(<String, Object?>{'files': <Object?>[], 'rejected': 3, 'tooLarge': true}, root);
      expect(batch.files, isEmpty);
      expect(batch.tooLarge, isTrue);
    });
  });

  test('intake copies go once their upload is done, skipped or cancelled; failed ones stay for Retry', () {
    final intake = _RecordingIntake();
    final queue = UploadQueue(client: BackendClient(baseUrl: 'http://localhost:1'), onFileUploaded: (_) {});
    UploadSource source(String name) => UploadSource(name: name, size: 1, openRead: () => const Stream<List<int>>.empty());
    final done = source('done');
    final failed = source('failed');
    final running = source('running');
    final batch = SharedBatch(files: <UploadSource>[done, failed, running]);
    final tasks = <UploadTask>[
      for (final file in batch.files) UploadTask(source: file, parentId: 'root')..status = UploadStatus.uploading,
    ];
    queue.tasks.addAll(tasks);

    releaseWhenUploaded(intake, queue, batch);
    expect(intake.released, isEmpty);

    tasks[0].status = UploadStatus.done;
    tasks[1].status = UploadStatus.failed;
    queue.notifyListeners();
    expect(intake.released, <UploadSource>[done]);

    queue.tasks.remove(tasks[1]);
    tasks[2].status = UploadStatus.cancelled;
    queue.notifyListeners();
    expect(intake.released, unorderedEquals(<UploadSource>[done, failed, running]));
  });
}
