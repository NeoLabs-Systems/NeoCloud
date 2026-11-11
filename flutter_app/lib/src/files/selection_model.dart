import 'dart:math' as math;

/// The file browser's selection and keyboard focus over an ordered list of
/// item ids: one focused item (the keyboard cursor), a set of selected ones
/// and the anchor that Shift-ranges start from.
class SelectionModel {
  List<String> _ids = const <String>[];
  final Set<String> selected = <String>{};
  int _focus = -1;
  String? _anchor;

  List<String> get ids => _ids;

  /// Index of the focused item, or -1.
  int get focus => _focus;

  String? get focusedId => _focus >= 0 && _focus < _ids.length ? _ids[_focus] : null;

  bool get isEmpty => selected.isEmpty;

  bool isSelected(String id) => selected.contains(id);

  /// Takes a new item list (after a load, a page, a removal) and keeps what
  /// still exists. A removed focused item hands the focus to its neighbour.
  void setItems(List<String> ids) {
    final focused = focusedId;
    final previous = _focus;
    _ids = List<String>.unmodifiable(ids);
    final present = ids.toSet();
    selected.retainAll(present);
    if (_anchor != null && !present.contains(_anchor)) _anchor = null;
    if (focused == null) {
      _focus = -1;
    } else {
      final index = ids.indexOf(focused);
      _focus = index >= 0 ? index : (ids.isEmpty ? -1 : math.min(previous, ids.length - 1));
    }
  }

  /// Moves the focus to [index]; with [extend] the selection becomes the range
  /// from the anchor to it (Shift+arrows).
  void focusIndex(int index, {bool extend = false}) {
    if (_ids.isEmpty) return;
    final target = index.clamp(0, _ids.length - 1);
    if (extend) {
      _anchor ??= focusedId ?? _ids[target];
      _selectRange(_ids.indexOf(_anchor!), target);
    } else {
      _anchor = _ids[target];
    }
    _focus = target;
  }

  /// Arrow keys: [delta] rows (or tiles) from the focused item. Without a
  /// focus, down/right start at the top and up/left at the bottom.
  void move(int delta, {bool extend = false}) {
    if (_ids.isEmpty) return;
    if (_focus < 0) {
      focusIndex(delta >= 0 ? 0 : _ids.length - 1, extend: extend);
      return;
    }
    focusIndex(_focus + delta, extend: extend);
  }

  void home({bool extend = false}) => focusIndex(0, extend: extend);

  void end({bool extend = false}) => focusIndex(_ids.length - 1, extend: extend);

  /// Space: adds or removes the focused item.
  void toggleFocused() {
    final id = focusedId;
    if (id != null) toggle(id);
  }

  /// Ctrl/⌘-click or a checkbox: adds or removes [id] and focuses it.
  void toggle(String id) {
    if (!selected.remove(id)) selected.add(id);
    _anchor = id;
    _focus = _ids.indexOf(id);
  }

  /// A plain mouse click: [id] alone is selected and focused.
  void click(String id) {
    selected
      ..clear()
      ..add(id);
    _anchor = id;
    _focus = _ids.indexOf(id);
  }

  /// Shift-click: everything from the anchor to [id].
  void rangeTo(String id) {
    final target = _ids.indexOf(id);
    if (target < 0) return;
    final anchor = _anchor == null ? -1 : _ids.indexOf(_anchor!);
    _selectRange(anchor < 0 ? target : anchor, target);
    _anchor ??= id;
    _focus = target;
  }

  void selectAll() => selected.addAll(_ids);

  void clear() => selected.clear();

  void _selectRange(int from, int to) {
    selected
      ..clear()
      ..addAll(_ids.sublist(math.min(from, to), math.max(from, to) + 1));
  }
}

/// Type-to-find: letters typed within [timeout] of each other build one
/// prefix; the same letter repeated cycles through the items starting with it.
class TypeAhead {
  TypeAhead({this.timeout = const Duration(milliseconds: 700)});

  final Duration timeout;
  String _buffer = '';
  DateTime? _last;

  String get buffer => _buffer;

  /// The index to focus after [character] was typed at [now], or null when
  /// nothing matches. [current] is the focused index (-1 for none).
  int? input(String character, List<String> names, int current, DateTime now) {
    if (names.isEmpty || character.isEmpty) return null;
    final last = _last;
    if (last == null || now.difference(last) > timeout) _buffer = '';
    _last = now;
    _buffer += character.toLowerCase();
    final first = _buffer[0];
    final repeated = _buffer.split('').every((c) => c == first);
    final needle = repeated ? first : _buffer;
    // A fresh letter looks past the current item; a longer prefix may still
    // match the item already focused.
    final start = needle.length == 1 ? current + 1 : math.max(current, 0);
    for (var offset = 0; offset < names.length; offset++) {
      final index = (start + offset) % names.length;
      if (names[index].toLowerCase().startsWith(needle)) return index;
    }
    return null;
  }

  void reset() {
    _buffer = '';
    _last = null;
  }
}
