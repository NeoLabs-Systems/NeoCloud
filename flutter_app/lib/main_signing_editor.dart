part of 'main.dart';

// The editor of a signing draft (/app/signing/<id> while the document is a
// draft). Document state lives in SigningEditorState (src/signing); this
// file is the page frame: top bar, layout for desktop, tablet and phone,
// keyboard shortcuts. The pages and their overlays are in
// main_signing_editor_pages.dart, the side panel in main_signing_inspector.dart.

/// How the pages are drawn. The app uses pdfrx; tests pass a stand-in
/// because PDFium isn't available there.
typedef SigningPagesBuilder = Widget Function(BuildContext context, Uint8List pdf, SigningPageController controller, PageOverlayBuilder overlay);

Widget _pdfPagesBuilder(BuildContext context, Uint8List pdf, SigningPageController controller, PageOverlayBuilder overlay) =>
    SigningPdfPages(bytes: pdf, controller: controller, overlayBuilder: overlay);

class SigningEditorPage extends StatefulWidget {
  const SigningEditorPage({
    super.key,
    required this.controller,
    required this.detail,
    this.onSent,
    this.pagesBuilder = _pdfPagesBuilder,
  });

  final NeoCloudController controller;
  final SigningDetail detail;

  /// The draft was sent; the page shows its progress now.
  final ValueChanged<SigningDetail>? onSent;
  final SigningPagesBuilder pagesBuilder;

  @override
  State<SigningEditorPage> createState() => _SigningEditorPageState();
}

class _SigningEditorPageState extends State<SigningEditorPage> with _DraftPages<SigningEditorPage> {
  late final SigningEditorState _state;
  final SigningPageController _pages = SigningPageController();
  final FocusNode _focus = FocusNode(debugLabel: 'signing-editor');
  final FocusNode _titleFocus = FocusNode();
  late final TextEditingController _title;
  bool _railOpen = true;

  NeoCloudController get _controller => widget.controller;

  @override
  NeoCloudController get draftController => widget.controller;
  @override
  SigningEditorState get draftState => _state;
  @override
  SigningPageController get draftPages => _pages;

  @override
  void initState() {
    super.initState();
    _state = SigningEditorState(
      api: _controller.signing,
      detail: widget.detail,
      partyName: (number) => appStrings.signingPartyDefaultName('$number'),
    );
    final me = _controller.user;
    if (_controller.emailConfigured && me != null && (me.email ?? '').isNotEmpty) {
      _state.me = (name: (me.displayName ?? '').trim().isEmpty ? me.username : me.displayName!.trim(), email: me.email!);
    }
    _title = TextEditingController(text: widget.detail.document.title);
    _titleFocus.addListener(() {
      if (!_titleFocus.hasFocus) _commitTitle();
    });
    watchDraftPdf();
  }

  @override
  void dispose() {
    unwatchDraftPdf();
    // Whatever is still unsaved goes out before the state is released.
    unawaited(_state.flush().whenComplete(_state.dispose));
    _pages.dispose();
    _focus.dispose();
    _titleFocus.dispose();
    _title.dispose();
    super.dispose();
  }

  void _commitTitle() {
    final text = _title.text.trim();
    if (text.isEmpty) {
      _title.text = _state.title;
      return;
    }
    _state.setTitle(text);
  }

