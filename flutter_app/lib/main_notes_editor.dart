part of 'main.dart';

// One note, Notion-quiet: a big title, the tags, then the text. Markdown is
// typed as text and shown styled; select text for the formatting bubble
// (phones: the bar above the keyboard), type `/` at the start of a line for
// blocks. Saves by itself a moment after typing stops (NoteSession).

class _NoteEditorView extends StatefulWidget {
  const _NoteEditorView({super.key, required this.controller, required this.session, required this.notebooks, required this.onClose, required this.onDelete});

  final NeoCloudController controller;
  final NoteSession session;
  final List<String> notebooks;
  final VoidCallback onClose;
  final VoidCallback onDelete;

  @override
  State<_NoteEditorView> createState() => _NoteEditorViewState();
}

/// One entry of the `/` menu.
class _SlashCommand {
  const _SlashCommand(this.icon, this.label, this.keywords, this.insert, {this.cursorAt, this.ai = false});

  final IconData icon;
  final String label;
  final String keywords;
  final String insert;
  final int? cursorAt;
  final bool ai;
}

class _NoteEditorViewState extends State<_NoteEditorView> {
  late final TextEditingController _title = TextEditingController(text: widget.session.note.title);
  late final _MarkdownEditingController _body = _MarkdownEditingController(text: widget.session.note.body);
  late final FocusNode _bodyFocus = FocusNode(onKeyEvent: _onBodyKey);
  final FocusNode _titleFocus = FocusNode();
  final ScrollController _scroll = ScrollController();
  final GlobalKey _bodyKey = GlobalKey();
  final GlobalKey _stackKey = GlobalKey();
  bool _reading = false;

  /// Where the selection (or the caret) is, in the editor's Stack.
  Rect? _anchor;
  int _slashIndex = 0;
  String? _suggestedTitle;
  bool _titleAsked = false;

  NoteSession get session => widget.session;
  NeoCloudController get controller => widget.controller;

  /// AI never reads a note in an encrypted notebook.
  bool get _aiOn => session.store.aiAllowed && controller.aiReview.enabled && controller.settings.aiFeatureOn('summaries');

