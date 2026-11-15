import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'handoff_stream.dart';

HandoffStream? createHandoffStream() => _IoHandoffStream();

class _IoHandoffStream implements HandoffStream {
  HttpClient? _client;
  StreamSubscription<String>? _lines;
  bool _closed = false;

  @override
  void open(
    Uri url,
    Map<String, String> headers, {
    required HandoffEventHandler onEvent,
    required void Function() onClosed,
  }) {
    close();
    _closed = false;
    unawaited(_run(url, headers, onEvent, onClosed));
  }

  Future<void> _run(
    Uri url,
    Map<String, String> headers,
    HandoffEventHandler onEvent,
    void Function() onClosed,
  ) async {
    final client = HttpClient()..idleTimeout = const Duration(minutes: 5);
    _client = client;
    try {
      final request = await client.getUrl(url);
      headers.forEach(request.headers.set);
      request.headers.set('Accept', 'text/event-stream');
      final response = await request.close();
      if (response.statusCode != 200) {
        await response.drain<void>();
        return;
      }
      final parser = SseParser(onEvent);
      final done = Completer<void>();
      _lines = response
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .listen(
            parser.addLine,
            onDone: done.complete,
            onError: (Object _) => done.complete(),
            cancelOnError: true,
          );
      await done.future;
    } catch (_) {
      // Offline or the server restarted: the owner reconnects.
    } finally {
      client.close(force: true);
      if (identical(_client, client)) _client = null;
      if (!_closed) onClosed();
    }
  }

  @override
  void close() {
    _closed = true;
    unawaited(_lines?.cancel());
    _lines = null;
    _client?.close(force: true);
    _client = null;
  }
}
