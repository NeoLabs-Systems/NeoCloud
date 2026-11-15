import 'dart:convert';
import 'dart:typed_data';

import 'package:neocloud/src/network/app_http_client.dart';

/// One request the app made.
class FakeCall {
  const FakeCall(this.method, this.path, this.body);

  final String method;
  final String path;
  final Map<String, dynamic> body;

  @override
  String toString() => '$method $path $body';
}

/// What the fake server answers.
class FakeReply {
  const FakeReply(this.status, [this.json = const <String, dynamic>{}]);

  final int status;
  final Object json;
}

typedef FakeHandler = FakeReply Function(FakeCall call);

/// The app's transport without a network: every request is recorded and
/// answered by [handler] (404 `NOT_FOUND` by default).
class FakeHttp implements AppHttpClient {
  FakeHttp([FakeHandler? handler]) : handler = handler ?? ((_) => const FakeReply(404, <String, dynamic>{'error': 'Not found', 'code': 'NOT_FOUND'}));

  FakeHandler handler;
  final List<FakeCall> calls = <FakeCall>[];

  Iterable<FakeCall> where(String method, String path) => calls.where((call) => call.method == method && call.path == path);

  @override
  Future<HttpResponseData> send(String method, Uri uri, {Map<String, String>? headers, Object? body}) async {
    final decoded = body is String && body.isNotEmpty ? (jsonDecode(body) as Map).cast<String, dynamic>() : const <String, dynamic>{};
    final call = FakeCall(method, uri.path, decoded);
    calls.add(call);
    final reply = handler(call);
    final text = jsonEncode(reply.json);
    return HttpResponseData(statusCode: reply.status, body: text, bodyBytes: Uint8List.fromList(utf8.encode(text)), headers: const <String, String>{});
  }

  @override
  Future<void> close() async {}

  @override
  void clearSession() {}

  @override
  void restoreSession(String? sessionCookie) {}

  @override
  String? get sessionCookie => null;
}
