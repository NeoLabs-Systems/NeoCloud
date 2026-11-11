part of 'main.dart';

// Shared with me (/app/shared): what other accounts on this server share
// with this one, each with its owner and what this account may do there.
// A shared folder opens in the Files page under /app/shared/<folderId>,
// where read-only shares only browse and download.

class SharedWithMePanel extends StatefulWidget {
  const SharedWithMePanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<SharedWithMePanel> createState() => _SharedWithMePanelState();
}

class _SharedWithMePanelState extends State<SharedWithMePanel> {
  bool _loading = true;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.fileChanges.addListener(_onFilesChangedElsewhere);
    unawaited(_load());
  }

  @override
  void dispose() {
    _controller.fileChanges.removeListener(_onFilesChangedElsewhere);
    super.dispose();
  }

  /// A share renamed or trashed by its owner changes the list.
  void _onFilesChangedElsewhere(FileChangeBatch _) => unawaited(_load());

  Future<void> _load() async {
    try {
      _controller.sharedWithMe = await _controller.client.sharedWithMe();
      if (mounted) setState(() => _error = null);
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _open(SharedWithMe share) {
    final node = share.node;
    if (node.isFolder) {
      _controller.openSharedFolder(node.id);
      return;
    }
    // A shared file on its own has no folder to save a new version into.
    unawaited(showFileViewer(context, _controller, <FileNode>[node], 0, readOnly: true));
  }

  Future<void> _leave(SharedWithMe share) {
    return _confirmDelete(
      context,
      title: appStrings.sharedLeave,
      message: appStrings.sharedLeaveConfirm(share.node.name, share.ownerName),
      confirmLabel: appStrings.sharedLeave,
      onConfirm: () async {
        try {
          await _controller.client.leaveShare(share.id);
          await _load();
        } catch (error) {
          if (mounted) setState(() => _error = _controller.friendlyError(error));
        }
      },
    );
  }

  List<_FileAction> _actions(SharedWithMe share) {
    final node = share.node;
    return <_FileAction>[
      _FileAction(node.isFolder ? Icons.folder_open_outlined : Icons.open_in_new_rounded, appStrings.open, () => _open(share)),
      _FileAction(
        Icons.download_rounded,
        node.isFolder ? appStrings.downloadZip : appStrings.download,
        () => unawaited(node.isFolder ? _controller.downloadZip(<FileNode>[node]) : _controller.downloadFile(node)),
        quick: true,
      ),
      _FileAction(Icons.logout_rounded, appStrings.sharedLeave, () => unawaited(_leave(share)), danger: true),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final shares = _controller.sharedWithMe;
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(title: appStrings.navSharedWithMe, subtitle: appStrings.sharedWithMeSubtitle),
        if (_error != null) _LoadError(message: _error!, onRetry: () => unawaited(_load())),
        if (_loading && shares.isEmpty && _error == null) const _LoadingPlaceholder(),
        if (!_loading && shares.isEmpty && _error == null)
          _EmptyCard(icon: Icons.folder_shared_outlined, title: appStrings.sharedWithMeEmpty, subtitle: appStrings.sharedWithMeEmptySubtitle),
        if (shares.isNotEmpty)
          _EntranceMotion(
            child: _AppCard(
              padding: EdgeInsets.zero,
              child: Column(
                children: <Widget>[
                  for (var i = 0; i < shares.length; i++) ...<Widget>[
                    if (i > 0) Divider(height: 1, color: _border),
                    _SharedRow(share: shares[i], onOpen: () => _open(shares[i]), actions: _actions(shares[i])),
                  ],
                ],
              ),
            ),
          ),
      ],
    );
  }
}

class _SharedRow extends StatelessWidget {
  const _SharedRow({required this.share, required this.onOpen, required this.actions});

  final SharedWithMe share;
  final VoidCallback onOpen;
  final List<_FileAction> actions;

  @override
  Widget build(BuildContext context) {
    final canWrite = share.permission.canWrite;
    return InkWell(
      onTap: onOpen,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Row(
          children: <Widget>[
            _FileIcon(node: share.node),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    share.node.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 14),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    appStrings.sharedBy(share.ownerName),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: _textMuted, fontSize: 12.5),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            _StatusPill(
              label: canWrite ? appStrings.memberCanEdit : appStrings.memberCanView,
              color: canWrite ? _success : _textMuted,
            ),
            const SizedBox(width: 4),
            _FileActionsMenu(actions: actions),
          ],
        ),
      ),
    );
  }
}
