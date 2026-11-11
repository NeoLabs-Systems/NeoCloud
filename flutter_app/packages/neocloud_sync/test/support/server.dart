import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:http/http.dart' as http;
import 'package:path/path.dart' as p;

/// The real NeoCloud server (`node server/index.js` from the repo root) on
/// a free port with its own temporary NEOCLOUD_HOME.
class TestServer {
  TestServer._(this.home, this.port, this._env);

  static Future<TestServer> start() async {
    final home = await Directory.systemTemp.createTemp('neocloud-sync-e2e-');
    final socket = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final port = socket.port;
    await socket.close();
    String hex() => List.generate(32, (_) => Random.secure().nextInt(256).toRadixString(16).padLeft(2, '0')).join();
    final server = TestServer._(home, port, {
      'NEOCLOUD_HOME': home.path,
      'PORT': '$port',
      'NODE_ENV': 'development',
      'NEOCLOUD_MASTER_KEY': hex(),
      'SESSION_SECRET': hex(),
    });
    await server.launch();
    return server;
  }

  final Directory home;
  final int port;
  final Map<String, String> _env;
  Process? _process;
  final StringBuffer log = StringBuffer();

  Uri get url => Uri.parse('http://127.0.0.1:$port');

  static String get repoRoot => p.normalize(p.join(Directory.current.path, '..', '..', '..'));

  Future<void> launch() async {
    final process = await Process.start('node', ['server/index.js'], workingDirectory: repoRoot, environment: _env);
    _process = process;
    process.stdout.transform(utf8.decoder).listen(log.write);
    process.stderr.transform(utf8.decoder).listen(log.write);
    final deadline = DateTime.now().add(const Duration(seconds: 30));
    while (DateTime.now().isBefore(deadline)) {
      try {
        final res = await http.get(url.replace(path: '/api/auth/status')).timeout(const Duration(seconds: 2));
        if (res.statusCode == 200) return;
      } on Exception {
        // Not up yet.
      }
      await Future<void>.delayed(const Duration(milliseconds: 200));
    }
    throw StateError('Server did not start:\n$log');
  }

  Future<void> halt() async {
    final process = _process;
    if (process == null) return;
    _process = null;
    process.kill(ProcessSignal.sigterm);
    await process.exitCode.timeout(
      const Duration(seconds: 10),
      onTimeout: () {
        process.kill(ProcessSignal.sigkill);
        return process.exitCode;
      },
    );
  }

  Future<void> dispose() async {
    await halt();
    await home.delete(recursive: true);
  }
}

/// A browser-style session (cookie) for the account routes.
class Session {
  Session(this.server, this.cookie);

  final TestServer server;
  final String cookie;

  static Future<Session> register(TestServer server, String username, String password) async {
    final res = await http.post(
      server.url.replace(path: '/api/auth/register'),
      headers: {'Content-Type': 'application/json'},
      body: jsonEncode({'username': username, 'email': '$username@example.com', 'password': password}),
    );
    if (res.statusCode != 200) throw StateError('register failed: ${res.statusCode} ${res.body}');
    final setCookie = res.headers['set-cookie'] ?? '';
    final match = RegExp(r'neocloud\.sid=[^;]+').firstMatch(setCookie);
    if (match == null) throw StateError('no session cookie: $setCookie');
    return Session(server, match.group(0)!);
  }

  Future<http.Response> post(String path, Map<String, dynamic> body) => http.post(
    server.url.replace(path: path),
    headers: {'Content-Type': 'application/json', 'Cookie': cookie},
    body: jsonEncode(body),
  );
}
