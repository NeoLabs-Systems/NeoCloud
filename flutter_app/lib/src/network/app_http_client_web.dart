import 'package:http/browser_client.dart';
import 'package:http/http.dart' as http;

import 'app_http_client.dart';

AppHttpClient createPlatformHttpClient() => WebAppHttpClient();

class WebAppHttpClient implements AppHttpClient {
  WebAppHttpClient() : _client = BrowserClient()..withCredentials = true;

  final BrowserClient _client;

  @override
  Future<HttpResponseData> send(
    String method,
    Uri uri, {
    Map<String, String>? headers,
    Object? body,
  }) async {
    final request = http.Request(method, uri);
    if (headers != null) request.headers.addAll(headers);
    if (body is String) {
      request.body = body;
    } else if (body is List<int>) {
      request.bodyBytes = body;
    }
    final response = await http.Response.fromStream(await _client.send(request));
    return HttpResponseData(
      statusCode: response.statusCode,
      body: response.body,
      bodyBytes: response.bodyBytes,
      headers: response.headers,
    );
  }

  @override
  Future<void> close() async => _client.close();

  @override
  void clearSession() {}

  @override
  void restoreSession(String? sessionCookie) {}

  @override
  String? get sessionCookie => null;
}
