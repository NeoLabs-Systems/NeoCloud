import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/public_signing_http.dart';
import 'package:neocloud/src/signing/signing.dart';

void main() {
  group('enums', () {
    test('parse wire names and fall back on unknown values', () {
      expect(SigningStatus.parse('in_progress'), SigningStatus.inProgress);
      expect(SigningStatus.parse('from_the_future'), SigningStatus.draft);
      expect(SigningStatus.parse(null), SigningStatus.draft);
      expect(SigningStatus.inProgress.wire, 'in_progress');
      expect(PartyStatus.parse('waiting'), PartyStatus.waiting);
      expect(PartyStatus.parse(7), PartyStatus.pending);
      expect(PublicSigningState.parse('???'), PublicSigningState.locked);
      expect(FieldType.parse('payment'), FieldType.text);
      expect(SigningOrder.parse('random'), SigningOrder.random);
      expect(SigningEventType.parse('link_regenerated'), SigningEventType.linkRegenerated);
      expect(SigningEventType.parse('something_new'), SigningEventType.other);
      expect(SignatureMethod.parse('typed'), SignatureMethod.typed);
      expect(SignatureMethod.parse('x'), isNull);
    });

    test('status helpers', () {
      expect(SigningStatus.completed.isFinished, isTrue);
      expect(SigningStatus.sent.isFinished, isFalse);
      expect(SigningStatus.sent.isEditable, isTrue);
      expect(SigningStatus.inProgress.isEditable, isFalse);
    });
  });

  group('SigningField', () {
    test('round-trips and keeps unknown prefs', () {
      final json = <String, dynamic>{
        'id': 'f1',
        'partyId': 'p1',
        'type': 'date',
        'name': 'Signed on',
        'required': false,
        'readonly': true,
        'defaultValue': 'today',
        'prefs': <String, dynamic>{'format': 'DD.MM.YYYY', 'futureKey': 3},
        'areas': <Map<String, dynamic>>[
          <String, dynamic>{'page': 1, 'x': 0.1, 'y': 0.2, 'w': 0.3, 'h': 0.04},
          <String, dynamic>{'page': 2, 'x': 0.1, 'y': 0.2, 'w': 0.3, 'h': 0.04, 'option': 'Yes'},
        ],
        'position': 4,
        'extra': 'ignored',
      };
      final field = SigningField.fromJson(json);
      expect(field.type, FieldType.date);
      expect(field.required, isFalse);
      expect(field.areas, hasLength(2));
      expect(field.areas.last.page, 2);
      expect(field.areas.last.option, 'Yes');
      expect(field.prefs.format, 'DD.MM.YYYY');
      final back = field.toJson();
      expect(back['id'], 'f1');
      expect((back['prefs']! as Map)['futureKey'], 3);
      expect((back['areas']! as List).first, <String, Object?>{'page': 1, 'x': 0.1, 'y': 0.2, 'w': 0.3, 'h': 0.04});
    });

    test('tolerates a nearly empty object', () {
      final field = SigningField.fromJson(const <String, dynamic>{});
      expect(field.type, FieldType.text);
      expect(field.required, isTrue);
      expect(field.areas, isEmpty);
      expect(field.prefs.isEmpty, isTrue);
    });

    test('a local field leaves its id out when saved', () {
      final field = SigningField.local(partyId: 'p', type: FieldType.text, areas: const <FieldArea>[FieldArea(page: 0, x: 0, y: 0, w: .1, h: .1)]);
      expect(field.isLocal, isTrue);
      expect(field.toJson().containsKey('id'), isFalse);
      expect(field.copyWith(id: 'srv1').toJson()['id'], 'srv1');
    });

    test('copyWith can clear the default value', () {
      final field = SigningField.fromJson(const <String, dynamic>{'id': 'a', 'defaultValue': 'x'});
      expect(field.copyWith(defaultValue: null).defaultValue, isNull);
      expect(field.copyWith(name: 'n').defaultValue, 'x');
    });
  });

  group('FieldPrefs', () {
    test('typed getters read numbers sent as text', () {
      final prefs = FieldPrefs.fromJson(<String, dynamic>{'min': '2', 'decimals': 1.0, 'options': <Object>['a', '', 3]});
      expect(prefs.min, 2);
      expect(prefs.decimals, 1);
      expect(prefs.options, <String>['a', '3']);
    });

    test('set returns a copy and null removes', () {
      final base = FieldPrefs(const <String, Object?>{'fontType': 'bold'});
      final next = base.set('fontSize', 12).set('fontType', null);
      expect(base.fontType, 'bold');
      expect(next.fontType, 'normal');
      expect(next.fontSize, 12);
      expect(base == FieldPrefs(const <String, Object?>{'fontType': 'bold'}), isTrue);
    });
  });

  group('documents and lists', () {
    test('detail parses document, parties and fields', () {
      final detail = SigningDetail.fromJson(<String, dynamic>{
        'document': <String, dynamic>{
          'id': 'd1',
          'title': 'Lease',
          'status': 'in_progress',
          'signingOrder': 'random',
          'expiresAt': '2026-12-01T10:00:00Z',
          'pageCount': 3,
        },
        'parties': <Map<String, dynamic>>[
          <String, dynamic>{'id': 'p1', 'name': 'Anna', 'status': 'completed', 'completedAt': 1700000000000, 'signUrl': 'https://x/app/sign/t'},
        ],
        'fields': <Map<String, dynamic>>[
          <String, dynamic>{'id': 'f1', 'partyId': 'p1', 'type': 'signature', 'areas': <Object>[]},
        ],
      });
      expect(detail.document.status, SigningStatus.inProgress);
      expect(detail.document.signingOrder, SigningOrder.random);
      expect(detail.document.expiresAt!.year, 2026);
      expect(detail.parties.single.completedAt, isNotNull);
      expect(detail.parties.single.signUrl, 'https://x/app/sign/t');
      expect(detail.fields.single.type, FieldType.signature);
    });

    test('list page and item counts', () {
      final page = SigningListPage.fromJson(<String, dynamic>{
        'items': <Map<String, dynamic>>[
          <String, dynamic>{
            'id': 'd',
            'title': 'T',
            'status': 'sent',
            'parties': <Map<String, dynamic>>[
              <String, dynamic>{'id': 'a', 'name': 'A', 'status': 'completed'},
              <String, dynamic>{'id': 'b', 'name': 'B', 'status': 'opened'},
            ],
          },
        ],
        'nextCursor': 'abc',
      });
      expect(page.nextCursor, 'abc');
      expect(page.items.single.completedParties, 1);
    });

    test('events and chain', () {
      final log = SigningEventLog.fromJson(<String, dynamic>{
        'events': <Map<String, dynamic>>[
          <String, dynamic>{'id': 'e', 'type': 'consented', 'at': '2026-01-01T00:00:00Z', 'ip': '1.2.3.4', 'hash': 'h'},
          <String, dynamic>{'id': 'f', 'type': 'brand_new'},
        ],
        'verify': <String, dynamic>{'chainValid': true},
      });
      expect(log.chainValid, isTrue);
      expect(log.events.first.type, SigningEventType.consented);
      expect(log.events.last.type, SigningEventType.other);
      expect(log.events.last.rawType, 'brand_new');
      expect(SigningEventLog.fromJson(const <String, dynamic>{}).chainValid, isNull);
    });

    test('data URLs decode', () {
      final bytes = Uint8List.fromList(<int>[1, 2, 3, 4]);
      final value = SigningValue.fromJson(<String, dynamic>{'fieldId': 'f', 'image': 'data:image/png;base64,${base64Encode(bytes)}'});
      expect(value.isFilled, isTrue);
      expect(value.inlineImageBytes, bytes);
      expect(decodeDataUrl('https://x/y.png'), isNull);
      expect(decodeDataUrl('data:image/png;base64,@@@'), isNull);
    });
  });

  group('PublicSigningView', () {
    final json = <String, dynamic>{
      'state': 'ready',
      'document': <String, dynamic>{'title': 'NDA', 'pageCount': 2, 'locale': 'de', 'consentText': 'I agree', 'consentVersion': 'v1'},
      'party': <String, dynamic>{'id': 'p2', 'name': 'Bob', 'color': '#3b82f6', 'status': 'opened'},
      'parties': <Map<String, dynamic>>[
        <String, dynamic>{'name': 'Anna', 'color': '#c9a227', 'status': 'completed'},
      ],
      'fields': <Map<String, dynamic>>[
        <String, dynamic>{'id': 'a', 'partyId': 'p2', 'type': 'text', 'areas': <Object>[]},
        <String, dynamic>{
          'id': 'b',
          'partyId': 'p1',
          'type': 'text',
          'areas': <Object>[],
          'value': 'Anna',
        },
      ],
      'myValues': <String, dynamic>{
        'a': 'hello',
        'c': <String>['x', 'y'],
        'd': <String, dynamic>{'kind': 'image', 'mime': 'image/png', 'method': 'draw'},
      },
      'turn': <String, dynamic>{'canSign': true},
    };

    test('splits own and other fields and reads values in either shape', () {
      final view = PublicSigningView.fromJson(json);
      expect(view.canSign, isTrue);
      expect(view.ownFields.map((f) => f.id), <String>['a']);
      expect(view.otherFields.single.value!.value, 'Anna');
      expect(view.valueOf('a')!.value, 'hello');
      expect(view.valueOf('c')!.value, <String>['x', 'y']);
      expect(view.valueOf('d')!.hasFile, isTrue);
      expect(view.valueOf('d')!.method, SignatureMethod.drawn);
      expect(view.document.locale, 'de');
    });

    test('myValues as a list also works', () {
      final view = PublicSigningView.fromJson(<String, dynamic>{
        ...json,
        'myValues': <Map<String, dynamic>>[
          <String, dynamic>{'fieldId': 'a', 'value': 'x'},
        ],
      });
      expect(view.valueOf('a')!.value, 'x');
    });

    test('is not signable unless ready and its turn', () {
      expect(PublicSigningView.fromJson(<String, dynamic>{...json, 'state': 'waiting'}).canSign, isFalse);
      expect(PublicSigningView.fromJson(<String, dynamic>{...json, 'turn': <String, dynamic>{}}).canSign, isFalse);
    });
  });

  group('SigningException', () {
    test('maps statuses to kinds', () {
      expect(SigningException.fromStatus(0).kind, SigningErrorKind.network);
      expect(SigningException.fromStatus(429, retryAfter: const Duration(seconds: 9)).retryAfter, const Duration(seconds: 9));
      expect(SigningException.fromStatus(429).kind, SigningErrorKind.rateLimited);
      expect(SigningException.fromStatus(404).kind, SigningErrorKind.notFound);
      expect(SigningException.fromStatus(410).kind, SigningErrorKind.expired);
      expect(SigningException.fromStatus(400, code: 'SIGN_EXPIRED').kind, SigningErrorKind.expired);
      expect(SigningException.fromStatus(409).kind, SigningErrorKind.conflict);
      expect(SigningException.fromStatus(422).kind, SigningErrorKind.invalid);
      expect(SigningException.fromStatus(401).kind, SigningErrorKind.unauthorized);
      expect(SigningException.fromStatus(500).kind, SigningErrorKind.server);
    });
  });

  group('HttpPublicSigningApi', () {
    late List<http.BaseRequest> seen;

    HttpPublicSigningApi api(Future<http.Response> Function(http.Request) handler) {
      seen = <http.BaseRequest>[];
      return HttpPublicSigningApi(
        token: 'tok/en',
        resolve: (path) => Uri.parse('https://cloud.test$path'),
        client: MockClient((request) {
          seen.add(request);
          return handler(request);
        }),
      );
    }

    http.Response json(Object body, [int status = 200]) =>
        http.Response(jsonEncode(body), status, headers: <String, String>{'content-type': 'application/json'});

    test('sends no credentials and encodes the token', () async {
      final client = api((_) async => json(<String, dynamic>{'state': 'ready', 'party': <String, dynamic>{'id': 'p'}}));
      final view = await client.view();
      expect(view.state, PublicSigningState.ready);
      expect(seen.single.url.toString(), 'https://cloud.test/api/public/sign/tok%2Fen');
      expect(seen.single.headers.containsKey('Authorization'), isFalse);
      expect(seen.single.headers.containsKey('Cookie'), isFalse);
    });

    test('saves a value, completes, declines', () async {
      final client = api((request) async {
        if (request.url.path.endsWith('/complete')) return json(<String, dynamic>{'state': 'completed'});
        return json(<String, dynamic>{});
      });
      await client.saveValue('f 1', 'hi');
      final put = seen.last as http.Request;
      expect(put.method, 'PUT');
      expect(put.url.path, '/api/public/sign/tok%2Fen/fields/f%201');
      expect(jsonDecode(put.body), <String, dynamic>{'value': 'hi'});
      expect(await client.complete(), PublicSigningState.completed);
      await client.decline(reason: '  no  ');
      expect(jsonDecode((seen.last as http.Request).body), <String, dynamic>{'reason': 'no'});
    });

    test('uploads the raw bytes, the method in the query', () async {
      final client = api((_) async => json(<String, dynamic>{}));
      await client.uploadValue('f', bytes: Uint8List.fromList(<int>[137, 80]), fileName: 's.png', method: SignatureMethod.typed, typedText: 'Ann');
      final request = seen.single as http.Request;
      expect(request.url.path, endsWith('/fields/f/upload'));
      expect(request.url.queryParameters, <String, String>{'method': 'type', 'text': 'Ann'});
      expect(request.headers['content-type'], startsWith('image/png'));
      expect(request.bodyBytes, <int>[137, 80]);
      expect(request.headers.containsKey('Authorization'), isFalse);
    });

    test('fetches PDF bytes', () async {
      final client = api((_) async => http.Response.bytes(<int>[37, 80, 68, 70], 200));
      expect(await client.pdf(), <int>[37, 80, 68, 70]);
    });

    test('maps failures to typed errors', () async {
      Future<SigningException> failing(int status, [Map<String, Object?> body = const <String, Object?>{}]) async {
        final client = api((_) async => json(body, status));
        try {
          await client.view();
        } on SigningException catch (error) {
          return error;
        }
        throw StateError('did not throw');
      }

      expect((await failing(404)).kind, SigningErrorKind.notFound);
      final limited = await failing(429, <String, Object?>{'retryAfter': 12});
      expect(limited.kind, SigningErrorKind.rateLimited);
      expect(limited.retryAfter, const Duration(seconds: 12));
      expect((await failing(410, <String, Object?>{'code': 'LINK_EXPIRED'})).kind, SigningErrorKind.expired);
      final offline = api((_) async => throw http.ClientException('down'));
      await expectLater(offline.view(), throwsA(isA<SigningException>().having((e) => e.kind, 'kind', SigningErrorKind.network)));
    });
  });

  group('FakeSigningApi', () {
    test('create, edit, send, sign', () async {
      final api = FakeSigningApi();
      final created = await api.createDocument(nodeId: 'n1', title: 'Deal');
      final id = created.document.id;
      final parties = await api.saveParties(id, const <PartyDraft>[PartyDraft(name: 'A'), PartyDraft(name: 'B')]);
      final fields = await api.saveFields(id, <SigningField>[
        SigningField.local(partyId: parties.first.id, type: FieldType.signature, areas: const <FieldArea>[FieldArea(page: 0, x: .1, y: .1, w: .2, h: .06)]),
      ]);
      expect(fields.single.isLocal, isFalse);
      final sent = await api.send(id);
      expect(sent.links, hasLength(2));
      expect(sent.document.status, SigningStatus.sent);
      expect((await api.get(id)).parties.last.status, PartyStatus.waiting);
      await expectLater(api.saveFields(id, fields), completes);
      api.setPartyStatus(id, parties.first.id, PartyStatus.opened);
      await expectLater(api.update(id, const SigningSettingsPatch(title: 'x')), throwsA(isA<SigningException>()));
      expect((await api.list()).items.single.id, id);
      expect(buildSamplePdf(), isNotEmpty);
    });

    test('failNext throws once', () async {
      final api = FakeSigningApi()..failNext = const SigningException(SigningErrorKind.rateLimited, statusCode: 429);
      await expectLater(api.list(), throwsA(isA<SigningException>()));
      expect((await api.list()).items, isEmpty);
    });
  });
}
