// End-to-end: QR pairing (docs/clients.md, section 5) against the real
// server. Run with `dart test -t e2e`.
@Tags(['e2e'])
library;

import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:neocloud_sync/neocloud_sync.dart';
import 'package:test/test.dart';

import 'support/server.dart';

late TestServer server;
late Session session;

/// `GET /api/devices/me` with a device token.
Future<Map<String, dynamic>> deviceMe(String token) async {
  final res = await http.get(server.url.replace(path: '/api/devices/me'), headers: {'Authorization': 'Bearer $token'});
  expect(res.statusCode, 200, reason: res.body);
  return jsonDecode(res.body) as Map<String, dynamic>;
}

Future<Map<String, dynamic>> pairingStatus(String code) async {
  final res = await http.get(
    server.url.replace(path: '/api/account/devices/pairing/$code'),
    headers: {'Cookie': session.cookie},
  );
  expect(res.statusCode, 200, reason: res.body);
  return jsonDecode(res.body) as Map<String, dynamic>;
}

void main() {
  setUpAll(() async {
    server = await TestServer.start();
    session = await Session.register(server, 'pairtester', 'Plum-Orbit-73!quartz');
  });

  tearDownAll(() async {
    await server.dispose();
  });

  test('a phone redeems the web app QR once and gets a working token', () async {
    final res = await session.post('/api/account/devices/pairing', {});
    expect(res.statusCode, 200, reason: res.body);
    final body = jsonDecode(res.body) as Map<String, dynamic>;
    final link = PairingLink.parse(body['qrPayload'] as String);
    expect(link.server, server.url);
    expect(link.code, body['code']);
    expect((await pairingStatus(link.code))['status'], 'pending');

    final credentials = await DeviceLogin.pair(link.server, code: link.code, name: 'Pixel 9', platform: 'android');
    expect(credentials.token, startsWith('nc_'));
    expect(credentials.username, 'pairtester');

    final me = await deviceMe(credentials.token);
    expect((me['device'] as Map)['id'], credentials.deviceId);
    expect((me['user'] as Map)['id'], credentials.userId);

    final status = await pairingStatus(link.code);
    expect(status['status'], 'used');
    expect((status['device'] as Map)['name'], 'Pixel 9');

    await expectLater(
      DeviceLogin.pair(link.server, code: link.code, name: 'Second', platform: 'android'),
      throwsA(isA<DeviceLoginException>().having((e) => e.code, 'code', 'EXPIRED_TOKEN')),
    );
  });

  test('the web fallback link works the same way', () async {
    final res = await session.post('/api/account/devices/pairing', {'name': 'iPad', 'platform': 'ios'});
    final code = (jsonDecode(res.body) as Map)['code'] as String;
    final link = PairingLink.parse(server.url.replace(path: '/app/pair', queryParameters: {'code': code}).toString());
    final credentials = await DeviceLogin.pair(link.server, code: link.code, name: '', platform: '');
    final me = await deviceMe(credentials.token);
    expect((me['device'] as Map)['name'], 'iPad');
  });

  test('an unknown code is EXPIRED_TOKEN', () async {
    await expectLater(
      DeviceLogin.pair(server.url, code: 'A' * 43, name: 'Guess', platform: 'android'),
      throwsA(isA<DeviceLoginException>().having((e) => e.code, 'code', 'EXPIRED_TOKEN')),
    );
  });
}
