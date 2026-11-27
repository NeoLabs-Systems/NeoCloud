part of 'main.dart';

// Signing (/app/signing): the list of documents to sign and, below it,
// /app/signing/<id>: the editor while a document is a draft, the progress
// page afterwards.

/// Shows the list, or the open document.
class SigningPanel extends StatelessWidget {
  const SigningPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final id = controller.openSigningId;
    if (id == null) return SigningListScreen(controller: controller);
    return SigningDocumentPage(key: ValueKey<String>('signing-$id'), controller: controller, documentId: id);
  }
}

/// A user-facing sentence for a failed signing call: the server's own words
/// for refusals about the content, a general line for everything else.
String signingErrorText(NeoCloudController controller, Object error, String fallback) {
  if (error is SigningException) {
    if (error.isNetwork) return controller.friendlyError(error);
    if (error.code == 'EMAIL_NOT_VERIFIED') return appStrings.signingMailNotVerified;
    if (error.code == 'MAIL_QUOTA_EXCEEDED') return appStrings.signingMailQuota;
    final refusal = error.kind == SigningErrorKind.invalid || error.kind == SigningErrorKind.conflict;
    if (refusal && error.message.trim().isNotEmpty) return error.message;
    return fallback;
  }
  return fallback;
}

String _statusLabel(SigningStatus status) {
  switch (status) {
    case SigningStatus.draft:
      return appStrings.signingStatusDraft;
    case SigningStatus.sent:
      return appStrings.signingStatusSent;
    case SigningStatus.inProgress:
      return appStrings.signingStatusInProgress;
    case SigningStatus.finalizing:
      return appStrings.signingStatusFinalizing;
    case SigningStatus.completed:
      return appStrings.signingStatusCompleted;
    case SigningStatus.declined:
      return appStrings.signingStatusDeclined;
    case SigningStatus.expired:
      return appStrings.signingStatusExpired;
    case SigningStatus.canceled:
      return appStrings.signingStatusCanceled;
  }
}

Color _statusColor(SigningStatus status) {
  switch (status) {
    case SigningStatus.completed:
      return _success;
    case SigningStatus.declined:
      return _danger;
    case SigningStatus.sent:
    case SigningStatus.finalizing:
      return _warning;
    case SigningStatus.inProgress:
      return _info;
    case SigningStatus.draft:
    case SigningStatus.expired:
    case SigningStatus.canceled:
      return _textMuted;
  }
}

String _partyStatusLabel(PartyStatus status) {
  switch (status) {
    case PartyStatus.pending:
      return appStrings.signingPartyPending;
    case PartyStatus.waiting:
      return appStrings.signingPartyWaiting;
    case PartyStatus.opened:
      return appStrings.signingPartyOpened;
    case PartyStatus.completed:
      return appStrings.signingPartyCompleted;
    case PartyStatus.declined:
      return appStrings.signingPartyDeclined;
  }
}

Color _partyStatusColor(PartyStatus status) {
  switch (status) {
    case PartyStatus.completed:
      return _success;
    case PartyStatus.declined:
      return _danger;
    case PartyStatus.opened:
      return _info;
    case PartyStatus.waiting:
    case PartyStatus.pending:
      return _textMuted;
  }
}

bool _canDeleteSigning(SigningStatus status) =>
    status == SigningStatus.draft || status == SigningStatus.completed || status == SigningStatus.declined || status == SigningStatus.expired || status == SigningStatus.canceled;

bool _canCancelSigning(SigningStatus status) => status == SigningStatus.sent || status == SigningStatus.inProgress;

/// The first letters of a party's name, for its avatar.
String _initialsOf(String name) {
  final words = name.trim().split(RegExp(r'\s+')).where((word) => word.isNotEmpty).toList();
  if (words.isEmpty) return '?';
  if (words.length == 1) return words.first.characters.take(2).toString().toUpperCase();
  return (words.first.characters.first + words.last.characters.first).toUpperCase();
}

/// Overlapping party circles; a signed party gets a check, a declined one a cross.
class _PartyAvatars extends StatelessWidget {
  const _PartyAvatars({required this.parties});

  final List<SigningListParty> parties;
  static const double size = 26;

