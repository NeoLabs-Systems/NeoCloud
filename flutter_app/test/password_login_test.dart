import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/device/password_login.dart';

/// The session routes the phone's login form uses, as the server answers
/// them: a cookie from sign-in, a second factor when [twoFactor], a token
/// for the cookie, and logout.
class _FakeServer {
  _FakeServer({this.twoFactor = false});

  final bool twoFactor;
  late final HttpServer _server;
  final List<String> calls = <String>[];
  final Set<String> liveSessions = <String>{};
  Map<String, dynamic>? deviceRequest;
  int _sessions = 0;

  String get origin => 'http://127.0.0.1:${_server.port}';

  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen(_handle);
  }

  Future<void> stop() => _server.close(force: true);

  String? _session(HttpRequest request) {
    final cookie = request.cookies.where((c) => c.name == 'neocloud.sid').firstOrNull;
    return cookie != null && liveSessions.contains(cookie.value) ? cookie.value : null;
  }

  void _setSession(HttpResponse response, String id) {
    response.headers.add('set-cookie', 'neocloud.sid=$id; Path=/; HttpOnly; SameSite=Lax');
  }

  Future<void> _handle(HttpRequest request) async {
    final body = await utf8.decoder.bind(request).join();
    final json = body.isEmpty ? <String, dynamic>{} : (jsonDecode(body) as Map).cast<String, dynamic>();
    calls.add('${request.method} ${request.uri.path}');
    final response = request.response..headers.contentType = ContentType.json;
    Object answer = <String, dynamic>{};
    const user = <String, dynamic>{'id': 7, 'username': 'ada'};
    switch (request.uri.path) {
      case '/api/auth/status':
        answer = <String, dynamic>{'hasUser': true, 'registrationOpen': false};
      case '/api/auth/login':
        if (json['password'] != 'right') {
          response.statusCode = 401;
          answer = <String, dynamic>{'error': 'Invalid credentials', 'code': 'INVALID_CREDENTIALS'};
        } else {
          final id = 's${++_sessions}';
          liveSessions.add(id);
          _setSession(response, id);
          answer = twoFactor ? <String, dynamic>{'success': false, 'requiresTwoFactor': true} : <String, dynamic>{'success': true, 'user': user};
        }
      case '/api/auth/login/2fa':
        if (_session(request) == null) {
          response.statusCode = 401;
          answer = <String, dynamic>{'error': 'expired', 'code': 'TWO_FACTOR_EXPIRED'};
        } else if (json['code'] == 'locked') {
          response.statusCode = 429;
          response.headers.set('retry-after', '120');
          answer = <String, dynamic>{'error': 'Too many', 'code': 'LOGIN_LOCKED', 'retryAfter': 120};
        } else {
          answer = <String, dynamic>{'success': true, 'user': user};
        }
      case '/api/account/devices':
        if (_session(request) == null) {
          response.statusCode = 401;
          answer = <String, dynamic>{'error': 'Unauthorized', 'code': 'UNAUTHORIZED'};
        } else {
          deviceRequest = json;
          response.statusCode = 201;
          answer = <String, dynamic>{'device': <String, dynamic>{'id': 3}, 'token': 'nc_secret'};
        }
      case '/api/auth/logout':
        final session = _session(request);
        if (session != null) liveSessions.remove(session);
        answer = <String, dynamic>{'success': true};
    }
    response.write(jsonEncode(answer));
    await response.close();
  }
}

void main() {
  late _FakeServer server;

  Future<PasswordDeviceLogin> login({bool twoFactor = false}) async {
    server = _FakeServer(twoFactor: twoFactor);
    await server.start();
    return PasswordDeviceLogin(
      server: server.origin,
      client: BackendClient(baseUrl: server.origin),
      deviceName: 'Pixel 9',
      platform: 'android',
    );
  }

  tearDown(() => server.stop());

  test('signing in swaps the session for a device token, then ends it', () async {
    final phone = await login();
    final status = await phone.status();
    expect(status.hasUser, isTrue);
    expect(status.registrationOpen, isFalse);

    final result = await phone.signIn('ada', 'right');
    final connection = result.connection!;
    expect(connection.token, 'nc_secret');
    expect(connection.server, server.origin);
    expect(connection.username, 'ada');
    expect(connection.accountId, 7);
    expect(server.deviceRequest, <String, dynamic>{'name': 'Pixel 9', 'platform': 'android'});
    expect(server.calls, <String>['GET /api/auth/status', 'POST /api/auth/login', 'POST /api/account/devices', 'POST /api/auth/logout']);
    expect(server.liveSessions, isEmpty, reason: 'the session is signed out');
    expect(phone.client.mediaHeaders['Cookie'], isNull, reason: 'the cookie is dropped');
  });

  test('the second factor keeps the session cookie until the code is right', () async {
    final phone = await login(twoFactor: true);
    final first = await phone.signIn('ada', 'right');
    expect(first.requiresTwoFactor, isTrue);
    expect(first.connection, isNull);
    expect(server.liveSessions, hasLength(1));

    final locked = await phone.verifyCode('locked').then<Object?>((_) => null, onError: (Object error) => error);
    expect(locked, isA<ApiException>());
    final lockout = locked! as ApiException;
    expect(lockout.code, 'LOGIN_LOCKED');
    expect(lockout.retryAfter, const Duration(seconds: 120));

    final done = await phone.verifyCode('123456');
    expect(done.connection!.token, 'nc_secret');
    expect(server.liveSessions, isEmpty);
  });

  test('a wrong password asks for no token and leaves no session', () async {
    final phone = await login();
    await expectLater(phone.signIn('ada', 'wrong'), throwsA(isA<ApiException>().having((e) => e.code, 'code', 'INVALID_CREDENTIALS')));
    expect(server.calls, isNot(contains('POST /api/account/devices')));
    expect(server.liveSessions, isEmpty);
  });
}
