part of 'main.dart';

// Notes (docs/notes.md): Markdown files under /Notes shown as cards like
// Google Keep (capture bar, pinned first, colors, tags, notebooks, archive),
// opened in a quiet editor (main_notes_editor.dart). Wide windows keep the
// list beside the open note; phones open it full screen.

class NotesPage extends StatefulWidget {
  const NotesPage({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<NotesPage> createState() => _NotesPageState();
}

class _NotesPageState extends State<NotesPage> {
  NotesListing _listing = NotesListing.empty;
  bool _loading = true;
  String? _error;
  String _query = '';
  String? _tag;

  /// null: every notebook; '': notes in no notebook.
  String? _notebook;
  bool _archived = false;
  NoteSession? _open;
  Timer? _searchTimer;
  final TextEditingController _capture = TextEditingController();
  final FocusNode _captureFocus = FocusNode();
  int _loadId = 0;

  NeoCloudController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _searchTimer?.cancel();
    _capture.dispose();
    _captureFocus.dispose();
    final open = _open;
    if (open != null) unawaited(open.flush().whenComplete(open.dispose));
    super.dispose();
  }

  Future<void> _load() async {
    final id = ++_loadId;
    try {
      final listing = await controller.client.notes(query: _query, tag: _tag, notebook: _notebook, archived: _archived);
      if (!mounted || id != _loadId) return;
      setState(() {
        _listing = listing;
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted || id != _loadId) return;
      setState(() {
        _loading = false;
        _error = controller.friendlyError(error);
      });
    }
  }

  void _search(String value) {
    _query = value;
    _searchTimer?.cancel();
    _searchTimer = Timer(const Duration(milliseconds: 250), () => unawaited(_load()));
  }

  void _filter({String? tag, String? notebook, bool clearTag = false, bool clearNotebook = false, bool? archived}) {
    setState(() {
      if (clearTag || tag != null) _tag = tag;
      if (clearNotebook || notebook != null) _notebook = notebook;
      if (archived != null) _archived = archived;
    });
    unawaited(_load());
  }

  void _toast(String message) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));

  Future<void> _openNote(String id) async {
    await _closeNote();
    try {
      final note = await controller.client.note(id);
      if (!mounted) return;
      setState(() => _open = NoteSession(controller.client, note));
    } catch (error) {
      _toast(controller.friendlyError(error));
    }
  }

  Future<void> _closeNote() async {
    final open = _open;
    if (open == null) return;
    setState(() => _open = null);
    await open.flush();
    open.dispose();
    if (mounted) unawaited(_load());
  }

  /// A new note: from the capture bar's text, as a checklist, or empty.
  Future<void> _create({String? body, bool open = true}) async {
    try {
      final note = await controller.client.createNote(body: body, notebook: _notebook == null || _notebook!.isEmpty ? null : _notebook);
      if (!mounted) return;
      _capture.clear();
      if (open) {
        await _closeNote();
        if (mounted) setState(() => _open = NoteSession(controller.client, note));
      }
      unawaited(_load());
    } catch (error) {
      _toast(controller.friendlyError(error));
    }
  }

  Future<void> _quick(NoteCard card, {bool? pinned, bool? archived, NoteColor? color}) async {
    try {
      await controller.client.updateNote(card.id, pinned: pinned, archived: archived, color: color, baseRevision: card.revision);
      if (archived != null) _toast(archived ? appStrings.notesArchived : appStrings.notesUnarchived);
    } catch (error) {
      _toast(controller.friendlyError(error));
    }
    unawaited(_load());
  }

  Future<void> _delete(NoteCard card) => _confirmDelete(
    context,
    title: appStrings.notesDeleteTitle,
    message: appStrings.notesDeleteMessage(card.title),
    confirmLabel: appStrings.notesMoveToTrash,
    onConfirm: () async {
      try {
        if (_open?.note.id == card.id) {
          final open = _open!;
          setState(() => _open = null);
          open.dispose();
        }
        await controller.client.deleteNote(card.id);
      } catch (error) {
        _toast(controller.friendlyError(error));
      }
      unawaited(_load());
    },
  );

