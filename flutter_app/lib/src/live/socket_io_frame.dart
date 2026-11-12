import 'dart:convert';

/// What one text frame of a socket.io v4 connection over a plain WebSocket
/// (engine.io v4, `?EIO=4&transport=websocket`) means for the app. Only the
/// parts NeoCloud's live updates use are decoded.
sealed class SocketIoFrame {
  const SocketIoFrame();

  static SocketIoFrame decode(String text) {
    if (text.isEmpty) return const SocketIoIgnored();
    switch (text[0]) {
      case '0':
        return const SocketIoOpen();
      case '1':
        return const SocketIoClosed();
      case '2':
        return const SocketIoPing();
      case '4':
        return _decodeMessage(text.substring(1));
      default:
        return const SocketIoIgnored();
    }
  }

  static SocketIoFrame _decodeMessage(String packet) {
    if (packet.isEmpty) return const SocketIoIgnored();
    switch (packet[0]) {
      case '0':
        return const SocketIoConnected();
      case '1':
      case '4':
        return const SocketIoClosed();
      case '2':
        final start = packet.indexOf('[');
        if (start < 0) return const SocketIoIgnored();
        try {
          final list = jsonDecode(packet.substring(start));
          if (list is! List || list.isEmpty || list.first is! String) return const SocketIoIgnored();
          return SocketIoEvent(list.first as String, list.length > 1 ? list[1] : null);
        } on FormatException {
          return const SocketIoIgnored();
        }
      default:
        return const SocketIoIgnored();
    }
  }
}

/// The transport is open; the client answers with [connectFrame].
class SocketIoOpen extends SocketIoFrame {
  const SocketIoOpen();
}

/// The server accepted the connection to the default namespace.
class SocketIoConnected extends SocketIoFrame {
  const SocketIoConnected();
}

/// The server pings; the client answers with [pongFrame].
class SocketIoPing extends SocketIoFrame {
  const SocketIoPing();
}

/// Closed or refused (e.g. no session): reconnect later.
class SocketIoClosed extends SocketIoFrame {
  const SocketIoClosed();
}

class SocketIoEvent extends SocketIoFrame {
  const SocketIoEvent(this.name, this.data);

  final String name;
  final Object? data;
}

class SocketIoIgnored extends SocketIoFrame {
  const SocketIoIgnored();
}

/// Joins the default namespace.
const String connectFrame = '40';

/// Answers a ping.
const String pongFrame = '3';

/// The WebSocket address of the socket.io endpoint next to [httpBase].
Uri socketIoUri(Uri httpBase) => httpBase.replace(
  scheme: httpBase.scheme == 'https' ? 'wss' : 'ws',
  path: '/socket.io/',
  queryParameters: const <String, String>{'EIO': '4', 'transport': 'websocket'},
);