  void _toast(String message) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));

  Future<void> _leave() async {
    _commitTitle();
    final saved = await _state.flush();
    if (!mounted) return;
    if (!saved && !_state.locked) {
      final leave = await showAdaptiveSheet<bool>(
        context: context,
        builder: (dialogContext) => _AdaptiveDialog(
          title: Text(appStrings.signingLeaveTitle),
          content: Text(appStrings.signingLeaveBody, style: TextStyle(color: _textSecondary, height: 1.45)),
          actions: <Widget>[
            TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.signingStay)),
            TextButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.signingLeave)),
          ],
        ),
      );
      if (leave != true || !mounted) return;
    }
    _controller.openSigning(null);
  }

  Future<void> _send() async {
    _commitTitle();
    if (_state.fieldsWithProblems.isNotEmpty) {
      _state.select(_state.fieldsWithProblems.first);
      _toast(appStrings.signingProblemsBlock);
      return;
    }
    final detail = await showSigningSendSheet(context, _controller, _state);
    if (detail != null && mounted) widget.onSent?.call(detail);
  }

  Future<void> _autodetect() async {
    try {
      final count = await _state.detectFields();
      if (mounted) _toast(count == 0 ? appStrings.signingAutodetectNone : appStrings.signingAutodetectDone('$count'));
    } catch (error) {
      if (mounted) _toast(signingErrorText(_controller, error, appStrings.signingLoadFailed));
    }
  }

  Future<void> _duplicateAsNew() async {
    try {
      final copy = await _controller.signing.duplicate(_state.document.id);
      _controller.openSigning(copy.document.id);
    } catch (error) {
      if (mounted) _toast(signingErrorText(_controller, error, appStrings.signingPrepareFailed));
    }
  }

  Future<void> _reload() async {
    try {
      _state.adopt(await _controller.signing.get(_state.document.id));
    } catch (error) {
      if (mounted) _toast(signingErrorText(_controller, error, appStrings.signingLoadFailed));
    }
  }

  // Keyboard

  KeyEventResult _onKey(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) return KeyEventResult.ignored;
    final typing = FocusManager.instance.primaryFocus?.context?.findAncestorStateOfType<EditableTextState>() != null;
    if (typing) return KeyEventResult.ignored;
    final keys = HardwareKeyboard.instance;
    final command = keys.isControlPressed || keys.isMetaPressed;
    final shift = keys.isShiftPressed;
    final key = event.logicalKey;
    if (command) {
      if (key == LogicalKeyboardKey.keyZ) {
        shift ? _state.redo() : _state.undo();
      } else if (key == LogicalKeyboardKey.keyY) {
        _state.redo();
      } else if (key == LogicalKeyboardKey.keyC) {
        _state.copySelected();
      } else if (key == LogicalKeyboardKey.keyV) {
        _state.paste(page: _pages.currentPage);
      } else if (key == LogicalKeyboardKey.keyD) {
        _state.duplicateSelected();
      } else {
        return KeyEventResult.ignored;
      }
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.delete || key == LogicalKeyboardKey.backspace) {
      if (_state.selected == null) return KeyEventResult.ignored;
      _state.deleteSelected();
      return KeyEventResult.handled;
    }
    if (key == LogicalKeyboardKey.escape) {
      if (_state.armedType != null) {
        _state.arm(null);
      } else {
        _state.select(null);
      }
      return KeyEventResult.handled;
    }
    final step = shift ? 0.02 : 0.002;
    Offset? move;
    if (key == LogicalKeyboardKey.arrowLeft) move = Offset(-step, 0);
    if (key == LogicalKeyboardKey.arrowRight) move = Offset(step, 0);
    if (key == LogicalKeyboardKey.arrowUp) move = Offset(0, -step);
    if (key == LogicalKeyboardKey.arrowDown) move = Offset(0, step);
    if (move != null && _state.selected != null) {
      _state.nudge(move.dx, move.dy);
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  // Layout

  @override
  Widget build(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    final phone = width < 700;
    final wide = width >= 1000;
    final pagesArea = _PagesArea(
      state: _state,
      pages: _pages,
      pdf: draftPdf,
      error: draftPdfError,
      pagesBuilder: widget.pagesBuilder,
      onRotate: rotateDraftPages,
    );
    return Focus(
      focusNode: _focus,
      autofocus: true,
      onKeyEvent: _onKey,
      child: Container(
        color: _bgPrimary,
        child: Column(
          children: <Widget>[
            _EditorTopBar(
              state: _state,
              title: _title,
              titleFocus: _titleFocus,
              phone: phone,
              railOpen: _railOpen,
              onBack: () => unawaited(_leave()),
              onSend: () => unawaited(_send()),
              onAutodetect: () => unawaited(_autodetect()),
              onToggleRail: () => setState(() => _railOpen = !_railOpen),
              onCommitTitle: _commitTitle,
            ),
            Divider(height: 1, color: _border),
            _EditorBanners(state: _state, onDuplicate: () => unawaited(_duplicateAsNew()), onReload: () => unawaited(_reload())),
            Expanded(
              child: phone
                  ? pagesArea
                  : Row(
                      children: <Widget>[
                        if (_railOpen || wide) _PageRail(state: _state, pages: _pages),
                        Expanded(child: pagesArea),
                        VerticalDivider(width: 1, color: _border),
                        SizedBox(
                          width: wide ? 330 : 290,
                          child: _SigningSidePanel(state: _state, onAutodetect: () => unawaited(_autodetect())),
                        ),
                      ],
                    ),
            ),
            if (phone) _PhoneBar(state: _state, pages: _pages, onAutodetect: () => unawaited(_autodetect())),
          ],
        ),
      ),
    );
  }
}

