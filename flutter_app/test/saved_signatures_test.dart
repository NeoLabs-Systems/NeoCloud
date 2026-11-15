import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/signing.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'helpers/public_signing_harness.dart';

Uint8List _png(int seed) => Uint8List.fromList(<int>[...tinyPng, seed]);

Future<SavedSignatureStore> _store({int cap = 5}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  var tick = 0;
  return SavedSignatureStore(await SharedPreferences.getInstance(), cap: cap, now: () => DateTime.utc(2026, 1, 1, 0, 0, tick++));
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('SavedSignatureStore', () {
    test('keeps png bytes, method, label and time, newest first', () async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      final typed = await store.add(SavedKind.signature, _png(2), method: SignatureMethod.typed, label: '  Anna  ');
      final list = store.list(SavedKind.signature);
      expect(list.first.id, typed.id);
      expect(list.first.png, _png(2));
      expect(list.first.method, SignatureMethod.typed);
      expect(list.first.label, 'Anna');
      expect(list.last.label, isNull);
      expect(list.first.createdAt.isAfter(list.last.createdAt), isTrue);
    });

    test('signature and initials are kept apart', () async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      await store.add(SavedKind.initials, _png(2), method: SignatureMethod.drawn);
      expect(store.list(SavedKind.signature), hasLength(1));
      expect(store.list(SavedKind.initials), hasLength(1));
      await store.clear(SavedKind.signature);
      expect(store.list(SavedKind.signature), isEmpty);
      expect(store.list(SavedKind.initials), hasLength(1));
    });

    test('at most five of each kind: the oldest go first', () async {
      final store = await _store();
      for (var i = 0; i < 7; i++) {
        await store.add(SavedKind.signature, _png(i), method: SignatureMethod.drawn);
      }
      final list = store.list(SavedKind.signature);
      expect(list, hasLength(5));
      expect(list.first.png, _png(6));
      expect(list.last.png, _png(2));
    });

    test('the same picture twice is kept once, moved to the front', () async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      await store.add(SavedKind.signature, _png(2), method: SignatureMethod.drawn);
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      expect(store.list(SavedKind.signature).map((item) => item.png), <Uint8List>[_png(1), _png(2)]);
    });

    test('remove one, remove all', () async {
      final store = await _store();
      final first = await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      await store.add(SavedKind.signature, _png(2), method: SignatureMethod.drawn);
      await store.add(SavedKind.initials, _png(3), method: SignatureMethod.drawn);
      await store.remove(SavedKind.signature, first.id);
      expect(store.list(SavedKind.signature), hasLength(1));
      await store.clearAll();
      expect(store.list(SavedKind.signature), isEmpty);
      expect(store.list(SavedKind.initials), isEmpty);
    });

    test('lives in local storage under its own keys and holds only the pictures', () async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.typed, label: 'Anna');
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getKeys(), <String>{SavedSignatureStore.keyFor(SavedKind.signature)});
      final raw = jsonDecode(prefs.getString(SavedSignatureStore.keyFor(SavedKind.signature))!) as List<dynamic>;
      expect((raw.single as Map<String, dynamic>).keys.toSet(), <String>{'id', 'png', 'method', 'label', 'createdAt'});
      expect(base64Decode((raw.single as Map<String, dynamic>)['png'] as String), _png(1));
    });

    test('damaged data is skipped, never thrown', () async {
      SharedPreferences.setMockInitialValues(<String, Object>{
        SavedSignatureStore.keyFor(SavedKind.signature): '[{"id":"a","png":"!!!","method":"draw"}, 5, {"id":"b","png":"${base64Encode(_png(9))}","method":"draw","createdAt":"2026-01-01T00:00:00Z"}]',
        SavedSignatureStore.keyFor(SavedKind.initials): 'not json',
      });
      final store = SavedSignatureStore(await SharedPreferences.getInstance());
      expect(store.list(SavedKind.signature).map((item) => item.id), <String>['b']);
      expect(store.list(SavedKind.initials), isEmpty);
    });
  });

  group('SignaturePanel with saved signatures', () {
    Future<void> pump(WidgetTester tester, SavedSignatureStore store, void Function(SignatureResult) onResult, {SavedKind kind = SavedKind.signature, Brightness brightness = Brightness.light}) {
      return tester.pumpWidget(
        MaterialApp(
          theme: neoCloudTheme(brightness),
          home: Scaffold(
            body: SingleChildScrollView(
              child: SignaturePanel(store: store, kind: kind, initialName: 'Dana', onCancel: () {}, onResult: onResult),
            ),
          ),
        ),
      );
    }

    testWidgets('saved ones are one tap, and the right kind is shown', (tester) async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.typed, label: 'Anna');
      await store.add(SavedKind.signature, _png(2), method: SignatureMethod.drawn);
      await store.add(SavedKind.initials, _png(3), method: SignatureMethod.drawn);
      SignatureResult? result;
      await pump(tester, store, (value) => result = value);
      expect(find.text('Saved on this device'), findsOneWidget);
      expect(find.byType(Image), findsNWidgets(2), reason: 'two signatures, not the initials');
      await tester.tap(find.byKey(ValueKey<String>('saved-${store.list(SavedKind.signature).last.id}')));
      expect(result!.png, _png(1));
      expect(result!.method, SignatureMethod.typed);
      expect(result!.typedText, 'Anna');
    });

    testWidgets('initials show only saved initials', (tester) async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      await store.add(SavedKind.initials, _png(3), method: SignatureMethod.drawn);
      await pump(tester, store, (_) {}, kind: SavedKind.initials);
      expect(find.byType(Image), findsOneWidget);
    });

    testWidgets('Remove drops one, Remove all clears the list', (tester) async {
      final store = await _store();
      final first = await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      await store.add(SavedKind.signature, _png(2), method: SignatureMethod.drawn);
      await pump(tester, store, (_) {});
      await tester.tap(find.byKey(ValueKey<String>('saved-remove-${first.id}')));
      await tester.pump();
      expect(store.list(SavedKind.signature), hasLength(1));
      expect(find.byType(Image), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey<String>('saved-remove-all')));
      await tester.pump();
      expect(store.list(SavedKind.signature), isEmpty);
      expect(find.text('Saved on this device'), findsNothing);
    });

    testWidgets('Save on this device is on by default, says it stays here, and saves what is applied', (tester) async {
      final store = await _store();
      SignatureResult? result;
      await pump(tester, store, (value) => result = value);
      expect(tester.widget<CheckboxListTile>(find.byKey(const ValueKey<String>('save-on-device'))).value, isTrue);
      expect(find.textContaining('stays on this device only'), findsOneWidget);
      await tester.tap(find.text('Type'));
      await tester.pump();
      await tester.runAsync(() async {
        await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
        await Future<void>.delayed(const Duration(milliseconds: 400));
      });
      await tester.pump();
      expect(result, isNotNull);
      final saved = store.list(SavedKind.signature);
      expect(saved, hasLength(1));
      expect(saved.single.label, 'Dana');
      expect(saved.single.method, SignatureMethod.typed);
    });

    testWidgets('with the box unticked nothing is kept', (tester) async {
      final store = await _store();
      SignatureResult? result;
      await pump(tester, store, (value) => result = value);
      await tester.tap(find.byKey(const ValueKey<String>('save-on-device')));
      await tester.pump();
      await tester.tap(find.text('Type'));
      await tester.pump();
      await tester.runAsync(() async {
        await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
        await Future<void>.delayed(const Duration(milliseconds: 400));
      });
      expect(result, isNotNull);
      expect(store.list(SavedKind.signature), isEmpty);
    });

    testWidgets('saved tiles are white paper in the dark theme too', (tester) async {
      final store = await _store();
      await store.add(SavedKind.signature, _png(1), method: SignatureMethod.drawn);
      await pump(tester, store, (_) {}, brightness: Brightness.dark);
      final tile = tester.widget<Container>(find.ancestor(of: find.byType(Image), matching: find.byType(Container)).first);
      expect((tile.decoration! as BoxDecoration).color, Colors.white);
    });

    testWidgets('without a store there is no saving and no list', (tester) async {
      await tester.pumpWidget(
        MaterialApp(theme: neoCloudTheme(Brightness.light), home: Scaffold(body: SingleChildScrollView(child: SignaturePanel(onCancel: () {}, onResult: (_) {})))),
      );
      expect(find.byKey(const ValueKey<String>('save-on-device')), findsNothing);
      expect(find.text('Saved on this device'), findsNothing);
    });
  });
}
