import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/public_signing_http.dart';
import 'package:neocloud/src/signing/signing.dart';

/// What the server saw of one request.
class _Seen {
  _Seen(this.method, this.uri, this.headers, this.body);

  final String method;
  final Uri uri;
  final HttpHeaders headers;
  final Uint8List body;
}

void main() {
  late HttpServer server;
  late HttpPublicSigningApi api;
  final seen = <_Seen>[];
  late Future<void> Function(HttpRequest request) handler;

  setUp(() async {
    seen.clear();
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    handler = (request) async {
      request.response.write('{}');
      await request.response.close();
    };
    server.listen((request) async {
      final body = await request.fold<BytesBuilder>(BytesBuilder(), (builder, chunk) => builder..add(chunk));
      seen.add(_Seen(request.method, request.uri, request.headers, body.takeBytes()));
      await handler(request);
    });
    api = HttpPublicSigningApi(token: 'abc_DEF-123', resolve: (path) => Uri.parse('http://127.0.0.1:${server.port}$path'));
  });

  tearDown(() async {
    api.close();
    await server.close(force: true);
  });

  test('every call goes to the link\'s own path with no credentials', () async {
    await api.view();
    await api.consent(signerName: 'Ada', timezone: 'Europe/Berlin', tzOffsetMinutes: 60);
    await api.saveValue('f1', 'text');
    await api.complete();
    await api.decline(reason: ' too late ');
    expect(seen.map((r) => '${r.method} ${r.uri.path}'), <String>[
      'GET /api/public/sign/abc_DEF-123',
      'POST /api/public/sign/abc_DEF-123/consent',
      'PUT /api/public/sign/abc_DEF-123/fields/f1',
      'POST /api/public/sign/abc_DEF-123/complete',
      'POST /api/public/sign/abc_DEF-123/decline',
    ]);
    for (final request in seen) {
      expect(request.headers.value('authorization'), isNull);
      expect(request.headers.value('cookie'), isNull);
    }
    expect(jsonDecode(utf8.decode(seen[1].body)), <String, dynamic>{'accepted': true, 'signerName': 'Ada', 'timezone': 'Europe/Berlin', 'tzOffsetMinutes': 60});
    expect(jsonDecode(utf8.decode(seen[2].body)), <String, dynamic>{'value': 'text'});
    expect(jsonDecode(utf8.decode(seen[4].body)), <String, dynamic>{'reason': 'too late'});
  });

  test('a signature is uploaded as the raw image with its method in the query', () async {
    final png = Uint8List.fromList(<int>[0x89, 0x50, 0x4e, 0x47, 1, 2, 3]);
    await api.uploadValue('f1', bytes: png, fileName: 'signature.png', method: SignatureMethod.typed, typedText: 'Ada Lovelace');
    final request = seen.single;
    expect(request.method, 'POST');
    expect(request.uri.path, '/api/public/sign/abc_DEF-123/fields/f1/upload');
    expect(request.uri.queryParameters, <String, String>{'method': 'type', 'text': 'Ada Lovelace'});
    expect(request.headers.contentType?.mimeType, 'image/png');
    expect(request.body, png);
  });

  test('a drawn signature sends no text; a file sends its name and type', () async {
    await api.uploadValue('s', bytes: Uint8List.fromList(<int>[1]), fileName: 's.png', method: SignatureMethod.drawn, typedText: 'ignored');
    await api.uploadValue('d', bytes: Uint8List.fromList(utf8.encode('hello')), fileName: 'note äöü.txt', contentType: 'text/plain');
    expect(seen[0].uri.queryParameters, <String, String>{'method': 'draw'});
    expect(seen[1].uri.queryParameters, <String, String>{'name': 'note äöü.txt'});
    expect(seen[1].headers.contentType?.mimeType, 'text/plain');
    expect(utf8.decode(seen[1].body), 'hello');
  });

  test('FIELDS_INCOMPLETE carries the field ids', () async {
    handler = (request) async {
      request.response.statusCode = 400;
      request.response.write(jsonEncode(<String, Object>{'error': 'Some required fields are still empty.', 'code': 'FIELDS_INCOMPLETE', 'fieldIds': <String>['a', 'b']}));
      await request.response.close();
    };
    await expectLater(
      api.complete(),
      throwsA(
        isA<SigningException>()
            .having((e) => e.kind, 'kind', SigningErrorKind.invalid)
            .having((e) => e.code, 'code', 'FIELDS_INCOMPLETE')
            .having((e) => e.fieldIds, 'fieldIds', <String>['a', 'b']),
      ),
    );
  });

  test('statuses map to kinds; 429 keeps its pause', () async {
    for (final entry in <int, SigningErrorKind>{404: SigningErrorKind.notFound, 409: SigningErrorKind.conflict, 413: SigningErrorKind.invalid, 500: SigningErrorKind.server}.entries) {
      handler = (request) async {
        request.response.statusCode = entry.key;
        await request.response.close();
      };
      await expectLater(api.view(), throwsA(isA<SigningException>().having((e) => e.kind, 'kind', entry.value)));
    }
    handler = (request) async {
      request.response.statusCode = 429;
      request.response.headers.set('retry-after', '7');
      await request.response.close();
    };
    await expectLater(api.view(), throwsA(isA<SigningException>().having((e) => e.retryAfter, 'retryAfter', const Duration(seconds: 7))));
  });

  test('the signed file comes with or without the signature pages', () async {
    await api.finalPdf();
    await api.finalPdf(certificate: false);
    expect(seen.map((r) => '${r.method} ${r.uri.path}${r.uri.hasQuery ? '?${r.uri.query}' : ''}'), <String>[
      'GET /api/public/sign/abc_DEF-123/final',
      'GET /api/public/sign/abc_DEF-123/final?certificate=0',
    ]);
  });

  test('the public view carries a count and progress, not names', () async {
    handler = (request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write('{"state":"waiting","document":{"title":"x"},"party":{"id":"p","name":"Me"},"turn":{"canSign":false,"waitingCount":2},"progress":{"done":1,"total":4}}');
      await request.response.close();
    };
    final view = await api.view();
    expect(view.turn.waitingCount, 2);
    expect(view.progress.total, 4);
  });

  test('an unreachable server is a network error that never mentions the link', () async {
    await server.close(force: true);
    try {
      await api.view();
      fail('should have thrown');
    } on SigningException catch (error) {
      expect(error.kind, SigningErrorKind.network);
      expect(error.toString(), isNot(contains('abc_DEF')));
      expect(error.message, isNot(contains('abc_DEF')));
    }
  });
}
