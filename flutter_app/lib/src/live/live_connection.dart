import 'dart:async';
import 'dart:math' as math;

import 'file_change_feed.dart';

/// What a live socket reports back.
class LiveSocketHandlers {
  const LiveSocketHandlers({required this.onConnected, required this.onEvent, required this.onClosed});

  final void Function() onConnected;
  final void Function(String event, Object? data) onEvent;

  /// The connection ended or was refused; not called after [LiveSocket.close].
  final void Function() onClosed;
}

abstract class LiveSocket {
  void close();
}

typedef LiveSocketOpener = LiveSocket Function(Uri url, LiveSocketHandlers handlers);

/// One page of `GET /api/sync/changes?cursor=`; a null cursor asks for the
/// journal's head only (`cursor=latest`).
typedef ChangesFetcher = Future<Map<String, dynamic>> Function(int? cursor);

/// Keeps [feed] told about file changes made elsewhere: through the server's
/// socket (`files:changed`) where it can, reconnecting with backoff and
/// refreshing everything after a reconnect; otherwise, and while the socket
/// is down, by polling the change journal every [pollInterval] while the
/// app is visible.
///
/// The journal is the account's own; folders other accounts share with it
/// change in theirs. The socket names those folders too. Without it,
/// [onPolled] runs after every poll so the app can refresh what it shows of
/// shared folders, and [onSharesChanged] reports the socket's
/// `shares:changed` (a share was added, changed, removed or left).
/// [onAiSuggestions] gets the payload of `ai:suggestions` (the review
/// counts).
class LiveFileUpdates {
  LiveFileUpdates({
    required this.feed,
    required this.fetchChanges,
    this.openSocket,
    this.socketUrl,
    this.onSharesChanged,
    this.onAiSuggestions,
    this.onPolled,
    this.pollInterval = const Duration(seconds: 15),
    this.maxBackoff = const Duration(seconds: 30),
  });

  final FileChangeFeed feed;
  final ChangesFetcher fetchChanges;

  /// Null where the socket can't sign in (device tokens).
  final LiveSocketOpener? openSocket;

  /// Asked at each connect, since the server address can change.
  final Uri Function()? socketUrl;
  final void Function()? onSharesChanged;
  final void Function(Map<Object?, Object?> counts)? onAiSuggestions;
  final void Function()? onPolled;
  final Duration pollInterval;
  final Duration maxBackoff;

  bool _running = false;
  bool _visible = true;
  bool _connected = false;
  bool _hadSocket = false;
  bool _polling = false;
  int _attempt = 0;
  int? _cursor;
  LiveSocket? _socket;
  Timer? _retry;
  Timer? _poll;

  bool get connected => _connected;

  void start() {
    if (_running) return;
    _running = true;
    _connect();
    _poll = Timer.periodic(pollInterval, (_) => unawaited(pollNow()));
    // Without a socket, learn the journal position right away.
    if (openSocket == null) unawaited(pollNow());
  }

  void stop() {
    _running = false;
    _connected = false;
    _hadSocket = false;
    _attempt = 0;
    _cursor = null;
    _retry?.cancel();
    _poll?.cancel();
    _retry = null;
    _poll = null;
    _socket?.close();
    _socket = null;
  }

  /// The app was hidden or shown again; coming back catches up at once.
  void setVisible(bool visible) {
    _visible = visible;
    if (visible) unawaited(pollNow());
  }

  void _connect() {
    final open = openSocket;
    final url = socketUrl;
    if (!_running || open == null || url == null) return;
    _socket = open(
      url(),
      LiveSocketHandlers(
        onConnected: () {
          _connected = true;
          _attempt = 0;
          // What changed while the socket was down is unknown.
          if (_hadSocket) feed.addEverything();
          _hadSocket = true;
        },
        onEvent: (event, data) {
          if (event == 'shares:changed') onSharesChanged?.call();
          if (event == 'ai:suggestions' && data is Map) onAiSuggestions?.call(data);
          if (event != 'files:changed' || data is! Map) return;
          final ids = data['folderIds'];
          if (ids is List) feed.addFolders(ids.whereType<String>());
        },
        onClosed: () {
          _connected = false;
          _socket = null;
          _retryLater();
        },
      ),
    );
  }

  /// 1, 2, 4 … seconds up to [maxBackoff].
  Duration backoff(int attempt) {
    final seconds = math.min(maxBackoff.inSeconds, 1 << math.min(attempt, 16));
    return Duration(seconds: seconds);
  }

  void _retryLater() {
    if (!_running) return;
    _retry?.cancel();
    _retry = Timer(backoff(_attempt), _connect);
    _attempt += 1;
  }

  /// Reads the journal since the last poll. The first read only learns the
  /// head (`cursor=latest`, without paging through the journal); when that
  /// happens after the socket dropped, everything is refreshed once since
  /// the gap is unknown.
  Future<void> pollNow() async {
    if (!_running || !_visible || _connected || _polling) return;
    _polling = true;
    try {
      final known = _cursor;
      int? cursor = known;
      final changes = <dynamic>[];
      while (true) {
        final page = await fetchChanges(cursor);
        if (!_running) return;
        cursor = (page['cursor'] as num?)?.toInt() ?? cursor ?? 0;
        if (known != null) changes.addAll((page['changes'] as List?) ?? const <dynamic>[]);
        if (page['hasMore'] != true) break;
      }
      _cursor = cursor;
      if (known == null) {
        if (_hadSocket) feed.addEverything();
      } else if (changes.isNotEmpty) {
        feed.add(batchFromJournal(changes));
      }
      onPolled?.call();
    } catch (_) {
      // Offline or signed out; the next tick tries again.
    } finally {
      _polling = false;
    }
  }
}
