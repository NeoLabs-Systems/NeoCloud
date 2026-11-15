import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'support/signing_harness.dart';

Future<(FakeSigningApi, NeoCloudController, SigningDetail)> _open(WidgetTester tester, {Size size = const Size(1440, 900), double ratio = 1}) async {
  useScreen(tester, size, ratio: ratio);
  final api = FakeSigningApi();
  final controller = await signingController(api);
  final detail = await api.createDocument(nodeId: 'n1', title: 'Lease');
  await tester.pumpWidget(app(SigningEditorPage(controller: controller, detail: detail, pagesBuilder: fakePages)));
  await tester.pump();
  await tester.pump();
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox());
    await tester.pump(const Duration(seconds: 2));
  });
  return (api, controller, detail);
}

Finder get _anyBox => find.byWidgetPredicate((widget) => widget.key is ValueKey<String> && (widget.key! as ValueKey<String>).value.startsWith('signing-box-'));

Future<void> _settle(WidgetTester tester) async {
  await tester.pump(const Duration(seconds: 2));
  await tester.pump();
}

Future<List<SigningField>> _saved(FakeSigningApi api, SigningDetail detail) async => (await api.get(detail.document.id)).fields;

/// Arms [type] and taps page 1 at [offset] from its top-left corner.
Future<void> _place(WidgetTester tester, String type, {Offset offset = const Offset(200, 150)}) async {
  final tile = find.byKey(ValueKey<String>('palette-$type'));
  if (tile.evaluate().isEmpty) {
    await tester.tap(find.byKey(const ValueKey<String>('signing-tab-fields')));
    await tester.pump();
  }
  await tester.ensureVisible(tile);
  await tester.tap(tile);
  await tester.pump();
  await tester.tapAt(tester.getTopLeft(find.byKey(const ValueKey<String>('signing-page-0'))) + offset);
  await tester.pump();
}

Future<void> _openSettings(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey<String>('signing-tab-settings')));
  await tester.pump();
}

/// A drag in small steps, the way a hand moves (one big jump is swallowed by the touch slop).
Future<void> _drag(WidgetTester tester, Offset from, Offset total) async {
  final gesture = await tester.startGesture(from);
  for (var step = 0; step < 10; step++) {
    await gesture.moveBy(total / 10);
    await tester.pump(const Duration(milliseconds: 16));
  }
  await gesture.up();
  await tester.pump();
}

Future<void> _chord(WidgetTester tester, LogicalKeyboardKey key, {bool shift = false}) async {
  await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
  if (shift) await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
  await tester.sendKeyEvent(key);
  if (shift) await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
  await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
  await tester.pump();
}

