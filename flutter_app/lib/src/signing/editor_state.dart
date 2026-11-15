import 'dart:async';
import 'dart:ui' show Color, Offset, Rect, Size;

import 'package:flutter/foundation.dart';

import 'field_problems.dart';
import 'field_types.dart';
import 'geometry.dart';
import 'models.dart';
import 'signing_api.dart';
import 'widgets/party_colors.dart';

/// Where the draft stands against the server.
enum SigningSaveState { saved, dirty, saving, error, locked }

int _localParties = 0;

/// Everything the editor changes: fields, parties and the draft's settings,
/// with undo/redo, a clipboard and an autosave that sends the whole draft to
/// the server a moment after the last edit (replace-all, so it is safe to
/// repeat). Widgets listen to this and hold no document state of their own.
class SigningEditorState extends ChangeNotifier {
  SigningEditorState({
    required this.api,
    required SigningDetail detail,
    required this.partyName,
    this.autosaveDelay = const Duration(milliseconds: 700),
    this.historyLimit = 100,
    this.maxParties = 20,
    this.maxFields = 500,
  }) : _document = detail.document,
       _parties = List<SigningParty>.of(detail.parties),
       _fields = List<SigningField>.of(detail.fields),
       _activePartyId = detail.parties.isEmpty ? '' : detail.parties.first.id,
       locked = !detail.document.status.isEditable || detail.parties.any((party) => party.openedAt != null) {
    if (locked) saveState = SigningSaveState.locked;
  }

  final SigningApi api;

  /// The default name of the [number]th party ("Party 2"), from the app's strings.
  final String Function(int number) partyName;
  final Duration autosaveDelay;
  final int historyLimit;
  final int maxParties;
  final int maxFields;

  SigningDocument _document;
  List<SigningParty> _parties;
  List<SigningField> _fields;
  String _activePartyId;

  String? _selectedId;
  FieldType? _armedType;
  SigningSaveState saveState = SigningSaveState.saved;
  SigningException? lastError;

  /// Once someone opened a link (or the server says so) nothing can change.
  bool locked;

  /// Lines the last drag snapped to, for drawing; cleared when it ends.
  final ValueNotifier<List<SnapGuide>> guides = ValueNotifier<List<SnapGuide>>(const <SnapGuide>[]);

  final List<List<SigningField>> _undo = <List<SigningField>>[];
  final List<List<SigningField>> _redo = <List<SigningField>>[];
  List<SigningField>? _clipboard;
  String? _lastCoalesceKey;
  DateTime _lastCoalesceAt = DateTime.fromMillisecondsSinceEpoch(0);

  Timer? _timer;
  bool _fieldsDirty = false;
  bool _partiesDirty = false;
  bool _settingsDirty = false;
  bool _saving = false;
  bool _disposed = false;
  Future<void> _chain = Future<void>.value();

  SigningDocument get document => _document;
  List<SigningParty> get parties => List<SigningParty>.unmodifiable(_parties);
  List<SigningField> get fields => List<SigningField>.unmodifiable(_fields);
  String get title => _document.title;
  SigningOrder get order => _document.signingOrder;
  String get activePartyId => _activePartyId;
  String? get selectedId => _selectedId;
  FieldType? get armedType => _armedType;
  bool get canUndo => _undo.isNotEmpty && !locked;
  bool get canRedo => _redo.isNotEmpty && !locked;
  bool get hasClipboard => _clipboard != null && _clipboard!.isNotEmpty;
  bool get canAddParty => !locked && _parties.length < maxParties;

  /// The signed-in person's name and address, set when mail is on, so the
  /// creator can add themselves as a signer in one tap.
  ({String name, String email})? me;

  bool get hasSelfParty => _parties.any((party) => party.isSelf);

  bool get canAddSelf => me != null && canAddParty && !hasSelfParty;
  bool get canAddField => !locked && _fields.length < maxFields;
  bool get hasUnsavedChanges => !locked && (_fieldsDirty || _partiesDirty || _settingsDirty || _saving);

  SigningField? get selected => fieldById(_selectedId);

  SigningField? fieldById(String? id) {
    if (id == null) return null;
    for (final field in _fields) {
      if (field.id == id) return field;
    }
    return null;
  }

  int partyIndex(String partyId) => _parties.indexWhere((party) => party.id == partyId);

  SigningParty? partyById(String partyId) {
    final index = partyIndex(partyId);
    return index < 0 ? null : _parties[index];
  }

