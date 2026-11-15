import 'dart:ui' show Offset, Size;

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';

const Size _page = Size(595, 842);

Future<(FakeSigningApi, SigningEditorState, SigningDetail)> _make({int historyLimit = 100}) async {
  final api = FakeSigningApi();
  final detail = await api.createDocument(nodeId: 'n', title: 'Lease');
  final state = SigningEditorState(
    api: api,
    detail: detail,
    partyName: (number) => 'Party $number',
    autosaveDelay: const Duration(milliseconds: 5),
    historyLimit: historyLimit,
  );
  addTearDown(state.dispose);
  return (api, state, detail);
}

void main() {
  group('registry defaults the server accepts', () {
    test('a cells field starts with a number of boxes, a choice field with choices', () {
      final cells = FieldTypes.create(type: FieldType.cells, partyId: 'p', page: 0, centerX: 0.5, centerY: 0.5);
      expect(cells.prefs.length, defaultCellCount);
      for (final type in <FieldType>[FieldType.select, FieldType.radio, FieldType.multiple]) {
        final field = FieldTypes.create(type: type, partyId: 'p', page: 0, centerX: 0.5, centerY: 0.5);
        expect(field.prefs.options, hasLength(2), reason: type.name);
        expect(fieldProblems(field), isEmpty, reason: type.name);
      }
      for (final spec in FieldTypes.all) {
        final field = FieldTypes.create(type: spec.type, partyId: 'p', page: 0, centerX: 0.5, centerY: 0.5);
        expect(fieldProblems(field), isEmpty, reason: '${spec.type.name} starts valid');
      }
    });

    test('prefs use the names the server reads', () {
      final all = FieldTypes.all.expand((spec) => spec.prefs.map((pref) => pref.key)).toSet();
      expect(all, isNot(contains('background')));
      expect(all, containsAll(<String>['cells', 'charset', 'minLength', 'maxLength', 'minSelect', 'maxSelect']));
      expect(FieldPrefs().fontType, 'normal');
      expect(PrefKeys.defaultToday, 'autoToday');
    });
  });

  group('fieldProblems', () {
    SigningField field(FieldType type, Map<String, Object?> prefs, {Object? value}) => SigningField.local(
      partyId: 'p',
      type: type,
      prefs: FieldPrefs(prefs),
      defaultValue: value,
      areas: const <FieldArea>[FieldArea(page: 0, x: 0, y: 0, w: 0.2, h: 0.03)],
    );

    test('flags empty or repeated choices, reversed limits and a default that does not fit', () {
      expect(fieldProblems(field(FieldType.select, <String, Object?>{'options': <String>[]})), <FieldProblem>[FieldProblem.options]);
      expect(fieldProblems(field(FieldType.select, <String, Object?>{'options': <String>['a', 'a']})), <FieldProblem>[FieldProblem.optionsUnique]);
      expect(fieldProblems(field(FieldType.number, <String, Object?>{'min': 5, 'max': 1})), <FieldProblem>[FieldProblem.range]);
      expect(fieldProblems(field(FieldType.text, <String, Object?>{'minLength': 9, 'maxLength': 3})), <FieldProblem>[FieldProblem.length]);
      expect(fieldProblems(field(FieldType.text, <String, Object?>{'fontSize': 3})), <FieldProblem>[FieldProblem.fontSize]);
      expect(fieldProblems(field(FieldType.cells, <String, Object?>{'cells': 80})), <FieldProblem>[FieldProblem.cells]);
      expect(fieldProblems(field(FieldType.number, <String, Object?>{'min': 10}, value: '3')), <FieldProblem>[FieldProblem.defaultValue]);
      expect(fieldProblems(field(FieldType.number, <String, Object?>{'min': 1}, value: '3')), isEmpty);
    });
  });

  group('editor state', () {
    test('autosave waits for a pause and sends the whole draft once', () async {
      final (api, state, detail) = await _make();
      state.addField(FieldType.text, 0, const Offset(0.5, 0.5), _page);
      state.addField(FieldType.date, 0, const Offset(0.5, 0.7), _page);
      expect(state.saveState, SigningSaveState.dirty);
      expect(await state.flush(), isTrue);
      expect(state.saveState, SigningSaveState.saved);
      expect((await api.get(detail.document.id)).fields, hasLength(2));
      expect(state.fields.every((field) => !field.isLocal), isTrue, reason: 'the server\'s ids replace the local ones');
    });

    test('history is bounded and undo skips what a deleted party took along', () async {
      final (_, state, _) = await _make(historyLimit: 3);
      for (var i = 0; i < 6; i++) {
        state.addField(FieldType.text, 0, Offset(0.5, 0.1 + i * 0.1), _page);
      }
      var undone = 0;
      while (state.canUndo) {
        state.undo();
        undone++;
      }
      expect(undone, 3);
      expect(state.fields, hasLength(3));

      final second = state.addParty()!;
      state.addField(FieldType.text, 0, const Offset(0.2, 0.9), _page);
      state.deleteParty(second.id);
      expect(state.fields.where((field) => field.partyId == second.id), isEmpty);
      state.undo();
      expect(state.fields.where((field) => field.partyId == second.id), isEmpty, reason: 'a field of a party that is gone is not brought back');
    });

    test('a new party gets its server id and its fields follow', () async {
      final (api, state, detail) = await _make();
      final party = state.addParty()!;
      expect(party.id.startsWith('local_'), isTrue);
      state.addField(FieldType.signature, 0, const Offset(0.5, 0.5), _page);
      expect(await state.flush(), isTrue);
      final stored = await api.get(detail.document.id);
      expect(stored.parties, hasLength(2));
      expect(state.parties.last.id, stored.parties.last.id);
      expect(state.fields.single.partyId, stored.parties.last.id);
      expect(state.activePartyId, stored.parties.last.id);
    });

    test('moving keeps a box on the page and resizing respects its minimum', () async {
      final (_, state, _) = await _make();
      final field = state.addField(FieldType.signature, 0, const Offset(0.5, 0.5), _page)!;
      final start = areaRect(field.areas.first);
      state.dragMove(field.id, 0, start, const Offset(5, 5));
      final moved = areaRect(state.fieldById(field.id)!.areas.first);
      expect(moved.right, lessThanOrEqualTo(1.0));
      expect(moved.bottom, lessThanOrEqualTo(1.0));
      state.dragResize(field.id, 0, start, ResizeHandle.bottomRight, const Offset(-1, -1), _page);
      final shrunk = areaRect(state.fieldById(field.id)!.areas.first);
      final min = FieldTypes.of(FieldType.signature).minSize;
      expect(shrunk.width, greaterThanOrEqualTo(min.width - 1e-9));
      expect(shrunk.height, greaterThanOrEqualTo(min.height - 1e-9));
    });

    test('a failed save is kept for the next try; a conflict locks', () async {
      final (api, state, detail) = await _make();
      api.failNext = const SigningException(SigningErrorKind.network);
      state.addField(FieldType.text, 0, const Offset(0.5, 0.5), _page);
      expect(await state.flush(), isFalse);
      expect(state.saveState, SigningSaveState.error);
      expect(state.hasUnsavedChanges, isTrue);
      expect(await state.flush(), isTrue, reason: 'the change was kept');
      expect((await api.get(detail.document.id)).fields, hasLength(1));

      api.failNext = const SigningException(SigningErrorKind.conflict, statusCode: 409);
      state.addField(FieldType.text, 0, const Offset(0.2, 0.2), _page);
      expect(await state.flush(), isFalse);
      expect(state.locked, isTrue);
      expect(state.saveState, SigningSaveState.locked);
      state.addField(FieldType.text, 0, const Offset(0.4, 0.4), _page);
      expect(state.fields, hasLength(2), reason: 'locked: no more edits');
    });

    test('the limits of parties and fields hold', () async {
      final api = FakeSigningApi();
      final detail = await api.createDocument(nodeId: 'n');
      final state = SigningEditorState(api: api, detail: detail, partyName: (n) => 'P$n', maxParties: 2, maxFields: 1);
      addTearDown(state.dispose);
      expect(state.addParty(), isNotNull);
      expect(state.addParty(), isNull);
      expect(state.addField(FieldType.text, 0, const Offset(0.5, 0.5), _page), isNotNull);
      expect(state.addField(FieldType.text, 0, const Offset(0.5, 0.6), _page), isNull);
    });
  });
}
