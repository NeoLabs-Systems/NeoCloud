import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'models.dart';

/// What a successful device login hands back. Store [token] in the OS
/// keychain; it is shown only once.
class DeviceCredentials {
  DeviceCredentials({required this.token, required this.deviceId, required this.userId, required this.username});

  final String token;
  final int deviceId;
  final int userId;
  final String username;
}

/// The device-code flow of docs/clients.md, section 1.
class DeviceLogin {
  DeviceLogin._();

  /// Asks the server for a code pair. Show [DeviceAuthorization.userCode]
  /// and open [DeviceAuthorization.verificationUrl], then await
  /// [DeviceAuthorization.waitForApproval].
  static Future<DeviceAuthorization> start(
    Uri server, {
    required String name,
    required String platform,
    http.Client? client,
  }) async {
    final httpClient = client ?? http.Client();
    final json = await _post(httpClient, server, '/api/devices/authorize', {'name': name, 'platform': platform});
    return DeviceAuthorization._(
      server: server,
      client: httpClient,
      deviceCode: json['deviceCode'] as String,
      userCode: json['userCode'] as String,
      verificationUrl: Uri.parse(json['verificationUrl'] as String),
      expiresIn: Duration(seconds: (json['expiresIn'] as num?)?.toInt() ?? 600),
      interval: Duration(seconds: (json['interval'] as num?)?.toInt() ?? 3),
    );
  }

  /// Redeems a QR pairing code shown by the web app (docs/clients.md,
  /// section 5); get [server] and [code] from [PairingLink.parse].
  ///
  /// Throws [DeviceLoginException] with `EXPIRED_TOKEN` for a used, expired
  /// or unknown code (or another server error code such as `RATE_LIMITED`),
  /// and [NeoCloudOfflineException] when the server can't be reached.
  static Future<DeviceCredentials> pair(
    Uri server, {
    required String code,
    required String name,
    required String platform,
    http.Client? client,
  }) async {
    final httpClient = client ?? http.Client();
    try {
      final json = await _post(httpClient, server, '/api/devices/pair', {
        'code': code,
        'name': name,
        'platform': platform,
      });
      return _credentials(json);
    } on NeoCloudApiException catch (e) {
      throw DeviceLoginException(e.code, e.message);
    } finally {
      if (client == null) httpClient.close();
    }
  }
}

/// A scanned QR pairing link: `neocloud://pair?server=<origin>&code=<code>`,
/// or the web fallback `https://<server>/app/pair?code=<code>`.
class PairingLink {
  PairingLink({required this.server, required this.code});

  /// The NeoCloud server, without a trailing slash.
  final Uri server;

  /// The single-use pairing code.
  final String code;

  static final _codePattern = RegExp(r'^[A-Za-z0-9_-]{32,}$');

  /// Parses a scanned text; throws [FormatException] for anything else
  /// (another app's QR code, a malformed server address, a missing code).
  static PairingLink parse(String text) {
    final Uri link;
    try {
      link = Uri.parse(text.trim());
    } on FormatException {
      throw FormatException('Not a NeoCloud pairing link.', text);
    }
    final Uri server;
    switch (link.scheme) {
      case 'neocloud':
        if (link.host != 'pair' || (link.path.isNotEmpty && link.path != '/')) {
          throw FormatException('Not a NeoCloud pairing link.', text);
        }
        server = _server(link.queryParameters['server'] ?? '', text);
      case 'http' || 'https':
        final path = link.path.endsWith('/') ? link.path.substring(0, link.path.length - 1) : link.path;
        if (!path.endsWith('/app/pair')) throw FormatException('Not a NeoCloud pairing link.', text);
        final base = path.substring(0, path.length - '/app/pair'.length);
        server = _server(link.replace(path: base, query: '', fragment: '').toString(), text);
      default:
        throw FormatException('Not a NeoCloud pairing link.', text);
    }
    final code = link.queryParameters['code'] ?? '';
    if (!_codePattern.hasMatch(code)) throw FormatException('The pairing link has no valid code.', text);
    return PairingLink(server: server, code: code);
  }