  /// The colour a party's boxes are drawn in.
  Color colorOf(String partyId) {
    final index = partyIndex(partyId);
    return resolvePartyColor(index < 0 ? null : _parties[index].color, index < 0 ? 0 : index);
  }

  List<SigningField> fieldsOnPage(int page) =>
      _fields.where((field) => field.areas.any((area) => area.page == page)).toList(growable: false);

  /// Ids of the fields whose settings the server would refuse.
  List<String> get fieldsWithProblems => <String>[
    for (final field in _fields)
      if (fieldProblems(field).isNotEmpty) field.id,
  ];

  // Selection and placing

  /// Which tab of the side panel shows: the settings of the selection, or
  /// the palette. Selecting a box opens its settings; placing a new one keeps
  /// the palette, so several can be placed in a row.
  bool showSettings = false;

  void select(String? id) {
    if (_selectedId == id && showSettings == (id != null)) return;
    _selectedId = id;
    showSettings = id != null;
    notifyListeners();
  }

  void showSettingsTab(bool value) {
    if (showSettings == value || (value && _selectedId == null)) return;
    showSettings = value;
    notifyListeners();
  }

  void setActiveParty(String partyId) {
    if (partyIndex(partyId) < 0 || _activePartyId == partyId) return;
    _activePartyId = partyId;
    notifyListeners();
  }

  /// "Tap a tile, then tap the page": the next tap on a page places [type].
  void arm(FieldType? type) {
    if (_armedType == type) type = null;
    _armedType = type;
    notifyListeners();
  }

  /// Places a new field of [type] centred on [center] (fractions) of [page].
  SigningField? addField(FieldType type, int page, Offset center, Size pagePixels, {String? partyId}) {
    if (!canAddField) return null;
    _remember();
    final field = FieldTypes.create(
      type: type,
      partyId: partyId ?? _activePartyId,
      page: page,
      centerX: center.dx,
      centerY: center.dy,
      pagePixels: pagePixels,
    );
    _fields = <SigningField>[..._fields, field];
    _selectedId = field.id;
    showSettings = false;
    _armedType = null;
    _changed();
    return field;
  }

  // Editing

  /// Call once when a drag starts, so the whole drag is one undo step.
  void beginGesture() => _remember();

  void endGesture() {
    if (guides.value.isNotEmpty) guides.value = const <SnapGuide>[];
  }

  /// Moves one area of a field; [start] is where the drag began and [delta]
  /// the total move since, both in fractions.
  void dragMove(String id, int areaIndex, Rect start, Offset delta) {
    final field = fieldById(id);
    if (field == null || locked || areaIndex >= field.areas.length) return;
    final page = field.areas[areaIndex].page;
    final others = <Rect>[
      for (final other in _fields)
        for (final (index, area) in other.areas.indexed)
          if (area.page == page && !(other.id == id && index == areaIndex)) areaRect(area),
    ];
    final matched = <SnapGuide>[];
    final moved = clampRect(snapToNeighbours(moveRect(start, delta), others, guides: matched));
    guides.value = matched;
    _setArea(field, areaIndex, rectToArea(moved, page).copyWith(option: field.areas[areaIndex].option));
  }

  /// Resizes one area; [pagePixels] keeps square boxes square.
  void dragResize(String id, int areaIndex, Rect start, ResizeHandle handle, Offset delta, Size pagePixels) {
    final field = fieldById(id);
    if (field == null || locked || areaIndex >= field.areas.length) return;
    final spec = FieldTypes.of(field.type);
    final resized = resizeRect(start, handle, delta, minSize: spec.minSize, aspect: spec.lockedAspect(pagePixels));
    _setArea(field, areaIndex, rectToArea(resized, field.areas[areaIndex].page).copyWith(option: field.areas[areaIndex].option));
  }

  void _setArea(SigningField field, int areaIndex, FieldArea area) {
    if (field.areas[areaIndex] == area) return;
    final areas = List<FieldArea>.of(field.areas)..[areaIndex] = area;
    _replace(field.copyWith(areas: areas));
    _changed();
  }

  /// Arrow-key nudge of the selected field by [dx], [dy] fractions.
  void nudge(double dx, double dy) {
    final field = selected;
    if (field == null || locked) return;
    _remember(coalesce: 'nudge:${field.id}');
    final areas = <FieldArea>[
      for (final area in field.areas) rectToArea(clampRect(areaRect(area).shift(Offset(dx, dy))), area.page).copyWith(option: area.option),
    ];
    _replace(field.copyWith(areas: areas));
    _changed();
  }

