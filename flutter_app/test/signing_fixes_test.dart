import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/l10n/gen/app_l10n.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';
// ignore: depend_on_referenced_packages
import 'package:plugin_platform_interface/plugin_platform_interface.dart';

import 'helpers/public_signing_harness.dart';

Finder key(String value) => find.byKey(ValueKey<String>(value));

class _Saver extends FilePicker with MockPlatformInterfaceMixin {
  final List<(String?, int)> saved = <(String?, int)>[];

  @override
  Future<String?> saveFile({
    String? dialogTitle,
    String? fileName,
    String? initialDirectory,
    FileType type = FileType.any,
    List<String>? allowedExtensions,
    Uint8List? bytes,
    bool lockParentWindow = false,
  }) async {
    saved.add((fileName, bytes?.length ?? 0));
    return null;
  }
}

String _today() => toSigningIsoDate(DateTime.now());

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('date fields', () {
    testSigning('a Today button fills in the current date and saves it', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('d', FieldType.date, name: 'Date', prefs: const <String, Object?>{'format': 'YYYY-MM-DD'})]));
      await pumpSigning(tester, api);
      expect(key('date-today-button'), findsOneWidget);
      expect(find.text('Today'), findsOneWidget);
      await tapKey(tester, 'date-today-button');
      await tester.pump(const Duration(seconds: 2));
      expect(tester.widget<TextField>(key('typed-input')).controller!.text, _today());
      expect(api.calls, contains('save:d'));
    });

    testSigning('the date picker opens on today', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(consentedAt: consented, fields: <SigningField>[field('d', FieldType.date, name: 'Date')]));
      await pumpSigning(tester, api);
      await tapKey(tester, 'date-picker-button');
      final picker = tester.widget<CalendarDatePicker>(find.byType(CalendarDatePicker));
      final now = DateTime.now();
      expect(DateUtils.isSameDay(picker.initialDate, now), isTrue);
    });

    testSigning('a field set to default to today arrives filled in', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(consentedAt: consented, fields: <SigningField>[field('d', FieldType.date, name: 'Date', prefs: const <String, Object?>{'autoToday': true, 'format': 'YYYY-MM-DD'})]),
      );
      await pumpSigning(tester, api);
      await tester.pump(const Duration(seconds: 2));
      expect(tester.widget<TextField>(key('typed-input')).controller!.text, _today());
      expect(api.calls, contains('save:d'));
    });

    test('the creator toggle is labelled as a default and writes autoToday', () {
      final spec = FieldTypes.of(FieldType.date).prefs.firstWhere((pref) => pref.key == PrefKeys.defaultToday);
      expect(spec.kind, PrefKind.toggle);
      expect(PrefKeys.defaultToday, 'autoToday');
      expect(spec.label(lookupAppL10n(const Locale('en'))), "Default to today's date");
    });
  });

  group('white paper', () {
    testSigning('the review shows ink on a white tile in the dark theme', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(
          consentedAt: consented,
          fields: <SigningField>[field('s', FieldType.signature, name: 'Signature')],
          myValues: const <SigningValue>[SigningValue(fieldId: 's', hasFile: true, method: SignatureMethod.drawn)],
        ),
      );
      await pumpSigning(tester, api, brightness: Brightness.dark);
      final tile = tester.widget<Container>(key('paper-tile'));
      expect((tile.decoration! as BoxDecoration).color, Colors.white);
    });

    testWidgets('pages are white sheets with dark ink in every theme', (tester) async {
      for (final brightness in Brightness.values) {
        await tester.pumpWidget(
          MaterialApp(
            theme: neoCloudTheme(brightness),
            home: Scaffold(
              body: SigningPageList(
                pageSizes: const <Size>[Size(595, 842)],
                pageContent: (context, index, size) => const SizedBox.expand(),
                overlayBuilder: (context, index, size) => <Widget>[const Text('ink', key: ValueKey<String>('ink'))],
              ),
            ),
          ),
        );
        await tester.pump();
        final sheet = tester.widgetList<DecoratedBox>(find.byType(DecoratedBox)).where((box) => box.decoration is BoxDecoration && (box.decoration as BoxDecoration).color == Colors.white);
        expect(sheet, isNotEmpty, reason: '$brightness page is white');
        final ink = DefaultTextStyle.of(tester.element(find.byKey(const ValueKey<String>('ink')))).style.color!;
        expect(ink.computeLuminance(), lessThan(0.1), reason: '$brightness text on paper is dark');
      }
    });
  });

  group('privacy: no other names', () {
    test('the public models never keep a name list', () {
      final view = PublicSigningView.fromJson(<String, dynamic>{
        'state': 'completed',
        'document': <String, dynamic>{'title': 'x'},
        'party': <String, dynamic>{'id': 'p', 'name': 'Me'},
        'parties': <Object>[
          <String, dynamic>{'name': 'Someone Else'},
        ],
        'progress': <String, dynamic>{'done': 2, 'total': 3},
      });
      expect(view.progress.done, 2);
      expect(view.progress.total, 3);
      expect(view.toString(), isNot(contains('Someone Else')));
    });

    testSigning('while waiting only a number is shown', (tester) async {
      final api = FakePublicSigningApi(view: viewOf(state: PublicSigningState.waiting, waitingCount: 1, consentedAt: consented));
      await pumpSigning(tester, api);
      expect(find.textContaining('1 more person has to sign first'), findsOneWidget);
    });
  });

  group('downloads', () {
    late _Saver saver;
    setUp(() {
      saver = _Saver();
      FilePicker.platform = saver;
    });

    testSigning('the signer gets the signed PDF, and a way to leave the signature pages out', (tester) async {
      final api = FakePublicSigningApi(
        view: viewOf(state: PublicSigningState.completed, consentedAt: consented, progress: const PublicProgress(done: 2, total: 2)),
        finalBytes: Uint8List.fromList(<int>[1, 2, 3, 4]),
      )..cleanBytes = Uint8List.fromList(<int>[1, 2]);
      await pumpSigning(tester, api);
      expect(find.text('Download signed document'), findsOneWidget);
      expect(find.text('Download without signature pages'), findsOneWidget);
      await tapKey(tester, 'download-final');
      await tapKey(tester, 'download-final-clean');
      expect(api.finalRequests, <bool>[true, false]);
    });

    test('the fake APIs keep the two files apart', () async {
      final api = FakePublicSigningApi(view: viewOf(), finalBytes: Uint8List.fromList(<int>[1, 2, 3]))..cleanBytes = Uint8List.fromList(<int>[9]);
      expect(await api.finalPdf(), <int>[1, 2, 3]);
      expect(await api.finalPdf(certificate: false), <int>[9]);
      final own = FakeSigningApi();
      expect(await own.cleanPdfForNode('n1'), isNotEmpty);
      expect(own.cleanRequests, <String>['n1']);
    });
  });
}
