part of 'main.dart';

// Public links to a file or folder (docs/clients.md §3). Opening the dialog
// for an item that already has an active link shows that link instead of
// minting another; the options form (main_share_form.dart) creates or edits.
// Settings › Shared links lists every link (main_shared_links_page.dart).

Future<void> showShareLinkDialog(BuildContext context, NeoCloudController controller, FileNode node) {
  return showDialog<void>(
    context: context,
    builder: (_) => _ShareLinkDialog(controller: controller, nodeId: node.id, nodeName: node.name, isFolder: node.isFolder),
  );
}

/// The editor for one existing link, from Settings › Shared links.
/// Resolves to true when the link was changed.
Future<bool?> showEditShareLinkDialog(BuildContext context, NeoCloudController controller, ShareLink link) {
  return showDialog<bool>(
    context: context,
    builder: (_) => _ShareLinkDialog(
      controller: controller,
      nodeId: link.nodeId,
      nodeName: link.nodeName,
      isFolder: link.isFolder,
      editLink: link,
    ),
  );
}

enum _ShareView { loading, link, form }

class _ShareLinkDialog extends StatefulWidget {
  const _ShareLinkDialog({
    required this.controller,
    required this.nodeId,
    required this.nodeName,
    required this.isFolder,
    this.editLink,
  });

  final NeoCloudController controller;
  final String nodeId;
  final String nodeName;
  final bool isFolder;

  /// Opens straight into editing this link.
  final ShareLink? editLink;

  @override
  State<_ShareLinkDialog> createState() => _ShareLinkDialogState();
}

class _ShareLinkDialogState extends State<_ShareLinkDialog> {
  _ShareView _view = _ShareView.loading;
  ShareLink? _link;
  int _otherLinks = 0;
  ShareDraft _draft = ShareDraft();
  bool _busy = false;
  bool _copied = false;
  bool _passwordCopied = false;
  bool _changed = false;
  String? _error;

