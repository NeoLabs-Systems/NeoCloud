import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'helpers/public_signing_harness.dart';

PublicSigningController flowFor(FakePublicSigningApi api) => PublicSigningController(
  api: api,
  saveDelay: const Duration(milliseconds: 5),
  retryBase: const Duration(milliseconds: 10),
  retryMax: const Duration(milliseconds: 40),
  pollInterval: const Duration(milliseconds: 30),
  resultPollInterval: const Duration(milliseconds: 30),
  now: () => DateTime(2026, 3, 7),
);

Future<void> pause([int ms = 40]) => Future<void>.delayed(Duration(milliseconds: ms));

void main() {
  group('the server\'s JSON', () {
    test('is read the way the public API sends it', () {
      final view = PublicSigningView.fromJson(<String, dynamic>{
        'state': 'ready',
        'document': <String, dynamic>{
          'title': 'Lease',
          'pageCount': 2,
          'pages': <Object>[<String, dynamic>{'width': 595, 'height': 842}],
          'locale': 'de',
          'expiresAt': '2026-12-01T10:00:00.000Z',
          'consentText': 'I agree',
          'consentVersion': 1,
        },
        'party': <String, dynamic>{'id': 'p1', 'name': 'Tenant', 'color': '#D4A017', 'status': 'opened', 'consentedAt': '2026-01-01T00:00:00.000Z', 'signerName': 'Ada'},
        'parties': <Object>[<String, dynamic>{'name': 'Tenant', 'color': '#D4A017', 'status': 'opened'}],
        'fields': <Object>[
          <String, dynamic>{'id': 'f1', 'partyId': 'p1', 'type': 'signature', 'name': 'Sign', 'required': true, 'readonly': false, 'defaultValue': null, 'prefs': <String, dynamic>{}, 'areas': <Object>[<String, dynamic>{'page': 0, 'x': 0.1, 'y': 0.2, 'w': 0.3, 'h': 0.05}], 'own': true},
          <String, dynamic>{'id': 'f2', 'partyId': 'other', 'type': 'text', 'name': '', 'required': true, 'readonly': true, 'prefs': <String, dynamic>{}, 'areas': <Object>[], 'own': false, 'value': 'Filled by the landlord'},
          <String, dynamic>{'id': 'f3', 'partyId': 'other', 'type': 'signature', 'name': '', 'required': true, 'readonly': true, 'prefs': <String, dynamic>{}, 'areas': <Object>[], 'own': false, 'value': <String, dynamic>{'kind': 'image', 'mime': 'image/png', 'method': 'type', 'text': 'Bob'}},
        ],
        'myValues': <String, dynamic>{
          'f1': <String, dynamic>{'kind': 'image', 'mime': 'image/png', 'width': 10, 'height': 5, 'method': 'draw'},
          'f9': <Object>['a', 'b'],
          'f8': true,
        },
        'turn': <String, dynamic>{'canSign': true},
      });
      expect(view.state, PublicSigningState.ready);
      expect(view.party.consentedAt, isNotNull);
      expect(view.party.signerName, 'Ada');
      expect(view.document.consentVersion, '1');
      final signature = view.valueOf('f1')!;
      expect(signature.hasFile, isTrue);
      expect(signature.isFilled, isTrue);
      expect(signature.method, SignatureMethod.drawn);
      expect(view.valueOf('f9')!.value, <String>['a', 'b']);
      expect(view.valueOf('f8')!.value, true);
      expect(view.otherFields[0].value!.value, 'Filled by the landlord');
      expect(view.otherFields[1].value!.hasFile, isTrue);
      expect(view.otherFields[1].value!.method, SignatureMethod.typed);
    });

    test('the waiting turn counts who is ahead and never names them', () {
      final view = PublicSigningView.fromJson(<String, dynamic>{
        'state': 'waiting',
        'document': <String, dynamic>{'title': 'x'},
        'party': <String, dynamic>{'id': 'p', 'name': 'B'},
        'turn': <String, dynamic>{'canSign': false, 'waitingCount': 2, 'waitingFor': <Object>['Anna', 'Ben']},
        'progress': <String, dynamic>{'done': 1, 'total': 3},
        'parties': <Object>[
          <String, dynamic>{'name': 'Anna'},
        ],
      });
      expect(view.turn.waitingCount, 2);
      expect(view.progress.done, 1);
      expect(view.progress.total, 3);
      expect(view.progress.everyoneDone, isFalse);
      expect(view.canSign, isFalse);
    });

    test('closed states carry little and still parse', () {
      final view = PublicSigningView.fromJson(<String, dynamic>{'state': 'expired', 'document': <String, dynamic>{'locale': 'de'}, 'party': <String, dynamic>{'id': 'p', 'name': 'B', 'color': '#fff', 'status': 'pending'}});
      expect(view.state, PublicSigningState.expired);
      expect(view.document.locale, 'de');
      expect(view.document.title, isEmpty);
    });

    test('signature methods use the server\'s words', () {
      expect(SignatureMethod.drawn.wire, 'draw');
      expect(SignatureMethod.typed.wire, 'type');
      expect(SignatureMethod.uploaded.wire, 'upload');
      expect(SignatureMethod.parse('upload'), SignatureMethod.uploaded);
    });
  });

  group('PublicSigningController', () {
    test('opens at the first open field, or at the review when nothing is open', () async {
      final fields = <SigningField>[field('a', FieldType.text, y: 0.1), field('b', FieldType.text, y: 0.2)];
      var flow = flowFor(FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: fields, myValues: const <SigningValue>[SigningValue(fieldId: 'a', value: 'x')])));
      await flow.start();
      expect(flow.step, SigningStep.fill);
      expect(flow.active!.id, 'b');
      flow.dispose();

      flow = flowFor(FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: fields, myValues: const <SigningValue>[SigningValue(fieldId: 'a', value: 'x'), SigningValue(fieldId: 'b', value: 'y')])));
      await flow.start();
      expect(flow.step, SigningStep.review);
      flow.dispose();

      flow = flowFor(FakePublicSigningApi(view: viewOf(fields: fields)));
      await flow.start();
      expect(flow.step, SigningStep.intro);
      flow.dispose();
    });

    test('fields come in reading order across pages and lines', () async {
      final flow = flowFor(
        FakePublicSigningApi(
          view: viewOf(
            consentedAt: consented,
            fields: <SigningField>[
              field('p2', FieldType.text, page: 1, y: 0.1),
              field('right', FieldType.text, x: 0.6, y: 0.2),
              field('left', FieldType.text, x: 0.1, y: 0.205),
              field('top', FieldType.text, y: 0.05),
            ],
          ),
        ),
      );
      await flow.start();
      expect(flow.fields.map((f) => f.id), <String>['top', 'left', 'right', 'p2']);
      flow.dispose();
    });

    test('headings and locked fields are not asked for', () async {
      final flow = flowFor(
        FakePublicSigningApi(
          view: viewOf(
            consentedAt: consented,
            fields: <SigningField>[
              field('h', FieldType.heading, defaultValue: 'Title', readonly: true),
              field('l', FieldType.text, defaultValue: 'Fixed', readonly: true),
              field('t', FieldType.text),
            ],
          ),
        ),
      );
      await flow.start();
      expect(flow.fields.map((f) => f.id), <String>['t']);
      expect(flow.fixedFields, hasLength(2));
      flow.dispose();
    });

    test('quick typing saves once, with the last value', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      final f = flow.fields.single;
      flow.setValue(f, 'A');
      flow.setValue(f, 'Ad');
      flow.setValue(f, 'Ada');
      await pause();
      expect(api.calls.where((c) => c == 'save:t'), hasLength(1));
      expect((await api.view()).valueOf('t')!.value, 'Ada');
      expect(flow.hasUnsaved, isFalse);
      flow.dispose();
    });

    test('a half-typed phone number is kept but not sent', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('p', FieldType.phone)]));
      final flow = flowFor(api);
      await flow.start();
      final f = flow.fields.single;
      flow.setValue(f, '+49 15');
      await pause();
      expect(api.calls.where((c) => c.startsWith('save:')), isEmpty);
      expect(flow.valueOf('p'), '+4915');
      expect(flow.issueFor(f), isNotNull);
      flow.setValue(f, '+49 151 2345678');
      await pause();
      expect(api.calls, contains('save:p'));
      flow.dispose();
    });

    test('clearing a field clears it on the server', () async {
      final api = FakePublicSigningApi(
        view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, required: false)], myValues: const <SigningValue>[SigningValue(fieldId: 't', value: 'x')]),
      );
      final flow = flowFor(api);
      await flow.start();
      flow.setValue(flow.fields.single, '');
      await pause();
      expect((await api.view()).valueOf('t'), isNull);
      flow.dispose();
    });

    test('a failing connection retries with growing pauses and loses nothing', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text), field('u', FieldType.text, y: 0.3)]));
      final flow = flowFor(api);
      await flow.start();
      api.failTimes(3, const SigningException(SigningErrorKind.network));
      flow.setValue(flow.fields[0], 'one');
      flow.setValue(flow.fields[1], 'two');
      await pause(15);
      expect(flow.saveState, SaveState.offline);
      expect(flow.isOffline, isTrue);
      expect(flow.hasUnsaved, isTrue);
      expect(await flow.flush(), isFalse, reason: 'still failing: the second attempt');
      await pause(200);
      expect(flow.hasUnsaved, isFalse);
      expect(flow.saveState, SaveState.idle);
      final saved = await api.view();
      expect(saved.valueOf('t')!.value, 'one');
      expect(saved.valueOf('u')!.value, 'two');
      flow.dispose();
    });

    test('an upload survives a failure and is sent again', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('s', FieldType.signature)]));
      final flow = flowFor(api);
      await flow.start();
      api.failTimes(1, const SigningException(SigningErrorKind.server, statusCode: 503));
      flow.setUpload(flow.fields.single, bytes: tinyPng, fileName: 'signature.png', method: SignatureMethod.typed, typedText: 'Ada');
      expect(flow.valueOf('s'), isA<UploadedValue>());
      await pause(120);
      expect(api.calls, contains('upload:s'));
      expect(api.uploadMethods['s'], SignatureMethod.typed);
      expect(flow.hasUnsaved, isFalse);
      flow.dispose();
    });

    test('a refused value is reported and dropped from the queue', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      api.failNext = const SigningException(SigningErrorKind.invalid, statusCode: 400, message: 'Nope.');
      flow.setValue(flow.fields.single, 'x');
      await pause();
      expect(flow.fieldErrors['t'], 'Nope.');
      expect(flow.hasUnsaved, isFalse);
      flow.dispose();
    });

    test('when the document moved on (409), the page reloads its state', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      api.serverMoves(viewOf(state: PublicSigningState.canceled));
      api.failNext = const SigningException(SigningErrorKind.conflict, statusCode: 409);
      flow.setValue(flow.fields.single, 'x');
      await pause(60);
      expect(flow.state, PublicSigningState.canceled);
      flow.dispose();
    });

    test('Next refuses an empty required field; Skip is for optional ones', () async {
      final flow = flowFor(
        FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('a', FieldType.text), field('b', FieldType.text, y: 0.2, required: false)])),
      );
      await flow.start();
      expect(flow.canSkip, isFalse);
      expect(flow.next(), isFalse);
      expect(flow.visibleIssue(flow.active!), isNotNull);
      flow.setValue(flow.active!, 'x');
      expect(flow.next(), isTrue);
      expect(flow.canSkip, isTrue);
      expect(flow.next(), isTrue);
      expect(flow.step, SigningStep.review);
      flow.back();
      expect(flow.step, SigningStep.fill);
      expect(flow.activeIndex, 1);
      flow.dispose();
    });

    test('a date that starts as today is filled in and saved', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('d', FieldType.date, prefs: const <String, Object?>{'autoToday': true})]));
      final flow = flowFor(api);
      await flow.start();
      await pause();
      expect(flow.valueOf('d'), '2026-03-07');
      expect(api.calls, contains('save:d'));
      flow.dispose();
    });

    test('finish: sends once, learns the server\'s verdict, reads the result', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]), finalBytes: Uint8List.fromList(<int>[1, 2, 3]));
      final flow = flowFor(api);
      await flow.start();
      flow.setValue(flow.fields.single, 'x');
      final first = flow.finish();
      final second = flow.finish();
      await Future.wait(<Future<void>>[first, second]);
      expect(api.calls.where((c) => c == 'complete'), hasLength(1));
      expect(flow.state, PublicSigningState.completed);
      await pause();
      expect(flow.finalReady, isTrue);
      expect(await flow.downloadFinal(), <int>[1, 2, 3]);
      flow.dispose();
    });

    test('finish while offline keeps everything and says so', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      flow.setValue(flow.fields.single, 'x');
      api.failTimes(1, const SigningException(SigningErrorKind.network));
      await flow.finish();
      expect(flow.state, PublicSigningState.ready);
      expect(flow.actionError?.kind, SigningErrorKind.network);
      expect(api.calls, isNot(contains('complete')));
      await pause(100);
      expect(api.calls, contains('save:t'), reason: 'the retry saved it later');
      flow.dispose();
    });

    test('consent records the name and opens the first field', () async {
      final api = FakePublicSigningApi(view: viewOf(fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      expect(await flow.consent('  Ada Lovelace '), isTrue);
      expect(api.lastSignerName, 'Ada Lovelace');
      expect(flow.step, SigningStep.fill);
      expect(flow.signerName, 'Ada Lovelace');
      flow.dispose();
    });

    test('a failing consent keeps the signer on the first step', () async {
      final api = FakePublicSigningApi(view: viewOf(fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      api.failNext = const SigningException(SigningErrorKind.network);
      expect(await flow.consent('Ada'), isFalse);
      expect(flow.step, SigningStep.intro);
      expect(flow.actionError, isNotNull);
      flow.dispose();
    });

    test('declining ends the flow', () async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]));
      final flow = flowFor(api);
      await flow.start();
      expect(await flow.decline('No thanks'), isTrue);
      expect(flow.state, PublicSigningState.declined);
      expect(api.lastDeclineReason, 'No thanks');
      flow.dispose();
    });

    test('waiting polls until the turn comes, then stops', () async {
      final api = FakePublicSigningApi(view: viewOf(state: PublicSigningState.waiting, waitingCount: 1));
      final flow = flowFor(api);
      await flow.start();
      expect(flow.waitingCount, 1);
      await pause(80);
      final looks = api.calls.where((c) => c == 'view').length;
      expect(looks, greaterThan(1));
      api.serverMoves(viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text)]));
      await pause(80);
      expect(flow.state, PublicSigningState.ready);
      final after = api.calls.where((c) => c == 'view').length;
      await pause(100);
      expect(api.calls.where((c) => c == 'view').length, after, reason: 'no more polling once ready');
      flow.dispose();
    });

    test('a network error while polling keeps the page and tries again', () async {
      final api = FakePublicSigningApi(view: viewOf(state: PublicSigningState.waiting, waitingCount: 1));
      final flow = flowFor(api);
      await flow.start();
      api.failNext = const SigningException(SigningErrorKind.network);
      await pause(50);
      expect(flow.view, isNotNull);
      expect(flow.loadError, isNull);
      flow.dispose();
    });

    test('stops all timers on dispose', () async {
      final api = FakePublicSigningApi(view: viewOf(state: PublicSigningState.waiting, waitingCount: 1));
      final flow = flowFor(api);
      await flow.start();
      flow.dispose();
      final seen = api.calls.length;
      await pause(100);
      expect(api.calls.length, seen);
    });
  });
}
