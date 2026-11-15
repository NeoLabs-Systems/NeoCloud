import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('every Geist weight the app asks for is a bundled asset', () async {
    final manifest = await AssetManifest.loadFromAssetBundle(rootBundle);
    final assets = manifest.listAssets();
    const files = <String>[
      'Geist-Light',
      'Geist-Regular',
      'Geist-Medium',
      'Geist-SemiBold',
      'Geist-Bold',
      'Geist-ExtraBold',
      'GeistMono-Regular',
      'GeistMono-Medium',
      'GeistMono-SemiBold',
      'GeistMono-Bold',
    ];
    for (final name in files) {
      expect(
        assets.any((asset) => asset.endsWith('assets/fonts/geist/$name.ttf')),
        isTrue,
        reason: name,
      );
    }
    expect(
      assets.any((asset) => asset.endsWith('assets/fonts/geist/OFL-Geist.txt')),
      isTrue,
    );
    expect(
      assets.any(
        (asset) => asset.endsWith('assets/fonts/geist/OFL-GeistMono.txt'),
      ),
      isTrue,
    );
  });

  testWidgets('Geist text loads from assets and never touches the network', (
    tester,
  ) async {
    GoogleFonts.config.allowRuntimeFetching = false;
    final problems = <String>[];
    final previous = debugPrint;
    debugPrint = (String? message, {int? wrapWidth}) {
      if (message != null && message.contains('google_fonts')) {
        problems.add(message);
      }
    };
    try {
      await HttpOverrides.runZoned(() async {
        await tester.pumpWidget(
          MaterialApp(
            theme: ThemeData(textTheme: GoogleFonts.geistTextTheme()),
            home: Column(
              children: <Widget>[
                Text(
                  'Sign here',
                  style: GoogleFonts.geist(fontWeight: FontWeight.w600),
                ),
                Text(
                  'A1B2C3',
                  style: GoogleFonts.geistMono(fontWeight: FontWeight.w500),
                ),
                Text('Body', style: GoogleFonts.geist()),
                Text(
                  'Heavy',
                  style: GoogleFonts.geist(fontWeight: FontWeight.w700),
                ),
              ],
            ),
          ),
        );
        await GoogleFonts.pendingFonts();
        await tester.pump();
      }, createHttpClient: (_) => throw StateError('network access attempted'));
    } finally {
      debugPrint = previous;
    }
    expect(problems, isEmpty, reason: problems.join('\n'));
    expect(tester.takeException(), isNull);
  });

  test('the pubspec registers the bundled fonts', () {
    final pubspec = File('pubspec.yaml').readAsStringSync();
    expect(pubspec, contains('assets/fonts/geist/'));
    expect(
      const LineSplitter().convert(File('lib/main.dart').readAsStringSync()),
      contains('  GoogleFonts.config.allowRuntimeFetching = false;'),
    );
  });
}