void main() {
  setUpAll(initSigningTests);

  group('placing fields', () {
    testWidgets('tap a tile, then tap the page: the field appears and autosaves', (tester) async {
      final (api, _, detail) = await _open(tester);
      expect(_anyBox, findsNothing);
      await _place(tester, 'signature');
      expect(_anyBox, findsOneWidget);
      expect(find.text('Saved'), findsNothing);
      await _settle(tester);
      final saved = await _saved(api, detail);
      expect(saved.single.type, FieldType.signature);
      expect(saved.single.partyId, detail.parties.first.id);
      expect(saved.single.areas.single.page, 0);
      // The default size comes from the registry and the box stays on the page.
      final area = saved.single.areas.single;
      expect(area.w, closeTo(FieldTypes.of(FieldType.signature).defaultSize.width, 0.001));
      expect(area.x + area.w, lessThanOrEqualTo(1));
      expect(find.text('Saved'), findsOneWidget);
    });

    testWidgets('a tile can be dragged onto the page', (tester) async {
      final (api, _, detail) = await _open(tester);
      final gesture = await tester.startGesture(tester.getCenter(find.byKey(const ValueKey<String>('palette-date'))));
      await tester.pump(const Duration(milliseconds: 50));
      await gesture.moveTo(tester.getTopLeft(find.byKey(const ValueKey<String>('signing-page-0'))) + const Offset(300, 200));
      await tester.pump();
      await gesture.up();
      await tester.pump();
      expect(_anyBox, findsOneWidget);
      await _settle(tester);
      expect((await _saved(api, detail)).single.type, FieldType.date);
    });

    testWidgets('a box near the edge is clamped inside the page', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text', offset: const Offset(2, 2));
      await _settle(tester);
      final area = (await _saved(api, detail)).single.areas.single;
      expect(area.x, greaterThanOrEqualTo(0));
      expect(area.y, greaterThanOrEqualTo(0));
    });
  });

  group('editing a field', () {
    testWidgets('dragging moves it; the new place is saved', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await _settle(tester);
      final before = (await _saved(api, detail)).single.areas.single;
      await _drag(tester, tester.getCenter(_anyBox), const Offset(160, 90));
      await _settle(tester);
      final after = (await _saved(api, detail)).single.areas.single;
      expect(after.x, greaterThan(before.x + 0.05));
      expect(after.y, greaterThan(before.y + 0.03));
      expect(after.w, before.w);
    });

    testWidgets('the corner handle resizes it', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await _settle(tester);
      final before = (await _saved(api, detail)).single.areas.single;
      expect(find.byKey(const ValueKey<String>('field-handle-bottomRight')), findsOneWidget);
      await _drag(tester, tester.getBottomRight(_anyBox) - const Offset(4, 4), const Offset(100, 40));
      await _settle(tester);
      final after = (await _saved(api, detail)).single.areas.single;
      expect(after.w, greaterThan(before.w + 0.03));
      expect(after.h, greaterThan(before.h));
      expect(after.x, before.x);
    });

    testWidgets('Delete removes the selected field', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await tester.sendKeyEvent(LogicalKeyboardKey.delete);
      await tester.pump();
      expect(_anyBox, findsNothing);
      await _settle(tester);
      expect(await _saved(api, detail), isEmpty);
    });

    testWidgets('the delete button of the inspector works too', (tester) async {
      final (_, _, _) = await _open(tester);
      await _place(tester, 'checkbox');
      await _openSettings(tester);
      await tester.tap(find.byKey(const ValueKey<String>('signing-field-delete')));
      await tester.pump();
      expect(_anyBox, findsNothing);
    });

    testWidgets('arrow keys nudge, Shift moves further', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await _settle(tester);
      final start = (await _saved(api, detail)).single.areas.single;
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
      await _settle(tester);
      final one = (await _saved(api, detail)).single.areas.single;
      expect(one.x - start.x, closeTo(0.002, 0.0006));
      await tester.sendKeyDownEvent(LogicalKeyboardKey.shiftLeft);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.shiftLeft);
      await _settle(tester);
      final two = (await _saved(api, detail)).single.areas.single;
      expect(two.y - start.y, closeTo(0.02, 0.0006));
    });

    testWidgets('copy and paste, and duplicate, make new fields', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await _chord(tester, LogicalKeyboardKey.keyC);
      await _chord(tester, LogicalKeyboardKey.keyV);
      expect(_anyBox, findsNWidgets(2));
      await _chord(tester, LogicalKeyboardKey.keyD);
      expect(_anyBox, findsNWidgets(3));
      await _settle(tester);
      expect(await _saved(api, detail), hasLength(3));
    });

    testWidgets('undo and redo walk through the history', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await _place(tester, 'date', offset: const Offset(200, 300));
      expect(_anyBox, findsNWidgets(2));
      await _chord(tester, LogicalKeyboardKey.keyZ);
      expect(_anyBox, findsOneWidget);
      await _chord(tester, LogicalKeyboardKey.keyZ);
      expect(_anyBox, findsNothing);
      await _chord(tester, LogicalKeyboardKey.keyZ, shift: true);
      expect(_anyBox, findsOneWidget);
      await _chord(tester, LogicalKeyboardKey.keyY);
      expect(_anyBox, findsNWidgets(2));
      await _settle(tester);
      expect(await _saved(api, detail), hasLength(2));
    });

    testWidgets('a field takes the colour of the party it is given to', (tester) async {
      final (api, _, detail) = await _open(tester);
      await tester.tap(find.byKey(const ValueKey<String>('signing-add-party')));
      await tester.pump();
      await _place(tester, 'text');
      await _settle(tester);
      final parties = (await api.get(detail.document.id)).parties;
      expect(parties, hasLength(2));
      expect((await _saved(api, detail)).single.partyId, parties.last.id, reason: 'the new party is active, so its fields belong to it');
      // Move the field to the first party in the inspector.
      await _openSettings(tester);
      await tester.tap(find.byKey(ValueKey<String>('field-party-${parties.last.id}')));
      await tester.pump();
      await tester.tap(find.text('Party 1').last);
      await tester.pump();
      await _settle(tester);
      expect((await _saved(api, detail)).single.partyId, parties.first.id);
    });
  });

  group('parties', () {
    testWidgets('add, rename and remove a party', (tester) async {
      final (api, _, detail) = await _open(tester);
      await tester.tap(find.byKey(const ValueKey<String>('signing-add-party')));
      await tester.pump();
      await _settle(tester);
      var parties = (await api.get(detail.document.id)).parties;
      expect(parties.map((party) => party.name), <String>['Party 1', 'Party 2']);

      await tester.tap(find.byKey(ValueKey<String>('signing-party-menu-${parties.last.id}')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rename'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const ValueKey<String>('party-name')), 'Landlord');
      await tester.enterText(find.byKey(const ValueKey<String>('party-email')), 'owner@example.com');
      await tester.tap(find.byKey(const ValueKey<String>('party-save')));
      await tester.pumpAndSettle();
      await _settle(tester);
      parties = (await api.get(detail.document.id)).parties;
      expect(parties.last.name, 'Landlord');
      expect(parties.last.email, 'owner@example.com');

      await _place(tester, 'text');
      await tester.tap(find.byKey(ValueKey<String>('signing-party-menu-${parties.last.id}')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();
      expect(find.text('Remove Landlord?'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
      await tester.pumpAndSettle();
      await _settle(tester);
      expect((await api.get(detail.document.id)).parties, hasLength(1));
      expect(await _saved(api, detail), isEmpty, reason: 'the removed party takes its fields along');
      expect(_anyBox, findsNothing);
    });

    testWidgets('the last party cannot be removed', (tester) async {
      final (_, _, detail) = await _open(tester);
      await tester.tap(find.byKey(ValueKey<String>('signing-party-menu-${detail.parties.first.id}')));
      await tester.pumpAndSettle();
      expect(find.text('Remove'), findsNothing);
    });
  });

  group('inspector', () {
    testWidgets('a choice field without choices shows the problem and blocks sending', (tester) async {
      final (_, _, _) = await _open(tester);
      await _place(tester, 'select');
      await _openSettings(tester);
      expect(find.byKey(const ValueKey<String>('problem-options')), findsNothing);
      await tester.tap(find.byKey(const ValueKey<String>('option-remove-1')));
      await tester.tap(find.byKey(const ValueKey<String>('option-remove-0')));
      await tester.pump();
      expect(find.text('Add at least one choice.'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey<String>('signing-send')));
      await tester.pump();
      expect(find.text('Some fields need attention first. They are marked in the list.'), findsOneWidget);
      expect(find.byKey(const ValueKey<String>('send-confirm')), findsNothing);
      await tester.tap(find.byKey(const ValueKey<String>('option-add')));
      await tester.pump();
      expect(find.byKey(const ValueKey<String>('problem-options')), findsNothing);
    });

    testWidgets('inconsistent limits are reported', (tester) async {
      final (_, _, _) = await _open(tester);
      await _place(tester, 'number');
      await _openSettings(tester);
      final inputs = find.byType(TextFormField);
      // Smallest value, largest value (the first two number settings).
      await tester.enterText(inputs.at(0), '10');
      await tester.enterText(inputs.at(1), '5');
      await tester.pump();
      expect(find.text('The smallest value is above the largest.'), findsOneWidget);
      await tester.enterText(inputs.at(1), '50');
      await tester.pump();
      expect(find.byKey(const ValueKey<String>('problem-range')), findsNothing);
    });

    testWidgets('label and required are saved', (tester) async {
      final (api, _, detail) = await _open(tester);
      await _place(tester, 'text');
      await _openSettings(tester);
      await tester.enterText(find.byKey(const ValueKey<String>('field-name')), 'Tenant name');
      await tester.tap(find.byKey(const ValueKey<String>('field-required')));
      await tester.pump();
      await _settle(tester);
      final saved = (await _saved(api, detail)).single;
      expect(saved.name, 'Tenant name');
      expect(saved.required, isFalse);
    });
  });

  group('state', () {
    testWidgets('a locked document is read-only and says why', (tester) async {
      useScreen(tester, const Size(1440, 900));
      final api = FakeSigningApi();
      final controller = await signingController(api);
      final draft = await api.createDocument(nodeId: 'n1', title: 'Lease');
      final locked = SigningDetail(
        document: draft.document,
        parties: <SigningParty>[SigningParty(id: draft.parties.first.id, name: 'Party 1', openedAt: DateTime(2026))],
        fields: const <SigningField>[],
      );
      await tester.pumpWidget(app(SigningEditorPage(controller: controller, detail: locked, pagesBuilder: fakePages)));
      await tester.pump();
      expect(find.textContaining('This document is locked'), findsWidgets);
      expect(find.byKey(const ValueKey<String>('signing-duplicate-new')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey<String>('palette-text')));
      await tester.pump();
      await tester.tapAt(tester.getTopLeft(find.byKey(const ValueKey<String>('signing-page-0'))) + const Offset(200, 150));
      await tester.pump();
      expect(_anyBox, findsNothing);
      await tester.pumpWidget(const SizedBox());
    });

    testWidgets('a conflict from the server locks the editor', (tester) async {
      final (api, _, _) = await _open(tester);
      api.failNext = const SigningException(SigningErrorKind.conflict, statusCode: 409);
      await _place(tester, 'text');
      await _settle(tester);
      expect(find.textContaining("can't be edited anymore"), findsOneWidget);
    });

    testWidgets('a failed save shows the error and tapping it tries again', (tester) async {
      final (api, _, detail) = await _open(tester);
      api.failNext = const SigningException(SigningErrorKind.network);
      await _place(tester, 'text');
      await tester.pump(const Duration(seconds: 1));
      expect(find.text("Couldn't save. Trying again…"), findsOneWidget);
      await tester.tap(find.text("Couldn't save. Trying again…"));
      await tester.pump(const Duration(milliseconds: 100));
      await _settle(tester);
      expect(find.text('Saved'), findsOneWidget);
      expect(await _saved(api, detail), hasLength(1));
    });
  });

  group('phone', () {
    testWidgets('the fields sheet arms a tile and the page places it', (tester) async {
      final (api, _, detail) = await _open(tester, size: const Size(412, 915));
      await tester.tap(find.byKey(const ValueKey<String>('signing-open-panel')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const ValueKey<String>('palette-initials')));
      await tester.pumpAndSettle();
      expect(find.textContaining('Tap the page to place'), findsOneWidget);
      await tester.tapAt(tester.getTopLeft(find.byKey(const ValueKey<String>('signing-page-0'))) + const Offset(100, 120));
      await tester.pump();
      expect(_anyBox, findsOneWidget);
      await _settle(tester);
      expect((await _saved(api, detail)).single.type, FieldType.initials);
    });
  });
}
