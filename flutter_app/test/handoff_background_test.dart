import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/handoff/background/handoff_background.dart';
import 'package:neocloud/src/handoff/background/handoff_background_platform.dart';
import 'package:neocloud/src/handoff/handoff_controller.dart';
import 'package:neocloud/src/handoff/handoff_models.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'helpers/fake_http.dart';

/// The Android side, recorded.
class FakePlatform implements HandoffBackgroundPlatform {
  FakePlatform({this.supported = true, this.serviceStarts = true});

  @override
  bool supported;
  bool serviceStarts;
  bool wipe = false;
  final List<String> calls = <String>[];
  Map<String, String>? texts;
  String? server;
  String? token;

  @override
  Future<bool> configure({
    required String server,
    required String token,
    required Map<String, String> texts,
  }) async {
    calls.add('configure');
    this.server = server;
    this.token = token;
    this.texts = texts;
    return serviceStarts;
  }

  @override
  Future<void> disable() async => calls.add('disable');

  @override
  Future<HandoffBackgroundStatus> status() async =>
      const HandoffBackgroundStatus();

  @override
  Future<bool> takeWipeRequest() async {
    final requested = wipe;
    wipe = false;
    return requested;
  }

  @override
  Future<void> dismissOffer(String offerId) async =>
      calls.add('dismiss $offerId');
}

Future<SharedPreferences> _prefs([
  Map<String, Object> values = const <String, Object>{},
]) async {
  SharedPreferences.setMockInitialValues(values);
  return SharedPreferences.getInstance();
}

BackendClient _client({
  String? token = 'nc_token',
  String url = 'https://cloud.example/',
}) => BackendClient(baseUrl: url, http: FakeHttp())..bearerToken = token;

Map<String, dynamic> _offer(String id, {String status = 'pending'}) =>
    <String, dynamic>{
      'id': id,
      'role': 'receiver',
      'status': status,
      'from': <String, dynamic>{
        'id': 'd:1',
        'name': 'Anna’s laptop',
        'platform': 'macos',
      },
      'to': <String, dynamic>{'id': 'd:2', 'name': 'Phone'},
      'totalSize': 3,
      'files': <Map<String, dynamic>>[
        <String, dynamic>{
          'id': 1,
          'name': 'a.txt',
          'mime': 'text/plain',
          'size': 3,
        },
      ],
    };