  @override
  void initState() {
    super.initState();
    session.addListener(_onSession);
    _body.addListener(_onBodyChanged);
    _bodyFocus.addListener(_onBodyChanged);
    _scroll.addListener(_onBodyChanged);
    // A new, empty note starts with the cursor in the text.
    if (session.note.body.isEmpty || session.note.body == '- [ ] ') {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        _bodyFocus.requestFocus();
        _body.selection = TextSelection.collapsed(offset: _body.text.length);
      });
    }
  }

  @override
  void dispose() {
    session.removeListener(_onSession);
    _title.dispose();
    _body.dispose();
    _bodyFocus.dispose();
    _titleFocus.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _onSession() {
    if (!mounted) return;
    setState(() {});
    if (session.state == NoteSaveState.saved) unawaited(_maybeSuggestTitle());
  }

  /// Untitled notes get a title suggestion from AI once there is enough text.
  Future<void> _maybeSuggestTitle() async {
    final note = session.note;
    if (_titleAsked || !_aiOn || _titleFocus.hasFocus) return;
    if (note.body.trim().length < 40 || !isAutomaticTitle(note.title, note.body)) return;
    _titleAsked = true;
    try {
      final answer = await controller.client.noteAi(note.id, NoteAiAction.title);
      if (mounted && answer.title != null && answer.title != session.note.title) setState(() => _suggestedTitle = answer.title);
    } catch (_) {
      // A suggestion that can't be made is simply not shown.
    }
  }

  void _onBodyChanged() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final anchor = _selectionAnchor();
      if (anchor != _anchor) setState(() => _anchor = anchor);
    });
    if (mounted) setState(() {});
  }

  RenderEditable? _renderEditable() {
    RenderEditable? found;
    void visit(RenderObject object) {
      if (found != null) return;
      if (object is RenderEditable) {
        found = object;
        return;
      }
      object.visitChildren(visit);
    }

    final root = _bodyKey.currentContext?.findRenderObject();
    if (root != null) visit(root);
    return found;
  }

  Rect? _selectionAnchor() {
    final editable = _renderEditable();
    final stack = _stackKey.currentContext?.findRenderObject() as RenderBox?;
    final selection = _body.selection;
    if (editable == null || stack == null || !selection.isValid || !editable.attached) return null;
    final points = editable.getEndpointsForSelection(selection);
    if (points.isEmpty) return null;
    final lineHeight = editable.preferredLineHeight;
    final start = stack.globalToLocal(editable.localToGlobal(points.first.point - Offset(0, lineHeight)));
    final end = stack.globalToLocal(editable.localToGlobal(points.last.point));
    return Rect.fromPoints(start, end);
  }

  void _setBody(TextEditingValue value) {
    _body.value = value;
    session.edit(body: value.text);
  }

  void _format(TextEditingValue Function(TextEditingValue value) change) {
    _setBody(change(_body.value));
    _bodyFocus.requestFocus();
  }

  List<_SlashCommand> get _commands => <_SlashCommand>[
    _SlashCommand(Icons.checklist_rounded, appStrings.notesBlockChecklist, 'todo task check checklist', '- [ ] '),
    _SlashCommand(Icons.title_rounded, appStrings.notesBlockHeading1, 'h1 heading title', '# '),
    _SlashCommand(Icons.text_fields_rounded, appStrings.notesBlockHeading2, 'h2 heading subtitle', '## '),
    _SlashCommand(Icons.format_list_bulleted_rounded, appStrings.notesBlockBullets, 'bullet list ul', '- '),
    _SlashCommand(Icons.format_list_numbered_rounded, appStrings.notesBlockNumbered, 'number list ol', '1. '),
    _SlashCommand(Icons.format_quote_rounded, appStrings.notesBlockQuote, 'quote', '> '),
    _SlashCommand(Icons.code_rounded, appStrings.notesBlockCode, 'code', '```\n\n```', cursorAt: 4),
    _SlashCommand(Icons.horizontal_rule_rounded, appStrings.notesBlockDivider, 'divider line hr', '---\n'),
    if (_aiOn) _SlashCommand(Icons.auto_awesome_outlined, appStrings.notesAskAi, 'ai ask write', '', ai: true),
  ];

  List<_SlashCommand> _slashMatches(String query) {
    final lower = query.toLowerCase();
    return _commands.where((command) => lower.isEmpty || command.label.toLowerCase().contains(lower) || command.keywords.contains(lower)).toList();
  }

  void _applySlash(_SlashCommand command) {
    if (command.ai) {
      _setBody(replaceSlashCommand(_body.value, ''));
      unawaited(_openAi());
      return;
    }
    _setBody(replaceSlashCommand(_body.value, command.insert, cursorAt: command.cursorAt));
    _bodyFocus.requestFocus();
  }

  KeyEventResult _onBodyKey(FocusNode node, KeyEvent event) {
    final query = slashQuery(_body.value);
    if (query == null || event is KeyUpEvent) return KeyEventResult.ignored;
    final matches = _slashMatches(query);
    if (matches.isEmpty) return KeyEventResult.ignored;
    if (event.logicalKey == LogicalKeyboardKey.arrowDown) {
      setState(() => _slashIndex = (_slashIndex + 1) % matches.length);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.arrowUp) {
      setState(() => _slashIndex = (_slashIndex - 1 + matches.length) % matches.length);
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.escape) {
      _setBody(replaceSlashCommand(_body.value, ''));
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  /// Enter continues a list, or picks the highlighted `/` command.
  TextEditingValue _onEnter(TextEditingValue oldValue, TextEditingValue newValue) {
    final query = slashQuery(oldValue);
    if (query != null) {
      final matches = _slashMatches(query);
      if (matches.isNotEmpty) {
        final command = matches[_slashIndex.clamp(0, matches.length - 1)];
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) _applySlash(command);
        });
        return oldValue;
      }
    }
    return continueList(oldValue) ?? newValue;
  }

  void _toggleTask(int line) => _setBody(TextEditingValue(text: toggleTaskLine(_body.text, line)));

  Future<void> _editTags() async {
    final tag = await _askText(context, title: appStrings.notesAddTag, label: appStrings.notesTagName);
    if (tag == null || tag.trim().isEmpty) return;
    await session.setProperties(tags: <String>[...session.note.tags, tag.trim().replaceFirst('#', '')]);
  }

  Future<void> _addTags(List<String> tags) => session.setProperties(tags: <String>{...session.note.tags, ...tags}.toList());

  Future<void> _rename(String title) async {
    _title.text = title;
    setState(() => _suggestedTitle = null);
    session.edit(title: title);
    await session.flush();
  }

  Future<void> _moveToNotebook() async {
    final choice = await showAdaptiveSheet<String>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(appStrings.notesMoveTo),
        content: SizedBox(
          width: 380,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              ListTile(leading: const Icon(Icons.notes_rounded), title: Text(appStrings.notesNoNotebook), selected: session.note.notebook.isEmpty, onTap: () => Navigator.of(dialogContext).pop('')),
              for (final name in widget.notebooks)
                ListTile(leading: const Icon(Icons.book_outlined), title: Text(name), selected: session.note.notebook == name, onTap: () => Navigator.of(dialogContext).pop(name)),
              ListTile(leading: const Icon(Icons.add_rounded), title: Text(appStrings.notesNewNotebook), onTap: () => Navigator.of(dialogContext).pop('\u0000new')),
            ],
          ),
        ),
        actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel))],
      ),
    );
    if (choice == null || !mounted) return;
    var target = choice;
    if (choice == '\u0000new') {
      final name = await _askText(context, title: appStrings.notesNewNotebook, label: appStrings.notesNotebookName);
      if (name == null || name.trim().isEmpty) return;
      target = name.trim();
    }
    await session.setProperties(notebook: target);
  }

  Future<void> _openAi() async {
    final selection = _body.selection;
    final selected = selection.isValid && !selection.isCollapsed ? selection.textInside(_body.text) : '';
    await session.flush();
    if (!mounted) return;
    await _showNoteAiMenu(
      context,
      controller: controller,
      note: session.note,
      selection: selected,
      onInsert: (text) => _format((value) => insertBelow(value, text)),
      onReplace: selected.isEmpty ? null : (text) => _format((value) => replaceSelection(value, text)),
      onTitle: (title) => unawaited(_rename(title)),
      onTags: (tags) => unawaited(_addTags(tags)),
    );
  }

  Future<void> _close() async {
    if (_title.text.trim().isEmpty) _title.text = session.note.title;
    widget.onClose();
  }

  @override
  Widget build(BuildContext context) {
    final phone = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    final note = session.note;
    final query = _bodyFocus.hasFocus && !_reading ? slashQuery(_body.value) : null;
    final matches = query == null ? const <_SlashCommand>[] : _slashMatches(query);
    if (_slashIndex >= matches.length) _slashIndex = 0;
    final selection = _body.selection;
    final showBubble = !phone && !_reading && _bodyFocus.hasFocus && selection.isValid && !selection.isCollapsed && matches.isEmpty;
    final anchor = _anchor;
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) unawaited(_close());
      },
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          _NoteTopBar(
            session: session,
            phone: phone,
            aiOn: _aiOn,
            reading: _reading,
            onBack: () => unawaited(_close()),
            onAi: () => unawaited(_openAi()),
            onReading: () => setState(() => _reading = !_reading),
            onPin: () => unawaited(session.setProperties(pinned: !note.pinned)),
            onColor: () async {
              final color = await _pickNoteColor(context, note.color);
              if (color != null) unawaited(session.setProperties(color: color));
            },
            menu: <_FileAction>[
              // A note can't leave its vault, nor join one: the server refuses both.
              if (!note.encrypted) _FileAction(Icons.drive_file_move_outline, appStrings.notesMoveTo, () => unawaited(_moveToNotebook())),
              _FileAction(note.archived ? Icons.unarchive_outlined : Icons.archive_outlined, note.archived ? appStrings.notesUnarchive : appStrings.notesArchive, () async {
                await session.setProperties(archived: !note.archived, pinned: note.archived ? null : false);
                if (!note.archived) widget.onClose();
              }),
              _FileAction(Icons.delete_outline_rounded, appStrings.notesMoveToTrash, widget.onDelete, danger: true),
            ],
          ),
          if (session.state == NoteSaveState.conflict)
            _NoteConflictBanner(onKeepMine: () => unawaited(session.overwrite()), onTakeTheirs: () async {
              await session.reload();
              if (!mounted) return;
              _body.text = session.note.body;
              _title.text = session.note.title;
            }),
          Expanded(
            child: Stack(
              key: _stackKey,
              clipBehavior: Clip.none,
              children: <Widget>[
                Positioned.fill(
                  child: SingleChildScrollView(
                    controller: _scroll,
                    padding: EdgeInsets.fromLTRB(phone ? 20 : 32, phone ? 12 : 40, phone ? 20 : 32, 120),
                    child: Center(
                      child: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 720),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: <Widget>[
                            TextField(
                              controller: _title,
                              focusNode: _titleFocus,
                              maxLines: null,
                              textInputAction: TextInputAction.next,
                              onChanged: (value) => session.edit(title: value),
                              onSubmitted: (_) => _bodyFocus.requestFocus(),
                              style: _displayTitleStyle(phone ? 26 : 34),
                              decoration: InputDecoration(
                                hintText: appStrings.notesTitleHint,
                                border: InputBorder.none,
                                enabledBorder: InputBorder.none,
                                focusedBorder: InputBorder.none,
                                filled: false,
                                isCollapsed: true,
                              ),
                            ),
                            if (_suggestedTitle != null) ...<Widget>[
                              const SizedBox(height: 10),
                              _TitleSuggestion(title: _suggestedTitle!, onUse: () => unawaited(_rename(_suggestedTitle!)), onDismiss: () => setState(() => _suggestedTitle = null)),
                            ],
                            const SizedBox(height: 12),
                            Wrap(
                              spacing: 6,
                              runSpacing: 6,
                              crossAxisAlignment: WrapCrossAlignment.center,
                              children: <Widget>[
                                if (note.encrypted)
                                  _NoteChip(label: note.notebook, icon: Icons.lock_outline_rounded, small: true, onTap: () {})
                                else if (note.notebook.isNotEmpty)
                                  _NoteChip(label: note.notebook, icon: Icons.book_outlined, small: true, onTap: () => unawaited(_moveToNotebook())),
                                for (final tag in note.tags)
                                  _TagPill(tag, onRemove: () => unawaited(session.setProperties(tags: note.tags.where((item) => item != tag).toList()))),
                                _NoteChip(label: appStrings.notesAddTagChip, icon: Icons.add_rounded, dashed: true, small: true, onTap: () => unawaited(_editTags())),
                              ],
                            ),
                            const SizedBox(height: 24),
                            if (_reading)
                              _NoteReadView(body: _body.text, onToggleTask: _toggleTask)
                            else
                              KeyedSubtree(
                                key: _bodyKey,
                                child: TextField(
                                  controller: _body,
                                  focusNode: _bodyFocus,
                                  maxLines: null,
                                  minLines: 8,
                                  keyboardType: TextInputType.multiline,
                                  inputFormatters: <TextInputFormatter>[_NoteEnterFormatter(_onEnter)],
                                  onChanged: (value) => session.edit(body: value),
                                  style: TextStyle(fontSize: 16, height: 1.65, color: _textPrimary.withValues(alpha: 0.92)),
                                  decoration: InputDecoration(
                                    hintText: appStrings.notesBodyHint,
                                    border: InputBorder.none,
                                    enabledBorder: InputBorder.none,
                                    focusedBorder: InputBorder.none,
                                    filled: false,
                                    isCollapsed: true,
                                  ),
                                ),
                              ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
                if (showBubble && anchor != null)
                  Positioned(
                    left: math.max(8, anchor.left - 20),
                    top: math.max(0, anchor.top - 48),
                    child: _FormatBubble(onFormat: _format, aiOn: _aiOn, onAi: () => unawaited(_openAi())),
                  ),
                if (matches.isNotEmpty && anchor != null)
                  Positioned(
                    left: phone ? 12 : math.max(8, anchor.left),
                    right: phone ? 12 : null,
                    top: anchor.bottom + 8,
                    child: _SlashMenu(commands: matches, highlighted: _slashIndex, onPick: _applySlash),
                  ),
              ],
            ),
          ),
          if (phone && _bodyFocus.hasFocus && !_reading) _PhoneFormatBar(onFormat: _format, aiOn: _aiOn, onAi: () => unawaited(_openAi())),
        ],
      ),
    );
  }
}