  /// Changes a field through [change]. Edits with the same [coalesce] key in
  /// quick succession (typing in a box) count as one undo step.
  void updateField(String id, SigningField Function(SigningField field) change, {String? coalesce}) {
    final field = fieldById(id);
    if (field == null || locked) return;
    _remember(coalesce: coalesce == null ? null : '$coalesce:$id');
    _replace(change(field));
    _changed();
  }

  void setFieldParty(String id, String partyId) {
    if (partyIndex(partyId) < 0) return;
    updateField(id, (field) => field.copyWith(partyId: partyId));
  }

  void deleteSelected() {
    final id = _selectedId;
    if (id == null || locked) return;
    _remember();
    _fields = _fields.where((field) => field.id != id).toList();
    _selectedId = null;
    _changed();
  }

  void duplicateSelected() {
    final field = selected;
    if (field == null || !canAddField) return;
    _remember();
    final copy = _copyOf(field, const Offset(0.02, 0.02));
    _fields = <SigningField>[..._fields, copy];
    _selectedId = copy.id;
    _changed();
  }

  SigningField _copyOf(SigningField field, Offset shift) => SigningField.local(
    partyId: field.partyId,
    type: field.type,
    name: field.name,
    required: field.required,
    prefs: field.prefs,
    defaultValue: field.defaultValue,
    areas: <FieldArea>[
      for (final area in field.areas) rectToArea(clampRect(areaRect(area).shift(shift)), area.page).copyWith(option: area.option),
    ],
  ).copyWith(readonly: field.readonly);

  void copySelected() {
    final field = selected;
    if (field != null) _clipboard = <SigningField>[field];
    notifyListeners();
  }

  /// Pastes the clipboard a little off its place, on [page] when given.
  void paste({int? page}) {
    final clip = _clipboard;
    if (clip == null || clip.isEmpty || !canAddField) return;
    _remember();
    final pasted = <SigningField>[];
    for (final field in clip) {
      final shifted = _copyOf(field, const Offset(0.02, 0.02));
      pasted.add(page == null ? shifted : shifted.copyWith(areas: <FieldArea>[shifted.areas.first.copyWith(page: page)]));
    }
    _fields = <SigningField>[..._fields, ...pasted];
    _selectedId = pasted.last.id;
    _changed();
  }

  // History

  void _remember({String? coalesce}) {
    final now = DateTime.now();
    if (coalesce != null && coalesce == _lastCoalesceKey && now.difference(_lastCoalesceAt) < const Duration(seconds: 2)) {
      _lastCoalesceAt = now;
      return;
    }
    _lastCoalesceKey = coalesce;
    _lastCoalesceAt = now;
    _undo.add(List<SigningField>.of(_fields));
    if (_undo.length > historyLimit) _undo.removeAt(0);
    _redo.clear();
  }

  void undo() {
    if (!canUndo) return;
    _redo.add(List<SigningField>.of(_fields));
    _restore(_undo.removeLast());
  }

  void redo() {
    if (!canRedo) return;
    _undo.add(List<SigningField>.of(_fields));
    _restore(_redo.removeLast());
  }

  void _restore(List<SigningField> snapshot) {
    final known = _parties.map((party) => party.id).toSet();
    _fields = snapshot.where((field) => known.contains(field.partyId)).toList();
    if (fieldById(_selectedId) == null) _selectedId = null;
    _lastCoalesceKey = null;
    _changed();
  }

  void _replace(SigningField next) {
    _fields = <SigningField>[for (final field in _fields) field.id == next.id ? next : field];
  }

  // Parties

  SigningParty? addParty() {
    if (!canAddParty) return null;
    final number = _parties.length + 1;
    final party = SigningParty(
      id: 'local_party_${DateTime.now().microsecondsSinceEpoch}_${_localParties++}',
      name: partyName(number),
      position: _parties.length,
      color: colorToHex(partyColor(_parties.length)),
    );
    _parties = <SigningParty>[..._parties, party];
    _activePartyId = party.id;
    _partiesChanged();
    return party;
  }

