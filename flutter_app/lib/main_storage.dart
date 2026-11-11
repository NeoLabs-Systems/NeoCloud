part of 'main.dart';

/// Settings › Storage: what takes the space (files, earlier versions,
/// trash), by type, the largest files and identical copies, with ways to
/// free it. Trashing is undoable; emptying the trash asks first.
class _StorageSettingsPage extends StatefulWidget {
  const _StorageSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_StorageSettingsPage> createState() => _StorageSettingsPageState();
}

class _StorageSettingsPageState extends State<_StorageSettingsPage> {
  StorageBreakdown? _breakdown;
  DuplicateReport? _duplicates;
  bool _loaded = false;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.filesRevision.addListener(_load);
    unawaited(_load());
  }

  @override
  void dispose() {
    _controller.filesRevision.removeListener(_load);
    super.dispose();
  }

  /// The breakdown and the duplicates; a server without them still shows
  /// the account's totals.
  Future<void> _load() async {
    unawaited(_controller.refreshAccount());
    final results = await Future.wait<Object?>(<Future<Object?>>[
      _controller.client.storageBreakdown().then<Object?>((value) => value, onError: (Object _) => null),
      _controller.client.storageDuplicates().then<Object?>((value) => value, onError: (Object _) => null),
    ]);
    if (!mounted) return;
    setState(() {
      _breakdown = results[0] as StorageBreakdown?;
      _duplicates = results[1] as DuplicateReport?;
      _loaded = true;
    });
  }

  Future<void> _trash(List<FileNode> nodes) async {
    final failures = await runBatch(nodes, (node) => _controller.client.trashNode(node.id));
    final done = nodes.where((node) => !failures.any((failure) => failure.item.id == node.id)).toList();
    if (done.isNotEmpty) {
      _controller.undo.show(UndoEntry(
        message: appStrings.undoTrashed(done.length, done.first.name),
        onUndo: () async {
          await runBatch(done, (node) => _controller.client.restoreFromTrash(node.id));
          _controller.notifyFilesChanged();
        },
      ));
      _controller.notifyFilesChanged();
    }
    if (failures.isNotEmpty && mounted) {
      setState(() => _error = '${appStrings.filesTrashFailed(_itemNames(failures.map((f) => f.item).toList()))} ${_controller.friendlyError(failures.first.error)}');
    }
  }

  /// Keeps the most recently changed copy of a duplicate group; the rest go to the trash.
  Future<void> _keepNewest(DuplicateGroup group) {
    final sorted = List<FileNode>.of(group.nodes)
      ..sort((a, b) => (b.modifiedAt ?? DateTime(0)).compareTo(a.modifiedAt ?? DateTime(0)));
    return _trash(sorted.skip(1).toList());
  }

  Future<void> _emptyTrash() => _confirmDelete(
    context,
    title: appStrings.emptyTrash,
    message: appStrings.emptyTrashConfirm,
    confirmLabel: appStrings.emptyTrash,
    onConfirm: () async {
      try {
        await _controller.client.emptyTrash();
        _controller.notifyFilesChanged();
      } catch (error) {
        if (mounted) setState(() => _error = _controller.friendlyError(error));
      }
    },
  );

  void _showInFolder(FileNode node) => _controller.openFolder(node.parentId);

  @override
  Widget build(BuildContext context) {
    final usage = _controller.storage;
    if (usage == null || !_loaded) return const _LoadingPlaceholder();
    final breakdown = _breakdown;
    final versions = breakdown?.versions ?? usage.versionBytes;
    final trash = breakdown?.trash ?? usage.trashBytes;
    final files = breakdown?.files ?? math.max(0, usage.usedBytes - versions - trash);
    final duplicates = _duplicates;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (_error != null) Padding(padding: const EdgeInsets.only(bottom: 18), child: _InlineError(message: _error!, onDismiss: () => setState(() => _error = null))),
        _SectionCard(
          title: appStrings.pageStorage,
          trailing: TextButton.icon(
            onPressed: trash == 0 ? null : () => unawaited(_emptyTrash()),
            icon: const Icon(Icons.delete_forever_outlined, size: 18),
            label: Text(appStrings.emptyTrash),
          ),
          child: _StorageBar(usage: usage, files: files, versions: versions, trash: trash),
        ),
        if (breakdown != null && breakdown.byType.isNotEmpty) ...<Widget>[
          const SizedBox(height: 24),
          _SectionCard(title: appStrings.storageByType, child: _StorageByType(types: breakdown.byType)),
        ],
        if (breakdown != null && breakdown.largest.isNotEmpty) ...<Widget>[
          const SizedBox(height: 24),
          _SectionCard(
            title: appStrings.storageLargest,
            child: Column(
              children: <Widget>[
                for (final node in breakdown.largest.take(10))
                  _StorageFileRow(
                    node: node,
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: <Widget>[
                        IconButton(tooltip: appStrings.showInFolder, onPressed: () => _showInFolder(node), icon: const Icon(Icons.folder_open_outlined, size: 18)),
                        IconButton(tooltip: appStrings.moveToTrash, onPressed: () => unawaited(_trash(<FileNode>[node])), icon: const Icon(Icons.delete_outline_rounded, size: 18)),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ],
        if (duplicates != null) ...<Widget>[
          const SizedBox(height: 24),
          _SectionCard(
            title: appStrings.storageDuplicates,
            trailing: duplicates.groups.isEmpty ? null : Text(appStrings.storageReclaimable(formatBytes(duplicates.reclaimable)), style: TextStyle(color: _textMuted, fontSize: 12.5)),
            child: duplicates.groups.isEmpty
                ? _EmptyText(appStrings.storageNoDuplicates)
                : Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      for (var i = 0; i < duplicates.groups.length && i < 20; i++) ...<Widget>[
                        if (i > 0) Divider(height: 24, color: _border),
                        _DuplicateGroupView(group: duplicates.groups[i], onKeepNewest: () => unawaited(_keepNewest(duplicates.groups[i]))),
                      ],
                    ],
                  ),
          ),
        ],
        if (breakdown == null && duplicates == null) ...<Widget>[
          const SizedBox(height: 16),
          _InlineNote(message: appStrings.storageDetailsUnavailable),
        ],
      ],
    );
  }
}