/// Sees Enter typed into the note (hardware or on-screen keyboard).
class _NoteEnterFormatter extends TextInputFormatter {
  _NoteEnterFormatter(this.onEnter);

  final TextEditingValue Function(TextEditingValue oldValue, TextEditingValue newValue) onEnter;

  @override
  TextEditingValue formatEditUpdate(TextEditingValue oldValue, TextEditingValue newValue) {
    final cursor = newValue.selection.baseOffset;
    final typedEnter = newValue.text.length == oldValue.text.length + 1 && cursor > 0 && newValue.selection.isCollapsed && newValue.text[cursor - 1] == '\n' && oldValue.selection.isCollapsed && oldValue.selection.baseOffset == cursor - 1;
    return typedEnter ? onEnter(oldValue, newValue) : newValue;
  }
}

class _NoteTopBar extends StatelessWidget {
  const _NoteTopBar({
    required this.session,
    required this.phone,
    required this.aiOn,
    required this.reading,
    required this.onBack,
    required this.onAi,
    required this.onReading,
    required this.onPin,
    required this.onColor,
    required this.menu,
  });

  final NoteSession session;
  final bool phone;
  final bool aiOn;
  final bool reading;
  final VoidCallback onBack;
  final VoidCallback onAi;
  final VoidCallback onReading;
  final VoidCallback onPin;
  final VoidCallback onColor;
  final List<_FileAction> menu;

