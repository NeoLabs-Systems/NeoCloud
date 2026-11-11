part of 'main.dart';

/// A short list of files on one card (Recent, Favorites); long folders use
/// the sliver list of the Files page instead.
class _FileListCard extends StatelessWidget {
  const _FileListCard({required this.items, required this.callbacks});

  final List<FileNode> items;
  final _ItemCallbacks callbacks;

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 760;
    return _EntranceMotion(
      child: _AppCard(
        padding: EdgeInsets.zero,
        child: Column(
          children: <Widget>[
            for (var i = 0; i < items.length; i++)
              SizedBox(
                height: _rowExtent,
                child: _FileRow(key: ValueKey<String>(items[i].id), node: items[i], callbacks: callbacks, wide: wide, first: i == 0, last: i == items.length - 1),
              ),
          ],
        ),
      ),
    );
  }
}

/// Opens a file in the viewer, or a folder on the Files page.
void _openNode(BuildContext context, NeoCloudController controller, List<FileNode> items, FileNode node) {
  if (node.isFolder) {
    controller.openFolder(node.id);
    return;
  }
  final files = items.where((item) => !item.isFolder).toList();
  unawaited(showFileViewer(context, controller, files, files.indexOf(node)));
}

/// Files the account changed most recently, across all folders.
class RecentPanel extends StatefulWidget {
  const RecentPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<RecentPanel> createState() => _RecentPanelState();
}

class _RecentPanelState extends State<RecentPanel> {
  List<FileNode>? _items;
  String? _error;

  @override
  void initState() {
    super.initState();
    widget.controller.filesRevision.addListener(_load);
    widget.controller.fileChanges.addListener(_onFilesChangedElsewhere);
    _load();
  }

  @override
  void dispose() {
    widget.controller.filesRevision.removeListener(_load);
    widget.controller.fileChanges.removeListener(_onFilesChangedElsewhere);
    super.dispose();
  }

  /// Spans every folder, so any change elsewhere may show here.
  void _onFilesChangedElsewhere(FileChangeBatch _) => unawaited(_load());

  Future<void> _load() async {
    try {
      final items = await widget.controller.client.recent(by: 'modified');
      if (mounted) setState(() => _items = items.where((node) => !node.inVault).toList());
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    final controller = widget.controller;
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(title: appStrings.recentTitle, subtitle: appStrings.recentSubtitle),
        if (_error != null) _LoadError(message: _error!, onRetry: _load),
        if (items == null && _error == null) const _LoadingPlaceholder(),
        if (items != null && items.isEmpty)
          _EmptyCard(icon: Icons.schedule_rounded, title: appStrings.recentEmpty, subtitle: appStrings.recentEmptySubtitle),
        if (items != null && items.isNotEmpty)
          _FileListCard(
            items: items,
            callbacks: _ItemCallbacks(
              isSelected: (_) => false,
              showPath: true,
              onTap: (node) => _openNode(context, controller, items, node),
              onOpen: (node) => _openNode(context, controller, items, node),
              actionsFor: (node) => <_FileAction>[
                _FileAction(Icons.folder_open_outlined, appStrings.showInFolder, () => controller.openFolder(node.parentId), quick: true),
                _FileAction(Icons.download_rounded, appStrings.download, () => unawaited(controller.downloadFile(node)), quick: true),
              ],
            ),
          ),
      ],
    );
  }
}

/// Starred files and folders.
class FavoritesPanel extends StatefulWidget {
  const FavoritesPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<FavoritesPanel> createState() => _FavoritesPanelState();
}

class _FavoritesPanelState extends State<FavoritesPanel> {
  List<FileNode>? _items;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.filesRevision.addListener(_load);
    _controller.fileChanges.addListener(_onFilesChangedElsewhere);
    _load();
  }

  @override
  void dispose() {
    _controller.filesRevision.removeListener(_load);
    _controller.fileChanges.removeListener(_onFilesChangedElsewhere);
    super.dispose();
  }

  /// Spans every folder, so any change elsewhere may show here.
  void _onFilesChangedElsewhere(FileChangeBatch _) => unawaited(_load());

  Future<void> _load() async {
    try {
      final items = await _controller.vaults.reveal(await _controller.client.favorites());
      if (mounted) setState(() => _items = items);
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    }
  }

  Future<void> _setFavorite(FileNode node, bool favorite) async {
    setState(() => _items = favorite ? <FileNode>[...?_items, node] : _items?.where((item) => item.id != node.id).toList());
    try {
      await _controller.client.setFavorite(node.id, favorite);
      if (!favorite) {
        _controller.undo.show(UndoEntry(message: appStrings.filesUnstarred(1), onUndo: () => _setFavorite(node, true)));
      }
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = '${appStrings.filesFavoriteFailed('“${node.name}”')} ${_controller.friendlyError(error)}');
      await _load();
    }
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(title: appStrings.filesNavFavorites, subtitle: appStrings.filesFavoritesSubtitle),
        if (_error != null) _LoadError(message: _error!, onRetry: () {
          setState(() => _error = null);
          unawaited(_load());
        }),
        if (items == null && _error == null) const _LoadingPlaceholder(),
        if (items != null && items.isEmpty)
          _EmptyCard(icon: Icons.star_outline_rounded, title: appStrings.filesFavoritesEmpty, subtitle: appStrings.filesFavoritesEmptySubtitle),
        if (items != null && items.isNotEmpty)
          _FileListCard(
            items: items,
            callbacks: _ItemCallbacks(
              isSelected: (_) => false,
              showPath: true,
              onTap: (node) => _openNode(context, _controller, items, node),
              onOpen: (node) => _openNode(context, _controller, items, node),
              actionsFor: (node) => <_FileAction>[
                _FileAction(Icons.folder_open_outlined, appStrings.showInFolder, () => _controller.openFolder(node.parentId), quick: true),
                if (!node.isFolder) _FileAction(Icons.download_rounded, appStrings.download, () => unawaited(_controller.downloadFile(node)), quick: true),
                _FileAction(Icons.star_rounded, appStrings.filesUnstar, () => unawaited(_setFavorite(node, false)), quick: true),
              ],
            ),
          ),
      ],
    );
  }
}