class _EditorTopBar extends StatelessWidget {
  const _EditorTopBar({
    required this.state,
    required this.title,
    required this.titleFocus,
    required this.phone,
    required this.railOpen,
    required this.onBack,
    required this.onSend,
    required this.onAutodetect,
    required this.onToggleRail,
    required this.onCommitTitle,
  });

  final SigningEditorState state;
  final TextEditingController title;
  final FocusNode titleFocus;
  final bool phone;
  final bool railOpen;
  final VoidCallback onBack;
  final VoidCallback onSend;
  final VoidCallback onAutodetect;
  final VoidCallback onToggleRail;
  final VoidCallback onCommitTitle;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) {
        return Padding(
          padding: EdgeInsets.symmetric(horizontal: phone ? 4 : 12, vertical: 6),
          child: Row(
            children: <Widget>[
              IconButton(key: const ValueKey<String>('signing-back'), tooltip: appStrings.signingFinish, onPressed: onBack, icon: const Icon(Icons.arrow_back_rounded)),
              Expanded(
                child: TextField(
                  key: const ValueKey<String>('signing-title'),
                  controller: title,
                  focusNode: titleFocus,
                  readOnly: state.locked,
                  maxLength: 200,
                  buildCounter: (context, {required currentLength, required isFocused, maxLength}) => null,
                  textInputAction: TextInputAction.done,
                  onSubmitted: (_) => onCommitTitle(),
                  style: TextStyle(color: _textPrimary, fontSize: 16, fontWeight: FontWeight.w600),
                  decoration: InputDecoration(
                    hintText: appStrings.signingTitleHint,
                    isDense: true,
                    filled: false,
                    border: InputBorder.none,
                    enabledBorder: InputBorder.none,
                    focusedBorder: UnderlineInputBorder(borderSide: BorderSide(color: _accent)),
                  ),
                ),
              ),
              _SaveStatus(state: state, compact: phone),
              if (!phone) ...<Widget>[
                IconButton(key: const ValueKey<String>('signing-undo'), tooltip: appStrings.signingUndo, onPressed: state.canUndo ? state.undo : null, icon: const Icon(Icons.undo_rounded)),
                IconButton(key: const ValueKey<String>('signing-redo'), tooltip: appStrings.signingRedo, onPressed: state.canRedo ? state.redo : null, icon: const Icon(Icons.redo_rounded)),
              ],
              PopupMenuButton<String>(
                tooltip: appStrings.filesMoreActions,
                icon: Icon(Icons.more_horiz_rounded, color: _textMuted),
                onSelected: (value) {
                  switch (value) {
                    case 'autodetect':
                      onAutodetect();
                    case 'rail':
                      onToggleRail();
                    case 'undo':
                      state.undo();
                    case 'redo':
                      state.redo();
                  }
                },
                itemBuilder: (context) => <PopupMenuEntry<String>>[
                  if (!state.locked) PopupMenuItem<String>(value: 'autodetect', child: Text(appStrings.signingAutodetect)),
                  if (phone) PopupMenuItem<String>(value: 'undo', enabled: state.canUndo, child: Text(appStrings.signingUndo)),
                  if (phone) PopupMenuItem<String>(value: 'redo', enabled: state.canRedo, child: Text(appStrings.signingRedo)),
                  if (!phone) PopupMenuItem<String>(value: 'rail', child: Text(appStrings.signingPages)),
                ],
              ),
              const SizedBox(width: 4),
              FilledButton(
                key: const ValueKey<String>('signing-send'),
                onPressed: state.locked ? null : onSend,
                child: Text(appStrings.signingSend),
              ),
            ],
          ),
        );
      },
    );
  }
}

class _SaveStatus extends StatelessWidget {
  const _SaveStatus({required this.state, required this.compact});