  @override
  Widget build(BuildContext context) {
    final note = session.note;
    final (label, color) = switch (session.state) {
      NoteSaveState.saved => (appStrings.notesSaved, _success),
      NoteSaveState.unsaved => (appStrings.notesUnsaved, _textMuted),
      NoteSaveState.saving => (appStrings.notesSaving, _textMuted),
      NoteSaveState.failed => (appStrings.notesSaveFailed, _danger),
      NoteSaveState.conflict => (appStrings.notesConflict, _warning),
    };
    return Container(
      padding: EdgeInsets.fromLTRB(phone ? 4 : 12, 8, 12, 8),
      decoration: BoxDecoration(border: Border(bottom: BorderSide(color: _border))),
      child: Row(
        children: <Widget>[
          IconButton(tooltip: appStrings.notesBack, onPressed: onBack, icon: Icon(phone ? Icons.arrow_back_rounded : Icons.close_rounded, color: _textSecondary)),
          if (!phone) ...<Widget>[
            Flexible(
              child: Text(
                note.notebook.isEmpty ? appStrings.notesTitle : '${appStrings.notesTitle} / ${note.notebook}',
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 13, color: _textMuted),
              ),
            ),
            const SizedBox(width: 14),
          ],
          Semantics(
            liveRegion: true,
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Container(width: 6, height: 6, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
                const SizedBox(width: 6),
                Text(label, style: TextStyle(fontSize: 12, color: _textSecondary)),
              ],
            ),
          ),
          const Spacer(),
          if (aiOn)
            phone
                ? IconButton(tooltip: appStrings.notesAi, onPressed: onAi, icon: Icon(Icons.auto_awesome_outlined, color: _accent))
                : OutlinedButton.icon(
                    onPressed: onAi,
                    style: OutlinedButton.styleFrom(foregroundColor: _accent, side: BorderSide(color: _accent.withValues(alpha: 0.4)), backgroundColor: _accentMuted),
                    icon: const Icon(Icons.auto_awesome_outlined, size: 16),
                    label: Text(appStrings.notesAi),
                  ),
          IconButton(
            tooltip: note.pinned ? appStrings.notesUnpin : appStrings.notesPin,
            onPressed: onPin,
            icon: Icon(note.pinned ? Icons.push_pin : Icons.push_pin_outlined, size: 20, color: note.pinned ? _accent : _textSecondary),
          ),
          if (!phone)
            IconButton(
              tooltip: appStrings.notesColor,
              onPressed: onColor,
              icon: Container(
                width: 18,
                height: 18,
                decoration: BoxDecoration(shape: BoxShape.circle, color: _noteTint(note.color).background, border: Border.all(color: _noteTint(note.color).dot, width: 2)),
              ),
            ),
          IconButton(
            tooltip: reading ? appStrings.notesEdit : appStrings.notesRead,
            onPressed: onReading,
            icon: Icon(reading ? Icons.edit_outlined : Icons.visibility_outlined, size: 20, color: _textSecondary),
          ),
          _FileActionsMenu(actions: phone ? <_FileAction>[_FileAction(Icons.palette_outlined, appStrings.notesColor, onColor), ...menu] : menu, title: note.title),
        ],
      ),
    );
  }
}

