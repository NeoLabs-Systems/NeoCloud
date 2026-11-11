part of 'main.dart';

/// Settings › Shared links: every link of the account, active ones first,
/// with copy, edit and revoke.
class _SharedLinksSettingsPage extends StatefulWidget {
  const _SharedLinksSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_SharedLinksSettingsPage> createState() => _SharedLinksSettingsPageState();
}

class _SharedLinksSettingsPageState extends State<_SharedLinksSettingsPage> with _PageLoad {
  List<ShareLink>? _links;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final links = await _controller.client.shares();
      if (mounted) setState(() => _links = links);
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _revoke(ShareLink link) {
    return _confirmDelete(
      context,
      title: appStrings.shareRevoke,
      message: appStrings.shareRevokeConfirm(link.nodeName),
      confirmLabel: appStrings.shareRevoke,
      onConfirm: () => runAction(_controller, () async {
        await _controller.client.deleteShare(link.id);
        await _load();
      }, notice: appStrings.shareRevoked),
    );
  }

  Future<void> _edit(ShareLink link) async {
    final changed = await showEditShareLinkDialog(context, _controller, link);
    if (changed != true || !mounted) return;
    setState(() => pageNotice = appStrings.shareSaved);
    await _load();
  }

  Widget _row(ShareLink link) {
    final locale = currentAppLanguage.code;
    return _SettingsRow(
      label: link.nodeName,
      tag: link.mode == ShareMode.uploadOnly
          ? appStrings.shareModeFileRequestTag
          : link.isFolder
          ? appStrings.shareFolderTag
          : null,
      description: shareListLine(appStrings, link, locale: locale),
      control: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          IconButton(
            tooltip: appStrings.copy,
            onPressed: !link.active
                ? null
                : () {
                    unawaited(Clipboard.setData(ClipboardData(text: link.url)));
                    setState(() => pageNotice = appStrings.shareCopied);
                  },
            icon: const Icon(Icons.copy_rounded, size: 18),
          ),
          IconButton(
            tooltip: appStrings.shareEdit,
            onPressed: busy ? null : () => unawaited(_edit(link)),
            icon: const Icon(Icons.edit_outlined, size: 18),
          ),
          TextButton(onPressed: busy ? null : () => unawaited(_revoke(link)), child: Text(appStrings.shareRevoke)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final links = _links;
    final active = <ShareLink>[for (final link in links ?? const <ShareLink>[]) if (link.active) link];
    final inactive = <ShareLink>[for (final link in links ?? const <ShareLink>[]) if (!link.active) link];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.shareGroupActive,
          description: appStrings.sharedLinksHint,
          children: <Widget>[
            if (links == null && pageError == null) const _LoadingPlaceholder(),
            if (links != null && active.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.sharedLinksEmpty)),
            for (final link in active) _row(link),
          ],
        ),
        if (inactive.isNotEmpty)
          _SettingsGroup(
            title: appStrings.shareGroupInactive,
            children: <Widget>[for (final link in inactive) _row(link)],
          ),
      ],
    );
  }
}
