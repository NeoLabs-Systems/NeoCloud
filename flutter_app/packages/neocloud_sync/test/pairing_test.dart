import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:neocloud_sync/neocloud_sync.dart';
import 'package:test/test.dart';

const code = 'q8Xc1Lr0bT3n_y-Vh2Kd9sWm4Pz7Ae6Jf5Gu0Ri1Oo';

void main() {
  group('PairingLink.parse', () {
    test('reads the app link', () {
      final query = Uri(queryParameters: {'server': 'https://cloud.example.com', 'code': code}).query;
      final link = PairingLink.parse('neocloud://pair?$query');
      expect(link.server, Uri.parse('https://cloud.example.com'));
      expect(link.code, code);
    });

    test('keeps a port and a path prefix, drops a trailing slash', () {
      final query = Uri(queryParameters: {'server': 'http://192.168.1.4:3000/cloud/', 'code': code}).query;
      final link = PairingLink.parse('  neocloud://pair?$query\n');
      expect(link.server.toString(), 'http://192.168.1.4:3000/cloud');
    });

    test('accepts the web fallback', () {
      final link = PairingLink.parse('https://cloud.example.com/app/pair?code=$code');
      expect(link.server.toString(), 'https://cloud.example.com');
      expect(link.code, code);

      final nested = PairingLink.parse('https://example.com:8443/neocloud/app/pair/?code=$code#x');
      expect(nested.server.toString(), 'https://example.com:8443/neocloud');
    });

    test('rejects everything else', () {
      final bad = [
        '',
        'hello',
        'https://cloud.example.com/app/device?code=ABCD-EFGH',
        'https://cloud.example.com/app/pair',
        'https://cloud.example.com/app/pair?code=short',
        'neocloud://pair?code=$code',
        'neocloud://pair?server=ftp://cloud.example.com&code=$code',
        'neocloud://pair?server=https://user:pw@cloud.example.com&code=$code',
        'neocloud://other?server=https://cloud.example.com&code=$code',
        'neocloud://pair/extra?server=https://cloud.example.com&code=$code',
        'neocloud://pair?server=https://cloud.example.com&code=${code.replaceRange(0, 1, '+')}',
        'otpauth://totp/x?secret=ABC',
      ];
      for (final text in bad) {
        expect(() => PairingLink.parse(text), throwsFormatException, reason: text);
      }
    });
  });

  group('DeviceLogin.pair', () {
    test('posts the code and returns credentials', () async {
      late http.Request sent;
      final client = MockClient((request) async {
        sent = request;
        return http.Response(
          jsonEncode({
            'token': 'nc_abc',
            'device': {'id': 7, 'name': 'Pixel 9', 'platform': 'android'},
            'user': {'id': 1, 'username': 'neo'},
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final credentials = await DeviceLogin.pair(
        Uri.parse('https://cloud.example.com/sub'),
        code: code,
        name: 'Pixel 9',
        platform: 'android',
        client: client,
      );
      expect(sent.method, 'POST');
      expect(sent.url.toString(), 'https://cloud.example.com/sub/api/devices/pair');
      expect(jsonDecode(sent.body), {'code': code, 'name': 'Pixel 9', 'platform': 'android'});
      expect(credentials.token, 'nc_abc');
      expect(credentials.deviceId, 7);
      expect(credentials.userId, 1);
      expect(credentials.username, 'neo');
    });

    test('a spent code is EXPIRED_TOKEN', () async {
      final client = MockClient(
        (_) async => http.Response(jsonEncode({'error': 'expired', 'code': 'EXPIRED_TOKEN'}), 410),
      );
      await expectLater(
        DeviceLogin.pair(Uri.parse('https://x.example'), code: code, name: 'a', platform: 'ios', client: client),
        throwsA(isA<DeviceLoginException>().having((e) => e.code, 'code', 'EXPIRED_TOKEN')),
      );
    });

    test('an unreachable server is offline, not a login failure', () async {
      final client = MockClient((_) async => throw http.ClientException('connection refused'));
      await expectLater(
        DeviceLogin.pair(Uri.parse('https://x.example'), code: code, name: 'a', platform: 'ios', client: client),
        throwsA(isA<NeoCloudOfflineException>()),
      );
    });
  });
}