void main() {
  group('HandoffBackground', () {
    test(
      'is on only where the platform supports it, however the choice was stored',
      () async {
        final prefs = await _prefs(<String, Object>{
          HandoffBackground.enabledKey: true,
        });
        final unsupported = HandoffBackground(
          client: _client(),
          preferences: prefs,
          platform: FakePlatform(supported: false),
        );
        expect(unsupported.enabled, isFalse);
        await unsupported.sync();
        expect(unsupported.serviceRunning, isFalse);
        final supported = HandoffBackground(
          client: _client(),
          preferences: prefs,
          platform: FakePlatform(),
        );
        expect(supported.enabled, isTrue);
      },
    );

    test(
      'configures the native receiver with the server, token and texts',
      () async {
        final platform = FakePlatform();
        final background = HandoffBackground(
          client: _client(),
          preferences: await _prefs(),
          platform: platform,
          texts: () => <String, String>{'accept': 'Accept'},
        );
        await background.setEnabled(true);
        expect(platform.calls, <String>['configure']);
        expect(platform.server, 'https://cloud.example');
        expect(platform.token, 'nc_token');
        expect(platform.texts, <String, String>{'accept': 'Accept'});
        expect(background.serviceRunning, isTrue);
        expect(background.active, isTrue);
      },
    );

    test(
      'reports a service Android refused to start, the check carries on',
      () async {
        final background = HandoffBackground(
          client: _client(),
          preferences: await _prefs(<String, Object>{
            HandoffBackground.enabledKey: true,
          }),
          platform: FakePlatform(serviceStarts: false),
        );
        await background.sync();
        expect(background.active, isTrue);
        expect(background.serviceRunning, isFalse);
      },
    );

    test(
      'stops and forgets the token when switched off, not receiving or signed out',
      () async {
        final platform = FakePlatform();
        var receiving = true;
        final client = _client();
        final background = HandoffBackground(
          client: client,
          preferences: await _prefs(<String, Object>{
            HandoffBackground.enabledKey: true,
          }),
          platform: platform,
          receiving: () => receiving,
        );
        await background.sync();
        expect(platform.calls.last, 'configure');
        receiving = false;
        await background.sync();
        expect(platform.calls.last, 'disable');
        expect(background.serviceRunning, isFalse);
        receiving = true;
        client.bearerToken = null;
        await background.sync();
        expect(platform.calls.last, 'disable');
        client.bearerToken = 'nc_token';
        await background.setEnabled(false);
        expect(platform.calls.last, 'disable');
        expect(background.enabled, isFalse);
      },
    );

    test(
      'signing out stops it but keeps the choice for the next sign-in',
      () async {
        final platform = FakePlatform();
        final background = HandoffBackground(
          client: _client(),
          preferences: await _prefs(<String, Object>{
            HandoffBackground.enabledKey: true,
          }),
          platform: platform,
        );
        await background.sync();
        await background.suspend();
        expect(platform.calls, <String>['configure', 'disable']);
        expect(background.enabled, isTrue);
      },
    );

    test(
      'the one-time hint is due once, only where it can be turned on',
      () async {
        final prefs = await _prefs();
        final background = HandoffBackground(
          client: _client(),
          preferences: prefs,
          platform: FakePlatform(),
        );
        expect(background.hintDue, isTrue);
        await background.markHintShown();
        expect(background.hintDue, isFalse);
        expect(
          HandoffBackground(
            client: _client(),
            preferences: prefs,
            platform: FakePlatform(),
          ).hintShown,
          isTrue,
        );

        final other = await _prefs();
        expect(
          HandoffBackground(
            client: _client(),
            preferences: other,
            platform: FakePlatform(supported: false),
          ).hintDue,
          isFalse,
        );
        expect(
          HandoffBackground(
            client: _client(),
            preferences: other,
            platform: FakePlatform(),
            receiving: () => false,
          ).hintDue,
          isFalse,
        );
        final on = await _prefs(<String, Object>{
          HandoffBackground.enabledKey: true,
        });
        expect(
          HandoffBackground(
            client: _client(),
            preferences: on,
            platform: FakePlatform(),
          ).hintDue,
          isFalse,
        );
      },
    );

    test(
      'a wipe request the background receiver kept is handed over once',
      () async {
        final platform = FakePlatform()..wipe = true;
        final background = HandoffBackground(
          client: _client(),
          preferences: await _prefs(),
          platform: platform,
        );
        expect(await background.takeWipeRequest(), isTrue);
        expect(await background.takeWipeRequest(), isFalse);
        expect(
          await HandoffBackground(
            client: _client(),
            preferences: await _prefs(),
            platform: FakePlatform(supported: false),
          ).takeWipeRequest(),
          isFalse,
        );
      },
    );

    test('the dialog tells the native side to drop the notification', () async {
      final platform = FakePlatform();
      HandoffBackground(
        client: _client(),
        preferences: await _prefs(),
        platform: platform,
      ).dismissOffer('abc');
      await Future<void>.delayed(Duration.zero);
      expect(platform.calls, <String>['dismiss abc']);
    });
  });

  group('the controller and the background receiver', () {
    Future<(HandoffController, FakePlatform, FakeHttp)> build({
      Map<String, Object> values = const <String, Object>{},
      FakeHandler? handler,
    }) async {
      final prefs = await _prefs(values);
      final http = FakeHttp(handler);
      final client = BackendClient(baseUrl: 'https://cloud.example', http: http)
        ..bearerToken = 'nc_token';
      final platform = FakePlatform();
      final handoff = HandoffController(
        client,
        preferences: prefs,
        streamFactory: () => null,
        background: HandoffBackground(
          client: client,
          preferences: prefs,
          platform: platform,
        ),
      );
      return (handoff, platform, http);
    }

    test('a session starting configures it, signing out stops it', () async {
      final (handoff, platform, _) = await build(
        values: <String, Object>{HandoffBackground.enabledKey: true},
      );
      handoff.start();
      await Future<void>.delayed(Duration.zero);
      expect(platform.calls, contains('configure'));
      handoff.stop();
      await Future<void>.delayed(Duration.zero);
      expect(platform.calls.last, 'disable');
    });

    test(
      '"Let my other devices send files here" is remembered and gates the background receiver',
      () async {
        final (handoff, platform, _) = await build(
          values: <String, Object>{HandoffBackground.enabledKey: true},
        );
        expect(handoff.receiving, isTrue);
        handoff.setReceiving(false);
        await Future<void>.delayed(Duration.zero);
        expect(platform.calls.last, 'disable');

        final prefs = await SharedPreferences.getInstance();
        final client = BackendClient(
          baseUrl: 'https://cloud.example',
          http: FakeHttp(),
        );
        expect(
          HandoffController(
            client,
            preferences: prefs,
            streamFactory: () => null,
          ).receiving,
          isFalse,
        );

        handoff.setReceiving(true);
        await Future<void>.delayed(Duration.zero);
        expect(platform.calls.last, 'configure');
      },
    );

    test('an offer a notification accepted leaves the waiting list', () async {
      final (handoff, _, _) = await build();
      handoff.handleLive('handoff:offer', _offer('x'));
      handoff.handleLive('handoff:offer', _offer('y'));
      handoff.handleLive('handoff:updated', <String, dynamic>{
        'offer': _offer('x', status: 'accepted'),
      });
      expect(handoff.incoming.map((offer) => offer.id), <String>['y']);
    });

    test('coming back, offers answered meanwhile are not asked again', () async {
      var inbox = <Map<String, dynamic>>[_offer('x'), _offer('y')];
      final (handoff, _, _) = await build(
        handler: (call) => call.path == '/api/handoff/offers'
            ? FakeReply(200, <String, dynamic>{
                'sent': <Object>[],
                'inbox': inbox,
              })
            : const FakeReply(404),
      );
      await handoff.refresh();
      expect(handoff.incoming.map((offer) => offer.id), <String>['x', 'y']);
      // x was accepted from its notification while the app was in the background.
      inbox = <Map<String, dynamic>>[
        _offer('x', status: 'delivered'),
        _offer('y'),
      ];
      await handoff.refresh();
      expect(handoff.incoming.map((offer) => offer.id), <String>['y']);
      expect(handoff.incoming.single.status, HandoffStatus.pending);
    });
  });
}