/// One stacked bar: current files, earlier versions and trash, with a
/// legend of dots. Neutral tones only; the quota warning is a dot, too.
class _StorageBar extends StatelessWidget {
  const _StorageBar({required this.usage, required this.files, required this.versions, required this.trash});

  final StorageUsage usage;
  final int files;
  final int versions;
  final int trash;

  @override
  Widget build(BuildContext context) {
    final used = files + versions + trash;
    final capacity = usage.unlimited ? math.max(used, 1) : math.max(usage.quotaBytes, used);
    final parts = <(String, int, Color)>[
      (appStrings.storageCurrentFiles, files, _textSecondary),
      (appStrings.storageVersions, versions, _accentAlt),
      (appStrings.storageTrash, trash, _textMuted.withValues(alpha: 0.5)),
    ];
    final label = usage.unlimited ? appStrings.storageUsedUnlimited(formatBytes(used)) : appStrings.storageUsedOf(formatBytes(used), formatBytes(usage.quotaBytes));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(child: Text(label, style: _displayTitleStyle(18))),
            if (!usage.unlimited && usage.fraction >= 0.9) _StatusPill(label: appStrings.storageAlmostFull, color: _warning),
          ],
        ),
        const SizedBox(height: 14),
        Semantics(
          label: parts.map((part) => '${part.$1} ${formatBytes(part.$2)}').join(', '),
          child: ClipRRect(
            borderRadius: BorderRadius.circular(AppRadius.pill),
            child: SizedBox(
              height: 10,
              child: Container(
                color: _bgTertiary,
                child: Row(
                  children: <Widget>[
                    for (final (_, bytes, color) in parts)
                      if (bytes > 0) Expanded(flex: math.max(1, (bytes / capacity * 1000).round()), child: Container(color: color)),
                    if (capacity > used) Expanded(flex: math.max(1, ((capacity - used) / capacity * 1000).round()), child: const SizedBox()),
                  ],
                ),
              ),
            ),
          ),
        ),
        const SizedBox(height: 14),
        Wrap(
          spacing: 20,
          runSpacing: 8,
          children: <Widget>[
            for (final (name, bytes, color) in parts) _StatusPill(label: '$name · ${formatBytes(bytes)}', color: color),
          ],
        ),
      ],
    );
  }
}

