part of 'main.dart';

// Folder ⋯ › Organize this folder…: a backfill over every file below the
// folder. Pick what to suggest (names, folders, tags), start, watch it go
// (GET /api/ai/jobs every two seconds) and open Review when it is done.
// Closing the sheet early leaves the work running on the server.

/// What a backfill can suggest, by its job kind, with its switch and label.
List<(String, String)> get _aiOrganizeKinds => <(String, String)>[
  ('names', appStrings.aiOrganizeNames),
  ('folders', appStrings.aiOrganizeFolders),
  ('tags', appStrings.aiOrganizeTags),
];

Future<void> showAiOrganizeSheet(BuildContext context, NeoCloudController controller, FileNode folder) {
  return showAdaptiveSheet<void>(
    context: context,
    builder: (_) => AiOrganizeSheet(controller: controller, folder: folder),
  );
}

class AiOrganizeSheet extends StatefulWidget {
  const AiOrganizeSheet({super.key, required this.controller, required this.folder, this.pollInterval = const Duration(seconds: 2)});

  final NeoCloudController controller;
  final FileNode folder;
  final Duration pollInterval;

  @override
  State<AiOrganizeSheet> createState() => _AiOrganizeSheetState();
}

class _AiOrganizeSheetState extends State<AiOrganizeSheet> {
  late final Set<String> _kinds = <String>{
    for (final (kind, _) in _aiOrganizeKinds)
      if (_available(kind)) kind,
  };
  bool _starting = false;
  String? _batchId;
  AiJobBatch? _batch;
  String? _paused;
  String? _error;
  Timer? _poll;

  NeoCloudController get _controller => widget.controller;

  bool _available(String kind) => _controller.settings.aiFeatureOn(kind);

  bool get _done => _batch?.finished ?? false;

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _start() async {
    setState(() {
      _starting = true;
      _error = null;
    });
    try {
      final batchId = await _controller.ai.requestSuggestions(folderId: widget.folder.id, kinds: _kinds.toList());
      if (!mounted) return;
      setState(() => _batchId = batchId);
      await _check();
      if (!mounted || _done) return;
      _poll = Timer.periodic(widget.pollInterval, (_) => unawaited(_check()));
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  Future<void> _check() async {
    final batchId = _batchId;
    if (batchId == null) return;
    try {
      final status = await _controller.ai.jobStatus(batchId: batchId);
      if (!mounted) return;
      final batch = status.batches.where((item) => item.id == batchId).firstOrNull ?? status.batches.firstOrNull;
      setState(() {
        _batch = batch;
        _paused = status.paused;
        _error = null;
      });
      if (batch != null && batch.finished) {
        _poll?.cancel();
        await _controller.aiReview.refreshSuggestions();
        if (mounted) setState(() {});
      }
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    }
  }

  void _openReview() {
    Navigator.of(context).pop();
    _controller.setSelectedSection(AppSection.review);
  }

  Widget _choose() {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(appStrings.aiOrganizeIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
        const SizedBox(height: 12),
        for (final (kind, label) in _aiOrganizeKinds)
          CheckboxListTile(
            dense: true,
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            value: _kinds.contains(kind),
            onChanged: !_available(kind) || _starting
                ? null
                : (value) => setState(() {
                    if (value == true) {
                      _kinds.add(kind);
                    } else {
                      _kinds.remove(kind);
                    }
                  }),
            title: Text(label),
            subtitle: _available(kind) ? null : Text(appStrings.aiOrganizeKindOff, style: TextStyle(color: _textMuted, fontSize: 12)),
          ),
        const SizedBox(height: 8),
        _InlineNote(message: appStrings.aiOrganizeNote),
      ],
    );
  }

  Widget _progress() {
    final batch = _batch;
    final total = batch?.total ?? 0;
    final finished = batch == null ? 0 : batch.done + batch.skipped + batch.failed;
    final share = total == 0 ? null : finished / total;
    final pending = _controller.aiReview.counts.total;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            if (_done)
              Icon(Icons.check_circle_outline_rounded, color: _success, size: 20)
            else
              const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                _done ? appStrings.aiOrganizeDone : appStrings.aiOrganizeRunning,
                style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600),
              ),
            ),
          ],
        ),
        const SizedBox(height: 14),
        ClipRRect(
          borderRadius: BorderRadius.circular(AppRadius.pill),
          child: LinearProgressIndicator(value: _done ? 1 : share, minHeight: 6, backgroundColor: _bgTertiary, color: _done ? _success : _accent),
        ),
        const SizedBox(height: 10),
        Text(
          total == 0 && !_done ? appStrings.aiOrganizeQueued : appStrings.aiOrganizeProgress(finished, total),
          style: TextStyle(color: _textSecondary, fontSize: 13),
        ),
        if (batch != null && (batch.skipped > 0 || batch.failed > 0)) ...<Widget>[
          const SizedBox(height: 4),
          Text(appStrings.aiOrganizeSkipped(batch.skipped, batch.failed), style: TextStyle(color: _textMuted, fontSize: 12.5)),
        ],
        if (_paused != null && !_done) ...<Widget>[
          const SizedBox(height: 12),
          _InlineNote(message: _aiErrorMessage(_paused) ?? appStrings.aiOrganizePaused, icon: Icons.pause_circle_outline_rounded, color: _warning),
        ],
        if (_done) ...<Widget>[
          const SizedBox(height: 14),
          Text(
            pending == 0 ? appStrings.aiOrganizeNothing : appStrings.aiOrganizeWaiting(pending),
            style: TextStyle(color: _textSecondary, height: 1.45),
          ),
        ] else ...<Widget>[
          const SizedBox(height: 12),
          Text(appStrings.aiOrganizeBackground, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
        ],
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final started = _batchId != null;
    return _AdaptiveDialog(
      title: Text(appStrings.aiOrganizeTitle(widget.folder.name)),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (started) _progress() else _choose(),
            if (_error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(started ? appStrings.close : appStrings.cancel)),
        if (!started)
          FilledButton(
            onPressed: _kinds.isEmpty || _starting ? null : () => unawaited(_start()),
            child: _starting
                ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent))
                : Text(appStrings.aiOrganizeStart),
          )
        else
          FilledButton(onPressed: _openReview, child: Text(appStrings.aiOpenReview)),
      ],
    );
  }
}
