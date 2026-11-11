import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/files/batch.dart';
import 'package:neocloud/src/files/file_format.dart';
import 'package:neocloud/src/files/selection_model.dart';
import 'package:neocloud/src/files/transfer_estimate.dart';
import 'package:neocloud/src/files/undo.dart';
import 'package:neocloud/src/theme/contrast.dart';
import 'package:neocloud/src/theme/palette.dart';

SelectionModel _model([int count = 6]) => SelectionModel()..setItems(<String>[for (var i = 0; i < count; i++) 'n$i']);

void main() {
  group('selection model', () {
    test('arrows move the focus without selecting; the first ↓ starts at the top', () {
      final model = _model();
      model.move(1);
      expect(model.focusedId, 'n0');
      model.move(1);
      model.move(1);
      expect(model.focusedId, 'n2');
      expect(model.selected, isEmpty);
      model.move(-10);
      expect(model.focusedId, 'n0');
      model.end();
      expect(model.focusedId, 'n5');
    });

    test('↑ without a focus starts at the bottom; grid rows move by the column count', () {
      final model = _model(10);
      model.move(-1);
      expect(model.focusedId, 'n9');
      model.home();
      model.move(4);
      expect(model.focusedId, 'n4');
    });

    test('Shift+arrows select the range from the anchor, in both directions', () {
      final model = _model();
      model.focusIndex(2);
      model.move(1, extend: true);
      model.move(1, extend: true);
      expect(model.selected, <String>{'n2', 'n3', 'n4'});
      model.move(-3, extend: true);
      expect(model.selected, <String>{'n1', 'n2'});
      model.end(extend: true);
      expect(model.selected, <String>{'n2', 'n3', 'n4', 'n5'});
    });

    test('Space toggles, a click selects only one, Shift-click a range', () {
      final model = _model();
      model.focusIndex(1);
      model.toggleFocused();
      model.focusIndex(3);
      model.toggleFocused();
      expect(model.selected, <String>{'n1', 'n3'});
      model.toggleFocused();
      expect(model.selected, <String>{'n1'});
      model.click('n4');
      expect(model.selected, <String>{'n4'});
      model.rangeTo('n2');
      expect(model.selected, <String>{'n2', 'n3', 'n4'});
      expect(model.focusedId, 'n2');
    });

    test('select all and clear; removed items leave the selection and pass the focus on', () {
      final model = _model();
      model.selectAll();
      expect(model.selected.length, 6);
      model.focusIndex(5);
      model.setItems(<String>['n0', 'n1', 'n2']);
      expect(model.selected, <String>{'n0', 'n1', 'n2'});
      expect(model.focusedId, 'n2');
      model.clear();
      expect(model.isEmpty, isTrue);
    });

    test('a reload keeps the focused item even when it moved', () {
      final model = _model();
      model.focusIndex(3);
      model.setItems(<String>['new', 'n0', 'n1', 'n2', 'n3']);
      expect(model.focusedId, 'n3');
      expect(model.focus, 4);
    });
  });

  group('type-ahead', () {
    final names = <String>['Archive', 'Budget.xlsx', 'brief.pdf', 'Notes', 'notes-old', 'Report'];
    final start = DateTime(2026, 11, 11, 12);

    test('letters within 700 ms build one prefix, case-insensitively', () {
      final typeAhead = TypeAhead();
      expect(typeAhead.input('n', names, -1, start), 3);
      expect(typeAhead.input('o', names, 3, start.add(const Duration(milliseconds: 200))), 3);
      expect(typeAhead.input('t', names, 3, start.add(const Duration(milliseconds: 400))), 3);
      expect(typeAhead.input('e', names, 3, start.add(const Duration(milliseconds: 600))), 3);
      expect(typeAhead.input('s', names, 3, start.add(const Duration(milliseconds: 800))), 3);
      expect(typeAhead.input('-', names, 3, start.add(const Duration(milliseconds: 1000))), 4);
    });

    test('a pause starts over; the same letter again cycles through matches', () {
      final typeAhead = TypeAhead();
      expect(typeAhead.input('b', names, -1, start), 1);
      expect(typeAhead.input('b', names, 1, start.add(const Duration(milliseconds: 300))), 2);
      expect(typeAhead.input('b', names, 2, start.add(const Duration(milliseconds: 600))), 1);
      expect(typeAhead.input('r', names, 1, start.add(const Duration(seconds: 2))), 5);
      expect(typeAhead.input('z', names, 5, start.add(const Duration(seconds: 4))), isNull);
    });
  });

  group('undo bar', () {
    testWidgets('hides after 10 s, and hovering pauses the clock', (tester) async {
      final center = UndoCenter();
      center.show(UndoEntry(message: 'moved'));
      await tester.pump(const Duration(seconds: 9));
      expect(center.current, isNotNull);
      await tester.pump(const Duration(seconds: 2));
      expect(center.current, isNull);

      center.show(UndoEntry(message: 'moved again'));
      center.pause();
      expect(center.isPaused, isTrue);
      await tester.pump(const Duration(seconds: 30));
      expect(center.current, isNotNull);
      center.resume();
      await tester.pump(const Duration(seconds: 11));
      expect(center.current, isNull);
    });

    testWidgets('undo runs the action once and a new entry replaces the old', (tester) async {
      final center = UndoCenter();
      var undone = 0;
      center.show(UndoEntry(message: 'first', onUndo: () async => undone++));
      center.show(UndoEntry(message: 'second', onUndo: () async => undone += 10));
      expect(center.current!.message, 'second');
      await center.undo();
      await center.undo();
      expect(undone, 10);
      expect(center.current, isNull);
      center.dispose();
    });
  });

  group('upload estimate', () {
    final start = DateTime(2026, 11, 11, 12);

    test('a steady speed gives a steady time left', () {
      final estimate = TransferEstimate();
      const total = 100 * 1000 * 1000;
      for (var second = 0; second <= 10; second++) {
        estimate.sample(second * 1000 * 1000, total, start.add(Duration(seconds: second)));
      }
      expect(estimate.bytesPerSecond, closeTo(1000 * 1000, 1));
      expect(estimate.eta!.inSeconds, closeTo(90, 1));
    });

    test('a spike moves the speed and the time left only a little', () {
      final estimate = TransferEstimate();
      const total = 100 * 1000 * 1000;
      var sent = 0;
      for (var second = 0; second <= 10; second++) {
        sent = second * 1000 * 1000;
        estimate.sample(sent, total, start.add(Duration(seconds: second)));
      }
      final before = estimate.eta!.inSeconds;
      // One second at ten times the speed.
      sent += 10 * 1000 * 1000;
      estimate.sample(sent, total, start.add(const Duration(seconds: 11)));
      expect(estimate.bytesPerSecond, lessThan(4 * 1000 * 1000));
      final raw = (total - sent) / (10 * 1000 * 1000);
      expect(estimate.eta!.inSeconds, greaterThan(raw * 3));
      expect(estimate.eta!.inSeconds, lessThan(before));
    });

    test('samples closer than half a second are ignored', () {
      final estimate = TransferEstimate();
      estimate.sample(0, 1000, start);
      estimate.sample(500, 1000, start.add(const Duration(milliseconds: 100)));
      expect(estimate.bytesPerSecond, isNull);
      estimate.reset();
      expect(estimate.eta, isNull);
    });
  });

  group('contrast', () {
    test('muted text passes WCAG AA (4.5:1) on cards and pages in both themes', () {
      for (final palette in <NeoCloudPalette>[darkPalette, lightPalette]) {
        for (final background in <Color>[palette.bgCard, palette.bgPrimary, palette.bgSecondary, palette.bgTertiary]) {
          expect(contrastRatio(palette.textMuted, background), greaterThanOrEqualTo(4.5), reason: '${palette.textMuted} on $background');
        }
        expect(contrastRatio(palette.textSecondary, palette.bgPrimary), greaterThanOrEqualTo(4.5));
        expect(contrastRatio(palette.textPrimary, palette.bgCard), greaterThanOrEqualTo(7));
      }
    });

    test('the ratio is symmetric and spans 1 to 21', () {
      const black = Color(0xFF000000);
      const white = Color(0xFFFFFFFF);
      expect(contrastRatio(black, white), closeTo(21, 0.01));
      expect(contrastRatio(white, black), closeTo(21, 0.01));
      expect(contrastRatio(white, white), closeTo(1, 0.001));
    });
  });

  group('batch operations', () {
    test('run in parallel, keep going after a failure and report failures in order', () async {
      var running = 0;
      var peak = 0;
      final failures = await runBatch<int>(List<int>.generate(20, (i) => i), (item) async {
        running++;
        peak = running > peak ? running : peak;
        await Future<void>.delayed(Duration(milliseconds: 5 + (item % 3)));
        running--;
        if (item == 3 || item == 17) throw StateError('nope $item');
      }, concurrency: 4);
      expect(peak, 4);
      expect(failures.map((failure) => failure.item), <int>[3, 17]);
    });
  });

  test('keep both names the copy after the first free number', () {
    expect(nextFreeName('report.txt', <String>{'report.txt'}), 'report (1).txt');
    expect(nextFreeName('report.txt', <String>{'report.txt', 'report (1).txt'}), 'report (2).txt');
    expect(nextFreeName('Photos', <String>{'Photos'}, isFolder: true), 'Photos (1)');
    expect(nextFreeName('.env', <String>{'.env'}), '.env (1)');
  });
}
