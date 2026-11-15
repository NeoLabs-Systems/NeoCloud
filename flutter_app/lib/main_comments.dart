part of 'main.dart';

const int _commentMaxLength = 2000;

/// The comments on one file or folder: a field to write one, then the list,
/// newest first. Authors edit their own; the author and the file's owner
/// delete. Reloads when a comment changes anywhere.
class CommentsSection extends StatefulWidget {
  const CommentsSection({super.key, required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  State<CommentsSection> createState() => _CommentsSectionState();
}

class _CommentsSectionState extends State<CommentsSection> {
  final TextEditingController _input = TextEditingController();
  final TextEditingController _editInput = TextEditingController();
  final List<NodeComment> _comments = <NodeComment>[];
  int? _next;
  bool _loaded = false;
  bool _busy = false;
  int? _editing;
  String? _error;

  NeoCloudController get _controller => widget.controller;
  String get _nodeId => widget.node.id;

  @override
  void initState() {
    super.initState();
    _input.addListener(() => setState(() {}));
    _controller.commentsRevision.addListener(_reload);
    unawaited(_load());
  }

  @override
  void dispose() {
    _controller.commentsRevision.removeListener(_reload);
    _input.dispose();
    _editInput.dispose();
    super.dispose();
  }

  void _reload() => unawaited(_load());

  Future<void> _load() async {
    try {
      final page = await _controller.client.comments(_nodeId);
      if (!mounted) return;
      setState(() {
        _comments
          ..clear()
          ..addAll(page.comments);
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
    if (cursor == null) return;
    try {
      final page = await _controller.client.comments(_nodeId, cursor: cursor);
      if (!mounted) return;
      setState(() {
        _comments.addAll(page.comments);
        _next = page.nextCursor;
      });
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    }
  }

  /// Runs a change and reloads; failures show above the list.
  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
      await _load();
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _post() async {
    final text = _input.text.trim();
    if (text.isEmpty) return;
    await _run(() async {
      await _controller.client.addComment(_nodeId, text);
      _input.clear();
    });
  }

  Future<void> _saveEdit(NodeComment comment) async {
    final text = _editInput.text.trim();
    if (text.isEmpty) return;
    await _run(() async {
      await _controller.client.editComment(_nodeId, comment.id, text);
      _editing = null;
    });
  }

  Future<void> _delete(NodeComment comment) async {
    final confirmed = await showAdaptiveSheet<bool>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(appStrings.commentsDeleteTitle),
        content: SizedBox(width: 380, child: Text(appStrings.commentsDeleteMessage, style: TextStyle(color: _textSecondary, height: 1.45))),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            style: FilledButton.styleFrom(backgroundColor: _danger, foregroundColor: Colors.white),
            child: Text(appStrings.delete),
          ),
        ],
      ),
    );
    if (confirmed == true) await _run(() => _controller.client.deleteComment(_nodeId, comment.id));
  }

  void _startEdit(NodeComment comment) {
    _editInput.text = comment.body;
    setState(() => _editing = comment.id);
  }

  Widget _composer() {
    final ready = _input.text.trim().isNotEmpty && !_busy;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.end,
      children: <Widget>[
        Expanded(
          child: TextField(
            controller: _input,
            minLines: 1,
            maxLines: 5,
            maxLength: _commentMaxLength,
            textInputAction: TextInputAction.newline,
            keyboardType: TextInputType.multiline,
            decoration: InputDecoration(hintText: appStrings.commentsHint, counterText: ''),
          ),
        ),
        const SizedBox(width: 8),
        IconButton.filled(
          tooltip: appStrings.commentsPost,
          onPressed: ready ? () => unawaited(_post()) : null,
          icon: const Icon(Icons.send_rounded, size: 18),
        ),
      ],
    );
  }

  Widget _commentRow(NodeComment comment, String locale) {
    final editing = _editing == comment.id;
    final when = formatDate(comment.createdAt, locale);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Text.rich(
                  TextSpan(
                    children: <InlineSpan>[
                      TextSpan(text: comment.author, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
                      TextSpan(
                        text: '  $when${comment.editedAt == null ? '' : ' · ${appStrings.commentsEdited}'}',
                        style: TextStyle(color: _textMuted),
                      ),
                    ],
                  ),
                  style: const TextStyle(fontSize: 12.5),
                ),
              ),
              if (!editing && (comment.mine || comment.canDelete))
                PopupMenuButton<String>(
                  tooltip: '',
                  icon: Icon(Icons.more_horiz_rounded, size: 18, color: _textMuted),
                  onSelected: (choice) => choice == 'edit' ? _startEdit(comment) : unawaited(_delete(comment)),
                  itemBuilder: (_) => <PopupMenuEntry<String>>[
                    if (comment.mine) PopupMenuItem<String>(value: 'edit', child: Text(appStrings.edit)),
                    if (comment.canDelete) PopupMenuItem<String>(value: 'delete', child: Text(appStrings.delete)),
                  ],
                ),
            ],
          ),
          const SizedBox(height: 2),
          if (editing) ...<Widget>[
            TextField(controller: _editInput, minLines: 1, maxLines: 6, maxLength: _commentMaxLength, decoration: const InputDecoration(counterText: '')),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: <Widget>[
                FilledButton(onPressed: _busy ? null : () => unawaited(_saveEdit(comment)), child: Text(appStrings.save)),
                TextButton(onPressed: () => setState(() => _editing = null), child: Text(appStrings.cancel)),
              ],
            ),
          ] else
            // Plain text only: the server never sends markup and the app never interprets any.
            SelectableText(comment.body, style: TextStyle(color: _textPrimary, fontSize: 13.5, height: 1.4)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final locale = currentAppLanguage.code;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _composer(),
        const SizedBox(height: 12),
        if (_error != null) ...<Widget>[_InlineError(message: _error!), const SizedBox(height: 8)],
        if (!_loaded && _error == null) const _LoadingPlaceholder(),
        if (_loaded && _comments.isEmpty)
          _EmptyState(icon: Icons.chat_bubble_outline_rounded, title: appStrings.commentsEmpty, subtitle: appStrings.commentsEmptySubtitle),
        for (final comment in _comments) _commentRow(comment, locale),
        if (_next != null) Align(alignment: Alignment.centerLeft, child: TextButton(onPressed: () => unawaited(_more()), child: Text(appStrings.commentsLoadMore))),
      ],
    );
  }
}