class _NoteConflictBanner extends StatelessWidget {
  const _NoteConflictBanner({required this.onKeepMine, required this.onTakeTheirs});

  final VoidCallback onKeepMine;
  final VoidCallback onTakeTheirs;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(20, 10, 12, 10),
      color: _warning.withValues(alpha: 0.12),
      child: Wrap(
        crossAxisAlignment: WrapCrossAlignment.center,
        spacing: 8,
        runSpacing: 6,
        children: <Widget>[
          Text(appStrings.notesConflictMessage, style: TextStyle(color: _textPrimary, fontSize: 13)),
          TextButton(onPressed: onTakeTheirs, child: Text(appStrings.notesTakeTheirs)),
          FilledButton(onPressed: onKeepMine, child: Text(appStrings.notesKeepMine)),
        ],
      ),
    );
  }
}

class _TitleSuggestion extends StatelessWidget {
  const _TitleSuggestion({required this.title, required this.onUse, required this.onDismiss});

  final String title;
  final VoidCallback onUse;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 6, 4, 6),
      decoration: BoxDecoration(color: _accentMuted, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _accent.withValues(alpha: 0.3))),
      child: Row(
        children: <Widget>[
          Icon(Icons.auto_awesome_outlined, size: 16, color: _accent),
          const SizedBox(width: 8),
          Expanded(
            child: Text.rich(
              TextSpan(children: <InlineSpan>[
                TextSpan(text: '${appStrings.notesSuggestedTitle} ', style: TextStyle(color: _textSecondary)),
                TextSpan(text: title, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
              ]),
              style: const TextStyle(fontSize: 13),
            ),
          ),
          TextButton(onPressed: onUse, child: Text(appStrings.notesUseTitle)),
          IconButton(tooltip: appStrings.close, onPressed: onDismiss, icon: Icon(Icons.close_rounded, size: 18, color: _textMuted)),
        ],
      ),
    );
  }
}

