part of 'main.dart';

// Review › Duplicates: copies of the same file and photos that look alike,
// in groups. No AI needed: the tab is there whether AI is on or off. In
// each group the suggested copy to keep is marked Keep and the others are
// picked for the trash; the trash keeps them restorable, and Undo puts them
// back at once.

const String _duplicatesTab = 'duplicates';

mixin _ReviewDuplicates on State<AiReviewPage> {
  NeoCloudController get _controller;

  ReviewDuplicates? _duplicates;
  String? _duplicatesError;
  bool _duplicatesBusy = false;
  Timer? _duplicatesPoll;
  int _duplicatesGeneration = 0;

  /// Group key → the files of it the person keeps. Starts with the
  /// server's pick.
  final Map<String, Set<String>> _kept = <String, Set<String>>{};

  int get _duplicateGroupCount => _duplicates?.groups.length ?? 0;

  void _disposeDuplicates() => _duplicatesPoll?.cancel();

  /// Reads the groups; while photos are still being compared, asks again
  /// every few seconds.
  Future<void> _loadDuplicates({bool quiet = false}) async {
    final generation = ++_duplicatesGeneration;
    _duplicatesPoll?.cancel();
    if (!quiet && mounted) setState(() => _duplicatesError = null);
    try {
      final report = await _controller.ai.duplicates();
      if (!mounted || generation != _duplicatesGeneration) return;
      setState(() {
        _duplicates = report;
        _duplicatesError = null;
        final keys = report.groups.map((group) => group.key).toSet();
        _kept.removeWhere((key, _) => !keys.contains(key));
      });
      if (report.pending > 0) {
        _duplicatesPoll = Timer(const Duration(seconds: 4), () => unawaited(_loadDuplicates(quiet: true)));
      }
    } catch (error) {
      if (!mounted || generation != _duplicatesGeneration) return;
      setState(() => _duplicatesError = _aiFriendlyError(_controller, error));
    }
  }

  Set<String> _keptIn(ReviewDuplicateGroup group) =>
      _kept[group.key] ??= <String>{for (final item in group.items) if (item.keep) item.node.id};

  List<FileNode> _picked(ReviewDuplicateGroup group) {
    final kept = _keptIn(group);
    return <FileNode>[for (final item in group.items) if (!kept.contains(item.node.id)) item.node];
  }

  /// Keep ↔ Remove; one file of a group always stays.
  void _toggleKeep(ReviewDuplicateGroup group, String nodeId) {
    final kept = _keptIn(group);
    setState(() {
      if (kept.contains(nodeId)) {
        if (kept.length > 1) kept.remove(nodeId);
      } else {
        kept.add(nodeId);
      }
    });
  }

  /// Moves the picked files of [groups] to the trash; Undo restores them.
  Future<void> _trashDuplicates(List<ReviewDuplicateGroup> groups) async {
    final nodes = <FileNode>[for (final group in groups) ..._picked(group)];
    if (nodes.isEmpty || _duplicatesBusy) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    setState(() => _duplicatesBusy = true);
    final failures = await runBatch(nodes, (node) => _controller.client.trashNode(node.id));
    final done = nodes.where((node) => !failures.any((failure) => failure.item.id == node.id)).toList();
    if (!mounted) return;
    final doneIds = done.map((node) => node.id).toSet();
    setState(() {
      _duplicatesBusy = false;
      final report = _duplicates;
      if (report != null) {
        // A group is done once at most one of its files is left.
        final left = report.groups
            .where((group) => group.items.where((item) => !doneIds.contains(item.node.id)).length > 1)
            .toList();
        _duplicates = ReviewDuplicates(groups: left, pending: report.pending);
      }
    });
    if (done.isNotEmpty) {
      _controller.undo.show(UndoEntry(
        message: appStrings.undoTrashed(done.length, done.first.name),
        onUndo: () async {
          await runBatch(done, (node) => _controller.client.restoreFromTrash(node.id));
          _controller.notifyFilesChanged();
          if (mounted) await _loadDuplicates(quiet: true);
        },
      ));
      _controller.notifyFilesChanged();
    }
    if (failures.isNotEmpty) messenger?.showSnackBar(SnackBar(content: Text(appStrings.duplicatesTrashFailed(failures.length))));
    await _loadDuplicates(quiet: true);
  }

  Future<void> _cleanUpAllDuplicates() async {
    final groups = _duplicates?.groups ?? const <ReviewDuplicateGroup>[];
    final count = groups.fold<int>(0, (sum, group) => sum + _picked(group).length);
    if (count == 0) return;
    final confirmed = await showAdaptiveSheet<bool>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(appStrings.duplicatesCleanUpTitle(count)),
        content: SizedBox(width: 420, child: Text(appStrings.duplicatesCleanUpBody, style: TextStyle(color: _textSecondary, height: 1.45))),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.moveToTrash)),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    await _trashDuplicates(groups);
  }

  Future<void> _dismissDuplicateGroup(ReviewDuplicateGroup group) async {
    final report = _duplicates;
    if (report != null) {
      setState(() => _duplicates = ReviewDuplicates(
        groups: report.groups.where((other) => other.key != group.key).toList(),
        pending: report.pending,
      ));
    }
    try {
      await _controller.ai.dismissDuplicates(group.items.map((item) => item.node.id).toList());
    } catch (error) {
      if (mounted) setState(() => _duplicatesError = _aiFriendlyError(_controller, error));
      await _loadDuplicates(quiet: true);
    }
  }

  void _openDuplicate(ReviewDuplicateGroup group, int index) {
    unawaited(showFileViewer(context, _controller, group.items.map((item) => item.node).toList(), index));
  }

  /// The tab's content: the "checking" note, Clean up all, the groups.
  List<Widget> _duplicateChildren() {
    final report = _duplicates;
    final error = _duplicatesError;
    final total = (report?.groups ?? const <ReviewDuplicateGroup>[]).fold<int>(0, (sum, group) => sum + _picked(group).length);
    return <Widget>[
      if (report != null && (report.pending > 0 || total > 0))
        Row(
          children: <Widget>[
            Expanded(
              child: report.pending > 0
                  ? Row(
                      children: <Widget>[
                        SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2, color: _textMuted)),
                        const SizedBox(width: 10),
                        Flexible(child: Text(appStrings.duplicatesChecking(report.pending), style: TextStyle(color: _textSecondary, fontSize: 13))),
                      ],
                    )
                  : const SizedBox.shrink(),
            ),
            if (total > 0)
              FilledButton.icon(
                onPressed: _duplicatesBusy ? null : () => unawaited(_cleanUpAllDuplicates()),
                icon: _duplicatesBusy
                    ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent))
                    : const Icon(Icons.cleaning_services_outlined, size: 18),
                label: Text(appStrings.duplicatesCleanUpAll(total)),
              ),
          ],
        ),
      if (error != null)
        _LoadError(
          message: error,
          onRetry: () {
            setState(() => _duplicatesError = null);
            unawaited(_loadDuplicates());
          },
        ),
      if (report == null && error == null) const _LoadingPlaceholder(),
      if (report != null && report.groups.isEmpty)
        _EmptyCard(icon: Icons.filter_none_rounded, title: appStrings.duplicatesEmptyTitle, subtitle: appStrings.duplicatesEmptyBody),
      if (report != null)
        for (final group in report.groups)
          _DuplicateGroupCard(
            key: ValueKey<String>(group.key),
            controller: _controller,
            group: group,
            kept: _keptIn(group),
            busy: _duplicatesBusy,
            onToggle: (nodeId) => _toggleKeep(group, nodeId),
            onOpen: (index) => _openDuplicate(group, index),
            onTrash: () => unawaited(_trashDuplicates(<ReviewDuplicateGroup>[group])),
            onDismiss: () => unawaited(_dismissDuplicateGroup(group)),
          ),
    ];
  }
}

