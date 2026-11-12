part of 'main.dart';

/// Whether typing goes into a text field right now, so single-key shortcuts
/// ('/', '?', type-ahead) must leave the keys alone.
bool _textInputFocused() {
  final context = FocusManager.instance.primaryFocus?.context;
  if (context == null) return false;
  return context.widget is EditableText || context.findAncestorWidgetOfExactType<EditableText>() != null;
}

bool get _commandKey {
  final keys = HardwareKeyboard.instance;
  return keys.isControlPressed || keys.isMetaPressed;
}

/// A pointer that can hover and double-click is around: a click selects, a
/// double-click opens. On touch a tap opens and a long press selects.
bool get _usesMouse => RendererBinding.instance.mouseTracker.mouseIsConnected;

/// Height of one list row, divider included; rows are fixed so the list can
/// jump to any of them.
const double _rowExtent = 57;

/// The keyboard focus model: arrows (←→ in the grid), Home/End, Space,
/// Shift-ranges, Enter, F2, Del, Ctrl/⌘+A/X/V, Esc, `/` and type-ahead; plus
/// what a click, a double-click and a long press do.
mixin _FilesKeyboard on _FilesBase, _FilesActions {
  final TypeAhead _typeAhead = TypeAhead();

  /// Marks where the items start inside the scroll view.
  final GlobalKey itemsStartKey = GlobalKey(debugLabel: 'items-start');

  /// Set while building the grid: tiles per row and a row's height.
  int gridColumns = 1;
  double gridRowExtent = 220;

  bool get gridView => folderView.grid;

  /// The focus ring shows while the page has keyboard focus.
  bool get showFocusRing => panelFocus.hasFocus && FocusManager.instance.highlightMode == FocusHighlightMode.traditional;

  @override
  void initState() {
    super.initState();
    panelFocus.addListener(_onFocusChanged);
  }

  @override
  void dispose() {
    panelFocus.removeListener(_onFocusChanged);
    super.dispose();
  }

  void _onFocusChanged() {
    if (mounted) setState(() {});
  }

  // Pointer

  /// A click opens, as in the other NeoLabs apps; selecting is the
  /// checkbox, Ctrl/⌘-click (toggle) and Shift-click (range). On touch a
  /// tap opens too, or toggles while a selection is being made.
  void onItemTap(FileNode node) {
    panelFocus.requestFocus();
    if (HardwareKeyboard.instance.isShiftPressed) {
      setState(() => selection.rangeTo(node.id));
      return;
    }
    if (_commandKey) {
      setState(() => selection.toggle(node.id));
      return;
    }
    if (_usesMouse) {
      setState(selection.clear);
      open(node);
      return;
    }
    if (!selection.isEmpty) {
      setState(() => selection.toggle(node.id));
      return;
    }
    open(node);
  }

  void onItemLongPress(FileNode node) {
    HapticFeedback.selectionClick();
    setState(() => selection.toggle(node.id));
  }

  void toggleSelected(FileNode node) {
    panelFocus.requestFocus();
    setState(() => selection.toggle(node.id));
  }

  void clearSelection() => setState(selection.clear);

  void selectAll() => setState(selection.selectAll);

  // Keys

  /// What the keys act on: the selection, or else the focused item.
  List<FileNode> get _targets {
    final selected = selectedNodes;
    if (selected.isNotEmpty) return selected;
    final focused = focusedNode;
    return focused == null ? const <FileNode>[] : <FileNode>[focused];
  }

  FileNode? get focusedNode {
    final id = selection.focusedId;
    if (id == null) return null;
    return visibleItems.where((node) => node.id == id).firstOrNull;
  }

  KeyEventResult onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) return KeyEventResult.ignored;
    if (_textInputFocused()) {
      // In the search field only Esc and ↓ (into the results) are ours.
      if (event.logicalKey == LogicalKeyboardKey.escape && searchFocus.hasFocus) {
        if (query.isNotEmpty || filters.active) {
          clearSearch();
        } else {
          panelFocus.requestFocus();
        }
        return KeyEventResult.handled;
      }
      if (event.logicalKey == LogicalKeyboardKey.arrowDown && searchFocus.hasFocus && visibleItems.isNotEmpty) {
        panelFocus.requestFocus();
        _moveFocus(() => selection.home());
        return KeyEventResult.handled;
      }
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    final shift = HardwareKeyboard.instance.isShiftPressed;
    final command = _commandKey;
    final columns = gridView ? gridColumns : 1;

    if (key == LogicalKeyboardKey.arrowDown) return _moveFocus(() => selection.move(columns, extend: shift));
    if (key == LogicalKeyboardKey.arrowUp) return _moveFocus(() => selection.move(-columns, extend: shift));
    if (gridView && key == LogicalKeyboardKey.arrowRight) return _moveFocus(() => selection.move(1, extend: shift));
    if (gridView && key == LogicalKeyboardKey.arrowLeft) return _moveFocus(() => selection.move(-1, extend: shift));
    if (key == LogicalKeyboardKey.home) return _moveFocus(() => selection.home(extend: shift));
    if (key == LogicalKeyboardKey.end) return _moveFocus(() => selection.end(extend: shift));
    if (key == LogicalKeyboardKey.pageDown) return _moveFocus(() => selection.move(columns * 10, extend: shift));
    if (key == LogicalKeyboardKey.pageUp) return _moveFocus(() => selection.move(-columns * 10, extend: shift));

    if (event is KeyRepeatEvent) return KeyEventResult.ignored;

    if (key == LogicalKeyboardKey.space && !command) {
      setState(selection.toggleFocused);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.enter || key == LogicalKeyboardKey.numpadEnter) {
      final focused = focusedNode ?? (selectedNodes.length == 1 ? selectedNodes.first : null);
      if (focused == null) return KeyEventResult.ignored;
      open(focused);
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.f2) {
      final targets = _targets;
      if (targets.length == 1) unawaited(rename(targets.first));
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.delete || (key == LogicalKeyboardKey.backspace && command)) {
      unawaited(trash(_targets));
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.escape) {
      if (selection.isEmpty && !searching && _cutNodes.isEmpty) return KeyEventResult.ignored;
      setState(() {
        selection.clear();
        _cutNodes = const <FileNode>[];
      });
      if (searching) clearSearch();
      return KeyEventResult.handled;
    }
    if (command && key == LogicalKeyboardKey.keyA) {
      selectAll();
      return KeyEventResult.handled;
    }
    if (command && key == LogicalKeyboardKey.keyX) {
      cut(_targets);
      return KeyEventResult.handled;
    }
    if (command && key == LogicalKeyboardKey.keyV) {
      unawaited(paste());
      return KeyEventResult.handled;
    }
    if ((command && key == LogicalKeyboardKey.keyF) || (!command && event.character == '/')) {
      searchFocus.requestFocus();
      return KeyEventResult.handled;
    }
    return _typeAheadKey(event, command);
  }

  KeyEventResult _typeAheadKey(KeyEvent event, bool command) {
    final character = event.character;
    if (command || HardwareKeyboard.instance.isAltPressed || character == null || character.length != 1) return KeyEventResult.ignored;
    if (character.trim().isEmpty || character == '?') return KeyEventResult.ignored;
    final names = visibleItems.map((node) => node.name).toList();
    final index = _typeAhead.input(character, names, selection.focus, DateTime.now());
    if (index == null) return KeyEventResult.handled;
    return _moveFocus(() => selection.focusIndex(index));
  }

  KeyEventResult _moveFocus(VoidCallback change) {
    if (visibleItems.isEmpty) return KeyEventResult.ignored;
    setState(change);
    _revealFocused();
    return KeyEventResult.handled;
  }

  /// Scrolls so the focused item is in view and clear of the floating bars
  /// at the bottom (undo, uploads, selection).
  void _revealFocused() {
    final index = selection.focus;
    final start = itemsStartKey.currentContext?.findRenderObject();
    if (index < 0 || start == null || !scroll.hasClients) return;
    final viewport = RenderAbstractViewport.maybeOf(start);
    if (viewport == null) return;
    final top = viewport.getOffsetToReveal(start, 0).offset;
    final extent = gridView ? gridRowExtent : _rowExtent;
    final itemTop = top + (gridView ? (index ~/ gridColumns) * extent : index * extent);
    final position = scroll.position;
    const topMargin = 16.0;
    const bottomMargin = 96.0;
    double? target;
    if (itemTop < position.pixels + topMargin) {
      target = itemTop - topMargin;
    } else if (itemTop + extent > position.pixels + position.viewportDimension - bottomMargin) {
      target = itemTop + extent - position.viewportDimension + bottomMargin;
    }
    if (target == null) return;
    scroll.jumpTo(target.clamp(position.minScrollExtent, position.maxScrollExtent));
  }
}