/// The formatting bubble above selected text (wide screens).
class _FormatBubble extends StatelessWidget {
  const _FormatBubble({required this.onFormat, required this.aiOn, required this.onAi});

  final void Function(TextEditingValue Function(TextEditingValue value) change) onFormat;
  final bool aiOn;
  final VoidCallback onAi;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: _bgTertiary,
      elevation: 8,
      shadowColor: Colors.black54,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12), side: BorderSide(color: _borderLight)),
      child: Padding(
        padding: const EdgeInsets.all(4),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            ..._formatButtons(onFormat, compact: true),
            if (aiOn) ...<Widget>[
              Container(width: 1, height: 18, color: _borderLight, margin: const EdgeInsets.symmetric(horizontal: 4)),
              TextButton.icon(
                onPressed: onAi,
                style: TextButton.styleFrom(foregroundColor: _accent, visualDensity: VisualDensity.compact),
                icon: const Icon(Icons.auto_awesome_outlined, size: 15),
                label: Text(appStrings.notesAskAi),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// The formatting bar above the phone's keyboard.
class _PhoneFormatBar extends StatelessWidget {
  const _PhoneFormatBar({required this.onFormat, required this.aiOn, required this.onAi});

  final void Function(TextEditingValue Function(TextEditingValue value) change) onFormat;
  final bool aiOn;
  final VoidCallback onAi;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(color: _bgSecondary, border: Border(top: BorderSide(color: _border))),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
      child: Row(
        children: <Widget>[
          Expanded(
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(children: _formatButtons(onFormat, compact: false, lines: true)),
            ),
          ),
          if (aiOn) IconButton(tooltip: appStrings.notesAskAi, onPressed: onAi, icon: Icon(Icons.auto_awesome_outlined, color: _accent)),
        ],
      ),
    );
  }
}