  Future<void> _newNotebook() async {
    final name = await _askText(context, title: appStrings.notesNewNotebook, label: appStrings.notesNotebookName);
    if (name == null || name.trim().isEmpty) return;
    try {
      await controller.client.createNotebook(name.trim());
      _filter(notebook: name.trim());
    } catch (error) {
      _toast(controller.friendlyError(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final open = _open;
        final split = constraints.maxWidth >= 1100;
        if (open != null && !split) {
          return _NoteEditorView(
            key: ValueKey<String>(open.note.id),
            controller: controller,
            session: open,
            notebooks: _listing.notebooks,
            onClose: () => unawaited(_closeNote()),
            onDelete: () => unawaited(_delete(open.note)),
          );
        }
        if (open != null) {
          return Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              SizedBox(width: 320, child: _NotesSideList(listing: _listing, openId: open.note.id, query: _query, onSearch: _search, onOpen: (id) => unawaited(_openNote(id)), onNew: () => unawaited(_create()))),
              VerticalDivider(width: 1, color: _border),
              Expanded(
                child: _NoteEditorView(
                  key: ValueKey<String>(open.note.id),
                  controller: controller,
                  session: open,
                  notebooks: _listing.notebooks,
                  onClose: () => unawaited(_closeNote()),
                  onDelete: () => unawaited(_delete(open.note)),
                ),
              ),
            ],
          );
        }
        return _grid(context, constraints.maxWidth);
      },
    );
  }

  Widget _grid(BuildContext context, double width) {
    final phone = width < AppBreakpoints.mobile;
    final pinned = _listing.notes.where((note) => note.pinned).toList();
    final others = _listing.notes.where((note) => !note.pinned).toList();
    final filtered = _query.isNotEmpty || _tag != null || _notebook != null;
    return Stack(
      children: <Widget>[
        RefreshIndicator(
          onRefresh: _load,
          child: ListView(
            padding: _pagePadding(context),
            children: <Widget>[
              Center(
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: kContentMaxWidth),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      _NotesHeader(
                        archived: _archived,
                        onSearch: _search,
                        onArchive: () => _filter(archived: !_archived),
                      ),
                      const SizedBox(height: 16),
                      _NotesFilters(
                        listing: _listing,
                        notebook: _notebook,
                        tag: _tag,
                        onNotebook: (value) => value == null ? _filter(clearNotebook: true) : _filter(notebook: value),
                        onTag: (value) => value == null ? _filter(clearTag: true) : _filter(tag: value),
                        onNewNotebook: () => unawaited(_newNotebook()),
                      ),
                      if (!_archived) ...<Widget>[
                        const SizedBox(height: 20),
                        Center(
                          child: _CaptureBar(
                            controller: _capture,
                            focusNode: _captureFocus,
                            onSubmit: (text) => unawaited(_create(body: text, open: false)),
                            onChecklist: () => unawaited(_create(body: '- [ ] ')),
                            onExpand: () => unawaited(_create(body: _capture.text.trim().isEmpty ? null : _capture.text)),
                          ),
                        ),
                      ],
                      const SizedBox(height: 24),
                      if (_loading)
                        const Padding(padding: EdgeInsets.all(48), child: Center(child: CircularProgressIndicator()))
                      else if (_error != null)
                        _EmptyState(icon: Icons.error_outline_rounded, title: appStrings.notesLoadFailed, subtitle: _error!, action: OutlinedButton(onPressed: _load, child: Text(appStrings.retry)))
                      else if (_listing.notes.isEmpty)
                        _EmptyState(
                          icon: _archived ? Icons.archive_outlined : Icons.sticky_note_2_outlined,
                          title: _archived ? appStrings.notesArchiveEmptyTitle : (filtered ? appStrings.notesNoMatchTitle : appStrings.notesEmptyTitle),
                          subtitle: _archived ? appStrings.notesArchiveEmptyBody : (filtered ? appStrings.notesNoMatchBody : appStrings.notesEmptyBody),
                        )
                      else ...<Widget>[
                        if (pinned.isNotEmpty) ...<Widget>[
                          _NotesSectionLabel(appStrings.notesPinned),
                          _NoteGrid(cards: pinned, onOpen: (id) => unawaited(_openNote(id)), menu: _cardMenu),
                          const SizedBox(height: 24),
                          if (others.isNotEmpty) _NotesSectionLabel(appStrings.notesOthers),
                        ],
                        _NoteGrid(cards: others, onOpen: (id) => unawaited(_openNote(id)), menu: _cardMenu),
                      ],
                      SizedBox(height: phone ? 96 : 24),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
        if (phone && !_archived)
          Positioned(
            right: 18,
            bottom: 18,
            child: FloatingActionButton(
              tooltip: appStrings.notesNew,
              onPressed: () => unawaited(_create()),
              child: const Icon(Icons.add_rounded),
            ),
          ),
      ],
    );
  }

  List<_FileAction> _cardMenu(NoteCard card) => <_FileAction>[
    _FileAction(card.pinned ? Icons.push_pin : Icons.push_pin_outlined, card.pinned ? appStrings.notesUnpin : appStrings.notesPin, () => unawaited(_quick(card, pinned: !card.pinned))),
    _FileAction(
      card.archived ? Icons.unarchive_outlined : Icons.archive_outlined,
      card.archived ? appStrings.notesUnarchive : appStrings.notesArchive,
      () => unawaited(_quick(card, archived: !card.archived, pinned: card.archived ? null : false)),
    ),
    _FileAction(Icons.palette_outlined, appStrings.notesColor, () async {
      final color = await _pickNoteColor(context, card.color);
      if (color != null) unawaited(_quick(card, color: color));
    }),
    _FileAction(Icons.delete_outline_rounded, appStrings.notesMoveToTrash, () => unawaited(_delete(card)), danger: true),
  ];
}

/// Title, search and the archive switch.
class _NotesHeader extends StatelessWidget {
  const _NotesHeader({required this.archived, required this.onSearch, required this.onArchive});

  final bool archived;
  final ValueChanged<String> onSearch;
  final VoidCallback onArchive;

  @override
  Widget build(BuildContext context) {
    final compact = MediaQuery.sizeOf(context).width < 760;
    final search = SizedBox(
      width: compact ? double.infinity : 320,
      child: TextField(
        onChanged: onSearch,
        textInputAction: TextInputAction.search,
        decoration: InputDecoration(
          isDense: true,
          hintText: appStrings.notesSearch,
          prefixIcon: Icon(Icons.search_rounded, size: 18, color: _textMuted),
        ),
      ),
    );
    final archive = archived
        ? FilledButton.tonalIcon(onPressed: onArchive, icon: const Icon(Icons.arrow_back_rounded, size: 18), label: Text(appStrings.notesBackToNotes))
        : OutlinedButton.icon(onPressed: onArchive, icon: const Icon(Icons.archive_outlined, size: 18), label: Text(appStrings.notesArchive));
    final title = Text(archived ? appStrings.notesArchiveTitle : appStrings.notesTitle, style: _displayTitleStyle(compact ? 24 : 28));
    if (compact) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(children: <Widget>[Expanded(child: title), archive]),
          const SizedBox(height: 12),
          search,
        ],
      );
    }
    return Row(children: <Widget>[Expanded(child: title), search, const SizedBox(width: 12), archive]);
  }
}