  @override
  Widget build(BuildContext context) {
    final shown = parties.take(5).toList();
    final more = parties.length - shown.length;
    return SizedBox(
      height: size,
      width: size + (shown.length - 1 + (more > 0 ? 1 : 0)) * (size * 0.68),
      child: Stack(
        children: <Widget>[
          for (final (index, party) in shown.indexed)
            Positioned(
              left: index * size * 0.68,
              child: Tooltip(
                message: '${party.name}: ${_partyStatusLabel(party.status)}',
                child: _PartyDot(name: party.name, color: resolvePartyColor(party.color, index), status: party.status, size: size),
              ),
            ),
          if (more > 0)
            Positioned(
              left: shown.length * size * 0.68,
              child: Container(
                width: size,
                height: size,
                alignment: Alignment.center,
                decoration: BoxDecoration(color: _bgTertiary, shape: BoxShape.circle, border: Border.all(color: _bgCard, width: 2)),
                child: Text('+$more', style: TextStyle(color: _textSecondary, fontSize: 10, fontWeight: FontWeight.w600)),
              ),
            ),
        ],
      ),
    );
  }
}

class _PartyDot extends StatelessWidget {
  const _PartyDot({required this.name, required this.color, required this.status, this.size = 26});

  final String name;
  final Color color;
  final PartyStatus status;
  final double size;

  @override
  Widget build(BuildContext context) {
    final on = onPartyColor(color);
    final IconData? badge = status == PartyStatus.completed ? Icons.check_rounded : (status == PartyStatus.declined ? Icons.close_rounded : null);
    return SizedBox(
      width: size,
      height: size,
      child: Stack(
        clipBehavior: Clip.none,
        children: <Widget>[
          Container(
            width: size,
            height: size,
            alignment: Alignment.center,
            decoration: BoxDecoration(color: color, shape: BoxShape.circle, border: Border.all(color: _bgCard, width: 2)),
            child: Text(_initialsOf(name), style: TextStyle(color: on, fontSize: size * 0.36, fontWeight: FontWeight.w700)),
          ),
          if (badge != null)
            Positioned(
              right: -3,
              bottom: -3,
              child: Container(
                width: size * 0.45,
                height: size * 0.45,
                decoration: BoxDecoration(color: _partyStatusColor(status), shape: BoxShape.circle, border: Border.all(color: _bgCard, width: 1.5)),
                child: Icon(badge, size: size * 0.3, color: Colors.white),
              ),
            ),
        ],
      ),
    );
  }
}

// The list

class SigningListScreen extends StatefulWidget {
  const SigningListScreen({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<SigningListScreen> createState() => _SigningListScreenState();
}

class _SigningListScreenState extends State<SigningListScreen> {
  final TextEditingController _search = TextEditingController();
  List<SigningListItem> _items = const <SigningListItem>[];
  String? _cursor;
  SigningStatus? _status;
  _SigningTab _tab = _SigningTab.mine;
  bool _loading = true;
  bool _loadingMore = false;
  String? _error;
  int _generation = 0;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.signingRevision.addListener(_changedElsewhere);
    unawaited(_load());
  }

  @override
  void dispose() {
    _controller.signingRevision.removeListener(_changedElsewhere);
    _search.dispose();
    super.dispose();
  }

  void _changedElsewhere() => unawaited(_load(quiet: true));

  Future<void> _load({bool quiet = false}) async {
    final generation = ++_generation;
    if (!quiet) setState(() => _loading = true);
    try {
      final page = await _controller.signing.list(status: _status, limit: 50);
      if (!mounted || generation != _generation) return;
      setState(() {
        _items = page.items;
        _cursor = page.nextCursor;
        _error = null;
      });
    } catch (error) {
      if (mounted && generation == _generation && !quiet) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted && generation == _generation) setState(() => _loading = false);
    }
  }

