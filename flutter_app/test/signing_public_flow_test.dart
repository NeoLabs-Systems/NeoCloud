import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

import 'helpers/public_signing_harness.dart';

Finder key(String value) => find.byKey(ValueKey<String>(value));

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('consent', () {
    testSigning('has to be agreed to, with a name, before anything is asked', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(fields: <SigningField>[field('t', FieldType.text, name: 'Full name')]));
      await pumpSigning(tester, api);

      expect(find.text('Sign “Rental agreement”'), findsOneWidget);
      expect(find.text('I agree to sign this document electronically.'), findsWidgets);
      expect(key('typed-input'), findsNothing);

      await tapKey(tester, 'consent-continue');
      expect(api.calls, isNot(contains('consent')));
      expect(find.text('Please agree to continue.'), findsOneWidget);
      expect(find.text('Please enter your full name.'), findsOneWidget);

      await tapKey(tester, 'consent-check');
      await tester.enterText(key('consent-name'), 'Ada Lovelace');
      await tapKey(tester, 'consent-continue');
      expect(api.calls, contains('consent'));
      expect(api.lastSignerName, 'Ada Lovelace');
      expect(key('typed-input'), findsOneWidget);
      expect(find.text('Full name'), findsWidgets);
    });

    testSigning('a signer who consented earlier goes straight to the fields', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'City')]));
      await pumpSigning(tester, api);
      expect(key('consent-continue'), findsNothing);
      expect(find.text('City'), findsWidgets);
      expect(find.text('1 of 1'), findsOneWidget);
    });
  });

  group('guided fill', () {
    final fields = <SigningField>[
      field('text', FieldType.text, name: 'Full name', y: 0.05),
      field('number', FieldType.number, name: 'Persons', y: 0.10, prefs: const <String, Object?>{'min': 1, 'max': 9}),
      field('phone', FieldType.phone, name: 'Phone', y: 0.15),
      field('date', FieldType.date, name: 'Start', y: 0.20),
      field('cells', FieldType.cells, name: 'Code', y: 0.25, prefs: const <String, Object?>{'cells': 4}),
      field('check', FieldType.checkbox, name: 'I accept the house rules', y: 0.30),
      field('radio', FieldType.radio, name: 'Floor', y: 0.35, prefs: const <String, Object?>{'options': <String>['Ground', 'Upstairs']}),
      field('select', FieldType.select, name: 'Parking', y: 0.40, prefs: const <String, Object?>{'options': <String>['None', 'Garage']}),
      field('multi', FieldType.multiple, name: 'Extras', y: 0.45, prefs: const <String, Object?>{'options': <String>['Cellar', 'Balcony']}),
      field('note', FieldType.text, name: 'Note', y: 0.50, required: false),
    ];

    Future<FakePublicSigningApi> start(WidgetTester tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: fields));
      await pumpSigning(tester, api);
      return api;
    }

    testSigning('steps through every type, saves each value, and finishes', (tester) async {
      final api = await start(tester);

      await tester.enterText(key('typed-input'), 'Ada Lovelace');
      await tapKey(tester, 'field-next');
      await tester.enterText(key('typed-input'), '3');
      await tapKey(tester, 'field-next');
      await tester.enterText(key('typed-input'), '+49 151 2345678');
      await tapKey(tester, 'field-next');
      await tester.enterText(key('typed-input'), '07.03.2026');
      await tapKey(tester, 'field-next');
      await tester.enterText(key('typed-input'), 'AB12');
      await tapKey(tester, 'field-next');
      await tapKey(tester, 'checkbox-input');
      await tapKey(tester, 'field-next');
      await tapKey(tester, 'choice-Upstairs');
      await tapKey(tester, 'field-next');
      await tester.tap(key('select-input'));
      await settle(tester);
      await tester.tap(find.text('Garage').last);
      await settle(tester);
      await tapKey(tester, 'field-next');
      await tapKey(tester, 'choice-Cellar');
      await tapKey(tester, 'choice-Balcony');
      await tapKey(tester, 'field-next');
      // The optional note can be skipped.
      expect(key('field-skip'), findsOneWidget);
      await tapKey(tester, 'field-skip');

      expect(find.text('Review your entries'), findsOneWidget);
      await tester.pump(const Duration(seconds: 1));
      expect(api.calls.where((call) => call.startsWith('save:')).toSet(), <String>{
        'save:text', 'save:number', 'save:phone', 'save:date', 'save:cells', 'save:check', 'save:radio', 'save:select', 'save:multi',
      });

      await tapKey(tester, 'review-finish');
      expect(api.calls, contains('complete'));
      expect(find.text('You’ve signed this document'), findsNothing);
    });

    testSigning('a required field blocks Next with a message, an optional one can be skipped', (tester) async {
      await start(tester);
      await tapKey(tester, 'field-next');
      expect(find.text('This is required.'), findsOneWidget);
      expect(find.text('1 of 10'), findsOneWidget);
      expect(key('field-skip'), findsNothing);
    });

    testSigning('a wrong value is explained once the signer leaves the field', (tester) async {
      await start(tester);
      await tester.enterText(key('typed-input'), 'Ada');
      await tapKey(tester, 'field-next');
      await tester.enterText(key('typed-input'), '12');
      await tapKey(tester, 'field-next');
      expect(find.text('Please enter 9 or less.'), findsOneWidget);
      expect(find.text('2 of 10'), findsOneWidget);
    });

    testSigning('the field list jumps to any field', (tester) async {
      await start(tester);
      await tapKey(tester, 'field-list-button');
      expect(find.text('All fields'), findsWidgets);
      await tapKey(tester, 'list-field-radio');
      expect(find.text('7 of 10'), findsOneWidget);
      expect(key('choice-Ground'), findsOneWidget);
    });

    testSigning('tapping a highlighted field on the page makes it the active one', (tester) async {
      await start(tester);
      final spots = find.byWidgetPredicate((widget) => widget.key is ValueKey<String> && (widget.key! as ValueKey<String>).value.startsWith('hotspot-phone'));
      expect(spots, findsOneWidget);
      await tester.tap(spots);
      await settle(tester);
      expect(find.text('3 of 10'), findsOneWidget);
    });

    testSigning('Back returns to the previous field with its value', (tester) async {
      await start(tester);
      await tester.enterText(key('typed-input'), 'Ada');
      await tapKey(tester, 'field-next');
      await tapKey(tester, 'field-back');
      expect(find.text('1 of 10'), findsOneWidget);
      expect(tester.widget<TextField>(key('typed-input')).controller!.text, 'Ada');
    });

    testSigning('Finish lists what is missing and jumps to the field', (tester) async {
      await start(tester);
      await tester.enterText(key('typed-input'), 'Ada');
      // Open the review straight away through the list of fields.
      final flow = (tester.state(find.byType(SigningPublicPage)) as dynamic).widget.flow as PublicSigningController;
      flow.go(9);
      flow.next();
      await settle(tester);
      expect(find.text('Review your entries'), findsOneWidget);
      await tapKey(tester, 'review-finish');
      expect(find.text('8 required fields are missing'), findsOneWidget);
      expect(api(tester).calls, isNot(contains('complete')));
      await tapKey(tester, 'review-row-number');
      expect(find.text('2 of 10'), findsOneWidget);
    });
  });
}

FakePublicSigningApi api(WidgetTester tester) =>
    ((tester.state(find.byType(SigningPublicPage)) as dynamic).widget.flow as PublicSigningController).api as FakePublicSigningApi;
