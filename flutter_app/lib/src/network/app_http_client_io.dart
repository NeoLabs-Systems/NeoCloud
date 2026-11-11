import 'package:http/http.dart' as http;

import 'app_http_client.dart';

AppHttpClient createPlatformHttpClient() => IoAppHttpClient();

/// The server's session cookie name (server/http/middleware.js).
const String sessionCookieName = 'neocloud.sid';

/// Picks the session cookie out of a `set-cookie` header.
///
/// `package:http` joins repeated `set-cookie` headers with commas, and
/// `Expires=` dates contain commas too, so split only where a new
/// `name=value` pair starts. Other cookies (load balancer affinity and the
/// like) are ignored, and an emptied session cookie keeps the current one.
String? sessionCookieFromSetCookie(String rawHeader) {
  for (final cookie in rawHeader.split(RegExp(r',(?=\s*[^;,=\s]+=)'))) {
    final pair = cookie.split(';').first.trim();
    final separatorIndex = pair.indexOf('=');
    if (separatorIndex <= 0) {
      continue;
    }
    final name = pair.substring(0, separatorIndex).trim();
    final value = pair.substring(separatorIndex + 1).trim();
    if (name == sessionCookieName && value.isNotEmpty) {
      return '$name=$value';
    }
  }
  return null;
}

class IoAppHttpClient implements AppHttpClient {
  final http.Client _client = http.Client();
  String? _sessionCookie;

  @override
  Future<HttpResponseData> send(
    String method,
    Uri uri, {
    Map<String, String>? headers,
    Object? body,
  }) async {
    final request = http.Request(method, uri);
    request.headers.addAll(_withCookie(headers));
    if (body is String) {
      request.body = body;
    } else if (body is List<int>) {
      request.bodyBytes = body;
    }
    final response = await http.Response.fromStream(await _client.send(request));
    _storeCookie(response.headers);
    return HttpResponseData(
      statusCode: response.statusCode,
      body: response.body,
      bodyBytes: response.bodyBytes,
      headers: response.headers,
    );
  }

  Map<String, String> _withCookie(Map<String, String>? headers) {
    final next = <String, String>{...?headers};
    final cookie = _sessionCookie;
    if (cookie != null && cookie.isNotEmpty) next['Cookie'] = cookie;
    return next;
  }

  void _storeCookie(Map<String, String> headers) {
    final rawCookie = headers['set-cookie'];
    if (rawCookie == null || rawCookie.isEmpty) return;
    final sessionCookie = sessionCookieFromSetCookie(rawCookie);
    if (sessionCookie != null) _sessionCookie = sessionCookie;
  }

  @override
  Future<void> close() async => _client.close();

  @override
  void clearSession() => _sessionCookie = null;

  @override
  void restoreSession(String? sessionCookie) {
    final normalized = sessionCookie?.trim() ?? '';
    _sessionCookie = normalized.isEmpty ? null : normalized;
  }

  @override
  String? get sessionCookie => _sessionCookie;
}