  Future<void> _loadMore() async {
    final cursor = _cursor;
    if (cursor == null || _loadingMore) return;
    setState(() => _loadingMore = true);
    try {
      final page = await _controller.signing.list(status: _status, limit: 50, cursor: cursor);
      if (mounted) {
        setState(() {
          _items = <SigningListItem>[..._items, ...page.items];
          _cursor = page.nextCursor;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _loadingMore = false);
    }
  }

  void _filter(SigningStatus? status) {
    if (_status == status) return;
    setState(() => _status = status);
    unawaited(_load());
  }

  List<SigningListItem> get _visible {
    final query = _search.text.trim().toLowerCase();
    if (query.isEmpty) return _items;
    return _items.where((item) => item.title.toLowerCase().contains(query) || item.parties.any((party) => party.name.toLowerCase().contains(query))).toList();
  }

  Future<void> _duplicate(SigningListItem item) async {
    try {
      final detail = await _controller.signing.duplicate(item.id);
      _controller.openSigning(detail.document.id);
    } catch (error) {
      _toast(signingErrorText(_controller, error, appStrings.signingPrepareFailed));
    }
  }

  Future<void> _cancel(SigningListItem item) => _confirmDelete(
    context,
    title: appStrings.signingCancelTitle,
    message: appStrings.signingCancelBody,
    confirmLabel: appStrings.signingCancelDoc,
    onConfirm: () async {
      try {
        await _controller.signing.cancel(item.id);
        await _load(quiet: true);
      } catch (error) {
        _toast(signingErrorText(_controller, error, appStrings.signingLoadFailed));
      }
    },
  );

  Future<void> _delete(SigningListItem item) => _confirmDelete(
    context,
    title: appStrings.signingDeleteTitle,
    message: appStrings.signingDeleteBody,
    confirmLabel: appStrings.signingDeleteDoc,
    onConfirm: () async {
      try {
        await _controller.signing.delete(item.id);
        await _load(quiet: true);
      } catch (error) {
        _toast(signingErrorText(_controller, error, appStrings.signingLoadFailed));
      }
    },
  );

  void _toast(String message) {
    if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));
  }

  List<_FileAction> _actions(SigningListItem item) => <_FileAction>[
    _FileAction(Icons.open_in_new_rounded, appStrings.signingOpen, () => _controller.openSigning(item.id)),
    _FileAction(Icons.copy_rounded, appStrings.signingDuplicate, () => unawaited(_duplicate(item))),
    if (_canCancelSigning(item.status)) _FileAction(Icons.block_rounded, appStrings.signingCancelDoc, () => unawaited(_cancel(item))),
    if (_canDeleteSigning(item.status)) _FileAction(Icons.delete_outline_rounded, appStrings.signingDeleteDoc, () => unawaited(_delete(item)), danger: true),
  ];

  static const List<SigningStatus> _filters = <SigningStatus>[
    SigningStatus.draft,
    SigningStatus.sent,
    SigningStatus.inProgress,
    SigningStatus.completed,
    SigningStatus.declined,
    SigningStatus.expired,
    SigningStatus.canceled,
  ];

  @override
  Widget build(BuildContext context) {
    final items = _visible;
    final chooseFile = Wrap(
      spacing: 8,
      runSpacing: 8,
      children: <Widget>[
        OutlinedButton.icon(
          key: const ValueKey<String>('signing-quick'),
          onPressed: () => unawaited(showSigningFilePicker(context, _controller, quick: true)),
          icon: const Icon(Icons.draw_outlined, size: 18),
          label: Text(appStrings.signingQuickTitle),
        ),
        FilledButton.icon(
          onPressed: () => unawaited(showSigningFilePicker(context, _controller)),
          icon: const Icon(Icons.add_rounded, size: 18),
          label: Text(appStrings.signingChooseFile),
        ),
      ],
    );
    return _PageScaffold(
      onRefresh: _load,
      children: <Widget>[
        _PageTitle(title: appStrings.signingNav, subtitle: appStrings.signingSubtitle, trailing: _items.isEmpty && _status == null ? null : chooseFile),
        if (_controller.emailConfigured) _SigningTabs(tab: _tab, todo: _controller.signingTodo, onSelect: (tab) => setState(() => _tab = tab)),
        if (_controller.emailConfigured && _tab != _SigningTab.mine)
          _SigningInboxView(key: ValueKey<String>('signing-inbox-${_tab.name}'), controller: _controller, done: _tab == _SigningTab.done)
        else ...<Widget>[
          Wrap(
            spacing: 8,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              ChoiceChip(
                label: Text(appStrings.signingFilterAll),
                selected: _status == null,
                showCheckmark: false,
                onSelected: (_) => _filter(null),
              ),
              for (final status in _filters)
                ChoiceChip(
                  key: ValueKey<String>('signing-filter-${status.name}'),
                  label: Text(_statusLabel(status)),
                  selected: _status == status,
                  showCheckmark: false,
                  onSelected: (_) => _filter(status),
                ),
            ],
          ),
          TextField(
            controller: _search,
            onChanged: (_) => setState(() {}),
            decoration: InputDecoration(prefixIcon: const Icon(Icons.search_rounded, size: 20), hintText: appStrings.signingSearchHint, isDense: true),
          ),
          if (_error != null) _LoadError(message: _error!, onRetry: () => unawaited(_load())),
          if (_loading && _items.isEmpty && _error == null) const _LoadingPlaceholder(),
          if (!_loading && _items.isEmpty && _error == null && _status == null)
            _EmptyCard(icon: AppSection.signing.icon, title: appStrings.signingEmptyTitle, subtitle: appStrings.signingEmptyBody, action: chooseFile),
          if (!_loading && _error == null && items.isEmpty && (_items.isNotEmpty || _status != null))
            _EmptyCard(icon: Icons.search_off_rounded, title: appStrings.signingNoResults, subtitle: ''),
          if (items.isNotEmpty)
            _EntranceMotion(
              child: _AppCard(
                padding: EdgeInsets.zero,
                child: Column(
                  children: <Widget>[
                    for (var i = 0; i < items.length; i++) ...<Widget>[
                      if (i > 0) Divider(height: 1, color: _border),
                      _SigningRow(
                        key: ValueKey<String>('signing-row-${items[i].id}'),
                        item: items[i],
                        actions: _actions(items[i]),
                        onOpen: () => _controller.openSigning(items[i].id),
                      ),
                    ],
                  ],
                ),
              ),
            ),
          if (_cursor != null)
            Center(
              child: OutlinedButton(onPressed: _loadingMore ? null : () => unawaited(_loadMore()), child: Text(_loadingMore ? appStrings.signingSaving : appStrings.signingLoadMore)),
            ),
        ],
      ],
    );
  }
}

