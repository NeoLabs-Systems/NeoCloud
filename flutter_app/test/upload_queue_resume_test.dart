import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/files/upload_queue.dart';

/// The server's resumable upload, in memory; the connection can drop at a
/// chosen part.
class _Server extends BackendClient {
  _Server() : super(baseUrl: 'http://localhost:1');

  static const int partSize = 16 * 1024 * 1024;

  final Map<String, Set<int>> live = <String, Set<int>>{};
  final Map<String, int> sizes = <String, int>{};
  final List<String> cancelled = <String>[];
  final List<int> sentParts = <int>[];
  int? dropAtPart;

  @override
  Future<Map<String, dynamic>> createUpload(
    String parentId,
    String name,
    int size, {
    required String conflict,
    String? sha256,
    DateTime? created,
    DateTime? modified,
  }) async {
    final id = 'up${sizes.length + 1}';
    live[id] = <int>{};
    sizes[id] = size;
    return <String, dynamic>{'id': id, 'partSize': partSize};
  }

  @override
  Future<Map<String, dynamic>> uploadStatus(String uploadId) async {
    final received = live[uploadId];
    if (received == null) throw const ApiException(404, 'Upload not found or expired', code: 'UPLOAD_NOT_FOUND');
    return <String, dynamic>{'size': sizes[uploadId], 'partSize': partSize, 'receivedParts': received.toList()};
  }

  @override
  Future<void> uploadPart(String uploadId, int index, Uint8List bytes) async {
    if (dropAtPart == index) {
      dropAtPart = null;
      throw const ApiException(0, 'Network error', code: 'NETWORK');
    }
    sentParts.add(index);
    live[uploadId]!.add(index);
  }

  @override
  Future<FileNode> completeUpload(String uploadId) async {
    live.remove(uploadId);
    return FileNode.fromJson(<String, dynamic>{'id': 'f1', 'type': 'file', 'name': 'VID.mp4', 'size': sizes[uploadId], 'etag': ''});
  }

  @override
  Future<void> cancelUpload(String uploadId) async {
    cancelled.add(uploadId);
    live.remove(uploadId);
  }
}

void main() {
  test('a hidden upload cut off by the network keeps its server session and resumes the missing parts', () async {
    final server = _Server()..dropAtPart = 2;
    final queue = UploadQueue(client: server, onFileUploaded: (_) {});
    final bytes = Uint8List.fromList(List<int>.generate(singleRequestLimit + 10, (i) => i % 251));
    UploadSource source() => UploadSource(name: 'VID.mp4', size: bytes.length, openRead: () => Stream<List<int>>.value(bytes));
    final sessions = <String>[];

    final first = await queue.send('camera', source(), onSession: (id) async => sessions.add(id));
    expect(first.status, UploadStatus.failed);
    expect((first.failure as ApiException).code, 'NETWORK');
    expect(sessions, <String>['up1']);
    expect(server.cancelled, isEmpty);
    expect(server.live['up1'], <int>{0, 1});

    final second = await queue.send('camera', source(), resumeUploadId: 'up1', onSession: (id) async => sessions.add(id));
    expect(second.status, UploadStatus.done);
    // No new session, and the two parts that arrived were not sent again.
    expect(sessions, <String>['up1']);
    expect(server.sentParts.where((index) => index < 2), hasLength(2));
    expect(server.live, isEmpty);
  });

  test('a resumable upload that expired meanwhile starts over', () async {
    final server = _Server();
    final queue = UploadQueue(client: server, onFileUploaded: (_) {});
    final bytes = Uint8List(singleRequestLimit + 1);
    final sessions = <String>[];
    final task = await queue.send(
      'camera',
      UploadSource(name: 'VID.mp4', size: bytes.length, openRead: () => Stream<List<int>>.value(bytes)),
      resumeUploadId: 'gone',
      onSession: (id) async => sessions.add(id),
    );
    expect(task.status, UploadStatus.done);
    expect(sessions, <String>['up1']);
  });

  test('an upload without an owner to resume it is cancelled when it fails', () async {
    final server = _Server()..dropAtPart = 1;
    final queue = UploadQueue(client: server, onFileUploaded: (_) {});
    final bytes = Uint8List(singleRequestLimit + 1);
    final task = await queue.send('camera', UploadSource(name: 'VID.mp4', size: bytes.length, openRead: () => Stream<List<int>>.value(bytes)));
    expect(task.status, UploadStatus.failed);
    expect(server.cancelled, <String>['up1']);
  });
}