  final SigningEditorState state;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final IconData icon;
    final String text;
    final Color color;
    switch (state.saveState) {
      case SigningSaveState.saved:
        icon = Icons.check_rounded;
        text = appStrings.signingSaved;
        color = _success;
      case SigningSaveState.saving:
        icon = Icons.sync_rounded;
        text = appStrings.signingSaving;
        color = _textMuted;
      case SigningSaveState.dirty:
        icon = Icons.edit_outlined;
        text = appStrings.signingUnsaved;
        color = _textMuted;
      case SigningSaveState.error:
        icon = Icons.error_outline_rounded;
        text = appStrings.signingSaveFailed;
        color = _danger;
      case SigningSaveState.locked:
        icon = Icons.lock_outline_rounded;
        text = appStrings.signingLockedTitle;
        color = _textMuted;
    }
    final label = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Icon(icon, size: 16, color: color),
        if (!compact) ...<Widget>[const SizedBox(width: 6), Text(text, key: const ValueKey<String>('signing-save-status'), style: TextStyle(color: color, fontSize: 12.5))],
      ],
    );
    return Tooltip(
      message: text,
      child: InkWell(
        borderRadius: BorderRadius.circular(AppRadius.tag),
        onTap: state.saveState == SigningSaveState.error ? () => unawaited(state.flush()) : null,
        child: Padding(padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6), child: label),
      ),
    );
  }
}

/// The locked and conflict explanations above the pages.
class _EditorBanners extends StatelessWidget {
  const _EditorBanners({required this.state, required this.onDuplicate, required this.onReload});

  final SigningEditorState state;
  final VoidCallback onDuplicate;
  final VoidCallback onReload;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) {
        if (!state.locked) return const SizedBox.shrink();
        final conflict = state.lastError?.kind == SigningErrorKind.conflict;
        return Container(
          width: double.infinity,
          color: _bgTertiary,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
          child: Wrap(
            spacing: 12,
            runSpacing: 6,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              Icon(Icons.lock_outline_rounded, size: 18, color: _textSecondary),
              ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 620),
                child: Text(
                  conflict ? appStrings.signingConflict : '${appStrings.signingLockedTitle}. ${appStrings.signingLockedBody}',
                  style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4),
                ),
              ),
              OutlinedButton(key: const ValueKey<String>('signing-duplicate-new'), onPressed: onDuplicate, child: Text(appStrings.signingDuplicateAsNew)),
              if (conflict) TextButton(onPressed: onReload, child: Text(appStrings.signingReload)),
            ],
          ),
        );
      },
    );
  }
}

/// Phone: the fields sheet, page buttons and the settings of the selection.
class _PhoneBar extends StatelessWidget {
  const _PhoneBar({required this.state, required this.pages, required this.onAutodetect});

  final SigningEditorState state;
  final SigningPageController pages;
  final VoidCallback onAutodetect;

  void _openPanel(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      backgroundColor: _bgCard,
      builder: (sheetContext) => FractionallySizedBox(
        heightFactor: 0.82,
        child: _SigningSidePanel(state: state, onAutodetect: onAutodetect, closeOnPlace: () => Navigator.of(sheetContext).pop()),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: Listenable.merge(<Listenable>[state, pages]),
      builder: (context, _) {
        final count = pages.pageCount == 0 ? state.document.pageCount : pages.pageCount;
        return Container(
          decoration: BoxDecoration(color: _bgSecondary, border: Border(top: BorderSide(color: _border))),
          padding: EdgeInsets.fromLTRB(8, 4, 8, 4 + MediaQuery.viewPaddingOf(context).bottom * 0),
          child: Row(
            children: <Widget>[
              Expanded(
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    key: const ValueKey<String>('signing-open-panel'),
                    onPressed: () => _openPanel(context),
                    icon: Icon(state.showSettings && state.selected != null ? Icons.tune_rounded : Icons.add_box_outlined, size: 20),
                    label: Text(state.showSettings && state.selected != null ? appStrings.signingFieldSettings : appStrings.signingFields, overflow: TextOverflow.ellipsis),
                  ),
                ),
              ),
              IconButton(
                tooltip: appStrings.signingPages,
                onPressed: pages.currentPage > 0 ? () => unawaited(pages.scrollToPage(pages.currentPage - 1)) : null,
                icon: const Icon(Icons.keyboard_arrow_up_rounded),
              ),
              Text('${pages.currentPage + 1} / $count', style: TextStyle(color: _textSecondary, fontSize: 13)),
              IconButton(
                tooltip: appStrings.signingPages,
                onPressed: pages.currentPage < count - 1 ? () => unawaited(pages.scrollToPage(pages.currentPage + 1)) : null,
                icon: const Icon(Icons.keyboard_arrow_down_rounded),
              ),
            ],
          ),
        );
      },
    );
  }
}
