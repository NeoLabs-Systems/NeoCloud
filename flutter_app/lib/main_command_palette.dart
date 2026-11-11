part of 'main.dart';

/// One entry of the command palette.
class _PaletteCommand {
  const _PaletteCommand(this.icon, this.label, this.run, {this.shortcut, this.detail});

  final IconData icon;
  final String label;
  final VoidCallback run;
  final String? shortcut;

  /// A second, muted line: a folder path or a settings page's description.
  final String? detail;
}

/// Commands the open Files page adds (upload, new folder, actions on the
/// selection); set while it is on screen.
List<_PaletteCommand> Function()? _filesPaletteCommands;

/// Ctrl/⌘+K opens the command palette and `?` the shortcut sheet, anywhere
/// in the signed-in app.
class _GlobalShortcuts extends StatelessWidget {
  const _GlobalShortcuts({required this.controller, required this.child});

  final NeoCloudController controller;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Focus(
      canRequestFocus: false,
      skipTraversal: true,
      onKeyEvent: (node, event) {
        if (event is! KeyDownEvent) return KeyEventResult.ignored;
        if (_commandKey && event.logicalKey == LogicalKeyboardKey.keyK) {
          unawaited(showCommandPalette(context, controller));
          return KeyEventResult.handled;
        }
        if (event.character == '?' && !_commandKey && !_textInputFocused()) {
          unawaited(showShortcutSheet(context));
          return KeyEventResult.handled;
        }
        return KeyEventResult.ignored;
      },
      child: child,
    );
  }
}

bool _paletteOpen = false;

Future<void> showCommandPalette(BuildContext context, NeoCloudController controller) async {
  if (_paletteOpen) return;
  _paletteOpen = true;
  final command = await showDialog<_PaletteCommand>(
    context: context,
    barrierColor: Colors.black.withValues(alpha: 0.35),
    builder: (_) => _CommandPalette(controller: controller),
  );
  _paletteOpen = false;
  command?.run();
}

class _CommandPalette extends StatefulWidget {
  const _CommandPalette({required this.controller});

  final NeoCloudController controller;

  @override
  State<_CommandPalette> createState() => _CommandPaletteState();
}

