import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/src/l10n/app_language.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';
// ignore: depend_on_referenced_packages
import 'package:plugin_platform_interface/plugin_platform_interface.dart';

import 'helpers/public_signing_harness.dart';

Finder key(String value) => find.byKey(ValueKey<String>(value));

class _Picker extends FilePicker with MockPlatformInterfaceMixin {
  _Picker(this.file);

  final PlatformFile? file;

  @override
  Future<FilePickerResult?> pickFiles({
    String? dialogTitle,
    String? initialDirectory,
    FileType type = FileType.any,
    List<String>? allowedExtensions,
    dynamic onFileLoading,
    bool allowCompression = true,
    int compressionQuality = 0,
    bool allowMultiple = false,
    bool withData = false,
    bool withReadStream = false,
    bool lockParentWindow = false,
    bool readSequential = false,
  }) async => file == null ? null : FilePickerResult(<PlatformFile>[file!]);
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('states of a link', () {
    testSigning('waiting: says how many are first, then polls and opens the fields when the turn comes', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(state: PublicSigningState.waiting, waitingCount: 3, consentedAt: consented),
      );
      await pumpSigning(tester, api);
      expect(find.text("It's not your turn yet"), findsOneWidget);
      expect(find.textContaining('3 more people have to sign first'), findsOneWidget);
      expect(find.textContaining('Landlord'), findsNothing, reason: 'no names, only a number');
      expect(api.calls.where((call) => call == 'view'), hasLength(1));

      await tester.pump(const Duration(seconds: 21));
      expect(api.calls.where((call) => call == 'view'), hasLength(2), reason: 'it asked again');
      expect(find.text("It's not your turn yet"), findsOneWidget);

      api.serverMoves(viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'Full name')]));
      await tester.pump(const Duration(seconds: 21));
      await settle(tester);
      expect(find.text("It's not your turn yet"), findsNothing);
      expect(key('typed-input'), findsOneWidget);
      expect(find.text('1 of 1'), findsOneWidget);
    });

    testSigning('done while others still have to sign', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(
          state: PublicSigningState.completed,
          consentedAt: consented,
          progress: const PublicProgress(done: 1, total: 3),
        ),
      );
      await pumpSigning(tester, api);
      expect(find.text("You've signed this document"), findsOneWidget);
      expect(find.textContaining('Thanks! 1 of 3 have signed'), findsOneWidget);
      expect(find.textContaining('Landlord'), findsNothing);
      expect(key('download-final'), findsNothing);
    });

    testSigning('done by everyone: the signed copy can be downloaded', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(
          state: PublicSigningState.completed,
          consentedAt: consented,
          progress: const PublicProgress(done: 2, total: 2),
        ),
        finalBytes: tinyPng,
      );
      final controller = await pumpSigning(tester, api);
      expect(find.text('Document completed'), findsOneWidget);
      expect(key('download-final'), findsOneWidget);
      await tapKey(tester, 'completed-close');
      // Outside a browser, Close leaves for the app (the web page shows a note instead).
      expect(controller.signingFlow, isNull);
    });

    testSigning('done by everyone but the signed copy is still being made', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(
          state: PublicSigningState.completed,
          progress: const PublicProgress(done: 2, total: 2),
        ),
      );
      await pumpSigning(tester, api);
      expect(find.textContaining('is being prepared'), findsOneWidget);
      expect(key('download-final'), findsNothing);
      api.finalBytes = tinyPng;
      await tester.pump(const Duration(seconds: 5));
      await settle(tester);
      expect(key('download-final'), findsOneWidget);
    });

    for (final entry in <PublicSigningState, String>{
      PublicSigningState.declined: 'This document was declined',
      PublicSigningState.expired: 'This request has expired',
      PublicSigningState.canceled: 'The sender canceled this request',
      PublicSigningState.locked: "This document can't be signed right now",
    }.entries) {
      testSigning('${entry.key.name} has a clear message', (tester) async {
        await pumpSigning(tester, FakePublicSigningApi(view: viewOf(state: entry.key)));
        expect(find.text(entry.value), findsOneWidget);
        expect(tester.takeException(), isNull);
      });
    }

    testSigning('an unknown link says it is not valid anymore', (tester) async {
      final api = FakePublicSigningApi(view: viewOf())..failNext = const SigningException(SigningErrorKind.notFound, statusCode: 404);
      await pumpSigning(tester, api);
      expect(find.text("This link isn't valid anymore"), findsOneWidget);
      expect(find.textContaining(testToken), findsNothing);
    });

    testSigning('too many requests: a retry that works', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'City')]))
        ..failNext = const SigningException(SigningErrorKind.rateLimited, statusCode: 429);
      await pumpSigning(tester, api);
      expect(find.text('Too many requests'), findsOneWidget);
      await tapKey(tester, 'load-retry');
      expect(find.text('City'), findsWidgets);
    });

    testSigning('offline: a retry that works', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'City')]))
        ..failNext = const SigningException(SigningErrorKind.network);
      await pumpSigning(tester, api);
      expect(find.text("Can't reach the server"), findsOneWidget);
      await tapKey(tester, 'load-retry');
      expect(find.text('City'), findsWidgets);
    });
  });

  group('resilience', () {
    testSigning('offline while typing: the entry is kept, a banner shows, and it is saved once back online', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'City')]));
      await pumpSigning(tester, api);
      api.failTimes(2, const SigningException(SigningErrorKind.network));
      await tester.enterText(key('typed-input'), 'Berlin');
      await tester.pump(const Duration(milliseconds: 700));
      await tester.pump();
      expect(find.textContaining('No connection'), findsOneWidget);
      expect(tester.widget<TextField>(key('typed-input')).controller!.text, 'Berlin');
      expect(api.calls.where((call) => call.startsWith('save:')), isEmpty);

      await tester.pump(const Duration(seconds: 1)); // first retry fails
      await tester.pump(const Duration(seconds: 2)); // second retry works
      await settle(tester);
      expect(api.calls, contains('save:t'));
      expect(find.textContaining('No connection'), findsNothing);
    });

    testSigning('"Try now" saves right away', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'City')]));
      await pumpSigning(tester, api);
      api.failNext = const SigningException(SigningErrorKind.server, statusCode: 503);
      await tester.enterText(key('typed-input'), 'Berlin');
      await tester.pump(const Duration(milliseconds: 700));
      await tester.pump();
      expect(find.text('Try now'), findsOneWidget);
      await tester.tap(find.text('Try now'));
      await settle(tester);
      expect(api.calls, contains('save:t'));
    });

    testSigning('a value the server refuses is explained next to the field', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'City')]));
      await pumpSigning(tester, api);
      api.failNext = const SigningException(SigningErrorKind.invalid, statusCode: 400, message: 'That city is not allowed.');
      await tester.enterText(key('typed-input'), 'Atlantis');
      await tester.pump(const Duration(milliseconds: 700));
      await settle(tester);
      expect(find.text('That city is not allowed.'), findsOneWidget);
    });

    testSigning('resumes where the signer left: stored values are shown, the first open field is active', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(
          consentedAt: consented,
          signerName: 'Ada',
          fields: <SigningField>[field('a', FieldType.text, name: 'First', y: 0.1), field('b', FieldType.text, name: 'Second', y: 0.2)],
          myValues: const <SigningValue>[SigningValue(fieldId: 'a', value: 'Done already')],
        ),
      );
      await pumpSigning(tester, api);
      expect(find.text('2 of 2'), findsOneWidget);
      await tapKey(tester, 'field-back');
      expect(tester.widget<TextField>(key('typed-input')).controller!.text, 'Done already');
    });

    testSigning('the server naming empty fields on Finish jumps to the first one', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(consentedAt: consented, fields: <SigningField>[field('a', FieldType.text, name: 'First', y: 0.1), field('b', FieldType.text, name: 'Second', y: 0.2)]),
      );
      await pumpSigning(tester, api);
      await tester.enterText(key('typed-input'), 'x');
      await tapKey(tester, 'field-next');
      await tester.enterText(key('typed-input'), 'y');
      await tapKey(tester, 'field-next');
      expect(find.text('Review your entries'), findsOneWidget);
      await tester.pump(const Duration(seconds: 1)); // the autosave goes first
      api.failNext = const SigningException(SigningErrorKind.invalid, statusCode: 400, code: 'FIELDS_INCOMPLETE', fieldIds: <String>['b']);
      await tapKey(tester, 'review-finish');
      await tester.pump(const Duration(seconds: 1));
      await settle(tester);
      expect(find.text('2 of 2'), findsOneWidget);
    });

    testSigning('finishing twice quickly sends only one request', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('a', FieldType.text, name: 'First')]));
      await pumpSigning(tester, api);
      await tester.enterText(key('typed-input'), 'x');
      await tapKey(tester, 'field-next');
      final finish = key('review-finish');
      await tester.tap(finish);
      await tester.tap(finish, warnIfMissed: false);
      await settle(tester);
      expect(api.calls.where((call) => call == 'complete'), hasLength(1));
    });
  });

  group('decline', () {
    testSigning('asks for a reason, confirms, and ends on the declined card', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('a', FieldType.text, name: 'First')]));
      await pumpSigning(tester, api);
      await tester.tap(find.byTooltip('More options'));
      await settle(tester);
      await tester.tap(find.text('Decline to sign'));
      await settle(tester);
      expect(find.text('Decline to sign?'), findsOneWidget);
      await tester.enterText(key('decline-reason'), 'Wrong rent');
      await tapKey(tester, 'decline-confirm');
      expect(api.lastDeclineReason, 'Wrong rent');
      expect(find.text('This document was declined'), findsOneWidget);
    });
  });

  group('signature', () {
    Future<void> drawAndApply(WidgetTester tester, {bool alsoInitials = false}) async {
      final pad = find.byType(SignaturePad);
      final gesture = await tester.startGesture(tester.getCenter(pad));
      await gesture.moveBy(const Offset(60, 20));
      await gesture.moveBy(const Offset(40, -30));
      await gesture.up();
      await tester.pump();
      if (alsoInitials) {
        await tester.tap(find.text('Use this for my initials too'));
        await tester.pump();
      }
      await tester.runAsync(() async {
        await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
        await Future<void>.delayed(const Duration(milliseconds: 400));
      });
      await settle(tester);
    }

    testSigning('draws a signature, uploads it, and offers the same for initials', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(
          consentedAt: consented,
          signerName: 'Ada',
          fields: <SigningField>[field('sig', FieldType.signature, name: 'Signature', y: 0.2), field('ini', FieldType.initials, name: 'Initials', y: 0.4)],
        ),
      );
      await pumpSigning(tester, api);
      await tapKey(tester, 'signature-add');
      expect(find.text('Add your signature'), findsWidgets);
      await drawAndApply(tester, alsoInitials: true);

      expect(api.calls, containsAll(<String>['upload:sig', 'upload:ini']));
      expect(api.uploadMethods['sig'], SignatureMethod.drawn);
      expect(key('signature-preview'), findsOneWidget);

      await tapKey(tester, 'field-next');
      expect(key('signature-preview'), findsOneWidget, reason: 'initials already filled');
      await tapKey(tester, 'signature-remove');
      expect(api.calls, contains('save:ini'));
      expect(key('signature-add'), findsOneWidget);
    });

    testSigning('a typed signature sends its text', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(consentedAt: consented, signerName: 'Ada Lovelace', fields: <SigningField>[field('sig', FieldType.signature, name: 'Signature')]),
      );
      await pumpSigning(tester, api);
      await tapKey(tester, 'signature-add');
      await tester.tap(find.text('Type'));
      await settle(tester);
      await tester.runAsync(() async {
        await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
        await Future<void>.delayed(const Duration(milliseconds: 400));
      });
      await settle(tester);
      expect(api.uploadMethods['sig'], SignatureMethod.typed);
    });
  });

  group('uploads', () {
    testSigning('an image is chosen, shown and uploaded; too large is refused', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('img', FieldType.image, name: 'ID photo')]));
      await pumpSigning(tester, api);
      FilePicker.platform = _Picker(PlatformFile(name: 'huge.png', size: 9 * 1024 * 1024, bytes: Uint8List(9 * 1024 * 1024)));
      await tapKey(tester, 'upload-pick');
      expect(find.textContaining('too large'), findsOneWidget);
      expect(api.calls.where((call) => call.startsWith('upload:')), isEmpty);

      FilePicker.platform = _Picker(PlatformFile(name: 'me.png', size: tinyPng.length, bytes: tinyPng));
      await tapKey(tester, 'upload-pick');
      expect(api.calls, contains('upload:img'));
      expect(key('upload-preview'), findsOneWidget);
    });
  });

  group('layout', () {
    final fields = <SigningField>[
      field('a', FieldType.text, name: 'A fairly long label for a field on a narrow screen', y: 0.1),
      field('r', FieldType.radio, name: 'Choose one', y: 0.2, prefs: const <String, Object?>{'options': <String>['First choice is long enough to wrap', 'Second', 'Third']}),
    ];

    for (final size in <Size>[const Size(412, 915), const Size(320, 640), const Size(1440, 900)]) {
      for (final scale in <double>[1, 1.3]) {
        testSigning('${size.width.toInt()} px wide at ${scale}x text: nothing overflows', (tester) async {
          final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: fields));
          await pumpSigning(tester, api, size: size, textScale: scale);
          expect(tester.takeException(), isNull);
          expect(key('field-next'), findsOneWidget);
          await tester.enterText(key('typed-input'), 'abc');
          await tapKey(tester, 'field-next');
          expect(tester.takeException(), isNull);
          expect(key('choice-Second'), findsOneWidget);
          // The main action is a large touch target.
          expect(tester.getSize(key('field-next')).height, greaterThanOrEqualTo(44));
          expect(tester.getSize(key('choice-Second')).height, greaterThanOrEqualTo(44));
        });
      }
    }

    testSigning('intro and review fit a phone at 320 px with large text', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(fields: fields));
      await pumpSigning(tester, api, size: const Size(320, 640), textScale: 1.3);
      expect(tester.takeException(), isNull);
      expect(key('consent-continue'), findsOneWidget);
    });
  });

  group('language', () {
    testSigning('follows the document and can be switched', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(locale: 'de', fields: <SigningField>[field('t', FieldType.text, name: 'Name')]));
      await pumpSigning(tester, api);
      await settle(tester);
      expect(currentAppLanguage, AppLanguage.german);
      expect(find.text('Elektronisch unterschreiben'), findsOneWidget);

      await tester.tap(find.byTooltip('Sprache'));
      await settle(tester);
      await tester.tap(find.text('English'));
      await settle(tester);
      expect(find.text('Sign electronically'), findsOneWidget);
    });
  });

  group('privacy', () {
    testSigning('the token is never shown', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('t', FieldType.text, name: 'Name')]));
      await pumpSigning(tester, api);
      final texts = <String>[for (final text in tester.widgetList<Text>(find.byType(Text))) text.data ?? ''];
      expect(texts.where((text) => text.contains(testToken)), isEmpty);
      expect(tester.takeException(), isNull);
    });
  });
}
