import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';

// Regression: a ButtonStyle text style replaces the theme's text style (the
// button's Material sets a plain DefaultTextStyle), so a style without a font
// family fell back to the platform font. On the web that is Roboto, fetched
// from fonts.gstatic.com, which the page's CSP blocks: the labels were blank.
// Every label must resolve to a bundled Geist family and weight.

const _bundled = <String>{
  'Geist_300',
  'Geist_regular',
  'Geist_500',
  'Geist_600',
  'Geist_700',
  'Geist_800',
};

TextStyle _labelStyle(WidgetTester tester, String label) {
  final element = tester.element(find.text(label));
  return DefaultTextStyle.of(element).style;
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  for (final brightness in Brightness.values) {
    testWidgets('every button label uses a bundled Geist weight (${brightness.name})', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: neoCloudTheme(brightness),
          home: Scaffold(
            body: Column(
              children: <Widget>[
                FilledButton(onPressed: () {}, child: const Text('Filled')),
                OutlinedButton(onPressed: () {}, child: const Text('Outlined')),
                TextButton(onPressed: () {}, child: const Text('Plain')),
                FilledButton.icon(onPressed: () {}, icon: const Icon(Icons.check), label: const Text('Icon')),
                SegmentedButton<int>(
                  segments: const <ButtonSegment<int>>[ButtonSegment<int>(value: 1, label: Text('Segment'))],
                  selected: const <int>{1},
                ),
                const Tooltip(message: 'Hint', child: Text('Hover')),
              ],
            ),
          ),
        ),
      );
      await GoogleFonts.pendingFonts();
      await tester.pump();
      for (final label in <String>['Filled', 'Outlined', 'Plain', 'Icon', 'Segment']) {
        final style = _labelStyle(tester, label);
        expect(style.fontFamily, isNotNull, reason: '$label has no font family (falls back to Roboto)');
        expect(_bundled, contains(style.fontFamily), reason: '$label: ${style.fontFamily}');
        // A label drawn in its own background colour would be blank too.
        final filled = label == 'Filled' || label == 'Icon';
        if (filled) {
          final material = tester.widget<Material>(
            find.ancestor(of: find.text(label), matching: find.byType(Material)).first,
          );
          expect(style.color, isNot(material.color), reason: '$label is drawn in its background colour');
        }
      }
    });
  }

  testWidgets('snack bars, tooltips and tabs resolve to a bundled Geist weight', (tester) async {
    final theme = neoCloudTheme(Brightness.light);
    expect(_bundled, contains(theme.snackBarTheme.contentTextStyle!.fontFamily));
    expect(_bundled, contains(theme.tooltipTheme.textStyle!.fontFamily));
    expect(_bundled, contains(theme.tabBarTheme.labelStyle!.fontFamily));
    expect(_bundled, contains(theme.tabBarTheme.unselectedLabelStyle!.fontFamily));
    expect(_bundled, contains(theme.dialogTheme.titleTextStyle!.fontFamily));
  });
}
