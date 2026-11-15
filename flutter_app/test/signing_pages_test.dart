import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/navigation/app_route.dart';
import 'package:qr_flutter/qr_flutter.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'support/signing_harness.dart';

SigningField _field(String partyId, FieldType type, {bool required = true, double y = 0.2}) => SigningField.local(
  partyId: partyId,
  type: type,
  required: required,
  areas: <FieldArea>[FieldArea(page: 0, x: 0.1, y: y, w: 0.3, h: 0.04)],
);

/// A sent two-party document: Ada signs first and has a signature field.
Future<(FakeSigningApi, NeoCloudController, String)> _sentDocument({bool sequential = true}) async {
  final api = FakeSigningApi();
  final controller = await signingController(api);
  final draft = await api.createDocument(nodeId: 'n1', title: 'Lease');
  final id = draft.document.id;
  final parties = await api.saveParties(id, const <PartyDraft>[PartyDraft(name: 'Ada'), PartyDraft(name: 'Grace', email: 'grace@example.com')]);
  await api.saveFields(id, <SigningField>[_field(parties[0].id, FieldType.signature), _field(parties[1].id, FieldType.text, y: 0.4)]);
  if (!sequential) await api.update(id, const SigningSettingsPatch(signingOrder: SigningOrder.random));
  await api.send(id);
  return (api, controller, id);
}

Future<void> _pumpStatus(WidgetTester tester, FakeSigningApi api, NeoCloudController controller, String id, {Size size = const Size(1440, 1000)}) async {
  useScreen(tester, size);
  final detail = await api.get(id);
  await tester.pumpWidget(app(SigningStatusPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
  await tester.pump();
  await tester.pump();
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 1));
  });
}

