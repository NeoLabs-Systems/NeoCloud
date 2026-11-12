import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'live_connection.dart';
import 'socket_io_frame.dart';

/// The browser's WebSocket, signed in by the session cookie.
LiveSocketOpener? liveSocketOpener() => _open;

LiveSocket _open(Uri url, LiveSocketHandlers handlers) => _WebLiveSocket(url, handlers);

class _WebLiveSocket implements LiveSocket {
  _WebLiveSocket(Uri url, this._handlers) : _socket = web.WebSocket(url.toString()) {
    _socket.onmessage = ((web.MessageEvent event) => _onMessage(event.data)).toJS;
    _socket.onclose = ((web.CloseEvent _) => _onClosed()).toJS;
    _socket.onerror = ((web.Event _) => _onClosed()).toJS;
  }

  final web.WebSocket _socket;
  final LiveSocketHandlers _handlers;
  bool _closed = false;

  void _onMessage(JSAny? data) {
    if (data == null || !data.isA<JSString>()) return;
    final frame = SocketIoFrame.decode((data as JSString).toDart);
    switch (frame) {
      case SocketIoOpen():
        _socket.send(connectFrame.toJS);
      case SocketIoPing():
        _socket.send(pongFrame.toJS);
      case SocketIoConnected():
        _handlers.onConnected();
      case SocketIoEvent(:final name, :final data):
        _handlers.onEvent(name, data);
      case SocketIoClosed():
        final wasOpen = !_closed;
        close();
        if (wasOpen) _handlers.onClosed();
      case SocketIoIgnored():
        break;
    }
  }

  void _onClosed() {
    if (_closed) return;
    _closed = true;
    _handlers.onClosed();
  }

  @override
  void close() {
    _closed = true;
    _socket.close();
  }
}