/// One group: a heading, the files as square thumbnails marked Keep or
/// Remove, and the group's actions.
class _DuplicateGroupCard extends StatelessWidget {
  const _DuplicateGroupCard({
    super.key,
    required this.controller,
    required this.group,
    required this.kept,
    required this.busy,
    required this.onToggle,
    required this.onOpen,
    required this.onTrash,
    required this.onDismiss,
  });

  final NeoCloudController controller;
  final ReviewDuplicateGroup group;
  final Set<String> kept;
  final bool busy;
  final ValueChanged<String> onToggle;
  final ValueChanged<int> onOpen;
  final VoidCallback onTrash;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final picked = group.items.where((item) => !kept.contains(item.node.id)).toList();
    final freed = picked.fold<int>(0, (sum, item) => sum + item.size);
    return _AppCard(
      padding: const EdgeInsets.fromLTRB(18, 16, 16, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(
            group.exact ? appStrings.duplicatesExactTitle : appStrings.duplicatesSimilarTitle(group.similarity),
            style: TextStyle(color: _textPrimary, fontSize: 14.5, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 3),
          Text(
            appStrings.duplicatesGroupSummary(group.items.length, formatBytes(freed)),
            style: TextStyle(color: _textSecondary, fontSize: 13),
          ),
          const SizedBox(height: 14),
          Wrap(
            spacing: 12,
            runSpacing: 14,
            children: <Widget>[
              for (var index = 0; index < group.items.length; index++)
                _DuplicateTile(
                  controller: controller,
                  item: group.items[index],
                  kept: kept.contains(group.items[index].node.id),
                  onToggle: busy ? null : () => onToggle(group.items[index].node.id),
                  onOpen: () => onOpen(index),
                ),
            ],
          ),
          const SizedBox(height: 12),
          Wrap(
            alignment: WrapAlignment.end,
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              TextButton(onPressed: busy ? null : onDismiss, child: Text(appStrings.duplicatesNotDuplicates)),
              OutlinedButton.icon(
                onPressed: busy || picked.isEmpty ? null : onTrash,
                icon: const Icon(Icons.delete_outline_rounded, size: 18),
                label: Text(appStrings.duplicatesTrashGroup(picked.length)),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _DuplicateTile extends StatelessWidget {
  const _DuplicateTile({required this.controller, required this.item, required this.kept, required this.onToggle, required this.onOpen});

  static const double _size = 128;

  final NeoCloudController controller;
  final ReviewDuplicateItem item;
  final bool kept;
  final VoidCallback? onToggle;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final node = item.node;
    final locale = currentAppLanguage.code;
    final date = item.takenAt ?? node.modifiedAt;
    final folder = node.path ?? '';
    final radius = BorderRadius.circular(10);
    return SizedBox(
      width: _size,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Material(
            color: _bgTertiary,
            borderRadius: radius,
            clipBehavior: Clip.antiAlias,
            child: InkWell(
              onTap: onOpen,
              child: Container(
                width: _size,
                height: _size,
                decoration: BoxDecoration(
                  borderRadius: radius,
                  border: Border.all(color: kept ? _accent : _border, width: kept ? 2 : 1),
                ),
                child: Opacity(opacity: kept ? 1 : 0.55, child: _DuplicateThumb(controller: controller, node: node)),
              ),
            ),
          ),
          const SizedBox(height: 6),
          Tooltip(
            message: appStrings.duplicatesToggleHint,
            child: InkWell(
              borderRadius: BorderRadius.circular(AppRadius.pill),
              onTap: onToggle,
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 2),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    Icon(
                      kept ? Icons.check_circle_rounded : Icons.delete_outline_rounded,
                      size: 16,
                      color: kept ? _accent : _danger,
                    ),
                    const SizedBox(width: 5),
                    Text(
                      kept ? appStrings.duplicatesKeep : appStrings.duplicatesRemove,
                      style: TextStyle(color: kept ? _textPrimary : _danger, fontSize: 12.5, fontWeight: FontWeight.w600),
                    ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 2),
          Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 12.5)),
          Text(
            '${formatBytes(item.size)} · ${formatDate(date, locale)}',
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(color: _textMuted, fontSize: 11.5),
          ),
          if (folder.isNotEmpty)
            Tooltip(
              message: folder,
              child: Text(folder, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 11.5)),
            ),
        ],
      ),
    );
  }
}

/// The photo grid's square thumbnail (`size=square&v=etag`), shared with
/// Photos through its cache; other files show their type's icon.
class _DuplicateThumb extends StatefulWidget {
  const _DuplicateThumb({required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  State<_DuplicateThumb> createState() => _DuplicateThumbState();
}

class _DuplicateThumbState extends State<_DuplicateThumb> {
  Uint8List? _bytes;

  PhotoThumbnailCache get _cache => widget.controller.photoLibrary.thumbnails;

  bool get _isImage => (widget.node.mime ?? '').startsWith('image/');

  ScrollPosition? _position;
  bool _requested = false;
  int _generation = 0;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final position = Scrollable.maybeOf(context)?.position;
    if (_position != position) {
      _position?.removeListener(_checkVisibility);
      _position = position;
      _position?.addListener(_checkVisibility);
    }
    WidgetsBinding.instance.addPostFrameCallback((_) => _checkVisibility());
  }

  void _checkVisibility() {
    if (!mounted || !_isImage || _bytes != null) return;
    final render = context.findRenderObject();
    final viewport = render == null
        ? null
        : RenderAbstractViewport.maybeOf(render);
    if (render == null ||
        viewport == null ||
        !render.attached ||
        _position == null) {
      return;
    }
    final start = viewport.getOffsetToReveal(render, 0).offset;
    final end = viewport.getOffsetToReveal(render, 1).offset;
    if (start >= _position!.pixels + _position!.viewportDimension || end <= _position!.pixels) {
      if (_requested) {
        _cache.release(widget.node.id, widget.node.etag);
        _requested = false;
        _generation += 1;
      }
      return;
    }
    if (_requested) return;
    _bytes = _cache.peek(widget.node.id, widget.node.etag);
    if (_bytes != null) {
      setState(() {});
      return;
    }
    _requested = true;
    final generation = ++_generation;
    final id = widget.node.id;
    final etag = widget.node.etag;
    unawaited(
      _cache.load(id, etag).then((bytes) {
        if (mounted &&
            generation == _generation &&
            widget.node.id == id &&
            widget.node.etag == etag &&
            bytes != null) {
          setState(() => _bytes = bytes);
        }
      }),
    );
  }

  @override
  void didUpdateWidget(_DuplicateThumb oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.node.id != widget.node.id ||
        oldWidget.node.etag != widget.node.etag ||
        oldWidget.controller != widget.controller) {
      if (_requested && _bytes == null) {
        oldWidget.controller.photoLibrary.thumbnails.release(
          oldWidget.node.id,
          oldWidget.node.etag,
        );
      }
      _bytes = null;
      _requested = false;
      _generation += 1;
      WidgetsBinding.instance.addPostFrameCallback((_) => _checkVisibility());
    }
  }

  @override
  void dispose() {
    _position?.removeListener(_checkVisibility);
    if (_requested && _bytes == null) {
      _cache.release(widget.node.id, widget.node.etag);
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final bytes = _bytes;
    if (bytes != null) return Image.memory(bytes, fit: BoxFit.cover, gaplessPlayback: true);
    return Center(child: Icon(fileKindIcon(fileKindOf(widget.node)), size: 34, color: _textMuted));
  }
}
