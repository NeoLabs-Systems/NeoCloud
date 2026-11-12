import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/imports/import_progress.dart';
import 'package:neocloud/src/pairing/pairing_link.dart';
import 'package:neocloud/src/pairing/scanned_code.dart';
import 'package:neocloud/src/search/snippet.dart';

const String _code = 'Q2hhbmdlTWVQbGVhc2VJdHNBVGVzdENvZGVYeXo';

ImportJob _job({String status = 'running', int totalFiles = 0, int totalBytes = 0, int doneFiles = 0, int doneBytes = 0}) =>
    ImportJob.fromJson(<String, dynamic>{
      'id': 'j1',
      'status': status,
      'totalFiles': totalFiles,
      'totalBytes': totalBytes,
      'doneFiles': doneFiles,
      'doneBytes': doneBytes,
    });

void main() {
  group('snippet highlights', () {
    test('marked terms become highlighted runs between plain ones', () {
      expect(parseSnippet('the \u0002quarterly\u0003 report for \u0002Q3\u0003.'), const <SnippetPart>[
        SnippetPart('the '),
        SnippetPart('quarterly', highlighted: true),
        SnippetPart(' report for '),
        SnippetPart('Q3', highlighted: true),
        SnippetPart('.'),
      ]);
    });

    test('adjacent marks merge, whitespace folds, stray and empty marks vanish', () {
      expect(parseSnippet('\u0002foo\u0003\u0002bar\u0003\n\n baz\u0003\u0002\u0003'), const <SnippetPart>[
        SnippetPart('foobar', highlighted: true),
        SnippetPart(' baz'),
      ]);
    });

    test('an unclosed mark runs to the end; no marks is one plain run', () {
      expect(parseSnippet('see \u0002invoice'), const <SnippetPart>[SnippetPart('see '), SnippetPart('invoice', highlighted: true)]);
      expect(parseSnippet('plain text'), const <SnippetPart>[SnippetPart('plain text')]);
      expect(parseSnippet(''), isEmpty);
      expect(plainSnippet('a \u0002b\u0003 c'), 'a b c');
    });
  });

  group('pairing links', () {
    test('the app link gives the server and the code', () {
      final link = parsePairingLink('neocloud://pair?server=https%3A%2F%2Fcloud.example.com%2F&code=$_code')!;
      expect(link.server.toString(), 'https://cloud.example.com');
      expect(link.code, _code);
      expect(parsePairingLink(link.appLink.toString())!.code, _code);
    });

    test('the web fallback works, also under a sub-path, and round-trips', () {
      final link = parsePairingLink('https://example.com/cloud/app/pair?code=$_code')!;
      expect(link.server.toString(), 'https://example.com/cloud');
      final web = pairingWebLink(link.server, link.code);
      expect(web.toString(), 'https://example.com/cloud/app/pair?code=$_code');
      expect(parsePairingLink(web.toString())!.server, link.server);
    });

    test('other codes, schemes, short codes and credentials are refused', () {
      expect(parsePairingLink('https://example.com/some/page?code=$_code'), isNull);
      expect(parsePairingLink('neocloud://other?server=https://a.example&code=$_code'), isNull);
      expect(parsePairingLink('neocloud://pair?server=ftp://a.example&code=$_code'), isNull);
      expect(parsePairingLink('neocloud://pair?server=https://user:pw@a.example&code=$_code'), isNull);
      expect(parsePairingLink('neocloud://pair?server=https://a.example&code=short'), isNull);
      expect(parsePairingLink('WIFI:S:home;T:WPA;P:secret;;'), isNull);
    });

    test('the countdown reads minutes and seconds and stops at zero', () {
      expect(formatCountdown(const Duration(minutes: 5)), '5:00');
      expect(formatCountdown(const Duration(seconds: 61, milliseconds: 200)), '1:02');
      expect(formatCountdown(const Duration(seconds: 9)), '0:09');
      expect(formatCountdown(const Duration(seconds: -3)), '0:00');
    });
  });

  group('scanned codes', () {
    test('a pairing link signs this phone in', () {
      final code = parseScannedCode('neocloud://pair?server=https%3A%2F%2Fcloud.example.com&code=$_code');
      expect(code, isA<PairingScan>());
      expect(code!.server.toString(), 'https://cloud.example.com');
    });

    test('a browser sign-in code gives the server, challenge and secret', () {
      const secret = 'aGVsbG8td29ybGQtdGhpcy1pcy1hLXNlY3JldA';
      final code = parseScannedCode(
        'neocloud://qr-login?v=1&backend=https%3A%2F%2Fcloud.example.com%2F&challenge=0b6f7a52-2b7e-4c1f-9a7d-3f1d2c4b5a69&secret=$secret',
      );
      expect(code, isA<QrLoginScanPayload>());
      final login = code! as QrLoginScanPayload;
      expect(login.server.toString(), 'https://cloud.example.com');
      expect(login.challengeId, '0b6f7a52-2b7e-4c1f-9a7d-3f1d2c4b5a69');
      expect(login.secret, secret);
      expect(sameServer(login.server, 'https://cloud.example.com/'), isTrue);
      expect(sameServer(login.server, 'https://other.example.com'), isFalse);
    });

    test('a device approval link gives the server and the user code', () {
      final code = parseScannedCode('https://example.com/cloud/app/device?code=abcd-efgh');
      expect(code, isA<DeviceCodeScan>());
      final device = code! as DeviceCodeScan;
      expect(device.server.toString(), 'https://example.com/cloud');
      expect(device.userCode, 'ABCD-EFGH');
    });

    test('anything else is refused', () {
      for (final raw in <String>[
        'https://example.com/',
        'https://example.com/app/device?code=ABCD',
        'https://example.com/app/device?code=ABCD-EFG1',
        'neocloud://qr-login?v=2&backend=https%3A%2F%2Fa.example&challenge=0b6f7a52-2b7e&secret=aGVsbG8td29ybGQtdGhpcy1p',
        'neocloud://qr-login?backend=ftp%3A%2F%2Fa.example&challenge=0b6f7a52-2b7e&secret=aGVsbG8td29ybGQtdGhpcy1p',
        'neocloud://qr-login?backend=https%3A%2F%2Fa.example&challenge=0b6f7a52-2b7e&secret=short',
        'neoagent://qr-login?backend=https%3A%2F%2Fa.example&challenge=0b6f7a52-2b7e&secret=aGVsbG8td29ybGQtdGhpcy1p',
        'hello',
      ]) {
        expect(parseScannedCode(raw), isNull, reason: raw);
      }
    });
  });

  group('import progress', () {
    test('progress counts bytes once known, files before, nothing while scanning', () {
      expect(importFraction(_job(totalBytes: 400, doneBytes: 100, totalFiles: 10, doneFiles: 9)), 0.25);
      expect(importFraction(_job(totalFiles: 10, doneFiles: 4)), 0.4);
      expect(importFraction(_job(status: 'scanning')), isNull);
      expect(importFraction(_job(status: 'done')), 1);
    });

    test('the counts line names files and sizes', () {
      expect(
        importCountsLine(_job(totalFiles: 5000, doneFiles: 1204, totalBytes: 4 * 1024 * 1024 * 1024, doneBytes: 1288490189)),
        '1,204 of 5,000 files · 1.2 GB of 4.0 GB',
      );
      expect(importCountsLine(_job(doneFiles: 3, doneBytes: 2048)), '3 files · 2.0 KB');
    });

    test('speed reads per second, the quota shortfall only when it does not fit', () {
      expect(formatTransferSpeed(null), isNull);
      expect(formatTransferSpeed(0), isNull);
      expect(formatTransferSpeed(3.5 * 1024 * 1024), '3.5 MB/s');
      const usage = StorageUsage(usedBytes: 800, quotaBytes: 1000, trashBytes: 0, versionBytes: 0);
      expect(importShortfall(150, usage), isNull);
      expect(importShortfall(500, usage), 300);
      expect(importShortfall(1 << 40, const StorageUsage(usedBytes: 1, quotaBytes: -1, trashBytes: 0, versionBytes: 0)), isNull);
    });

    test('speed meters measure running jobs and forget stopped ones', () {
      final meters = ImportSpeedMeters();
      final start = DateTime(2026);
      meters.sample(<ImportJob>[_job(totalBytes: 10000, doneBytes: 0)], start);
      meters.sample(<ImportJob>[_job(totalBytes: 10000, doneBytes: 2000)], start.add(const Duration(seconds: 2)));
      expect(meters.speedOf('j1'), 1000);
      expect(meters.etaOf('j1'), const Duration(seconds: 8));
      meters.sample(<ImportJob>[_job(status: 'paused', totalBytes: 10000, doneBytes: 2000)], start.add(const Duration(seconds: 4)));
      expect(meters.speedOf('j1'), isNull);
    });
  });

  group('adaptive sheets', () {
    Future<void> open(WidgetTester tester, Size size) async {
      tester.view.physicalSize = size;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => showAdaptiveSheet<void>(context: context, builder: (_) => const Text('sheet body')),
            child: const Text('open'),
          ),
        ),
      ));
      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
      expect(find.text('sheet body'), findsOneWidget);
    }

    testWidgets('a phone gets a bottom sheet', (tester) async {
      await open(tester, const Size(390, 844));
      expect(find.byType(BottomSheet), findsOneWidget);
      expect(find.byType(Dialog), findsNothing);
    });

    testWidgets('a wide window keeps the dialog', (tester) async {
      await open(tester, const Size(1280, 900));
      expect(find.byType(BottomSheet), findsNothing);
    });
  });

  group('pairing dialog', () {
    setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

    testWidgets('counts down, shows the paired device, and offers a new code once expired', (tester) async {
      var now = DateTime(2026, 11, 11, 12);
      var status = const PairingStatus(state: PairingState.pending);
      var starts = 0;
      Future<PairingTicket> start() async {
        starts += 1;
        return PairingTicket(
          code: '$_code$starts',
          expiresAt: now.add(const Duration(minutes: 5)),
          server: 'https://cloud.example.com',
          qrPayload: 'neocloud://pair?server=https://cloud.example.com&code=$_code',
        );
      }

      Future<void> advance(Duration step) async {
        now = now.add(step);
        await tester.pump(step);
      }

      tester.view.physicalSize = const Size(1200, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      await tester.pumpWidget(MaterialApp(
        home: Scaffold(
          body: PairPhoneDialog(start: start, poll: (_) async => status, friendlyError: (error) => '$error', now: () => now),
        ),
      ));
      await tester.pump();
      expect(find.text('Expires in 5:00'), findsOneWidget);
      expect(find.textContaining('/app/pair?code='), findsOneWidget);

      await advance(const Duration(seconds: 1));
      expect(find.text('Expires in 4:59'), findsOneWidget);
      await advance(const Duration(seconds: 59));
      expect(find.text('Expires in 4:00'), findsOneWidget);

      // Runs out: the code is greyed out and a new one can be asked for.
      await advance(const Duration(minutes: 4));
      await advance(const Duration(seconds: 1));
      expect(find.text('Code expired'), findsOneWidget);
      await tester.tap(find.text('New code'));
      await tester.pump();
      expect(starts, 2);
      expect(find.text('Expires in 5:00'), findsOneWidget);

      // A phone redeems it: the next poll says so.
      status = const PairingStatus(state: PairingState.used, deviceName: 'Pixel 9');
      await advance(const Duration(seconds: 2));
      await tester.pump();
      expect(find.text('Paired with Pixel 9 ✓'), findsOneWidget);
      expect(find.textContaining('Expires in'), findsNothing);
    });
  });
}