class _SigningRow extends StatelessWidget {
  const _SigningRow({super.key, required this.item, required this.actions, required this.onOpen});

  final SigningListItem item;
  final List<_FileAction> actions;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final total = item.parties.length;
    final done = item.completedParties;
    final started = item.status != SigningStatus.draft;
    final updated = item.updatedAt == null ? null : formatDate(item.updatedAt!.toLocal(), currentAppLanguage.code);
    return InkWell(
      onTap: onOpen,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Row(
          children: <Widget>[
            Icon(Icons.description_outlined, color: _textMuted, size: 26),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(item.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 14)),
                  const SizedBox(height: 4),
                  Wrap(
                    spacing: 12,
                    runSpacing: 2,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: <Widget>[
                      _StatusPill(label: _statusLabel(item.status), color: _statusColor(item.status)),
                      if (started && total > 0) Text(appStrings.signingProgressCount('$done', '$total'), style: TextStyle(color: _textMuted, fontSize: 12.5)),
                      if (updated != null) Text(appStrings.signingUpdatedAt(updated), style: TextStyle(color: _textMuted, fontSize: 12.5)),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            if (MediaQuery.sizeOf(context).width >= AppBreakpoints.mobile) _PartyAvatars(parties: item.parties),
            const SizedBox(width: 4),
            _FileActionsMenu(actions: actions, title: item.title),
          ],
        ),
      ),
    );
  }
}

// Opening a document

/// Loads the document and shows the editor for a draft, the progress page
/// for everything else.
class SigningDocumentPage extends StatefulWidget {
  const SigningDocumentPage({super.key, required this.controller, required this.documentId});

  final NeoCloudController controller;
  final String documentId;

  @override
  State<SigningDocumentPage> createState() => _SigningDocumentPageState();
}

class _SigningDocumentPageState extends State<SigningDocumentPage> {
  SigningDetail? _detail;
  String? _error;
  bool _missing = false;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final detail = await widget.controller.signing.get(widget.documentId);
      if (mounted) setState(() => _detail = detail);
    } on SigningException catch (error) {
      if (!mounted) return;
      if (error.kind == SigningErrorKind.notFound) {
        setState(() => _missing = true);
      } else {
        setState(() => _error = widget.controller.friendlyError(error));
      }
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    final detail = _detail;
    if (_missing) {
      return Center(
        child: _EmptyState(
          title: appStrings.signingLoadFailed,
          subtitle: '',
          icon: Icons.search_off_rounded,
          action: OutlinedButton(onPressed: () => widget.controller.openSigning(null), child: Text(appStrings.signingFinish)),
        ),
      );
    }
    if (_error != null) {
      return _PageScaffold(children: <Widget>[_LoadError(message: _error!, onRetry: () => unawaited(_load()))]);
    }
    if (detail == null) return const Center(child: CircularProgressIndicator());
    if (detail.document.status == SigningStatus.draft) {
      return SigningEditorPage(controller: widget.controller, detail: detail);
    }
    return SigningStatusPage(controller: widget.controller, detail: detail);
  }
}

// Starting from a file

/// Makes [node] a signing draft and opens the editor.
Future<void> startSigningFromFile(BuildContext context, NeoCloudController controller, FileNode node) async {
  final detail = await _makeSigningDraft(context, controller, node);
  if (detail != null) controller.openSigning(detail.document.id);
}