/// Notebooks as chips, then the tags in use.
class _NotesFilters extends StatelessWidget {
  const _NotesFilters({required this.listing, required this.notebook, required this.tag, required this.onNotebook, required this.onTag, required this.onNewNotebook});

  final NotesListing listing;
  final String? notebook;
  final String? tag;
  final ValueChanged<String?> onNotebook;
  final ValueChanged<String?> onTag;
  final VoidCallback onNewNotebook;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        _NoteChip(label: appStrings.notesAll, selected: notebook == null, onTap: () => onNotebook(null)),
        for (final name in listing.notebooks) _NoteChip(label: name, icon: Icons.book_outlined, selected: notebook == name, onTap: () => onNotebook(notebook == name ? null : name)),
        _NoteChip(label: appStrings.notesNewNotebookChip, icon: Icons.add_rounded, dashed: true, onTap: onNewNotebook),
        if (listing.tags.isNotEmpty) Container(width: 1, height: 20, color: _borderLight, margin: const EdgeInsets.symmetric(horizontal: 4)),
        for (final item in listing.tags.take(12))
          _NoteChip(label: '#${item.name} · ${item.count}', small: true, selected: tag == item.name, onTap: () => onTag(tag == item.name ? null : item.name)),
      ],
    );
  }
}

class _NoteChip extends StatelessWidget {
  const _NoteChip({required this.label, required this.onTap, this.icon, this.selected = false, this.dashed = false, this.small = false});