  /// Adds the creator as a party (their name and address, marked `isSelf`).
  SigningParty? addSelfParty() {
    final who = me;
    if (who == null || !canAddSelf) return null;
    final party = addParty();
    if (party == null) return null;
    _editParty(party.id, (current) => _partyWith(current, name: who.name, email: who.email, isSelf: true));
    return partyById(party.id);
  }

  /// Marks (or unmarks) [id] as the creator's own part.
  void setPartySelf(String id, bool isSelf) => _editParty(id, (party) => _partyWith(party, isSelf: isSelf));

  void renameParty(String id, String name) => _editParty(id, (party) => _partyWith(party, name: name.trim().isEmpty ? party.name : name.trim()));

  void setPartyEmail(String id, String? email) => _editParty(id, (party) => _partyWith(party, email: email ?? '', clearEmail: email == null || email.isEmpty));

  void _editParty(String id, SigningParty Function(SigningParty party) change) {
    if (locked || partyIndex(id) < 0) return;
    _parties = <SigningParty>[for (final party in _parties) party.id == id ? change(party) : party];
    _partiesChanged();
  }

  SigningParty _partyWith(SigningParty party, {String? name, String? email, bool clearEmail = false, bool? isSelf}) => SigningParty(
    id: party.id,
    name: name ?? party.name,
    position: party.position,
    color: party.color,
    email: clearEmail ? null : (email ?? party.email),
    status: party.status,
    isSelf: isSelf ?? party.isSelf,
  );

  /// Removes a party and its fields. The last party stays.
  void deleteParty(String id) {
    if (locked || _parties.length < 2 || partyIndex(id) < 0) return;
    _remember();
    _parties = <SigningParty>[
      for (final party in _parties)
        if (party.id != id) party,
    ];
    _fields = _fields.where((field) => field.partyId != id).toList();
    if (_activePartyId == id) _activePartyId = _parties.first.id;
    if (fieldById(_selectedId) == null) _selectedId = null;
    _partiesDirty = true;
    _changed();
  }

  int fieldCountOf(String partyId) => _fields.where((field) => field.partyId == partyId).length;

  void _partiesChanged() {
    _partiesDirty = true;
    _scheduleSave();
    notifyListeners();
  }

  // Settings

  void setTitle(String value) {
    final text = value.trim();
    if (locked || text.isEmpty || text == _document.title) return;
    _document = _withDocument(title: text);
    _settingsDirty = true;
    _scheduleSave();
    notifyListeners();
  }

  void setOrder(SigningOrder value) {
    if (locked || value == _document.signingOrder) return;
    _document = _withDocument(order: value);
    _settingsDirty = true;
    _scheduleSave();
    notifyListeners();
  }

  SigningDocument _withDocument({String? title, SigningOrder? order}) => SigningDocument(
    id: _document.id,
    title: title ?? _document.title,
    status: _document.status,
    signingOrder: order ?? _document.signingOrder,
    locale: _document.locale,
    expiresAt: _document.expiresAt,
    pageCount: _document.pageCount,
    sourceNodeId: _document.sourceNodeId,
    finalNodeId: _document.finalNodeId,
    pageSizes: _document.pageSizes,
    createdAt: _document.createdAt,
    updatedAt: _document.updatedAt,
  );

  // Saving

  void _changed() {
    _fieldsDirty = true;
    _scheduleSave();
    notifyListeners();
  }

  void _scheduleSave() {
    if (locked) return;
    saveState = SigningSaveState.dirty;
    _timer?.cancel();
    _timer = Timer(autosaveDelay, () => unawaited(flush()));
  }

  /// Saves what changed and resolves with whether everything is stored.
  Future<bool> flush() {
    _timer?.cancel();
    final result = _chain.then((_) => _saveAll());
    _chain = result.then((_) {}, onError: (Object _) {});
    return result;
  }

  Future<bool> _saveAll() async {
    if (locked) return false;
    while (_fieldsDirty || _partiesDirty || _settingsDirty) {
      if (_disposed) return false;
      _saving = true;
      saveState = SigningSaveState.saving;
      notifyListeners();
      final (parties, settings, fields) = (_partiesDirty, _settingsDirty, _fieldsDirty);
      try {
        if (parties) await _saveParties();
        if (settings) await _saveSettings();
        if (fields) await _saveFields();
      } on SigningException catch (error) {
        // Nothing of this round is stored for sure: try it all again later.
        _partiesDirty = _partiesDirty || parties;
        _settingsDirty = _settingsDirty || settings;
        _fieldsDirty = _fieldsDirty || fields;
        _saving = false;
        lastError = error;
        if (error.kind == SigningErrorKind.conflict) {
          locked = true;
          saveState = SigningSaveState.locked;
        } else {
          saveState = SigningSaveState.error;
          if (error.kind != SigningErrorKind.invalid && error.kind != SigningErrorKind.unauthorized) _timer = Timer(const Duration(seconds: 5), () => unawaited(flush()));
        }
        if (!_disposed) notifyListeners();
        return false;
      }
    }
    _saving = false;
    lastError = null;
    saveState = SigningSaveState.saved;
    if (!_disposed) notifyListeners();
    return true;
  }

