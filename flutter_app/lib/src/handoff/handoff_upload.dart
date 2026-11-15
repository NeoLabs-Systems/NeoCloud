import 'dart:async';

import 'package:http/http.dart' as http;

import '../files/upload_queue.dart';

/// Sends [source] as the body of one PUT without holding the file in memory
/// (the platform's own client carries the session cookie in a browser).
/// Returns the status code and the response body. [onProgress] gets the bytes
/// handed to the connection so far.
Future<({int status, String body})> putStreamed(
  Uri uri,
  Map<String, String> headers,
  UploadSource source, {
  void Function(int sent)? onProgress,
}) async {
  final client = http.Client();
  try {
    final request = http.StreamedRequest('PUT', uri)
      ..headers.addAll(headers)
      ..headers['Content-Type'] = 'application/octet-stream'
      ..contentLength = source.size;
    var sent = 0;
    unawaited(() async {
      try {
        await for (final chunk in source.openRead()) {
          request.sink.add(chunk);
          sent += chunk.length;
          onProgress?.call(sent);
        }
      } catch (error, stack) {
        request.sink.addError(error, stack);
      } finally {
        await request.sink.close();
      }
    }());
    final response = await http.Response.fromStream(await client.send(request));
    return (status: response.statusCode, body: response.body);
  } finally {
    client.close();
  }
}