  final String label;
  final VoidCallback onTap;
  final IconData? icon;
  final bool selected;
  final bool dashed;
  final bool small;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(AppRadius.pill);
    return Material(
      color: selected ? _accentMuted : (small ? _bgTertiary : Colors.transparent),
      borderRadius: radius,
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Container(
          height: small ? 28 : 32,
          padding: EdgeInsets.symmetric(horizontal: small ? 10 : 14),
          decoration: BoxDecoration(
            borderRadius: radius,
            border: small && !selected
                ? null
                : Border.all(color: selected ? _accent.withValues(alpha: 0.4) : _borderLight),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              if (icon != null) ...<Widget>[Icon(icon, size: 15, color: selected ? _accent : _textMuted), const SizedBox(width: 6)],
              Text(
                label,
                style: TextStyle(
                  fontSize: small ? 12 : 13,
                  fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                  color: selected ? _textPrimary : (dashed ? _textMuted : _textSecondary),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// "Take a note…": Enter saves it as a note; the buttons open a checklist
/// or the full editor.
class _CaptureBar extends StatelessWidget {
  const _CaptureBar({required this.controller, required this.focusNode, required this.onSubmit, required this.onChecklist, required this.onExpand});

  final TextEditingController controller;
  final FocusNode focusNode;
  final ValueChanged<String> onSubmit;
  final VoidCallback onChecklist;
  final VoidCallback onExpand;

  @override
  Widget build(BuildContext context) {
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 600),
      child: Container(
        decoration: BoxDecoration(
          color: _bgCard,
          borderRadius: BorderRadius.circular(AppRadius.card),
          border: Border.all(color: _borderLight),
          boxShadow: <BoxShadow>[BoxShadow(color: _palette.shadow, blurRadius: 24, offset: const Offset(0, 6))],
        ),
        padding: const EdgeInsets.fromLTRB(18, 4, 6, 4),
        child: Row(
          children: <Widget>[
            Expanded(
              child: TextField(
                controller: controller,
                focusNode: focusNode,
                textInputAction: TextInputAction.done,
                onSubmitted: (text) {
                  if (text.trim().isNotEmpty) onSubmit(text.trim());
                },
                style: TextStyle(color: _textPrimary, fontSize: 15),
                decoration: InputDecoration(
                  hintText: appStrings.notesTakeNote,
                  border: InputBorder.none,
                  enabledBorder: InputBorder.none,
                  focusedBorder: InputBorder.none,
                  filled: false,
                ),
              ),
            ),
            IconButton(tooltip: appStrings.notesNewChecklist, onPressed: onChecklist, icon: Icon(Icons.checklist_rounded, color: _textSecondary)),
            IconButton(tooltip: appStrings.notesOpenEditor, onPressed: onExpand, icon: Icon(Icons.open_in_full_rounded, size: 20, color: _textSecondary)),
          ],
        ),
      ),
    );
  }
}

class _NotesSectionLabel extends StatelessWidget {
  const _NotesSectionLabel(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 10),
    child: Text(text.toUpperCase(), style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: 0.9, color: _textMuted)),
  );
}

/// Cards in columns, each card as tall as its text (Keep's masonry).
class _NoteGrid extends StatelessWidget {
  const _NoteGrid({required this.cards, required this.onOpen, required this.menu});

  final List<NoteCard> cards;
  final ValueChanged<String> onOpen;
  final List<_FileAction> Function(NoteCard card) menu;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 14.0;
        final columns = math.max(1, math.min(4, ((constraints.maxWidth + gap) / 250).floor()));
        final lanes = List<List<NoteCard>>.generate(columns, (_) => <NoteCard>[]);
        for (var index = 0; index < cards.length; index += 1) {
          lanes[index % columns].add(cards[index]);
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            for (var lane = 0; lane < columns; lane += 1) ...<Widget>[
              if (lane > 0) const SizedBox(width: gap),
              Expanded(
                child: Column(
                  children: <Widget>[
                    for (final card in lanes[lane])
                      Padding(
                        padding: const EdgeInsets.only(bottom: gap),
                        child: _NoteCardView(card: card, onTap: () => onOpen(card.id), actions: menu(card)),
                      ),
                  ],
                ),
              ),
            ],
          ],
        );
      },
    );
  }
}