String _storageTypeLabel(String type) {
  for (final (key, label) in _searchTypeOptions) {
    if (key == type) return label;
  }
  if (type == 'vault') return appStrings.storageTypeVaults;
  return type == 'other' || type.isEmpty ? appStrings.storageTypeOther : type[0].toUpperCase() + type.substring(1);
}

class _StorageByType extends StatelessWidget {
  const _StorageByType({required this.types});

  final List<StorageTypeUsage> types;

  @override
  Widget build(BuildContext context) {
    final largest = types.map((type) => type.bytes).fold<int>(1, math.max);
    return Column(
      children: <Widget>[
        for (final type in types)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 7),
            child: Row(
              children: <Widget>[
                SizedBox(width: 120, child: Text(_storageTypeLabel(type.type), style: TextStyle(color: _textPrimary, fontSize: 13.5))),
                Expanded(
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(AppRadius.pill),
                    child: LinearProgressIndicator(
                      minHeight: 6,
                      value: type.bytes / largest,
                      backgroundColor: _bgTertiary,
                      valueColor: AlwaysStoppedAnimation<Color>(_textSecondary),
                    ),
                  ),
                ),
                SizedBox(
                  width: 150,
                  child: Text(
                    '${formatBytes(type.bytes)} · ${appStrings.dragItems(type.count)}',
                    textAlign: TextAlign.right,
                    style: TextStyle(color: _textMuted, fontSize: 12.5),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class _StorageFileRow extends StatelessWidget {
  const _StorageFileRow({required this.node, this.trailing});

  final FileNode node;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: <Widget>[
          _FileIcon(node: node, size: 30),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 13.5, fontWeight: FontWeight.w500)),
                Text(
                  '${formatBytes(node.size)} · ${_displayPath(node) ?? formatDate(node.modifiedAt, currentAppLanguage.code)}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: _textMuted, fontSize: 12),
                ),
              ],
            ),
          ),
          ?trailing,
        ],
      ),
    );
  }
}

class _DuplicateGroupView extends StatelessWidget {
  const _DuplicateGroupView({required this.group, required this.onKeepNewest});

  final DuplicateGroup group;
  final VoidCallback onKeepNewest;

  @override
  Widget build(BuildContext context) {
    final newest = group.nodes.fold<FileNode?>(
      null,
      (best, node) => best == null || (node.modifiedAt ?? DateTime(0)).isAfter(best.modifiedAt ?? DateTime(0)) ? node : best,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Text(
                appStrings.storageDuplicateGroup(group.nodes.length, formatBytes(group.size)),
                style: TextStyle(color: _textSecondary, fontSize: 13, fontWeight: FontWeight.w600),
              ),
            ),
            OutlinedButton(onPressed: onKeepNewest, child: Text(appStrings.storageKeepNewest)),
          ],
        ),
        const SizedBox(height: 4),
        for (final node in group.nodes)
          _StorageFileRow(
            node: node,
            trailing: node == newest ? _StatusPill(label: appStrings.storageNewest, color: _success) : null,
          ),
      ],
    );
  }
}
