import 'dart:typed_data';

/// The transport the API client talks through. The web version lets the
/// browser carry the session cookie; the native one keeps it itself.
abstract class AppHttpClient {
  Future<HttpResponseData> send(
    String method,
    Uri uri, {
    Map<String, String>? headers,
    Object? body,
  });

  Future<void> close();

  void clearSession();

  void restoreSession(String? sessionCookie);

  String? get sessionCookie;
}

class HttpResponseData {
  const HttpResponseData({
    required this.statusCode,
    required this.body,
    required this.bodyBytes,
    required this.headers,
  });

  final int statusCode;
  final String body;
  final Uint8List bodyBytes;
  final Map<String, String> headers;
}
