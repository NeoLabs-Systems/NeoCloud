part of 'main.dart';

// What AI knows about one file, for the viewer's side panel and the
// "Summarize" action: the summary (asked for on demand, kept per content
// and labelled with the model that wrote it), then the tags accepted from
// AI and the fields it read, kept quiet below.

/// Opens [node]'s summary on its own (the file's ⋯ › Summarize) and starts
/// it at once.
Future<void> showAiSummarySheet(BuildContext context, NeoCloudController controller, FileNode node) {
  return showAdaptiveSheet<void>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(appStrings.aiSummaryTitle),
      content: SizedBox(
        width: 520,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Row(
              children: <Widget>[
                _FileIcon(node: node, size: 30),
                const SizedBox(width: 10),
                Expanded(child: Text(node.name, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600))),
              ],
            ),
            const SizedBox(height: 16),
            AiSummarySection(controller: controller, node: node, autoStart: true, showTitle: false),
          ],
        ),
      ),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close))],
    ),
  );
}

/// The summary of one file: a Summarize button until there is one, then
/// the text with the model and a refresh.
class AiSummarySection extends StatefulWidget {
  const AiSummarySection({super.key, required this.controller, required this.node, this.autoStart = false, this.showTitle = true});

  final NeoCloudController controller;
  final FileNode node;

  /// Summarize right away instead of waiting for the button.
  final bool autoStart;
  final bool showTitle;

  @override
  State<AiSummarySection> createState() => _AiSummarySectionState();
}

class _AiSummarySectionState extends State<AiSummarySection> {
  AiSummary? _summary;
  bool _working = false;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _summary = _controller.aiReview.summaryFor(widget.node);
    if (widget.autoStart && _summary == null) unawaited(_summarize());
  }

  @override
  void didUpdateWidget(AiSummarySection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.node.id != widget.node.id || oldWidget.node.etag != widget.node.etag) {
      setState(() {
        _summary = _controller.aiReview.summaryFor(widget.node);
        _error = null;
      });
    }
  }

  Future<void> _summarize({bool refresh = false}) async {
    final node = widget.node;
    setState(() {
      _working = true;
      _error = null;
    });
    try {
      final summary = await _controller.ai.summarize(node.id, refresh: refresh);
      _controller.aiReview.keepSummary(node, summary);
      if (!mounted || widget.node.id != node.id) return;
      setState(() => _summary = summary);
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    } finally {
      if (mounted) setState(() => _working = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final summary = _summary;
    final model = _aiModelName(summary?.model);
    final created = summary?.createdAt;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        if (widget.showTitle) ...<Widget>[
          Row(
            children: <Widget>[
              Icon(Icons.auto_awesome_outlined, size: 15, color: _textMuted),
              const SizedBox(width: 7),
              Expanded(child: Text(appStrings.aiSummaryTitle, style: _sectionTitleStyle())),
              if (summary != null)
                IconButton(
                  tooltip: appStrings.aiSummaryRefresh,
                  visualDensity: VisualDensity.compact,
                  onPressed: _working ? null : () => unawaited(_summarize(refresh: true)),
                  icon: const Icon(Icons.refresh_rounded, size: 18),
                ),
            ],
          ),
          const SizedBox(height: 8),
        ],
        if (_working && summary == null)
          Row(
            children: <Widget>[
              const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)),
              const SizedBox(width: 10),
              Text(appStrings.aiSummaryWorking, style: TextStyle(color: _textSecondary, fontSize: 13)),
            ],
          )
        else if (summary == null)
          Align(
            alignment: Alignment.centerLeft,
            child: OutlinedButton.icon(
              onPressed: () => unawaited(_summarize()),
              icon: const Icon(Icons.auto_awesome_outlined, size: 17),
              label: Text(appStrings.aiSummarize),
            ),
          )
        else ...<Widget>[
          Opacity(
            opacity: _working ? 0.5 : 1,
            child: SelectableText(summary.summary, style: TextStyle(color: _textPrimary, fontSize: 14, height: 1.5)),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              Text(
                model.isEmpty ? appStrings.aiSummaryByAi : appStrings.aiSummaryBy(model),
                style: TextStyle(color: _textMuted, fontSize: 12),
              ),
              if (created != null)
                Text('· ${formatDate(created, currentAppLanguage.code)}', style: TextStyle(color: _textMuted, fontSize: 12)),
              if (!widget.showTitle)
                TextButton.icon(
                  onPressed: _working ? null : () => unawaited(_summarize(refresh: true)),
                  icon: const Icon(Icons.refresh_rounded, size: 16),
                  label: Text(appStrings.aiSummaryRefresh),
                ),
            ],
          ),
        ],
        if (_error != null) ...<Widget>[const SizedBox(height: 10), _InlineError(message: _error!)],
      ],
    );
  }
}

/// The viewer's AI panel: the summary, then the tags accepted from AI and
/// what AI read from the file, with the model.
class AiFileDetails extends StatelessWidget {
  const AiFileDetails({super.key, required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  Widget build(BuildContext context) {
    final meta = node.aiMeta ?? const <String, dynamic>{};
    String? field(String key) {
      final value = meta[key];
      if (value == null || '$value'.isEmpty) return null;
      return '$value';
    }

    final type = field('type');
    final amount = field('amount');
    final currency = field('currency');
    final rows = <(String, String)>[
      if (type != null) (appStrings.aiFieldType, _aiDocTypeLabel(type)),
      if (field('issuer') case final issuer?) (appStrings.aiFieldIssuer, issuer),
      if (field('date') case final date?) (appStrings.aiFieldDate, date),
      if (field('subject') case final subject?) (appStrings.aiFieldSubject, subject),
      if (amount != null) (appStrings.aiFieldAmount, currency == null ? amount : '$amount $currency'),
      if (field('caption') case final caption?) (appStrings.aiFieldCaption, caption),
    ];
    final model = _aiModelName(field('model'));
    final summaries = controller.settings.aiFeatureOn('summaries');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        if (summaries) AiSummarySection(controller: controller, node: node),
        if (node.aiTags.isNotEmpty) ...<Widget>[
          if (summaries) const SizedBox(height: 24),
          Text(appStrings.aiTagsTitle, style: _sectionTitleStyle()),
          const SizedBox(height: 8),
          Wrap(spacing: 6, runSpacing: 6, children: <Widget>[for (final tag in node.aiTags) _AiTagChip(label: tag)]),
        ],
        if (rows.isNotEmpty) ...<Widget>[
          const SizedBox(height: 24),
          Text(appStrings.aiFieldsTitle, style: _sectionTitleStyle()),
          const SizedBox(height: 8),
          for (final (label, value) in rows)
            Padding(
              padding: const EdgeInsets.only(bottom: 6),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  SizedBox(width: 92, child: Text(label, style: TextStyle(color: _textMuted, fontSize: 12.5))),
                  Expanded(child: Text(value, style: TextStyle(color: _textPrimary, fontSize: 13))),
                ],
              ),
            ),
          if (model.isNotEmpty) Text(appStrings.aiFieldsBy(model), style: TextStyle(color: _textMuted, fontSize: 12)),
        ],
      ],
    );
  }
}
