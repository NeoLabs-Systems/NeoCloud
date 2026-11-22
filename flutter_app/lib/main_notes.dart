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

  /// An encrypted notebook (a vault under /Notes) shown instead of the others.
  EncryptedNotebook? _vaultNotebook;

  /// The notes on screen: the server's, plus those of unlocked encrypted notebooks.
  List<NoteCard> _cards = const <NoteCard>[];
  List<NoteTag> _tags = const <NoteTag>[];
  final Map<String, VaultNoteStore> _vaultStores = <String, VaultNoteStore>{};
  bool _archived = false;
  NoteSession? _open;
  Timer? _searchTimer;
  int _loadId = 0;

  NeoCloudController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    controller.vaults.addListener(_onVaults);
    unawaited(_load());
  }

  /// A vault was unlocked or locked: its notes appear or go.
  void _onVaults() {
    final open = _open;
    if (open != null && open.note.encrypted && !controller.vaults.isUnlocked(open.note.vaultId!)) {
      setState(() => _open = null);
      open.dispose();
    }
    unawaited(_load());
  }

  @override
  void dispose() {
    controller.vaults.removeListener(_onVaults);
    _searchTimer?.cancel();
    final open = _open;
    if (open != null) unawaited(open.flush().whenComplete(open.dispose));
    super.dispose();
  }

  VaultNoteStore _vaultStore(EncryptedNotebook notebook) =>
      _vaultStores.putIfAbsent(notebook.vaultId, () => VaultNoteStore(controller.client, controller.vaults, notebook));

  NoteStore _storeFor(NoteCard card) {
    final vaultId = card.vaultId;
    if (vaultId == null) return ServerNoteStore(controller.client);
    return _vaultStores[vaultId]!;
  }

  /// Search, tag and archive for notes the server can't read.
  bool _matches(NoteCard card) {
    if (card.archived != _archived) return false;
    final tag = _tag?.toLowerCase();
    if (tag != null && !card.tags.any((item) => item.toLowerCase() == tag)) return false;
    final query = _query.trim().toLowerCase();
    if (query.isEmpty) return true;
    return <String>[card.title, card.snippet, ...card.tags].any((text) => text.toLowerCase().contains(query));
  }

  Future<void> _load() async {
    final id = ++_loadId;
    try {
      final vaultOnly = _vaultNotebook != null;
      final listing = await controller.client.notes(query: _query, tag: _tag, notebook: vaultOnly ? null : _notebook, archived: _archived);
      // Encrypted notebooks are read here, the unlocked ones only.
      final shown = vaultOnly ? <EncryptedNotebook>[_vaultNotebook!] : (_notebook == null ? listing.encryptedNotebooks : const <EncryptedNotebook>[]);
      final vaultCards = <NoteCard>[];
      final vaultTags = <String, int>{};
      for (final notebook in listing.encryptedNotebooks) {
        if (!controller.vaults.isUnlocked(notebook.vaultId)) continue;
        final cards = await _vaultStore(notebook).list();
        for (final card in cards.where((card) => !card.archived)) {
          for (final tag in card.tags) {
            vaultTags[tag] = (vaultTags[tag] ?? 0) + 1;
          }
        }
        if (shown.any((item) => item.vaultId == notebook.vaultId)) vaultCards.addAll(cards.where(_matches));
      }
      final cards = <NoteCard>[if (!vaultOnly) ...listing.notes, ...vaultCards]
        ..sort((a, b) => a.pinned != b.pinned ? (a.pinned ? -1 : 1) : (b.modifiedAt ?? DateTime(0)).compareTo(a.modifiedAt ?? DateTime(0)));
      final tagCounts = <String, int>{for (final tag in listing.tags) tag.name: tag.count};
      vaultTags.forEach((name, count) => tagCounts[name] = (tagCounts[name] ?? 0) + count);
      final tags = tagCounts.entries.map((entry) => NoteTag(entry.key, entry.value)).toList()..sort((a, b) => b.count.compareTo(a.count));
      if (!mounted || id != _loadId) return;
      setState(() {
        _listing = listing;
        _cards = cards;
        _tags = tags;
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

  void _filter({String? tag, String? notebook, EncryptedNotebook? vault, bool clearTag = false, bool clearNotebook = false, bool? archived}) {
    setState(() {
      if (clearTag || tag != null) _tag = tag;
      if (clearNotebook || notebook != null || vault != null) {
        _notebook = notebook;
        _vaultNotebook = vault;
      }
      if (archived != null) _archived = archived;
    });
    unawaited(_load());
  }

  void _toast(String message) => ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));

  Future<void> _openNote(String id) async {
    final card = _cards.where((item) => item.id == id).firstOrNull;
    if (card == null) return;
    await _closeNote();
    try {
      final store = _storeFor(card);
      final note = await store.load(id);
      if (!mounted) return;
      setState(() => _open = NoteSession(store, note));
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
  Future<void> _create({String? title, String? body, bool open = true}) async {
    try {
      final vault = _vaultNotebook;
      final NoteStore store = vault == null ? ServerNoteStore(controller.client) : _vaultStore(vault);
      final note = vault == null
          ? await controller.client.createNote(title: title, body: body, notebook: _notebook == null || _notebook!.isEmpty ? null : _notebook)
          : await _vaultStore(vault).create(title: title, body: body);
      if (!mounted) return;
      if (open) {
        await _closeNote();
        if (mounted) setState(() => _open = NoteSession(store, note));
      }
      unawaited(_load());
    } catch (error) {
      _toast(controller.friendlyError(error));
    }
  }

  Future<void> _quick(NoteCard card, {bool? pinned, bool? archived, NoteColor? color}) async {
    try {
      await _storeFor(card).update(card.id, NoteChange(pinned: pinned, archived: archived, color: color), revision: card.revision);
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
        await _storeFor(card).trash(card.id);
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
              SizedBox(
                width: 320,
                child: _NotesSideList(cards: _cards, openId: open.note.id, query: _query, onSearch: _search, onOpen: (id) => unawaited(_openNote(id)), onNew: () => unawaited(_create())),
              ),
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
    final pinned = _cards.where((note) => note.pinned).toList();
    final others = _cards.where((note) => !note.pinned).toList();
    final filtered = _query.isNotEmpty || _tag != null || _notebook != null || _vaultNotebook != null;
    final vault = _vaultNotebook;
    final locked = vault != null && !controller.vaults.isUnlocked(vault.vaultId);
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
                        tags: _tags,
                        notebook: _notebook,
                        vault: _vaultNotebook,
                        unlocked: controller.vaults.isUnlocked,
                        tag: _tag,
                        onNotebook: (value) => value == null ? _filter(clearNotebook: true) : _filter(notebook: value),
                        onVault: (value) => value == null ? _filter(clearNotebook: true) : _filter(vault: value),
                        onTag: (value) => value == null ? _filter(clearTag: true) : _filter(tag: value),
                        onNewNotebook: () => unawaited(_newNotebook()),
                        onNewEncrypted: () => unawaited(_newEncryptedNotebook()),
                      ),
                      if (locked) ...<Widget>[
                        const SizedBox(height: 24),
                        _VaultUnlockCard(controller: controller, vaultId: vault.vaultId, folderName: vault.name),
                      ] else if (!_archived) ...<Widget>[
                        const SizedBox(height: 20),
                        Center(
                          child: _CaptureBar(
                            onSave: (title, body) => _create(title: title, body: body, open: false),
                            onOpen: (title, body) => unawaited(_create(title: title, body: body)),
                          ),
                        ),
                      ],
                      const SizedBox(height: 24),
                      if (locked)
                        const SizedBox.shrink()
                      else if (_loading)
                        const Padding(
                          padding: EdgeInsets.all(48),
                          child: Center(child: CircularProgressIndicator()),
                        )
                      else if (_error != null)
                        _EmptyState(
                          icon: Icons.error_outline_rounded,
                          title: appStrings.notesLoadFailed,
                          subtitle: _error!,
                          action: OutlinedButton(onPressed: _load, child: Text(appStrings.retry)),
                        )
                      else if (_cards.isEmpty)
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
        if (phone && !_archived && !locked)
          Positioned(
            right: 18,
            bottom: 18,
            child: FloatingActionButton(tooltip: appStrings.notesNew, onPressed: () => unawaited(_create()), child: const Icon(Icons.add_rounded)),
          ),
      ],
    );
  }

  /// An encrypted notebook: a vault in the Notes folder, set up with the
  /// same passphrase and recovery words as any vault.
  Future<void> _newEncryptedNotebook() async {
    try {
      final rootId = _listing.rootId ?? await controller.client.notesRootId();
      if (!mounted) return;
      final folder = await showNewVaultDialog(context, controller, parentId: rootId);
      if (folder == null || !mounted) return;
      await _load();
      final created = _listing.encryptedNotebooks.where((item) => item.folderId == folder.id).firstOrNull;
      if (created != null) _filter(vault: created);
    } catch (error) {
      _toast(controller.friendlyError(error));
    }
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
          Row(
            children: <Widget>[
              Expanded(child: title),
              archive,
            ],
          ),
          const SizedBox(height: 12),
          search,
        ],
      );
    }
    return Row(
      children: <Widget>[
        Expanded(child: title),
        search,
        const SizedBox(width: 12),
        archive,
      ],
    );
  }
}

