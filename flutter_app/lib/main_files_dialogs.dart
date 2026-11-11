part of 'main.dart';

/// "already exists": both files side by side (size, date), the name "keep
/// both" would give, and "apply to all N" for the rest of the batch.
Future<ConflictDecision?> showConflictDialog(BuildContext context, NeoCloudController controller, UploadConflict conflict) {
  return showDialog<ConflictDecision>(
    context: context,
    builder: (dialogContext) => _ConflictDialog(controller: controller, conflict: conflict),
  );
}

class _ConflictDialog extends StatefulWidget {
  const _ConflictDialog({required this.controller, required this.conflict});

  final NeoCloudController controller;
  final UploadConflict conflict;

  @override
  State<_ConflictDialog> createState() => _ConflictDialogState();
}

class _ConflictDialogState extends State<_ConflictDialog> {
  bool _applyToAll = false;
  FileNode? _existing;
  Set<String> _taken = <String>{};

  UploadSource get _source => widget.conflict.source;

  @override
  void initState() {
    super.initState();
    unawaited(_loadExisting());
  }

  /// The file already there, and the names taken next to it.
  Future<void> _loadExisting() async {
    try {
      final found = await widget.controller.client.search(_source.name, parentId: widget.conflict.folderId, limit: 50);
      final siblings = found.where((node) => node.parentId == widget.conflict.folderId).toList();
      if (!mounted) return;
      setState(() {
        _existing = siblings.where((node) => node.name == _source.name).firstOrNull;
        _taken = siblings.map((node) => node.name).toSet()..add(_source.name);
      });
    } catch (_) {
      // The dialog still works with the names alone.
    }
  }

  void _choose(ConflictChoice choice) => Navigator.of(context).pop(ConflictDecision(choice, applyToAll: _applyToAll));

  Widget _fileCard(String title, int? size, DateTime? date) {
    final locale = currentAppLanguage.code;
    return Expanded(
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _border)),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(title, style: _sectionEyebrowStyle()),
            const SizedBox(height: 6),
            Text(size == null ? '—' : formatBytes(size), style: TextStyle(color: _textPrimary, fontSize: 14, fontWeight: FontWeight.w600)),
            const SizedBox(height: 2),
            Text(date == null ? '—' : formatDate(date, locale), style: TextStyle(color: _textSecondary, fontSize: 12.5)),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final existing = _existing;
    final remaining = widget.conflict.remaining;
    final keepBothName = nextFreeName(_source.name, _taken.isEmpty ? <String>{_source.name} : _taken);
    return AlertDialog(
      title: Text(appStrings.nameExistsTitle(_source.name)),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(appStrings.nameExistsMessage, style: TextStyle(color: _textSecondary, height: 1.45)),
            const SizedBox(height: 14),
            Row(
              children: <Widget>[
                _fileCard(appStrings.uploadConflictExisting, existing?.size, existing?.modifiedAt),
                const SizedBox(width: 10),
                _fileCard(appStrings.uploadConflictIncoming, _source.size, _source.modifiedAt),
              ],
            ),
            const SizedBox(height: 12),
            Text(appStrings.uploadConflictKeepBothAs(keepBothName), style: TextStyle(color: _textMuted, fontSize: 12.5)),
            if (remaining > 0)
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                value: _applyToAll,
                onChanged: (value) => setState(() => _applyToAll = value ?? false),
                title: Text(appStrings.uploadConflictApplyToAll(remaining), style: const TextStyle(fontSize: 13.5)),
                controlAffinity: ListTileControlAffinity.leading,
              ),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => _choose(ConflictChoice.skip), child: Text(appStrings.skip)),
        OutlinedButton(onPressed: () => _choose(ConflictChoice.keepBoth), child: Text(appStrings.keepBoth)),
        FilledButton(onPressed: () => _choose(ConflictChoice.replace), child: Text(appStrings.replace)),
      ],
    );
  }
}

/// Picks a destination folder by browsing the tree; resolves to its id and
/// name. Folders in [exclude] (what is being moved) can't be entered;
/// [hideVaults] leaves out vault folders, for targets that can't encrypt.
Future<(String, String)?> showFolderPicker(
  BuildContext context,
  NeoCloudController controller, {
  Set<String> exclude = const <String>{},
  String? title,
  String? confirmLabel,
  bool hideVaults = false,
}) {
  return showDialog<(String, String)>(
    context: context,
    builder: (dialogContext) => _FolderPickerDialog(
      controller: controller,
      exclude: exclude,
      title: title ?? appStrings.moveTo,
      confirmLabel: confirmLabel ?? appStrings.moveHere,
      hideVaults: hideVaults,
    ),
  );
}

class _FolderPickerDialog extends StatefulWidget {
  const _FolderPickerDialog({
    required this.controller,
    required this.exclude,
    required this.title,
    required this.confirmLabel,
    required this.hideVaults,
  });

  final NeoCloudController controller;
  final Set<String> exclude;
  final String title;
  final String confirmLabel;
  final bool hideVaults;

