import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_account_api.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'support/signing_harness.dart';

Finder _key(String value) => find.byKey(ValueKey<String>(value));

SigningField _spot(FieldType type, double y, {String name = ''}) => SigningField.local(
  partyId: 'any',
  type: type,
  name: name,
  areas: <FieldArea>[FieldArea(page: 0, x: 0.1, y: y, w: 0.3, h: 0.04)],
);

Finder get _boxes => find.byWidgetPredicate((widget) => widget.key is ValueKey<String> && (widget.key! as ValueKey<String>).value.startsWith('signing-box-'));

void main() {
  setUpAll(initSigningTests);

  group('editor state', () {
    Future<(FakeSigningApi, SigningEditorState)> make() async {
      final api = FakeSigningApi();
      final detail = await api.createDocument(nodeId: 'n', title: 'Lease');
      final state = SigningEditorState(api: api, detail: detail, partyName: (n) => 'Party $n', autosaveDelay: const Duration(milliseconds: 5));
      addTearDown(state.dispose);
      return (api, state);
    }

    test('turning pages saves first, adopts the new file and starts a fresh history', () async {
      final (api, state) = await make();
      state.addField(FieldType.signature, 0, const Offset(0.3, 0.3), const Size(595, 842));
      expect(state.canUndo, isTrue);
      await state.rotatePages(quarterTurns: 1, pages: <int>[0]);
      expect(api.lastRotation?.quarterTurns, 1);
    expect(api.lastRotation?.pages, <int>[0]);
      expect(state.document.snapshotSha256, isNotNull);
      expect(state.fields, hasLength(1), reason: 'the unsaved box went out before the turn');
      expect(state.canUndo, isFalse);
      expect(state.rotating, isFalse);
      // A full turn is nothing to do.
      api.lastRotation = null;
      await state.rotatePages(quarterTurns: 4);
      expect(api.lastRotation, isNull);
    });

    test('detected fields arrive as one undo step, filtered by role', () async {
      final (api, state) = await make();
      api.detectable = <({SigningField field, String role})>[
        (field: _spot(FieldType.signature, 0.7), role: 'signature'),
        (field: _spot(FieldType.text, 0.8, name: 'Ort'), role: 'place'),
      ];
      expect(await state.detectFields(roles: quickSignRoles), 1);
      expect(api.lastDetectRoles, quickSignRoles);
      expect(state.fields.single.type, FieldType.signature);
      expect(state.fields.single.partyId, state.parties.first.id);
      state.undo();
      expect(state.fields, isEmpty);
    });
  });

  testWidgets('the editor turns the page in view or all pages and shows the new file', (tester) async {
    useScreen(tester, const Size(1440, 900));
    final api = FakeSigningApi();
    final controller = await signingController(api);
    final detail = await api.createDocument(nodeId: 'n1', title: 'Scan');
    await tester.pumpWidget(app(SigningEditorPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
    await tester.pump();
    await tester.pump();
    expect(api.pdfRequests, 1);

    await tester.tap(_key('signing-rotate-right'));
    await tester.pump();
    await tester.pump();
    expect(api.lastRotation?.quarterTurns, 1);
    expect(api.lastRotation?.pages, <int>[0]);
    expect(api.pdfRequests, 2);

    await tester.tap(_key('signing-rotate-all'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Turn all pages upside down'));
    await tester.pumpAndSettle();
    expect(api.lastRotation?.quarterTurns, 2);
    expect(api.lastRotation?.pages, null);
    expect(api.pdfRequests, 3);
    final events = await api.events(detail.document.id);
    expect(events.events.map((event) => event.type), contains(SigningEventType.pagesRotated));

    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 2));
  });

  testWidgets('quick sign fills the spots it finds and signs them', (tester) async {
    useScreen(tester, const Size(1440, 900));
    final api = FakeSigningApi()
      ..detectable = <({SigningField field, String role})>[
        (field: _spot(FieldType.signature, 0.7, name: 'Unterschrift'), role: 'signature'),
        (field: _spot(FieldType.date, 0.8, name: 'Datum'), role: 'date'),
        (field: _spot(FieldType.text, 0.85, name: 'Name'), role: 'name'),
        (field: _spot(FieldType.checkbox, 0.9), role: 'checkbox'),
      ];
    final account = FakeSigningAccountApi(profile: const SigningProfile(displayName: 'Ada Lovelace'));
    await account.addImage(kFakePng, kind: SavedKind.signature, method: SignatureMethod.drawn, label: 'Mine');
    final controller = await signingController(api, account: account);
    final detail = await api.createDocument(nodeId: 'n1', title: 'Lease');
    await tester.pumpWidget(app(QuickSignPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
    await tester.pump();
    await tester.pump();
    await tester.pump();

    expect(api.lastDetectRoles, quickSignRoles);
    expect(_boxes, findsNWidgets(3));
    expect(find.text('3 spots found and filled in. Remove any you don\'t need.'), findsOneWidget);
    expect(find.text('Ada Lovelace'), findsWidgets);

    // Asking again finds nothing new and says so.
    api.detectable = const <({SigningField field, String role})>[];
    await tester.tap(_key('quick-detect'));
    await tester.pump();
    await tester.pump();
    expect(find.text('No spots found. Tap the page to place boxes yourself.'), findsOneWidget);

    await tester.tap(_key('quick-submit'));
    await tester.pump(const Duration(seconds: 1));
    final request = api.lastQuickSign!;
    expect(request.fields.map((field) => field.type), <FieldType>[FieldType.signature, FieldType.date, FieldType.text]);
    final json = request.toJson()['fields']! as List<Object?>;
    expect((json[0]! as Map)['imageId'], isNotNull);
    expect((json[2]! as Map)['value'], 'Ada Lovelace');
  });

  testWidgets('quick sign turns pages too', (tester) async {
    useScreen(tester, const Size(1440, 900));
    final api = FakeSigningApi();
    final controller = await signingController(api, account: FakeSigningAccountApi());
    final detail = await api.createDocument(nodeId: 'n1', title: 'Scan');
    await tester.pumpWidget(app(QuickSignPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
    await tester.pump();
    await tester.pump();
    await tester.tap(_key('signing-rotate-left'));
    await tester.pump();
    await tester.pump();
    expect(api.lastRotation?.quarterTurns, -1);
    expect(api.lastRotation?.pages, <int>[0]);
    expect(api.pdfRequests, 2);
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 2));
  });
}