/// Makes [node] a signing draft, saying on the way what goes wrong; null
/// when it could not be made.
Future<SigningDetail?> _makeSigningDraft(BuildContext context, NeoCloudController controller, FileNode node) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  final kind = signableKindOf(node.name, node.mime);
  if (!kind.isSignable) {
    messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingUnsupportedFile)));
    return null;
  }
  try {
    if (kind == SignableKind.office && !(await controller.signing.capabilities()).office) {
      messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingOfficeUnavailable)));
      return null;
    }
    messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingPreparing), duration: const Duration(seconds: 30)));
    final detail = await controller.signing.createDocument(nodeId: node.id, locale: currentAppLanguage.code);
    messenger?.hideCurrentSnackBar();
    if (detail.warnings.contains('FORM_FIELDS_FLATTENED')) {
      messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingFlattened), duration: const Duration(seconds: 10)));
    }
    return detail;
  } catch (error) {
    messenger?.hideCurrentSnackBar();
    messenger?.showSnackBar(SnackBar(content: Text(signingErrorText(controller, error, appStrings.signingPrepareFailed))));
    return null;
  }
}

/// A small browser for the library: folders and the files that can be
/// signed. With [quick] the picked file is signed right away by the account
/// itself, and a file can also be uploaded into the folder shown.
Future<void> showSigningFilePicker(BuildContext context, NeoCloudController controller, {bool quick = false}) {
  return showAdaptiveSheet<void>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(quick ? appStrings.signingQuickPickerTitle : appStrings.signingPickerTitle),
      content: SizedBox(
        width: 460,
        height: 380,
        child: _SigningFileBrowser(
          controller: controller,
          onPick: (node) {
            Navigator.of(dialogContext).pop();
            unawaited(quick ? startQuickSign(context, controller, node) : startSigningFromFile(context, controller, node));
          },
          onUpload: !quick
              ? null
              : (folderId) {
                  Navigator.of(dialogContext).pop();
                  unawaited(_uploadAndQuickSign(context, controller, folderId));
                },
        ),
      ),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel))],
    ),
  );
}

class _SigningFileBrowser extends StatefulWidget {
  const _SigningFileBrowser({required this.controller, required this.onPick, this.onUpload});

  final NeoCloudController controller;
  final ValueChanged<FileNode> onPick;

  /// Uploads a file from the device into the folder shown (its id).
  final ValueChanged<String>? onUpload;

  @override
  State<_SigningFileBrowser> createState() => _SigningFileBrowserState();
}

class _SigningFileBrowserState extends State<_SigningFileBrowser> {
  String _folderId = 'root';
  FolderListing? _listing;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final listing = await widget.controller.client.listFolder(_folderId);
      if (mounted) {
        setState(() {
          _listing = listing;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    final listing = _listing;
    if (_error != null) return _LoadError(message: _error!, onRetry: () => unawaited(_load()));
    if (listing == null) return const Center(child: CircularProgressIndicator());
    final entries = listing.items.where((node) => !node.inVault && (node.isFolder || signableKindOf(node.name, node.mime).isSignable)).toList();
    final parent = listing.folder.parentId;
    final onUpload = widget.onUpload;
    return ListView(
      children: <Widget>[
        if (onUpload != null)
          ListTile(
            key: const ValueKey<String>('signing-pick-upload'),
            leading: const Icon(Icons.upload_file_outlined),
            title: Text(appStrings.signingQuickUpload),
            subtitle: Text(appStrings.signingQuickUploadHint),
            onTap: () => onUpload(_folderId),
          ),
        if (parent != null && _folderId != 'root')
          ListTile(
            leading: const Icon(Icons.arrow_upward_rounded),
            title: Text(listing.path.length > 1 ? listing.path[listing.path.length - 2].name : '/'),
            onTap: () {
              _folderId = parent;
              unawaited(_load());
            },
          ),
        if (entries.isEmpty) Padding(padding: const EdgeInsets.all(24), child: Text(appStrings.signingPickerEmpty, style: TextStyle(color: _textMuted))),
        for (final node in entries)
          ListTile(
            key: ValueKey<String>('signing-pick-${node.id}'),
            leading: Icon(node.isFolder ? Icons.folder_outlined : Icons.description_outlined),
            title: Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis),
            onTap: () {
              if (node.isFolder) {
                _folderId = node.id;
                unawaited(_load());
              } else {
                widget.onPick(node);
              }
            },
          ),
      ],
    );
  }
}