class _CommandPaletteState extends State<_CommandPalette> {
  final TextEditingController _query = TextEditingController();
  final ScrollController _scroll = ScrollController();
  List<FileNode> _recent = const <FileNode>[];
  List<FileNode> _folders = const <FileNode>[];
  Timer? _debounce;
  int _index = 0;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_loadRecent());
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _query.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _loadRecent() async {
    try {
      final files = await _controller.client.recent(by: 'opened');
      if (mounted) setState(() => _recent = files.where((node) => !node.inVault && !node.isFolder).take(6).toList());
    } catch (_) {
      // The palette works without them.
    }
  }

  void _onChanged(String _) {
    setState(() => _index = 0);
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 200), () => unawaited(_findFolders()));
  }

  /// "Go to folder": folders whose name matches what is typed.
  Future<void> _findFolders() async {
    final text = _query.text.trim();
    if (text.isEmpty) {
      setState(() => _folders = const <FileNode>[]);
      return;
    }
    try {
      final found = await _controller.client.search(text, type: 'folder', limit: 8);
      if (mounted && _query.text.trim() == text) setState(() => _folders = found.where((node) => node.isFolder && !node.inVault).toList());
    } catch (_) {
      // Search is optional here.
    }
  }

  void _go(AppSection section) => _controller.setSelectedSection(section);

  /// Every command for the current query, grouped: actions, folders, recent
  /// files, then places and settings pages.
  List<(String, List<_PaletteCommand>)> _groups() {
    final controller = _controller;
    final actions = <_PaletteCommand>[
      ...?_filesPaletteCommands?.call(),
      _PaletteCommand(Icons.keyboard_outlined, appStrings.paletteShortcuts, () => unawaited(showShortcutSheet(_navigatorKey.currentContext!)), shortcut: '?'),
    ];
    final places = <_PaletteCommand>[
      _PaletteCommand(Icons.folder_outlined, appStrings.navAllFiles, () => controller.openFolder(null)),
      _PaletteCommand(Icons.folder_shared_outlined, appStrings.navSharedWithMe, () => controller.openSharedFolder(null)),
      _PaletteCommand(Icons.star_outline_rounded, appStrings.filesNavFavorites, () => _go(AppSection.favorites)),
      _PaletteCommand(Icons.schedule_rounded, appStrings.navRecent, () => _go(AppSection.recent)),
      _PaletteCommand(Icons.delete_outline_rounded, appStrings.navTrash, () => _go(AppSection.trash)),
      if (controller.isAdmin) _PaletteCommand(Icons.admin_panel_settings_outlined, appStrings.navAdmin, () => _go(AppSection.admin)),
    ];
    final settings = <_PaletteCommand>[
      for (final page in SettingsPage.values.where((page) => page.visibleFor(controller)))
        _PaletteCommand(page.icon, '${appStrings.navSettings} › ${page.label}', () {
          controller.setSettingsPage(page);
          _go(AppSection.settings);
        }, detail: page.description),
    ];
    final folders = <_PaletteCommand>[
      for (final folder in _folders)
        _PaletteCommand(Icons.folder_rounded, folder.name, () => controller.openFolder(folder.id), detail: _displayPath(folder)),
    ];
    final recent = <_PaletteCommand>[
      for (final file in _recent)
        _PaletteCommand(Icons.insert_drive_file_outlined, file.name, () {
          final context = _navigatorKey.currentContext;
          if (context != null) unawaited(showFileViewer(context, controller, _recent, _recent.indexOf(file)));
        }, detail: _displayPath(file)),
    ];
    final text = _query.text.trim().toLowerCase();
    List<_PaletteCommand> matching(List<_PaletteCommand> commands) =>
        text.isEmpty ? commands : commands.where((command) => command.label.toLowerCase().contains(text)).toList();
    return <(String, List<_PaletteCommand>)>[
      (appStrings.paletteActions, matching(actions)),
      (appStrings.paletteFolders, folders),
      (appStrings.paletteRecent, matching(recent)),
      (appStrings.palettePlaces, matching(places)),
      (appStrings.navSettings, matching(settings)),
    ].where((group) => group.$2.isNotEmpty).toList();
  }

  KeyEventResult _onKey(FocusNode node, KeyEvent event, List<_PaletteCommand> flat) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) return KeyEventResult.ignored;
    if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
      setState(() => _index = flat.isEmpty ? 0 : (_index + 1) % flat.length);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
      setState(() => _index = flat.isEmpty ? 0 : (_index - 1 + flat.length) % flat.length);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.enter && flat.isNotEmpty) {
      Navigator.of(context).pop(flat[_index.clamp(0, flat.length - 1)]);
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  Widget _row(_PaletteCommand command, bool active) {
    return Material(
      color: active ? _accentMuted : Colors.transparent,
      borderRadius: BorderRadius.circular(AppRadius.control),
      child: InkWell(
        borderRadius: BorderRadius.circular(AppRadius.control),
        onTap: () => Navigator.of(context).pop(command),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          child: Row(
            children: <Widget>[
              Icon(command.icon, size: 18, color: active ? _accent : _textSecondary),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(command.label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 13.5)),
                    if (command.detail != null && command.detail!.isNotEmpty)
                      Text(command.detail!, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 12)),
                  ],
                ),
              ),
              if (command.shortcut != null) _KeyCap(command.shortcut!),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final groups = _groups();
    final flat = <_PaletteCommand>[for (final group in groups) ...group.$2];
    if (_index >= flat.length) _index = 0;
    var position = 0;
    return Dialog(
      alignment: const Alignment(0, -0.6),
      insetPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 40),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560, maxHeight: 480),
        child: Focus(
          onKeyEvent: (node, event) => _onKey(node, event, flat),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Padding(
                padding: const EdgeInsets.all(12),
                child: TextField(
                  controller: _query,
                  autofocus: true,
                  onChanged: _onChanged,
                  decoration: InputDecoration(
                    hintText: appStrings.paletteHint,
                    prefixIcon: Icon(Icons.search_rounded, size: 18, color: _textMuted),
                    suffixIcon: const Padding(padding: EdgeInsets.all(10), child: _KeyCap('Esc')),
                  ),
                ),
              ),
              Divider(height: 1, color: _border),
              Flexible(
                child: flat.isEmpty
                    ? Padding(padding: const EdgeInsets.all(20), child: _EmptyText(appStrings.paletteNothing))
                    : ListView(
                        controller: _scroll,
                        shrinkWrap: true,
                        padding: const EdgeInsets.fromLTRB(8, 6, 8, 10),
                        children: <Widget>[
                          for (final (title, commands) in groups) ...<Widget>[
                            Padding(padding: const EdgeInsets.fromLTRB(12, 10, 12, 4), child: Text(title.toUpperCase(), style: _sectionEyebrowStyle())),
                            for (final command in commands) _row(command, position++ == _index),
                          ],
                        ],
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// A key as printed on the keyboard: "⌘K", "Esc".
class _KeyCap extends StatelessWidget {
  const _KeyCap(this.label);

  final String label;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: _bgTertiary,
        borderRadius: BorderRadius.circular(6),
        border: Border.all(color: _borderLight),
      ),
      child: Text(label, style: GoogleFonts.geistMono(fontSize: 11.5, color: _textSecondary, fontWeight: FontWeight.w500)),
    );
  }
}

/// `?`: every keyboard shortcut of the file browser on one sheet.
Future<void> showShortcutSheet(BuildContext context) {
  final shortcuts = <(String, String)>[
    (_shortcutLabel('K'), appStrings.paletteTitle),
    ('/  ·  ${_shortcutLabel('F')}', appStrings.paletteSearchFiles),
    ('↑ ↓ ← →', appStrings.paletteShortcutMove),
    ('Shift + ↑ ↓', appStrings.paletteShortcutExtend),
    ('Home  ·  End', appStrings.paletteShortcutHomeEnd),
    ('Space', appStrings.paletteShortcutToggle),
    ('↵', appStrings.open),
    ('F2', appStrings.rename),
    ('Del', appStrings.moveToTrash),
    (_shortcutLabel('A'), appStrings.paletteShortcutSelectAll),
    ('${_shortcutLabel('X')}  ·  ${_shortcutLabel('V')}', appStrings.paletteShortcutCutPaste),
    ('Esc', appStrings.paletteShortcutClear),
    ('a–z', appStrings.paletteShortcutTypeAhead),
    ('?', appStrings.paletteShortcuts),
  ];
  return showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(appStrings.paletteShortcuts),
      content: SizedBox(
        width: 440,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              for (final (keys, label) in shortcuts)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 6),
                  child: Row(
                    children: <Widget>[
                      Expanded(child: Text(label, style: TextStyle(color: _textPrimary, fontSize: 13.5))),
                      _KeyCap(keys),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ),
      actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close))],
    ),
  );
}
