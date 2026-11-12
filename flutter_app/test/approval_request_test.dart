import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';

/// A server from before phone approvals: the approval routes refuse a
/// device token, while [tokenValid] says whether the token works elsewhere.
Future<HttpServer> _oldServer({required bool tokenValid}) async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  server.listen((request) async {
    await request.drain<void>();
    final response = request.response..headers.contentType = ContentType.json;
    final ok = request.uri.path == '/api/devices/me' && tokenValid;
    response.statusCode = ok ? 200 : 401;
    response.write(jsonEncode(ok ? <String, dynamic>{'device': <String, dynamic>{}} : <String, dynamic>{'error': 'Unauthorized', 'code': 'UNAUTHORIZED'}));
    await response.close();
  });
  return server;
}

void main() {
  test('a refused approval keeps a working phone signed in', () async {
    final server = await _oldServer(tokenValid: true);
    addTearDown(() => server.close(force: true));
    var expired = false;
    final client = BackendClient(baseUrl: 'http://127.0.0.1:${server.port}')
      ..bearerToken = 'nc_phone'
      ..onSessionExpired = () => expired = true;
    await expectLater(
      client.deviceAuthorization('ABCD-EFGH'),
      throwsA(isA<ApiException>().having((e) => e.code, 'code', 'APPROVAL_NEEDS_SESSION')),
    );
    await expectLater(client.approveQrLogin('c', 's', const <String, Object?>{}), throwsA(isA<ApiException>()));
    expect(expired, isFalse);
  });

  test('account routes an older server refuses keep a working phone signed in', () async {
    final server = await _oldServer(tokenValid: true);
    addTearDown(() => server.close(force: true));
    var expired = false;
    final client = BackendClient(baseUrl: 'http://127.0.0.1:${server.port}')
      ..bearerToken = 'nc_phone'
      ..onSessionExpired = () => expired = true;
    for (final call in <Future<Object?> Function()>[client.accountUser, client.settings, client.sessions, client.adminUsers]) {
      await expectLater(call(), throwsA(isA<ApiException>().having((e) => e.code, 'code', 'SERVER_UPDATE_NEEDED')));
    }
    expect(expired, isFalse);
  });

  test('a revoked token still signs the phone out', () async {
    final server = await _oldServer(tokenValid: false);
    addTearDown(() => server.close(force: true));
    var expired = false;
    final client = BackendClient(baseUrl: 'http://127.0.0.1:${server.port}')
      ..bearerToken = 'nc_revoked'
      ..onSessionExpired = () => expired = true;
    await expectLater(client.deviceAuthorization('ABCD-EFGH'), throwsA(isA<ApiException>().having((e) => e.statusCode, 'status', 401)));
    expect(expired, isTrue);
  });
}
