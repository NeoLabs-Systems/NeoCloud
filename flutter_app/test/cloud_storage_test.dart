import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';

import 'helpers/fake_http.dart';

// Cloud storage: what the client sends and how the answers are read. The
// server never returns a secret, so the models have none to hold.

FakeReply _server(FakeCall call) {
  switch ('${call.method} ${call.path}') {
    case 'GET /api/external-storage':
      return const FakeReply(200, <String, dynamic>{
        'items': <Map<String, dynamic>>[
          <String, dynamic>{
            'id': 'm1', 'name': 'Photos', 'provider': 's3', 'readOnly': true,
            'config': <String, dynamic>{'bucket': 'b', 'secretAccessKeySet': true},
          },
        ],
      });
    case 'GET /api/external-storage/m1/list':
      return const FakeReply(200, <String, dynamic>{
        'path': 'a',
        'truncated': false,
        'items': <Map<String, dynamic>>[
          <String, dynamic>{'name': 'sub', 'path': 'a/sub', 'type': 'folder', 'size': 0, 'mtime': null},
          <String, dynamic>{'name': 'x.txt', 'path': 'a/x.txt', 'type': 'file', 'size': 12, 'mtime': '2026-01-02T03:04:05.000Z', 'mime': 'text/plain'},
        ],
      });
    case 'POST /api/external-storage/test':
      return const FakeReply(200, <String, dynamic>{'ok': false, 'error': 'The server refused the user name or password.'});
    default:
      return const FakeReply(404, <String, dynamic>{'error': 'Not found'});
  }
}

void main() {
  test('connections and listings are read, secrets only as flags', () async {
    final client = BackendClient(baseUrl: 'http://localhost:1', http: FakeHttp(_server));
    final mounts = await client.externalMounts();
    expect(mounts.single.provider, ExternalProvider.s3);
    expect(mounts.single.readOnly, isTrue);
    expect(mounts.single.config['secretAccessKeySet'], isTrue);
    expect(mounts.single.config.containsKey('secretAccessKey'), isFalse);

    final listing = await client.listExternal('m1', 'a');
    expect(listing.items.map((item) => item.name), <String>['sub', 'x.txt']);
    expect(listing.items.first.isFolder, isTrue);
    expect(listing.items.last.size, 12);
    expect(listing.items.last.modified, isNotNull);

    final test = await client.testExternalMount(<String, Object?>{'provider': 'webdav'});
    expect(test.ok, isFalse);
    expect(test.error, contains('refused'));
  });

  test('download addresses carry the path and the download flag', () {
    final client = BackendClient(baseUrl: 'http://localhost:1', http: FakeHttp(_server));
    final uri = client.externalFileUri('m1', 'a/x y.txt', download: true);
    expect(uri.path, '/api/external-storage/m1/file');
    expect(uri.queryParameters, <String, String>{'path': 'a/x y.txt', 'download': '1'});
  });
}