  /// A password typed in this dialog, so it can be copied after saving.
  String? _password;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    final edit = widget.editLink;
    if (edit != null) {
      _link = edit;
      _startEditing();
    } else {
      unawaited(_loadExisting());
    }
  }

  Future<void> _loadExisting() async {
    try {
      // Older servers ignore nodeId; filter here as well.
      final links = (await _controller.client.shares(nodeId: widget.nodeId))
          .where((link) => link.nodeId == widget.nodeId && link.active)
          .toList();
      if (!mounted) return;
      setState(() {
        if (links.isEmpty) {
          _draft = ShareDraft();
          _view = _ShareView.form;
        } else {
          _link = links.first;
          _otherLinks = links.length - 1;
          _view = _ShareView.link;
        }
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = _controller.friendlyError(error);
          _view = _ShareView.form;
        });
      }
    }
  }

  void _startEditing() {
    setState(() {
      _draft = ShareDraft.fromLink(_link!);
      _error = null;
      _view = _ShareView.form;
    });
  }

  void _startNew() {
    setState(() {
      _link = null;
      _draft = ShareDraft();
      _error = null;
      _view = _ShareView.form;
    });
  }

  Future<void> _submit() async {
    final now = DateTime.now();
    final problem = _draft.validate(now);
    if (problem != null) {
      setState(() => _error = _shareDraftProblemText(problem));
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final draft = _draft;
      final ShareLink link;
      if (draft.editing) {
        final patch = draft.patch(now);
        link = patch.isEmpty ? draft.original! : await _controller.client.updateShare(draft.original!.id, patch);
      } else {
        link = await _controller.client.createShare(
          widget.nodeId,
          expiresAt: draft.expiresAt(now),
          password: draft.protect ? draft.password : null,
          mode: widget.isFolder ? draft.mode : ShareMode.view,
          maxDownloads: draft.maxDownloads,
          note: draft.trimmedNote,
        );
      }
      if (!mounted) return;
      setState(() {
        _changed = true;
        _link = link;
        _password = draft.protect && draft.password.isNotEmpty ? draft.password : null;
        _copied = false;
        _passwordCopied = false;
        _view = _ShareView.link;
      });
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _revoke() async {
    final link = _link;
    if (link == null) return;
    await _confirmDelete(
      context,
      title: appStrings.shareRevoke,
      message: appStrings.shareRevokeConfirm(link.nodeName),
      confirmLabel: appStrings.shareRevoke,
      onConfirm: () async {
        setState(() => _busy = true);
        try {
          await _controller.client.deleteShare(link.id);
          if (mounted) Navigator.of(context).pop(true);
        } catch (error) {
          if (mounted) {
            setState(() {
              _busy = false;
              _error = _controller.friendlyError(error);
            });
          }
        }
      },
    );
  }

  Future<void> _copyUrl(String url) async {
    await Clipboard.setData(ClipboardData(text: url));
    if (mounted) setState(() => _copied = true);
  }

  Future<void> _copyPassword(String password) async {
    await Clipboard.setData(ClipboardData(text: password));
    if (mounted) setState(() => _passwordCopied = true);
  }

  List<Widget> _linkView(ShareLink link) {
    final locale = currentAppLanguage.code;
    final password = _password;
    return <Widget>[
      _AppCard(
        padding: const EdgeInsets.fromLTRB(14, 10, 6, 10),
        child: Row(
          children: <Widget>[
            Expanded(child: SelectableText(link.url, style: GoogleFonts.geistMono(fontSize: 12.5, color: _textPrimary))),
            const SizedBox(width: 8),
            FilledButton.icon(
              onPressed: () => unawaited(_copyUrl(link.url)),
              icon: Icon(_copied ? Icons.check_rounded : Icons.copy_rounded, size: 16),
              label: Text(_copied ? appStrings.copied : appStrings.copy),
            ),
          ],
        ),
      ),
      const SizedBox(height: AppSpacing.sm),
      Text(shareSummary(appStrings, link, locale: locale), style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.45)),
      if (link.note != null) ...<Widget>[
        const SizedBox(height: 6),
        Text('${appStrings.shareNoteTag}: ${link.note}', style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
      ],
      if (password != null) ...<Widget>[
        const SizedBox(height: AppSpacing.md),
        Row(
          children: <Widget>[
            Expanded(child: Text(appStrings.sharePassword, style: TextStyle(color: _textPrimary, fontSize: 13.5))),
            OutlinedButton.icon(
              onPressed: () => unawaited(_copyPassword(password)),
              icon: Icon(_passwordCopied ? Icons.check_rounded : Icons.key_rounded, size: 16),
              label: Text(_passwordCopied ? appStrings.sharePasswordCopied : appStrings.sharePasswordCopy),
            ),
          ],
        ),
        const SizedBox(height: AppSpacing.sm),
        _InlineNote(message: appStrings.sharePasswordSendSeparately),
      ],
      if (_otherLinks > 0) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        Text(appStrings.shareMoreLinks(_otherLinks), style: TextStyle(color: _textMuted, fontSize: 12.5)),
      ],
    ];
  }

  List<Widget> _actions() {
    switch (_view) {
      case _ShareView.loading:
        return <Widget>[TextButton(onPressed: () => Navigator.of(context).pop(_changed), child: Text(appStrings.cancel))];
      case _ShareView.link:
        return <Widget>[
          TextButton(
            onPressed: _busy ? null : () => unawaited(_revoke()),
            style: TextButton.styleFrom(foregroundColor: _danger),
            child: Text(appStrings.shareRevoke),
          ),
          if (widget.editLink == null) TextButton(onPressed: _busy ? null : _startNew, child: Text(appStrings.shareNewLink)),
          OutlinedButton(onPressed: _busy ? null : _startEditing, child: Text(appStrings.shareEdit)),
          TextButton(onPressed: () => Navigator.of(context).pop(_changed), child: Text(appStrings.close)),
        ];
      case _ShareView.form:
        final canGoBack = _link != null && widget.editLink == null;
        return <Widget>[
          TextButton(
            onPressed: _busy
                ? null
                : canGoBack
                ? () => setState(() {
                    _error = null;
                    _view = _ShareView.link;
                  })
                : () => Navigator.of(context).pop(_changed),
            child: Text(appStrings.cancel),
          ),
          FilledButton(
            onPressed: _busy ? null : () => unawaited(_submit()),
            child: _busy
                ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                : Text(_draft.editing ? appStrings.shareSave : appStrings.shareCreate),
          ),
        ];
    }
  }

  @override
  Widget build(BuildContext context) {
    final link = _link;
    return AlertDialog(
      title: Text(appStrings.shareLinkTitle(widget.nodeName)),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (_view == _ShareView.loading) const _LoadingPlaceholder(),
              if (_view == _ShareView.link && link != null) ..._linkView(link),
              if (_view == _ShareView.form)
                _ShareForm(
                  key: ObjectKey(_draft),
                  draft: _draft,
                  isFolder: widget.isFolder,
                  enabled: !_busy,
                  onChanged: () => setState(() => _error = null),
                ),
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: _actions(),
    );
  }
}