  static Uri _server(String value, String source) {
    final Uri? uri = Uri.tryParse(value);
    final valid =
        uri != null && (uri.scheme == 'https' || uri.scheme == 'http') && uri.host.isNotEmpty && uri.userInfo.isEmpty;
    if (!valid) throw FormatException('The pairing link has no valid server address.', source);
    final path = uri.path.endsWith('/') ? uri.path.substring(0, uri.path.length - 1) : uri.path;
    return Uri(scheme: uri.scheme, host: uri.host, port: uri.hasPort ? uri.port : null, path: path);
  }

  @override
  String toString() => 'PairingLink($server)';
}

/// Why a device login ended without a token.
class DeviceLoginException implements Exception {
  DeviceLoginException(this.code, this.message);

  /// ACCESS_DENIED, EXPIRED_TOKEN, TIMEOUT or a server error code.
  final String code;
  final String message;

  @override
  String toString() => 'DeviceLoginException($code: $message)';
}

class DeviceAuthorization {
  DeviceAuthorization._({
    required this.server,
    required http.Client client,
    required this.deviceCode,
    required this.userCode,
    required this.verificationUrl,
    required this.expiresIn,
    required this.interval,
  }) : _client = client;

  final Uri server;
  final http.Client _client;
  final String deviceCode;

  /// `ABCD-EFGH`, typed or confirmed by the user in the web app.
  final String userCode;
  final Uri verificationUrl;
  final Duration expiresIn;
  final Duration interval;

  bool _cancelled = false;

  /// Stops a running [waitForApproval]; it throws `CANCELLED`.
  void cancel() => _cancelled = true;

  /// Polls until the user approves (token), denies (`ACCESS_DENIED`) or the
  /// code expires (`EXPIRED_TOKEN`). Network hiccups are retried.
  Future<DeviceCredentials> waitForApproval({Duration timeout = const Duration(minutes: 10)}) async {
    final deadline = DateTime.now().add(timeout < expiresIn ? timeout : expiresIn);
    while (true) {
      if (_cancelled) throw DeviceLoginException('CANCELLED', 'Login cancelled.');
      try {
        return _credentials(await _post(_client, server, '/api/devices/token', {'deviceCode': deviceCode}));
      } on NeoCloudApiException catch (e) {
        if (e.code != 'AUTHORIZATION_PENDING' && e.code != 'SLOW_DOWN' && e.status != 429) {
          throw DeviceLoginException(e.code, e.message);
        }
      } on NeoCloudOfflineException {
        // Try again on the next tick.
      }
      if (DateTime.now().isAfter(deadline)) {
        throw DeviceLoginException('TIMEOUT', 'The code was not approved in time.');
      }
      await Future<void>.delayed(interval);
    }
  }
}

// The `{ token, device, user }` answer of /api/devices/token and /pair.
DeviceCredentials _credentials(Map<String, dynamic> json) {
  final device = json['device'] as Map;
  final user = json['user'] as Map;
  return DeviceCredentials(
    token: json['token'] as String,
    deviceId: (device['id'] as num).toInt(),
    userId: (user['id'] as num).toInt(),
    username: user['username'] as String,
  );
}

Future<Map<String, dynamic>> _post(http.Client client, Uri server, String path, Map<String, dynamic> body) async {
  final base = server.path.endsWith('/') ? server.path.substring(0, server.path.length - 1) : server.path;
  final http.Response response;
  try {
    response = await client
        .post(
          server.replace(path: '$base$path'),
          headers: {'Content-Type': 'application/json', 'Accept': 'application/json'},
          body: jsonEncode(body),
        )
        .timeout(const Duration(seconds: 30));
  } on Exception catch (e) {
    throw NeoCloudOfflineException(e);
  }
  final decoded = response.body.isEmpty ? const {} : jsonDecode(response.body);
  final json = decoded is Map<String, dynamic> ? decoded : <String, dynamic>{};
  if (response.statusCode >= 200 && response.statusCode < 300) return json;
  if (response.statusCode >= 502 && response.statusCode <= 504) {
    throw NeoCloudOfflineException('HTTP ${response.statusCode}');
  }
  throw NeoCloudApiException(
    response.statusCode,
    json['code']?.toString() ?? 'HTTP_${response.statusCode}',
    json['error']?.toString() ?? 'Request failed (${response.statusCode})',
  );
}
