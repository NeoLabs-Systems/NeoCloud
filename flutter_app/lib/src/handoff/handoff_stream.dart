import 'handoff_stream_stub.dart'
    if (dart.library.io) 'handoff_stream_io.dart'
    as platform;

/// What the event stream reports.
typedef HandoffEventHandler = void Function(String event, Object? data);

/// A server-sent event stream (`GET /api/handoff/events`) that makes this
/// device online and brings it offers. Signed-in phone apps have no socket,
/// so they hold this instead; the web app gets the same events through its
/// socket and never opens it.
abstract class HandoffStream {
  /// Opens the stream; [onClosed] runs when it ends or fails. Safe to call
  /// again after it ended.
  void open(
    Uri url,
    Map<String, String> headers, {
    required HandoffEventHandler onEvent,
    required void Function() onClosed,
  });

  void close();
}

HandoffStream? createHandoffStream() => platform.createHandoffStream();

/// Parses a server-sent event stream: a line at a time. Comment lines (the
/// server's heartbeat) are skipped; an event with no data is dropped.
class SseParser {
  SseParser(this.onEvent);

  final HandoffEventHandler onEvent;
  String _name = '';
  final List<String> _data = <String>[];

  void addLine(String line) {
    if (line.isEmpty) {
      if (_name.isNotEmpty && _data.isNotEmpty) {
        onEvent(_name, _data.join('\n'));
      }
      _name = '';
      _data.clear();
    } else if (line.startsWith(':') || line.startsWith('retry:')) {
      return;
    } else if (line.startsWith('event:')) {
      _name = line.substring(6).trim();
    } else if (line.startsWith('data:')) {
      final value = line.substring(5);
      _data.add(value.startsWith(' ') ? value.substring(1) : value);
    }
  }
}