List<Widget> _formatButtons(void Function(TextEditingValue Function(TextEditingValue value) change) onFormat, {required bool compact, bool lines = false}) {
  Widget button(String tooltip, Widget icon, TextEditingValue Function(TextEditingValue value) change) => IconButton(
    tooltip: tooltip,
    visualDensity: compact ? VisualDensity.compact : VisualDensity.standard,
    onPressed: () => onFormat(change),
    icon: icon,
  );
  Widget glyph(String text, {FontWeight weight = FontWeight.w700, FontStyle style = FontStyle.normal, TextDecoration? decoration}) =>
      Text(text, style: TextStyle(fontSize: 15, fontWeight: weight, fontStyle: style, decoration: decoration, color: _textPrimary));
  return <Widget>[
    if (lines) button(appStrings.notesBlockChecklist, Icon(Icons.checklist_rounded, size: 20, color: _textPrimary), (value) => toggleLinePrefix(value, '- [ ] ')),
    if (lines) button(appStrings.notesBlockHeading2, glyph('H'), (value) => toggleLinePrefix(value, '## ')),
    button(appStrings.notesBold, glyph('B', weight: FontWeight.w800), (value) => toggleInlineMarker(value, '**')),
    button(appStrings.notesItalic, glyph('I', weight: FontWeight.w400, style: FontStyle.italic), (value) => toggleInlineMarker(value, '*')),
    button(appStrings.notesStrike, glyph('S', weight: FontWeight.w500, decoration: TextDecoration.lineThrough), (value) => toggleInlineMarker(value, '~~')),
    button(appStrings.notesCode, Icon(Icons.code_rounded, size: 18, color: _textPrimary), (value) => toggleInlineMarker(value, '`')),
    if (lines) button(appStrings.notesBlockBullets, Icon(Icons.format_list_bulleted_rounded, size: 20, color: _textPrimary), (value) => toggleLinePrefix(value, '- ')),
    if (!lines) button(appStrings.notesLink, Icon(Icons.link_rounded, size: 18, color: _textPrimary), (value) => toggleInlineMarker(value, '[', closing: '](https://)')),
  ];
}

/// The `/` menu at the cursor.
class _SlashMenu extends StatelessWidget {
  const _SlashMenu({required this.commands, required this.highlighted, required this.onPick});

  final List<_SlashCommand> commands;
  final int highlighted;
  final ValueChanged<_SlashCommand> onPick;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: _bgTertiary,
      elevation: 10,
      shadowColor: Colors.black54,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14), side: BorderSide(color: _borderLight)),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 300, maxHeight: 320),
        child: ListView(
          padding: const EdgeInsets.all(6),
          shrinkWrap: true,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(10, 6, 10, 6),
              child: Text(appStrings.notesBlocks.toUpperCase(), style: TextStyle(fontSize: 11, letterSpacing: 0.9, color: _textMuted, fontWeight: FontWeight.w600)),
            ),
            for (var index = 0; index < commands.length; index += 1)
              Material(
                color: index == highlighted ? _accentMuted : Colors.transparent,
                borderRadius: BorderRadius.circular(10),
                child: InkWell(
                  borderRadius: BorderRadius.circular(10),
                  onTap: () => onPick(commands[index]),
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
                    child: Row(
                      children: <Widget>[
                        Container(
                          width: 32,
                          height: 32,
                          decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(8), border: Border.all(color: _border)),
                          child: Icon(commands[index].icon, size: 17, color: commands[index].ai || index == highlighted ? _accent : _textSecondary),
                        ),
                        const SizedBox(width: 12),
                        Text(commands[index].label, style: TextStyle(fontSize: 14, color: _textPrimary, fontWeight: FontWeight.w500)),
                      ],
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
