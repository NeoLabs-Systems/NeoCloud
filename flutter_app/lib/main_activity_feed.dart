part of 'main.dart';

// The activity list shared by the Activity page and the details of one file
// or folder: newest first, a "Show more" button for older pages, reloaded
// when files or comments change anywhere.

IconData _activityIcon(String kind) {
  switch (kind) {
    case 'upload':
      return Icons.upload_file_outlined;
    case 'folder':
      return Icons.create_new_folder_outlined;
    case 'copy':
      return Icons.copy_rounded;
    case 'version':
      return Icons.history_rounded;
    case 'rename':
      return Icons.drive_file_rename_outline_rounded;
    case 'move':
      return Icons.drive_file_move_outline;
    case 'trash':
      return Icons.delete_outline_rounded;
    case 'restore':
      return Icons.restore_from_trash_outlined;
    case 'share':
      return Icons.person_add_alt_outlined;
    default:
      return Icons.chat_bubble_outline_rounded;
  }
}

/// "Anna uploaded 3 files", "You renamed A to B": one line per entry. The
/// `actor` argument picks the sentence for the signed-in account.
String _activitySentence(ActivityItem item) {
  final strings = appStrings;
  final who = item.actor.name;
  final actor = item.actor.self ? 'self' : 'other';
  final name = item.nodeName;
  final many = item.count > 1;
  switch (item.kind) {
    case 'upload':
      return many ? strings.activityUploadedMany(actor, who, item.count) : strings.activityUploaded(actor, who, name);
    case 'folder':
      return many ? strings.activityFolderMany(actor, who, item.count) : strings.activityFolder(actor, who, name);
    case 'copy':
      return many ? strings.activityCopiedMany(actor, who, item.count) : strings.activityCopied(actor, who, name);
    case 'version':
      return strings.activityVersion(actor, who, name);
    case 'rename':
      return strings.activityRenamed(actor, who, item.from ?? name, name);
    case 'move':
      return many ? strings.activityMovedMany(actor, who, item.count) : strings.activityMoved(actor, who, name);
    case 'trash':
      return many ? strings.activityDeletedMany(actor, who, item.count) : strings.activityDeleted(actor, who, name);
    case 'restore':
      return many ? strings.activityRestoredMany(actor, who, item.count) : strings.activityRestored(actor, who, name);
    case 'share':
      final member = item.member;
      if (member != null && !member.self) return strings.activitySharedWith(actor, who, name, member.name);
      return strings.activitySharedWithYou(actor, who, name);
    default:
      return strings.activityCommented(actor, who, name);
  }
}

class ActivityFeed extends StatefulWidget {
  const ActivityFeed({super.key, required this.controller, this.nodeId, this.scope = ActivityScope.all});

  final NeoCloudController controller;

  /// Limits the feed to this file or folder and what is directly inside it.
  final String? nodeId;
  final ActivityScope scope;

  @override
  State<ActivityFeed> createState() => _ActivityFeedState();
}

class _ActivityFeedState extends State<ActivityFeed> {
  final List<ActivityItem> _items = <ActivityItem>[];
  int? _next;
  bool _loaded = false;
  bool _loadingMore = false;
  String? _error;
  Timer? _debounce;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.filesRevision.addListener(_changed);
    _controller.commentsRevision.addListener(_changed);
    _controller.fileChanges.addListener(_changedBatch);
    unawaited(_load());
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.filesRevision.removeListener(_changed);
    _controller.commentsRevision.removeListener(_changed);
    _controller.fileChanges.removeListener(_changedBatch);
    super.dispose();
  }

  @override
  void didUpdateWidget(ActivityFeed old) {
    super.didUpdateWidget(old);
    if (old.scope != widget.scope || old.nodeId != widget.nodeId) {
      setState(() => _loaded = false);
      unawaited(_load());
    }
  }

  void _changedBatch(FileChangeBatch _) => _changed();

  /// A burst of changes reloads once, shortly after the last.
  void _changed() {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 700), () => unawaited(_load()));
  }

  Future<void> _load() async {
    try {
      final page = await _controller.client.activity(scope: widget.scope, nodeId: widget.nodeId);
      if (!mounted) return;
      setState(() {
        _items
          ..clear()
          ..addAll(page.items);
        _next = page.nextCursor;
        _loaded = true;
        _error = null;
      });
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    }
  }

  Future<void> _more() async {
    final cursor = _next;
    if (cursor == null || _loadingMore) return;
    setState(() => _loadingMore = true);
    try {
      final page = await _controller.client.activity(scope: widget.scope, nodeId: widget.nodeId, cursor: cursor);
      if (!mounted) return;
      setState(() {
        _items.addAll(page.items);
        _next = page.nextCursor;
      });
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _loadingMore = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_error != null && !_loaded) {
      return _LoadError(message: _error!, onRetry: () {
        setState(() => _error = null);
        unawaited(_load());
      });
    }
    if (!_loaded) return const _LoadingPlaceholder();
    if (_items.isEmpty) {
      return _EmptyState(icon: Icons.timeline_rounded, title: appStrings.activityEmpty, subtitle: appStrings.activityEmptySubtitle);
    }
    final locale = currentAppLanguage.code;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (_error != null) _InlineError(message: _error!),
        for (final item in _items) _ActivityRow(item: item, locale: locale),
        if (_next != null)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(onPressed: _loadingMore ? null : () => unawaited(_more()), child: Text(appStrings.activityLoadMore)),
          ),
      ],
    );
  }
}

class _ActivityRow extends StatelessWidget {
  const _ActivityRow({required this.item, required this.locale});

  final ActivityItem item;
  final String locale;

  @override
  Widget build(BuildContext context) {
    final names = item.count > 1 ? item.names.join(', ') + (item.count > item.names.length ? ' …' : '') : '';
    final where = <String>[
      formatDate(item.at, locale),
      if (item.folder != null) appStrings.activityIn(item.folder!),
    ].join(' · ');
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(color: _bgTertiary, shape: BoxShape.circle, border: Border.all(color: _border)),
            child: Icon(_activityIcon(item.kind), size: 17, color: _textSecondary),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(_activitySentence(item), style: TextStyle(color: _textPrimary, fontSize: 13.5, height: 1.35)),
                if (names.isNotEmpty) Text(names, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
                Text(where, style: TextStyle(color: _textMuted, fontSize: 12)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