  Future<void> _saveParties() async {
    _partiesDirty = false;
    final sent = List<SigningParty>.of(_parties);
    final stored = await api.saveParties(_document.id, <PartyDraft>[
      for (final party in sent) PartyDraft(id: party.id.startsWith('local_') ? null : party.id, name: party.name, email: party.email, color: party.color, isSelf: party.isSelf),
    ]);
    if (stored.length != sent.length) return;
    final renamed = <String, String>{for (final (index, party) in sent.indexed) party.id: stored[index].id};
    _parties = <SigningParty>[
      for (final party in _parties)
        if (renamed[party.id] != null)
          _storedParty(party, stored[sent.indexWhere((candidate) => candidate.id == party.id)])
        else
          party,
    ];
    _fields = <SigningField>[for (final field in _fields) renamed[field.partyId] == null ? field : field.copyWith(partyId: renamed[field.partyId]!)];
    for (final snapshot in <List<List<SigningField>>>[_undo, _redo]) {
      for (final (index, fields) in snapshot.indexed) {
        snapshot[index] = <SigningField>[for (final field in fields) renamed[field.partyId] == null ? field : field.copyWith(partyId: renamed[field.partyId]!)];
      }
    }
    _activePartyId = renamed[_activePartyId] ?? _activePartyId;
    _clipboard = _clipboard?.map((field) => renamed[field.partyId] == null ? field : field.copyWith(partyId: renamed[field.partyId]!)).toList();
  }

  SigningParty _storedParty(SigningParty mine, SigningParty server) => SigningParty(
    id: server.id,
    name: mine.name,
    position: server.position,
    color: server.color.isEmpty ? mine.color : server.color,
    email: mine.email,
    status: server.status,
    isSelf: mine.isSelf,
  );

  Future<void> _saveSettings() async {
    _settingsDirty = false;
    await api.update(_document.id, SigningSettingsPatch(title: _document.title, signingOrder: _document.signingOrder));
  }

  Future<void> _saveFields() async {
    _fieldsDirty = false;
    final sent = List<SigningField>.of(_fields);
    final stored = await api.saveFields(_document.id, sent);
    if (stored.length != sent.length) return;
    final ids = <String, String>{for (final (index, field) in sent.indexed) if (field.isLocal) field.id: stored[index].id};
    if (ids.isEmpty) return;
    _fields = <SigningField>[for (final field in _fields) ids[field.id] == null ? field : field.copyWith(id: ids[field.id])];
    if (_selectedId != null && ids[_selectedId] != null) _selectedId = ids[_selectedId];
  }

  /// Imports the PDF's own form fields; resolves with how many were found.
  Future<int> importFields() async {
    if (locked) return 0;
    if (!await flush()) return 0;
    final count = await api.importFields(_document.id);
    if (count > 0) {
      final detail = await api.get(_document.id);
      _remember();
      _fields = List<SigningField>.of(detail.fields);
      _parties = List<SigningParty>.of(detail.parties);
      notifyListeners();
    }
    return count;
  }

  /// Adopts the server's current state (after a conflict, or a reload).
  void adopt(SigningDetail detail) {
    _document = detail.document;
    _parties = List<SigningParty>.of(detail.parties);
    _fields = List<SigningField>.of(detail.fields);
    locked = !detail.document.status.isEditable || detail.parties.any((party) => party.openedAt != null);
    saveState = locked ? SigningSaveState.locked : SigningSaveState.saved;
    _fieldsDirty = _partiesDirty = _settingsDirty = false;
    _undo.clear();
    _redo.clear();
    if (fieldById(_selectedId) == null) _selectedId = null;
    notifyListeners();
  }

  @override
  void notifyListeners() {
    if (!_disposed) super.notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _timer?.cancel();
    guides.dispose();
    super.dispose();
  }
}