/// The list beside an open note on wide windows.
class _NotesSideList extends StatelessWidget {
  const _NotesSideList({required this.listing, required this.openId, required this.query, required this.onSearch, required this.onOpen, required this.onNew});

  final NotesListing listing;
  final String openId;
  final String query;
  final ValueChanged<String> onSearch;
  final ValueChanged<String> onOpen;
  final VoidCallback onNew;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(14, 22, 14, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(child: Text(appStrings.notesTitle, style: _displayTitleStyle(20))),
              FilledButton.icon(onPressed: onNew, icon: const Icon(Icons.add_rounded, size: 18), label: Text(appStrings.notesNewShort)),
            ],
          ),
          const SizedBox(height: 12),
          TextFormField(
            initialValue: query,
            onChanged: onSearch,
            decoration: InputDecoration(isDense: true, hintText: appStrings.notesSearch, prefixIcon: Icon(Icons.search_rounded, size: 18, color: _textMuted)),
          ),
          const SizedBox(height: 10),
          Expanded(
            child: ListView.builder(
              itemCount: listing.notes.length,
              itemBuilder: (context, index) {
                final card = listing.notes[index];
                final selected = card.id == openId;
                return Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Material(
                    color: selected ? _accentMuted : Colors.transparent,
                    borderRadius: BorderRadius.circular(12),
                    child: InkWell(
                      borderRadius: BorderRadius.circular(12),
                      onTap: () => onOpen(card.id),
                      child: Padding(
                        padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
                        child: Row(
                          children: <Widget>[
                            if (card.color != NoteColor.plain) ...<Widget>[
                              Container(width: 8, height: 8, decoration: BoxDecoration(color: _noteTint(card.color).dot, shape: BoxShape.circle)),
                              const SizedBox(width: 8),
                            ],
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: <Widget>[
                                  Text(card.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: _textPrimary)),
                                  if (card.snippet.isNotEmpty)
                                    Text(card.snippet, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12, color: selected ? _textSecondary : _textMuted)),
                                ],
                              ),
                            ),
                            if (card.pinned) Icon(Icons.push_pin, size: 14, color: _accent),
                          ],
                        ),
                      ),
                    ),
                  ),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

/// Asks for one line of text.
Future<String?> _askText(BuildContext context, {required String title, required String label, String initial = ''}) {
  final input = TextEditingController(text: initial);
  return showAdaptiveSheet<String>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(title),
      content: SizedBox(
        width: 380,
        child: TextField(
          controller: input,
          autofocus: true,
          decoration: InputDecoration(labelText: label),
          onSubmitted: (value) => Navigator.of(dialogContext).pop(value),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel)),
        FilledButton(onPressed: () => Navigator.of(dialogContext).pop(input.text), child: Text(appStrings.save)),
      ],
    ),
  ).whenComplete(input.dispose);
}