  @override
  State<_FolderPickerDialog> createState() => _FolderPickerDialogState();
}

class _FolderPickerDialogState extends State<_FolderPickerDialog> {
  FolderListing? _listing;
  String? _error;

  @override
  void initState() {
    super.initState();
    _open('root');
  }

  Future<void> _open(String id) async {
    try {
      final listing = await widget.controller.client.listFolder(id);
      final vaults = widget.controller.vaults;
      final items = await vaults.reveal(listing.items);
      final path = listing.folder.vaultId == null ? listing.path : await vaults.revealPath(listing.path, listing.folder.vaultId!);
      if (mounted) setState(() => _listing = FolderListing(folder: listing.folder, path: path, items: items));
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  bool _isLocked(FileNode folder) => folder.inVault && !widget.controller.vaults.isUnlocked(folder.vaultId!);

  @override
  Widget build(BuildContext context) {
    final listing = _listing;
    final folders = listing?.items
            .where((n) => n.isFolder && !widget.exclude.contains(n.id) && !(widget.hideVaults && n.inVault))
            .toList() ??
        const <FileNode>[];
    final name = listing == null || listing.folder.parentId == null ? appStrings.home : (listing.path.lastOrNull?.name ?? listing.folder.name);
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 460,
        height: 380,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (listing != null) _Breadcrumbs(path: listing.path, onOpen: (id) => _open(id ?? 'root')),
            if (_error != null) _InlineError(message: _error!),
            const SizedBox(height: 8),
            Expanded(
              child: listing == null
                  ? const _LoadingPlaceholder()
                  : _AppCard(
                      padding: EdgeInsets.zero,
                      child: ListView(
                        children: <Widget>[
                          for (final folder in folders)
                            ListTile(
                              leading: _FileIcon(node: folder, size: 30),
                              title: Text(folder.name, style: const TextStyle(fontSize: 14)),
                              trailing: Icon(
                                _isLocked(folder) ? Icons.lock_outline_rounded : Icons.chevron_right_rounded,
                                color: _textMuted,
                              ),
                              // A locked vault is unlocked from the Files page first.
                              onTap: _isLocked(folder) ? null : () => _open(folder.id),
                            ),
                          if (folders.isEmpty) Padding(padding: const EdgeInsets.all(16), child: _EmptyText(appStrings.emptyFolderTitle)),
                        ],
                      ),
                    ),
            ),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: listing == null ? null : () => Navigator.of(context).pop((listing.folder.id, name)),
          child: Text(widget.confirmLabel),
        ),
      ],
    );
  }
}

Future<void> showVersionsDialog(BuildContext context, NeoCloudController controller, FileNode node) {
  return showDialog<void>(
    context: context,
    builder: (dialogContext) => _VersionsDialog(controller: controller, node: node),
  );
}

class _VersionsDialog extends StatefulWidget {
  const _VersionsDialog({required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  State<_VersionsDialog> createState() => _VersionsDialogState();
}

class _VersionsDialogState extends State<_VersionsDialog> {
  List<FileVersion>? _versions;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final versions = await widget.controller.client.versions(widget.node.id);
      if (mounted) setState(() => _versions = versions);
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  Future<void> _restore(FileVersion version) async {
    try {
      await widget.controller.client.restoreVersion(widget.node.id, version.version);
      widget.controller.notifyFilesChanged();
      if (mounted) Navigator.of(context).pop();
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    final versions = _versions;
    final locale = currentAppLanguage.code;
    final client = widget.controller.client;
    return AlertDialog(
      title: Text(appStrings.versionsTitle(widget.node.name)),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (_error != null) _InlineError(message: _error!),
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: _StatusPill(label: '', color: _success),
              title: Text(appStrings.versionCurrent),
              subtitle: Text('${formatBytes(widget.node.size)} · ${formatDate(widget.node.modifiedAt, locale)}'),
            ),
            if (versions == null) const _LoadingPlaceholder(),
            if (versions != null && versions.isEmpty) _EmptyText(appStrings.noVersions),
            for (final version in versions ?? const <FileVersion>[])
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: Icon(Icons.history_rounded, color: _textMuted),
                title: Text(appStrings.versionNumber(version.version)),
                subtitle: Text('${formatBytes(version.size)} · ${formatDate(version.createdAt, locale)}'),
                trailing: Wrap(
                  children: <Widget>[
                    IconButton(
                      tooltip: appStrings.download,
                      onPressed: () => unawaited(startDownload(
                        client.uri('/api/files/${widget.node.id}/content', <String, String>{'download': '1', 'version': '${version.version}'}),
                        fileName: widget.node.name,
                        headers: client.mediaHeaders,
                      )),
                      icon: const Icon(Icons.download_rounded, size: 18),
                    ),
                    TextButton(onPressed: () => unawaited(_restore(version)), child: Text(appStrings.restore)),
                  ],
                ),
              ),
          ],
        ),
      ),
      actions: <Widget>[FilledButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.close))],
    );
  }
}
