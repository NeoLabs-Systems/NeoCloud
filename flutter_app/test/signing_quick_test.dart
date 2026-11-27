import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_account_api.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'support/signing_harness.dart';

Finder _key(String value) => find.byKey(ValueKey<String>(value));

Future<(FakeSigningApi, FakeSigningAccountApi, NeoCloudController, SigningDetail)> _open(WidgetTester tester, {bool savedSignature = true, Size size = const Size(1440, 900)}) async {
  useScreen(tester, size);
  final api = FakeSigningApi();
  final account = FakeSigningAccountApi(profile: const SigningProfile(displayName: 'Ada Lovelace'));
  if (savedSignature) await account.addImage(kFakePng, kind: SavedKind.signature, method: SignatureMethod.drawn, label: 'Mine');
  final controller = await signingController(api, account: account);
  final detail = await api.createDocument(nodeId: 'n1', title: 'Lease');
  await tester.pumpWidget(app(QuickSignPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
  await tester.pump();
  await tester.pump();
  return (api, account, controller, detail);
}

Future<void> _place(WidgetTester tester, String type, Offset offset) async {
  await tester.tap(_key('palette-$type'));
  await tester.pump();
  await tester.tapAt(tester.getTopLeft(_key('signing-page-0')) + offset);
  await tester.pump();
}

void main() {
  setUpAll(initSigningTests);

  testWidgets('places the saved signature, date and name and signs in one request', (tester) async {
    final (api, _, controller, detail) = await _open(tester);
    expect(find.text('From your signing profile'), findsOneWidget);
    expect(find.textContaining('“Lease (signed).pdf”'), findsOneWidget);

    await _place(tester, 'signature', const Offset(200, 500));
    await _place(tester, 'date', const Offset(200, 600));
    await _place(tester, 'text', const Offset(400, 600));
    expect(find.text('Ada Lovelace'), findsWidgets);

    await tester.tap(_key('quick-submit'));
    await tester.pump(const Duration(seconds: 1));

    final request = api.lastQuickSign!;
    expect(request.signerName, 'Ada Lovelace');
    expect(request.fields.map((field) => field.type), <FieldType>[FieldType.signature, FieldType.date, FieldType.text]);
    final json = request.toJson()['fields']! as List<Object?>;
    expect((json[0]! as Map)['imageId'], isNotNull);
    expect((json[0]! as Map).containsKey('image'), isFalse);
    expect((json[1]! as Map)['value'], toSigningIsoDate(DateTime.now()));
    expect((json[2]! as Map)['value'], 'Ada Lovelace');
    expect(controller.openSigningId, detail.document.id);
    expect((await api.get(detail.document.id)).document.status, SigningStatus.finalizing);
  });

  testWidgets('without a saved signature it asks for one when the box is placed', (tester) async {
    final (api, account, _, _) = await _open(tester, savedSignature: false);
    expect(find.text('Your signature'), findsOneWidget);
    await _place(tester, 'signature', const Offset(200, 500));
    await tester.pumpAndSettle();
    expect(find.byType(SignaturePad), findsOneWidget);

    final gesture = await tester.startGesture(tester.getCenter(find.byType(SignaturePad)));
    await gesture.moveBy(const Offset(60, 20));
    await gesture.moveBy(const Offset(40, -30));
    await gesture.up();
    await tester.pump();
    await tester.runAsync(() async {
      await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
      await Future<void>.delayed(const Duration(milliseconds: 400));
    });
    await tester.pumpAndSettle();
    // Offered for the profile; this time it stays with the document only.
    await tester.tap(_key('quick-keep-no'));
    await tester.pumpAndSettle();
    expect(account.current.signatures, isEmpty);
    expect(find.text('Only for this document'), findsOneWidget);

    await tester.tap(_key('quick-submit'));
    await tester.pump(const Duration(seconds: 1));
    final field = (api.lastQuickSign!.toJson()['fields']! as List<Object?>).single! as Map;
    expect(field['image'], isA<String>());
    expect(field['method'], 'draw');
    expect(field.containsKey('imageId'), isFalse);
  });

  testWidgets('says what is missing instead of signing', (tester) async {
    final (api, _, _, _) = await _open(tester);
    await tester.tap(_key('quick-submit'));
    await tester.pump();
    expect(find.text('Place at least one box on the page first.'), findsOneWidget);

    await _place(tester, 'date', const Offset(200, 600));
    await tester.enterText(_key('quick-name'), ' ');
    await tester.tap(_key('quick-submit'));
    await tester.pump();
    expect(find.text('Enter your name.'), findsOneWidget);
    expect(api.lastQuickSign, isNull);
  });

  testWidgets('fits a phone', (tester) async {
    await _open(tester, size: const Size(390, 844));
    await _place(tester, 'signature', const Offset(150, 200));
    expect(tester.takeException(), isNull);
    expect(_key('quick-submit'), findsOneWidget);
  });

  testWidgets('leaving without signing deletes the draft made for it', (tester) async {
    final (api, _, _, detail) = await _open(tester);
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 2));
    await expectLater(api.get(detail.document.id), throwsA(isA<SigningException>()));
  });
}