/// Deleted items, with restore, permanent deletion and when each goes for good.
class TrashPanel extends StatefulWidget {
  const TrashPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<TrashPanel> createState() => _TrashPanelState();
}

class _TrashPanelState extends State<TrashPanel> {
  List<FileNode>? _items;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.filesRevision.addListener(_load);
    _controller.fileChanges.addListener(_onFilesChangedElsewhere);
    _load();
  }

  @override
  void dispose() {
    _controller.filesRevision.removeListener(_load);
    _controller.fileChanges.removeListener(_onFilesChangedElsewhere);
    super.dispose();
  }

  /// Spans every folder, so any change elsewhere may show here.
  void _onFilesChangedElsewhere(FileChangeBatch _) => unawaited(_load());

  Future<void> _load() async {
    try {
      final items = await _controller.client.trash();
      if (mounted) setState(() => _items = items);
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    try {
      await action();
      _controller.notifyFilesChanged();
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    }
  }

  /// Restores right away; Undo puts it back in the trash.
  Future<void> _restore(FileNode node) async {
    setState(() => _items = _items?.where((item) => item.id != node.id).toList());
    await _run(() async {
      await _controller.client.restoreFromTrash(node.id);
      _controller.undo.show(UndoEntry(
        message: appStrings.undoRestored(node.name),
        onUndo: () => _run(() => _controller.client.trashNode(node.id)),
      ));
    });
  }

  String _expiry(FileNode node) {
    final expires = node.expiresAt;
    if (expires == null) return '';
    final days = expires.difference(DateTime.now()).inHours / 24;
    return ' · ${appStrings.filesTrashExpiresIn(math.max(0, days.ceil()))}';
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    final locale = currentAppLanguage.code;
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(
          title: appStrings.trashTitle,
          subtitle: appStrings.trashSubtitle,
          trailing: items == null || items.isEmpty
              ? null
              : OutlinedButton.icon(
                  onPressed: () => unawaited(_confirmDelete(
                    context,
                    title: appStrings.emptyTrash,
                    message: appStrings.emptyTrashConfirm,
                    confirmLabel: appStrings.emptyTrash,
                    onConfirm: () => _run(_controller.client.emptyTrash),
                  )),
                  icon: const Icon(Icons.delete_forever_outlined, size: 18),
                  label: Text(appStrings.emptyTrash),
                ),
        ),
        if (_error != null) _InlineError(message: _error!, onDismiss: () => setState(() => _error = null)),
        if (items == null && _error == null) const _LoadingPlaceholder(),
        if (items != null && items.isEmpty)
          _EmptyCard(icon: Icons.delete_outline_rounded, title: appStrings.trashIsEmpty, subtitle: appStrings.trashIsEmptySubtitle),
        if (items != null && items.isNotEmpty)
          _AppCard(
            padding: EdgeInsets.zero,
            child: Column(
              children: <Widget>[
                for (var i = 0; i < items.length; i++) ...<Widget>[
                  if (i > 0) Divider(height: 1, color: _border),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                    child: Row(
                      children: <Widget>[
                        _FileIcon(node: items[i], size: 34),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Text(items[i].name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 14, fontWeight: FontWeight.w500)),
                              const SizedBox(height: 2),
                              Text(
                                appStrings.deletedOn(formatDate(items[i].trashedAt, locale), items[i].originalPath ?? '/') + _expiry(items[i]),
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(color: _textMuted, fontSize: 12.5),
                              ),
                            ],
                          ),
                        ),
                        TextButton.icon(
                          onPressed: () => unawaited(_restore(items[i])),
                          icon: const Icon(Icons.restore_rounded, size: 18),
                          label: Text(appStrings.restore),
                        ),
                        IconButton(
                          tooltip: appStrings.deleteForever,
                          onPressed: () => unawaited(_confirmDelete(
                            context,
                            title: appStrings.deleteForever,
                            message: appStrings.deleteForeverConfirm(items[i].name),
                            confirmLabel: appStrings.deleteForever,
                            onConfirm: () => _run(() => _controller.client.deleteForever(items[i].id)),
                          )),
                          icon: const Icon(Icons.delete_forever_outlined, size: 20),
                        ),
                      ],
                    ),
                  ),
                ],
              ],
            ),
          ),
      ],
    );
  }
}
