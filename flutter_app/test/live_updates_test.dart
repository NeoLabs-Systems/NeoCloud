import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/live/file_change_feed.dart';
import 'package:neocloud/src/live/live_connection.dart';
import 'package:neocloud/src/live/socket_io_frame.dart';

class _FakeSocket implements LiveSocket {
  _FakeSocket(this.handlers);

  final LiveSocketHandlers handlers;
  bool closed = false;

  @override
  void close() => closed = true;
}

void main() {
  group('FileChangeFeed', () {
    testWidgets('a burst of notices becomes one batch after the delay', (tester) async {
      final feed = FileChangeFeed(delay: const Duration(milliseconds: 400));
      final batches = <FileChangeBatch>[];
      feed.addListener(batches.add);
      feed
        ..addFolders(<String>['a'])
        ..addFolders(<String>['b', 'a']);
      await tester.pump(const Duration(milliseconds: 399));
      expect(batches, isEmpty);
      await tester.pump(const Duration(milliseconds: 1));
      expect(batches, hasLength(1));
      expect(batches.single.folderIds, <String>{'a', 'b'});
      expect(batches.single.affects('a'), isTrue);
      expect(batches.single.affects('c'), isFalse);
      await tester.pump(const Duration(seconds: 2));
      expect(batches, hasLength(1));
    });

    testWidgets('a steady stream still refreshes once per delay', (tester) async {
      final feed = FileChangeFeed(delay: const Duration(milliseconds: 400));
      final batches = <FileChangeBatch>[];
      feed.addListener(batches.add);
      for (var i = 0; i < 10; i++) {
        feed.addFolders(<String>['f$i']);
        await tester.pump(const Duration(milliseconds: 100));
      }
      await tester.pump(const Duration(milliseconds: 400));
      expect(batches.length, inInclusiveRange(2, 4));
      expect(batches.expand((batch) => batch.folderIds).toSet(), <String>{for (var i = 0; i < 10; i++) 'f$i'});
    });

    testWidgets('a hold keeps batches back until it is released', (tester) async {
      final feed = FileChangeFeed(delay: const Duration(milliseconds: 400));
      final batches = <FileChangeBatch>[];
      feed.addListener(batches.add);
      final rename = Object();
      final drag = Object();
      feed
        ..hold(rename)
        ..hold(drag)
        ..addFolders(<String>['a']);
      await tester.pump(const Duration(seconds: 2));
      expect(batches, isEmpty);
      expect(feed.hasPending, isTrue);
      feed.release(rename);
      await tester.pump(const Duration(seconds: 2));
      expect(batches, isEmpty);
      feed.release(drag);
      await tester.pump(const Duration(milliseconds: 400));
      expect(batches.single.folderIds, <String>{'a'});
    });

    testWidgets('everything affects every folder; clear drops what waits', (tester) async {
      final feed = FileChangeFeed(delay: const Duration(milliseconds: 10));
      final batches = <FileChangeBatch>[];
      feed.addListener(batches.add);
      feed.addEverything();
      await tester.pump(const Duration(milliseconds: 10));
      expect(batches.single.affects('anything'), isTrue);
      feed
        ..addFolders(<String>['x'])
        ..clear();
      await tester.pump(const Duration(milliseconds: 50));
      expect(batches, hasLength(1));
    });
  });

  test('journal pages name the parent folders, or everything for moves', () {
    final edits = batchFromJournal(<dynamic>[
      <String, dynamic>{'op': 'create', 'node': <String, dynamic>{'parentId': 'p1'}},
      <String, dynamic>{'op': 'update', 'node': <String, dynamic>{'parentId': 'p2'}},
    ]);
    expect(edits.folderIds, <String>{'p1', 'p2'});
    expect(edits.everything, isFalse);
    expect(batchFromJournal(<dynamic>[<String, dynamic>{'op': 'move', 'node': <String, dynamic>{'parentId': 'p1'}}]).everything, isTrue);
    expect(batchFromJournal(<dynamic>[<String, dynamic>{'op': 'update', 'node': null}]).everything, isTrue);
  });

  group('socket.io frames', () {
    test('decodes open, ping, connect, events and refusals', () {
      expect(SocketIoFrame.decode('0{"sid":"x","pingInterval":25000}'), isA<SocketIoOpen>());
      expect(SocketIoFrame.decode('2'), isA<SocketIoPing>());
      expect(SocketIoFrame.decode('40{"sid":"y"}'), isA<SocketIoConnected>());
      expect(SocketIoFrame.decode('44{"message":"Unauthorized"}'), isA<SocketIoClosed>());
      final event = SocketIoFrame.decode('42["files:changed",{"folderIds":["a","b"]}]');
      expect(event, isA<SocketIoEvent>());
      expect((event as SocketIoEvent).name, 'files:changed');
      expect(SocketIoFrame.decode('42not json'), isA<SocketIoIgnored>());
    });

    test('the socket address sits next to the server', () {
      expect(socketIoUri(Uri.parse('https://cloud.example/')).toString(), 'wss://cloud.example/socket.io/?EIO=4&transport=websocket');
      expect(socketIoUri(Uri.parse('http://localhost:3344/')).toString(), 'ws://localhost:3344/socket.io/?EIO=4&transport=websocket');
    });
  });

  group('LiveFileUpdates', () {
    testWidgets('socket events feed the folders; a reconnect refreshes everything', (tester) async {
      final feed = FileChangeFeed(delay: const Duration(milliseconds: 10));
      final batches = <FileChangeBatch>[];
      feed.addListener(batches.add);
      final sockets = <_FakeSocket>[];
      final live = LiveFileUpdates(
        feed: feed,
        fetchChanges: (_) async => <String, dynamic>{'changes': <dynamic>[], 'cursor': 0, 'hasMore': false},
        openSocket: (url, handlers) {
          final socket = _FakeSocket(handlers);
          sockets.add(socket);
          return socket;
        },
        socketUrl: () => Uri.parse('ws://x/socket.io/'),
      );
      live.start();
      sockets.last.handlers.onConnected();
      sockets.last.handlers.onEvent('files:changed', <String, dynamic>{'folderIds': <String>['a']});
      await tester.pump(const Duration(milliseconds: 10));
      expect(batches.single.folderIds, <String>{'a'});

      sockets.last.handlers.onClosed();
      expect(live.connected, isFalse);
      await tester.pump(live.backoff(0));
      expect(sockets, hasLength(2));
      sockets.last.handlers.onConnected();
      await tester.pump(const Duration(milliseconds: 10));
      expect(batches.last.everything, isTrue);
      live.stop();
    });

    test('shares:changed is reported; other events are not', () {
      var reported = 0;
      final sockets = <_FakeSocket>[];
      final live = LiveFileUpdates(
        feed: FileChangeFeed(),
        fetchChanges: (_) async => const <String, dynamic>{},
        openSocket: (url, handlers) {
          final socket = _FakeSocket(handlers);
          sockets.add(socket);
          return socket;
        },
        socketUrl: () => Uri.parse('ws://x/socket.io/'),
        onSharesChanged: () => reported += 1,
      );
      live.start();
      sockets.last.handlers.onEvent('shares:changed', <String, dynamic>{});
      sockets.last.handlers.onEvent('something:else', <String, dynamic>{});
      expect(reported, 1);
      live.stop();
    });

    testWidgets('every poll is reported, so shared folders can refresh', (tester) async {
      var polls = 0;
      final live = LiveFileUpdates(
        feed: FileChangeFeed(),
        fetchChanges: (_) async => <String, dynamic>{'changes': <dynamic>[], 'cursor': 5, 'hasMore': false},
        onPolled: () => polls += 1,
      );
      live.start();
      await tester.pump();
      expect(polls, 1);
      await tester.pump(const Duration(seconds: 15));
      expect(polls, 2);
      live.stop();
    });

    test('backoff doubles up to the cap', () {
      final live = LiveFileUpdates(feed: FileChangeFeed(), fetchChanges: (_) async => const <String, dynamic>{});
      expect(<int>[for (var i = 0; i < 7; i++) live.backoff(i).inSeconds], <int>[1, 2, 4, 8, 16, 30, 30]);
    });

    testWidgets('without a socket the journal is polled from its head', (tester) async {
      final feed = FileChangeFeed(delay: const Duration(milliseconds: 10));
      final batches = <FileChangeBatch>[];
      feed.addListener(batches.add);
      final asked = <int?>[];
      var journal = <String, dynamic>{'changes': <dynamic>[], 'cursor': 41, 'hasMore': false};
      final live = LiveFileUpdates(
        feed: feed,
        fetchChanges: (cursor) async {
          asked.add(cursor);
          return journal;
        },
        pollInterval: const Duration(seconds: 15),
      );
      live.start();
      await tester.pump();
      expect(asked, <int?>[null]);
      await tester.pump(const Duration(seconds: 1));
      expect(batches, isEmpty);

      journal = <String, dynamic>{
        'changes': <dynamic>[<String, dynamic>{'op': 'create', 'node': <String, dynamic>{'parentId': 'p'}}],
        'cursor': 42,
        'hasMore': false,
      };
      await tester.pump(const Duration(seconds: 15));
      await tester.pump(const Duration(milliseconds: 10));
      expect(asked, <int?>[null, 41]);
      expect(batches.single.folderIds, <String>{'p'});

      live.setVisible(false);
      await tester.pump(const Duration(seconds: 15));
      expect(asked, hasLength(2));
      live.stop();
    });
  });
}
