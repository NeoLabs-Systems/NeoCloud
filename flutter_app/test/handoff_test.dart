import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/handoff/handoff_controller.dart';
import 'package:neocloud/src/handoff/handoff_models.dart';
import 'package:neocloud/src/handoff/handoff_stream.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'helpers/fake_http.dart';

Map<String, dynamic> offerJson(
  String id, {
  String role = 'receiver',
  String status = 'pending',
}) => <String, dynamic>{
  'id': id,
  'role': role,
  'status': status,
  'from': <String, dynamic>{
    'id': 'd:1',
    'name': 'Anna’s iPhone',
    'platform': 'ios',
  },
  'to': <String, dynamic>{'id': 'd:2', 'name': 'Office PC'},
  'totalSize': 12,
  'files': <Map<String, dynamic>>[
    <String, dynamic>{
      'id': 7,
      'name': 'a.txt',
      'mime': 'text/plain',
      'size': 12,
    },
  ],
};

HandoffController controllerWith(FakeHttp http) {
  return HandoffController(
    BackendClient(baseUrl: 'http://localhost:1', http: http),
    streamFactory: () => null,
  );
}

void main() {
  test(
    'devices only count as targets when online, receiving and not this one',
    () {
      HandoffDevice device(Map<String, dynamic> extra) =>
          HandoffDevice.fromJson(<String, dynamic>{
            'id': 'd:9',
            'name': 'Laptop',
            'platform': 'macos',
            'kind': 'desktop',
            'canReceive': true,
            'online': true,
            'current': false,
            ...extra,
          });
      expect(device(const <String, dynamic>{}).isTarget, isTrue);
      expect(
        device(const <String, dynamic>{'online': false}).isTarget,
        isFalse,
      );
      expect(
        device(const <String, dynamic>{'canReceive': false}).isTarget,
        isFalse,
      );
      expect(
        device(const <String, dynamic>{'current': true}).isTarget,
        isFalse,
      );
    },
  );

  test('offers parse for both sides, and unknown states count as finished', () {
    final received = HandoffOffer.fromJson(offerJson('a'));
    expect(received.receiving, isTrue);
    expect(received.fromName, 'Anna’s iPhone');
    expect(received.files.single.name, 'a.txt');
    expect(received.finished, isFalse);
    expect(
      HandoffOffer.fromJson(
        offerJson('b', role: 'sender', status: 'delivered'),
      ).finished,
      isTrue,
    );
    expect(
      HandoffOffer.fromJson(offerJson('c', status: 'from-the-future')).finished,
      isTrue,
    );
  });

  test('the same offer pushed again is announced once', () {
    final handoff = controllerWith(FakeHttp());
    final announced = <String>[];
    handoff.onIncoming = (offer) => announced.add(offer.id);
    handoff.handleLive('handoff:offer', offerJson('x'));
    handoff.handleLive('handoff:offer', offerJson('x'));
    handoff.handleLive('handoff:offer', offerJson('y'));
    expect(announced, <String>['x', 'y']);
    expect(handoff.incoming.map((offer) => offer.id), <String>['x', 'y']);
  });

  test('an offer is dropped once it is no longer waiting', () {
    final handoff = controllerWith(FakeHttp());
    handoff.handleLive('handoff:offer', offerJson('x'));
    handoff.handleLive('handoff:updated', <String, dynamic>{
      'offer': offerJson('x', status: 'cancelled'),
    });
    expect(handoff.incoming, isEmpty);
  });

  test('a device that does not receive ignores offers', () {
    final handoff = controllerWith(FakeHttp())..receiving = false;
    handoff.handleLive('handoff:offer', offerJson('x'));
    expect(handoff.incoming, isEmpty);
  });

  test(
    'sent offers follow the server, newest first, and carry JSON given as text',
    () {
      final handoff = controllerWith(FakeHttp());
      handoff.handleLive('handoff:updated', <String, dynamic>{
        'offer': offerJson('s1', role: 'sender', status: 'pending'),
      });
      handoff.handleLive(
        'handoff:updated',
        jsonEncode(<String, dynamic>{'offer': offerJson('s2', role: 'sender')}),
      );
      handoff.handleLive('handoff:updated', <String, dynamic>{
        'offer': offerJson('s1', role: 'sender', status: 'delivered'),
      });
      expect(
        handoff.sent.map((offer) => '${offer.id}:${offer.status.name}'),
        <String>['s1:delivered', 's2:pending'],
      );
    },
  );

  test('devices are fetched again when the server says they changed', () async {
    final http = FakeHttp((call) {
      if (call.path == '/api/handoff/devices') {
        return FakeReply(200, <String, dynamic>{
          'devices': <Map<String, dynamic>>[
            <String, dynamic>{
              'id': 'd:2',
              'name': 'Office PC',
              'platform': 'windows',
              'kind': 'desktop',
              'canReceive': true,
              'online': true,
              'current': false,
            },
            <String, dynamic>{
              'id': 'd:3',
              'name': 'Old tablet',
              'platform': 'android',
              'kind': 'phone',
              'canReceive': true,
              'online': false,
              'current': false,
            },
          ],
        });
      }
      return const FakeReply(404);
    });
    final handoff = controllerWith(http);
    handoff.handleLive('handoff:devices', const <String, dynamic>{});
    await Future<void>.delayed(Duration.zero);
    expect(handoff.targets.map((device) => device.name), <String>['Office PC']);
  });

  test(
    'sending files to a chosen device creates the offer with nodes and cancels nothing on success',
    () async {
      final http = FakeHttp((call) {
        if (call.method == 'POST' && call.path == '/api/handoff/offers') {
          return FakeReply(201, offerJson('n', role: 'sender'));
        }
        return const FakeReply(404);
      });
      final handoff = controllerWith(http);
      final target = HandoffDevice.fromJson(const <String, dynamic>{
        'id': 'd:2',
        'name': 'Office PC',
        'online': true,
      });
      final offer = await handoff.send(target, nodeIds: <String>['node-1']);
      expect(offer.id, 'n');
      final call = http.where('POST', '/api/handoff/offers').single;
      expect(call.body['targetId'], 'd:2');
      expect(call.body['nodeIds'], <String>['node-1']);
      expect(call.body.containsKey('files'), isFalse);
      expect(http.where('DELETE', '/api/handoff/offers/n'), isEmpty);
    },
  );

  test(
    'browser notifications stay off until the person turns them on',
    () async {
      SharedPreferences.setMockInitialValues(const <String, Object>{});
      final handoff = HandoffController(
        BackendClient(baseUrl: 'http://localhost:1', http: FakeHttp()),
        preferences: await SharedPreferences.getInstance(),
        streamFactory: () => null,
      );
      expect(handoff.notifyInBrowser, isFalse);
      await handoff.setNotifyInBrowser(true);
      expect(handoff.notifyInBrowser, isTrue);
    },
  );

  test('the event stream parser skips heartbeats and incomplete events', () {
    final events = <String>[];
    final parser = SseParser((event, data) => events.add('$event=$data'));
    for (final line in <String>[
      'retry: 3000',
      ': connected',
      '',
      'event: handoff:offer',
      'data: {"id":"a"}',
      '',
      ': ping',
      '',
      'event: handoff:devices',
      'data: {}',
      '',
      'event: lonely',
      '',
      'data: {"x":1}',
      '',
    ]) {
      parser.addLine(line);
    }
    expect(events, <String>['handoff:offer={"id":"a"}', 'handoff:devices={}']);
  });
}