/// Notebooks as chips, then the tags in use.
class _NotesFilters extends StatelessWidget {
  const _NotesFilters({
    required this.listing,
    required this.tags,
    required this.notebook,
    required this.vault,
    required this.unlocked,
    required this.tag,
    required this.onNotebook,
    required this.onVault,
    required this.onTag,
    required this.onNewNotebook,
    required this.onNewEncrypted,
  });

  final NotesListing listing;
  final List<NoteTag> tags;
  final String? notebook;
  final EncryptedNotebook? vault;
  final bool Function(String vaultId) unlocked;
  final String? tag;
  final ValueChanged<String?> onNotebook;
  final ValueChanged<EncryptedNotebook?> onVault;
  final ValueChanged<String?> onTag;
  final VoidCallback onNewNotebook;
  final VoidCallback onNewEncrypted;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        _NoteChip(label: appStrings.notesAll, selected: notebook == null && vault == null, onTap: () => onNotebook(null)),
        for (final name in listing.notebooks) _NoteChip(label: name, icon: Icons.book_outlined, selected: notebook == name, onTap: () => onNotebook(notebook == name ? null : name)),
        for (final item in listing.encryptedNotebooks)
          _NoteChip(
            label: item.name,
            icon: unlocked(item.vaultId) ? Icons.lock_open_rounded : Icons.lock_outline_rounded,
            selected: vault?.vaultId == item.vaultId,
            onTap: () => onVault(vault?.vaultId == item.vaultId ? null : item),
          ),
        _NoteChip(label: appStrings.notesNewNotebookChip, icon: Icons.add_rounded, dashed: true, onTap: onNewNotebook),
        _NoteChip(label: appStrings.notesNewEncryptedChip, icon: Icons.enhanced_encryption_outlined, dashed: true, onTap: onNewEncrypted),
        if (tags.isNotEmpty) Container(width: 1, height: 20, color: _borderLight, margin: const EdgeInsets.symmetric(horizontal: 4)),
        for (final item in tags.take(12)) _NoteChip(label: '#${item.name} · ${item.count}', small: true, selected: tag == item.name, onTap: () => onTag(tag == item.name ? null : item.name)),
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
            border: small && !selected ? null : Border.all(color: selected ? _accent.withValues(alpha: 0.4) : _borderLight),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              if (icon != null) ...<Widget>[Icon(icon, size: 15, color: selected ? _accent : _textMuted), const SizedBox(width: 6)],
              Text(
                label,
                style: TextStyle(fontSize: small ? 12 : 13, fontWeight: selected ? FontWeight.w600 : FontWeight.w500, color: selected ? _textPrimary : (dashed ? _textMuted : _textSecondary)),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// "Take a note…" like Google Keep: one line until it is tapped, then a
/// title and as many lines as needed. Done (or Ctrl/⌘+Enter, or tapping
/// away with text in it) saves; the expand button opens the full editor.
class _CaptureBar extends StatefulWidget {
  const _CaptureBar({required this.onSave, required this.onOpen});

  final Future<void> Function(String? title, String body) onSave;
  final void Function(String? title, String? body) onOpen;

  @override
  State<_CaptureBar> createState() => _CaptureBarState();
}

class _CaptureBarState extends State<_CaptureBar> {
  final TextEditingController _title = TextEditingController();
  final _MarkdownEditingController _body = _MarkdownEditingController();
  final FocusNode _bodyFocus = FocusNode();
  final FocusNode _titleFocus = FocusNode();
  bool _expanded = false;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _bodyFocus.addListener(_onFocus);
    _titleFocus.addListener(_onFocus);
  }

  @override
  void dispose() {
    _title.dispose();
    _body.dispose();
    _bodyFocus.dispose();
    _titleFocus.dispose();
    super.dispose();
  }

  bool get _empty => _title.text.trim().isEmpty && _body.text.trim().isEmpty;

  void _onFocus() {
    if (_bodyFocus.hasFocus || _titleFocus.hasFocus) {
      if (!_expanded) setState(() => _expanded = true);
      return;
    }
    // Tapping away: a note with text is kept, an empty one just closes.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _bodyFocus.hasFocus || _titleFocus.hasFocus) return;
      if (_empty) {
        setState(() => _expanded = false);
      } else {
        unawaited(_save());
      }
    });
  }

  String? get _titleText => _title.text.trim().isEmpty ? null : _title.text.trim();

  Future<void> _save() async {
    if (_saving) return;
    if (_empty) {
      _close();
      return;
    }
    setState(() => _saving = true);
    try {
      await widget.onSave(_titleText, _body.text.trimRight());
      _title.clear();
      _body.clear();
      _close();
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  void _close() {
    _bodyFocus.unfocus();
    _titleFocus.unfocus();
    if (mounted) setState(() => _expanded = false);
  }

  void _checklist() {
    _body.value = toggleLinePrefix(_body.value.selection.isValid ? _body.value : _body.value.copyWith(selection: TextSelection.collapsed(offset: _body.text.length)), '- [ ] ');
    _bodyFocus.requestFocus();
  }

  void _openFull() {
    final title = _titleText;
    final body = _body.text.trim().isEmpty ? null : _body.text;
    _title.clear();
    _body.clear();
    _close();
    widget.onOpen(title, body);
  }

  @override
  Widget build(BuildContext context) {
    final borderless = InputDecoration(border: InputBorder.none, enabledBorder: InputBorder.none, focusedBorder: InputBorder.none, filled: false, isCollapsed: true);
    // Taps on the card's own buttons don't count as tapping away.
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 600),
      child: TextFieldTapRegion(
        child: Container(
          decoration: BoxDecoration(
            color: _bgCard,
            borderRadius: BorderRadius.circular(AppRadius.card),
            border: Border.all(color: _borderLight),
            boxShadow: <BoxShadow>[BoxShadow(color: _palette.shadow, blurRadius: 24, offset: const Offset(0, 6))],
          ),
          padding: EdgeInsets.fromLTRB(18, _expanded ? 14 : 4, 6, _expanded ? 6 : 4),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (_expanded) ...<Widget>[
                TextField(
                  controller: _title,
                  focusNode: _titleFocus,
                  textInputAction: TextInputAction.next,
                  onSubmitted: (_) => _bodyFocus.requestFocus(),
                  style: TextStyle(color: _textPrimary, fontSize: 16, fontWeight: FontWeight.w600),
                  decoration: borderless.copyWith(hintText: appStrings.notesTitleHint),
                ),
                const SizedBox(height: 10),
              ],
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(
                    child: Padding(
                      padding: EdgeInsets.symmetric(vertical: _expanded ? 0 : 13),
                      child: CallbackShortcuts(
                        bindings: <ShortcutActivator, VoidCallback>{
                          const SingleActivator(LogicalKeyboardKey.enter, control: true): () => unawaited(_save()),
                          const SingleActivator(LogicalKeyboardKey.enter, meta: true): () => unawaited(_save()),
                          const SingleActivator(LogicalKeyboardKey.escape): () => unawaited(_save()),
                        },
                        child: TextField(
                          controller: _body,
                          focusNode: _bodyFocus,
                          minLines: _expanded ? 3 : 1,
                          maxLines: _expanded ? 14 : 1,
                          keyboardType: TextInputType.multiline,
                          inputFormatters: <TextInputFormatter>[_NoteEnterFormatter((oldValue, newValue) => continueList(oldValue) ?? newValue)],
                          style: TextStyle(color: _textPrimary, fontSize: 15, height: 1.5),
                          decoration: borderless.copyWith(hintText: appStrings.notesTakeNote),
                        ),
                      ),
                    ),
                  ),
                  if (!_expanded) ...<Widget>[
                    IconButton(
                      tooltip: appStrings.notesNewChecklist,
                      onPressed: _checklist,
                      icon: Icon(Icons.checklist_rounded, color: _textSecondary),
                    ),
                    IconButton(
                      tooltip: appStrings.notesOpenEditor,
                      onPressed: _openFull,
                      icon: Icon(Icons.open_in_full_rounded, size: 20, color: _textSecondary),
                    ),
                  ],
                ],
              ),
              if (_expanded) ...<Widget>[
                const SizedBox(height: 6),
                Row(
                  children: <Widget>[
                    IconButton(
                      tooltip: appStrings.notesBlockChecklist,
                      onPressed: _checklist,
                      icon: Icon(Icons.checklist_rounded, size: 20, color: _textSecondary),
                    ),
                    IconButton(
                      tooltip: appStrings.notesOpenEditor,
                      onPressed: _openFull,
                      icon: Icon(Icons.open_in_full_rounded, size: 19, color: _textSecondary),
                    ),
                    const Spacer(),
                    TextButton(onPressed: _saving ? null : () => unawaited(_save()), child: Text(_empty ? appStrings.close : appStrings.notesDone)),
                  ],
                ),
              ],
            ],
          ),
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
    child: Text(
      text.toUpperCase(),
      style: TextStyle(fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: 0.9, color: _textMuted),
    ),
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
  const _NotesSideList({required this.cards, required this.openId, required this.query, required this.onSearch, required this.onOpen, required this.onNew});

  final List<NoteCard> cards;
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
            decoration: InputDecoration(
              isDense: true,
              hintText: appStrings.notesSearch,
              prefixIcon: Icon(Icons.search_rounded, size: 18, color: _textMuted),
            ),
          ),
          const SizedBox(height: 10),
          Expanded(
            child: ListView.builder(
              itemCount: cards.length,
              itemBuilder: (context, index) {
                final card = cards[index];
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
                              Container(
                                width: 8,
                                height: 8,
                                decoration: BoxDecoration(color: _noteTint(card.color).dot, shape: BoxShape.circle),
                              ),
                              const SizedBox(width: 8),
                            ],
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: <Widget>[
                                  Text(
                                    card.title,
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                    style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: _textPrimary),
                                  ),
                                  if (card.snippet.isNotEmpty)
                                    Text(
                                      card.snippet,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: TextStyle(fontSize: 12, color: selected ? _textSecondary : _textMuted),
                                    ),
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