void main() {
  setUpAll(initSigningTests);

  group('send dialog', () {
    Future<(FakeSigningApi, String, List<SigningDetail>)> open(WidgetTester tester, {bool secondPartyHasField = true}) async {
      useScreen(tester, const Size(1440, 900));
      final api = FakeSigningApi();
      final controller = await signingController(api);
      final draft = await api.createDocument(nodeId: 'n1', title: 'Lease');
      final id = draft.document.id;
      final parties = await api.saveParties(id, const <PartyDraft>[PartyDraft(name: 'Ada'), PartyDraft(name: 'Grace')]);
      await api.saveFields(id, <SigningField>[
        _field(parties[0].id, FieldType.signature),
        if (secondPartyHasField) _field(parties[1].id, FieldType.text, y: 0.4),
      ]);
      final sent = <SigningDetail>[];
      await tester.pumpWidget(app(SigningEditorPage(controller: controller, detail: await api.get(id), onSent: sent.add, pagesBuilder: fakePages)));
      await tester.pump();
      await tester.pump();
      await tester.tap(find.byKey(const ValueKey<String>('signing-send')));
      await tester.pumpAndSettle();
      addTearDown(() async {
        await tester.pumpWidget(const SizedBox());
        await tester.pump(const Duration(seconds: 2));
      });
      return (api, id, sent);
    }

    testWidgets('asks for names, validates and shows one link per party', (tester) async {
      mockClipboard();
      final (api, id, sent) = await open(tester);
      expect(find.text('Recipients'), findsOneWidget);
      expect(find.textContaining('Mail delivery'), findsNothing, reason: 'no note about mail coming later');
      expect(find.textContaining('comes later'), findsNothing);

      await tester.enterText(find.byKey(const ValueKey<String>('send-name-1')), '  ');
      await tester.enterText(find.byKey(const ValueKey<String>('send-email-0')), 'not-an-address');
      await tester.tap(find.byKey(const ValueKey<String>('send-confirm')));
      await tester.pump();
      expect(find.text('Please enter a name.'), findsOneWidget);
      expect(find.text("That email address doesn't look right."), findsOneWidget);
      expect((await api.get(id)).document.status, SigningStatus.draft, reason: 'nothing is sent while the form is invalid');

      await tester.enterText(find.byKey(const ValueKey<String>('send-name-1')), 'Grace Hopper');
      await tester.enterText(find.byKey(const ValueKey<String>('send-email-0')), 'ada@example.com');
      await tester.tap(find.text('Any order'));
      await tester.pump();
      await tester.tap(find.byKey(const ValueKey<String>('send-confirm')));
      await tester.pumpAndSettle();

      expect(find.text('Links are ready'), findsOneWidget);
      expect(find.textContaining('shown only now'), findsOneWidget);
      final detail = await api.get(id);
      expect(detail.document.status, SigningStatus.sent);
      expect(detail.document.signingOrder, SigningOrder.random);
      expect(detail.parties.map((party) => party.name), <String>['Ada', 'Grace Hopper']);
      expect(detail.parties.first.email, 'ada@example.com');
      for (final party in detail.parties) {
        expect(find.byKey(ValueKey<String>('link-${party.id}')), findsOneWidget);
      }
      expect(find.textContaining('https://cloud.example/app/sign/'), findsNWidgets(2));

      await tester.tap(find.byKey(const ValueKey<String>('link-qr')).first);
      await tester.pump();
      expect(find.byType(QrImageView), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey<String>('link-copy')).first);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      expect(find.text('Link copied'), findsOneWidget);
      expect(clipboardText, startsWith('https://cloud.example/app/sign/'));

      await tester.tap(find.byKey(const ValueKey<String>('send-done')));
      await tester.pumpAndSettle();
      expect(sent.single.document.status, SigningStatus.sent, reason: 'the editor hands over to the progress page');
    });

    testWidgets('a party without fields blocks sending and says who', (tester) async {
      await open(tester, secondPartyHasField: false);
      expect(find.text('Grace has nothing to fill in yet. Add a field for them first.'), findsOneWidget);
      expect(find.textContaining('has nothing to fill in yet'), findsOneWidget);
      final button = tester.widget<FilledButton>(find.byKey(const ValueKey<String>('send-confirm')));
      expect(button.onPressed, isNull);
    });

    testWidgets('a party with only optional fields is a warning, sending stays possible', (tester) async {
      useScreen(tester, const Size(1440, 900));
      final api = FakeSigningApi();
      final controller = await signingController(api);
      final draft = await api.createDocument(nodeId: 'n1', title: 'Lease');
      await api.saveFields(draft.document.id, <SigningField>[_field(draft.parties.first.id, FieldType.text, required: false)]);
      await tester.pumpWidget(app(SigningEditorPage(controller: controller, detail: await api.get(draft.document.id), pagesBuilder: fakePages)));
      await tester.pump();
      await tester.tap(find.byKey(const ValueKey<String>('signing-send')));
      await tester.pumpAndSettle();
      expect(find.textContaining('could finish without doing anything'), findsOneWidget);
      expect(find.text('Send anyway'), findsOneWidget);
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 2));
    });

    test('checkSend tells who is missing what', () {
      const ada = SigningParty(id: 'a', name: 'Ada');
      const grace = SigningParty(id: 'g', name: 'Grace');
      const lin = SigningParty(id: 'l', name: 'Lin');
      final fields = <SigningField>[
        _field('a', FieldType.signature),
        _field('g', FieldType.text, required: false),
        _field('l', FieldType.heading),
      ];
      final check = checkSend(<SigningParty>[ada, grace, lin], fields);
      expect(check.withoutFields, <String>['l'], reason: 'a heading is not something to fill in');
      expect(check.withoutRequired, <String>['g']);
      expect(check.blocked, isTrue);
      expect(checkSend(<SigningParty>[ada], fields).blocked, isFalse);
    });
  });

  group('list', () {
    Future<(FakeSigningApi, NeoCloudController)> seed(WidgetTester tester) async {
      useScreen(tester, const Size(1200, 900));
      final api = FakeSigningApi();
      final controller = await signingController(api);
      await api.createDocument(nodeId: 'a', title: 'Draft contract');
      final (sentApi, _, _) = (api, controller, '');
      final second = await sentApi.createDocument(nodeId: 'b', title: 'Waiting lease');
      await api.saveFields(second.document.id, <SigningField>[_field(second.parties.first.id, FieldType.signature)]);
      await api.send(second.document.id);
      final third = await api.createDocument(nodeId: 'c', title: 'Finished NDA');
      await api.saveFields(third.document.id, <SigningField>[_field(third.parties.first.id, FieldType.signature)]);
      await api.send(third.document.id);
      api.setPartyStatus(third.document.id, third.parties.first.id, PartyStatus.completed);
      return (api, controller);
    }

    Future<void> pumpList(WidgetTester tester, NeoCloudController controller) async {
      await tester.pumpWidget(app(SigningListScreen(controller: controller)));
      await tester.pump();
      await tester.pump();
      addTearDown(() async {
        await tester.pumpWidget(const SizedBox());
        await tester.pump(const Duration(seconds: 1));
      });
    }

    testWidgets('shows documents with status and progress, filters by status and search', (tester) async {
      final (_, controller) = await seed(tester);
      await pumpList(tester, controller);
      expect(find.text('Draft contract'), findsOneWidget);
      expect(find.text('Waiting lease'), findsOneWidget);
      expect(find.text('Finished NDA'), findsOneWidget);
      expect(find.text('1 of 1 signed'), findsOneWidget);
      expect(find.text('0 of 1 signed'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey<String>('signing-filter-completed')));
      await tester.pump();
      await tester.pump();
      expect(find.text('Finished NDA'), findsOneWidget);
      expect(find.text('Draft contract'), findsNothing);

      await tester.tap(find.byKey(const ValueKey<String>('signing-filter-draft')));
      await tester.pump();
      await tester.pump();
      expect(find.text('Draft contract'), findsOneWidget);
      expect(find.text('Finished NDA'), findsNothing);

      await tester.tap(find.text('All'));
      await tester.pump();
      await tester.pump();
      await tester.enterText(find.byType(TextField), 'lease');
      await tester.pump();
      expect(find.text('Waiting lease'), findsOneWidget);
      expect(find.text('Draft contract'), findsNothing);
      await tester.enterText(find.byType(TextField), 'zzz');
      await tester.pump();
      expect(find.text('No documents match.'), findsOneWidget);
    });

    testWidgets('opens a document through the controller', (tester) async {
      final (api, controller) = await seed(tester);
      await pumpList(tester, controller);
      final id = (await api.list()).items.firstWhere((item) => item.title == 'Waiting lease').id;
      await tester.tap(find.byKey(ValueKey<String>('signing-row-$id')));
      await tester.pump();
      expect(controller.openSigningId, id);
      expect(controller.currentRoute?.location, '/signing/$id');
    });

    testWidgets('delete asks first; a waiting document can be canceled but not deleted', (tester) async {
      final (api, controller) = await seed(tester);
      await pumpList(tester, controller);
      final items = (await api.list()).items;
      final draft = items.firstWhere((item) => item.title == 'Draft contract');
      final waiting = items.firstWhere((item) => item.title == 'Waiting lease');

      await tester.tap(find.descendant(of: find.byKey(ValueKey<String>('signing-row-${waiting.id}')), matching: find.byType(PopupMenuButton<int>)));
      await tester.pumpAndSettle();
      expect(find.text('Cancel signing'), findsOneWidget);
      expect(find.text('Delete'), findsNothing);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();

      await tester.tap(find.descendant(of: find.byKey(ValueKey<String>('signing-row-${draft.id}')), matching: find.byType(PopupMenuButton<int>)));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete'));
      await tester.pumpAndSettle();
      expect(find.text('Delete this document?'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
      await tester.pumpAndSettle();
      expect(find.text('Draft contract'), findsNothing);
      expect((await api.list()).items, hasLength(2));
    });

    testWidgets('an empty list explains itself and offers to choose a file', (tester) async {
      useScreen(tester, const Size(1200, 900));
      final controller = await signingController(FakeSigningApi());
      await pumpList(tester, controller);
      expect(find.text('Nothing to sign yet'), findsOneWidget);
      expect(find.text('Choose a file'), findsOneWidget);
    });

    testWidgets('the list reloads when the server reports a change', (tester) async {
      final (api, controller) = await seed(tester);
      await pumpList(tester, controller);
      await api.createDocument(nodeId: 'z', title: 'Brand new');
      expect(find.text('Brand new'), findsNothing);
      controller.signingRevision.value += 1;
      await tester.pump();
      await tester.pump();
      expect(find.text('Brand new'), findsOneWidget);
    });
  });

  group('progress page', () {
    testWidgets('in progress: parties, progress, links and the timeline', (tester) async {
      final (api, controller, id) = await _sentDocument();
      final parties = (await api.get(id)).parties;
      api.setPartyStatus(id, parties[0].id, PartyStatus.completed);
      await _pumpStatus(tester, api, controller, id);
      expect(find.text('In progress'), findsOneWidget);
      expect(find.text('1 of 2 signed'), findsOneWidget);
      expect(tester.widget<LinearProgressIndicator>(find.byKey(const ValueKey<String>('signing-progress'))).value, 0.5);
      expect(find.byKey(ValueKey<String>('party-card-${parties[0].id}')), findsOneWidget);
      expect(find.text('Signed'), findsOneWidget);
      expect(find.text('Waiting for their turn'), findsOneWidget);
      expect(find.text('Sent for signing'), findsOneWidget);
      expect(find.text('Document created'), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('signing-cancel')), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('signing-open-signed')), findsNothing);
      // The finished party has no link to manage, the other one has.
      expect(find.byKey(ValueKey<String>('regen-${parties[0].id}')), findsNothing);
      expect(find.byKey(ValueKey<String>('regen-${parties[1].id}')), findsOneWidget);
    });

    testWidgets('a new link is shown once after asking', (tester) async {
      final (api, controller, id) = await _sentDocument();
      final parties = (await api.get(id)).parties;
      await _pumpStatus(tester, api, controller, id);
      await tester.tap(find.byKey(ValueKey<String>('regen-${parties[1].id}')));
      await tester.pumpAndSettle();
      expect(find.text('Make a new link?'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'New link'));
      await tester.pumpAndSettle();
      expect(find.text('New link for Grace'), findsOneWidget);
      expect(find.textContaining('https://cloud.example/app/sign/'), findsOneWidget);
      await tester.tap(find.text('Done'));
      await tester.pumpAndSettle();
      expect(find.textContaining('https://cloud.example/app/sign/'), findsNothing);
    });

    testWidgets('cancel asks first and the page then shows canceled', (tester) async {
      final (api, controller, id) = await _sentDocument();
      await _pumpStatus(tester, api, controller, id);
      await tester.tap(find.byKey(const ValueKey<String>('signing-cancel')));
      await tester.pumpAndSettle();
      expect(find.text('Cancel this document?'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Cancel signing'));
      await tester.pumpAndSettle();
      expect(find.text('Canceled'), findsWidgets);
      expect(find.byKey(const ValueKey<String>('signing-cancel')), findsNothing);
      expect(find.byKey(const ValueKey<String>('signing-delete')), findsOneWidget);
      expect(find.text('Document canceled'), findsOneWidget);
    });

    testWidgets('completed: offers the signed file', (tester) async {
      final (api, controller, id) = await _sentDocument();
      final parties = (await api.get(id)).parties;
      api.setPartyStatus(id, parties[0].id, PartyStatus.completed);
      api.setPartyStatus(id, parties[1].id, PartyStatus.completed);
      api.setFinalNode(id, 'final-node');
      await _pumpStatus(tester, api, controller, id);
      expect(find.text('Completed'), findsWidgets);
      expect(find.text('2 of 2 signed'), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('signing-open-signed')), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('signing-download-signed')), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('signing-cancel')), findsNothing);
      expect(tester.widget<LinearProgressIndicator>(find.byKey(const ValueKey<String>('signing-progress'))).value, 1);
    });

    testWidgets('completed: one download button with the other file in a menu', (tester) async {
      final (api, controller, id) = await _sentDocument();
      final parties = (await api.get(id)).parties;
      api.setPartyStatus(id, parties[0].id, PartyStatus.completed);
      api.setPartyStatus(id, parties[1].id, PartyStatus.completed);
      api.setFinalNode(id, 'final-node');
      await _pumpStatus(tester, api, controller, id);
      expect(find.text('Download signed PDF'), findsOneWidget);
      expect(find.widgetWithText(FilledButton, 'Download signed PDF'), findsNothing, reason: 'the gold button is another action');
      await tester.tap(find.byKey(const ValueKey<String>('signing-download-signed-menu')));
      await tester.pumpAndSettle();
      expect(find.text('Download with signature pages'), findsOneWidget);
      expect(find.text('Download without signature pages'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey<String>('signing-download-signed-clean')));
      await tester.pumpAndSettle();
      expect(api.finalRequests, <bool>[false]);
    });

    testWidgets('verifying the audit trail reports the result', (tester) async {
      final (api, controller, id) = await _sentDocument();
      await _pumpStatus(tester, api, controller, id);
      await tester.ensureVisible(find.byKey(const ValueKey<String>('signing-verify')));
      await tester.tap(find.byKey(const ValueKey<String>('signing-verify')));
      await tester.pump();
      await tester.pump();
      expect(find.textContaining('The audit trail is intact'), findsOneWidget);
    });

    testWidgets('the page follows a change reported by the server', (tester) async {
      final (api, controller, id) = await _sentDocument();
      await _pumpStatus(tester, api, controller, id);
      expect(find.text('0 of 2 signed'), findsOneWidget);
      final parties = (await api.get(id)).parties;
      api.setPartyStatus(id, parties[0].id, PartyStatus.completed);
      controller.signingRevision.value += 1;
      await tester.pump();
      await tester.pump();
      expect(find.text('1 of 2 signed'), findsOneWidget);
    });

    testWidgets('the preview draws what was entered over the pages', (tester) async {
      final (api, controller, id) = await _sentDocument();
      final fields = (await api.get(id)).fields;
      final text = fields.firstWhere((field) => field.type == FieldType.text);
      api.setPreviewValues(id, <SigningValue>[SigningValue(fieldId: text.id, value: 'Grace Hopper')]);
      await _pumpStatus(tester, api, controller, id);
      expect(find.text('Grace Hopper'), findsOneWidget);
      expect(find.byKey(ValueKey<String>('status-box-${text.id}')), findsOneWidget);
    });
  });

  group('routes', () {
    testWidgets('the signing page and a document are part of the address', (tester) async {
      final controller = await signingController(FakeSigningApi());
      controller.openRoute(const AppRoute('signing', 'abc'));
      expect(controller.selectedSection, AppSection.signing);
      expect(controller.openSigningId, 'abc');
      expect(controller.currentRoute, const AppRoute('signing', 'abc'));
      controller.openSigning(null);
      expect(controller.currentRoute, const AppRoute('signing'));
      expect(AppRoute.parse('/app/signing/xyz'), const AppRoute('signing', 'xyz'));
    });

    test('files that can be signed', () {
      expect(signableKindOf('Lease.PDF', null), SignableKind.pdf);
      expect(signableKindOf('scan', 'image/jpeg'), SignableKind.image);
      expect(signableKindOf('logo.svg', 'image/svg+xml'), SignableKind.none);
      expect(signableKindOf('a.docx', null), SignableKind.office);
      expect(signableKindOf('movie.mp4', 'video/mp4'), SignableKind.none);
    });
  });
}
