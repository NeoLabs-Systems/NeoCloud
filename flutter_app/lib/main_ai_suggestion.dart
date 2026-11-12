part of 'main.dart';

// One AI suggestion as the Review page and the file list show it: what
// would change (old → new), why, how sure the model is (a dot plus a word)
// and who proposed it (AI or one of the account's rules), with Accept,
// Edit and Reject. Accepting offers Undo in the floating bar; a suggestion
// that went stale or was decided elsewhere just goes, with a short note.

/// The model's own name, without the provider (`openai::gpt-4o` → `gpt-4o`).
String _aiModelName(String? id) {
  if (id == null || id.isEmpty) return '';
  final cut = id.indexOf('::');
  return cut < 0 ? id : id.substring(cut + 2);
}

/// The document types the extraction knows, as people read them.
String _aiDocTypeLabel(String type) => switch (type) {
  'invoice' => appStrings.aiDocTypeInvoice,
  'receipt' => appStrings.aiDocTypeReceipt,
  'contract' => appStrings.aiDocTypeContract,
  'payslip' => appStrings.aiDocTypePayslip,
  'letter' => appStrings.aiDocTypeLetter,
  'statement' => appStrings.aiDocTypeStatement,
  'certificate' => appStrings.aiDocTypeCertificate,
  'manual' => appStrings.aiDocTypeManual,
  'report' => appStrings.aiDocTypeReport,
  'notes' => appStrings.aiDocTypeNotes,
  'presentation' => appStrings.aiDocTypePresentation,
  'spreadsheet' => appStrings.aiDocTypeSpreadsheet,
  'screenshot' => appStrings.aiDocTypeScreenshot,
  'photo' => appStrings.aiDocTypePhoto,
  'scan' => appStrings.aiDocTypeScan,
  'other' => appStrings.aiDocTypeOther,
  _ => type,
};

const List<String> _aiDocTypes = <String>[
  'invoice', 'receipt', 'contract', 'payslip', 'letter', 'statement', 'certificate', 'manual',
  'report', 'notes', 'presentation', 'spreadsheet', 'screenshot', 'photo', 'scan', 'other',
];

/// "/" is the root; everything else reads "Home / Finance / Telekom".
String _aiFolderLabel(String path) {
  final parts = path.split('/').where((part) => part.isNotEmpty).toList();
  return <String>[appStrings.home, ...parts].join(' / ');
}

/// What a suggestion ends in when accepted, for the Undo bar.
String _aiAcceptedMessage(AiSuggestion suggestion, Object? value, String? valueLabel) {
  switch (suggestion.kind) {
    case 'rename':
      return appStrings.aiAcceptedRename(value is String ? value : suggestion.toName);
    case 'move':
      return appStrings.aiAcceptedMove(valueLabel ?? _aiFolderLabel(suggestion.toFolder?.path ?? '/'));
    default:
      return appStrings.aiAcceptedTags(suggestion.node?.name ?? '');
  }
}

/// A suggestion that can't be decided any more: the file changed since
/// (stale), someone decided it already, or it is gone.
String? _aiGoneNote(Object error) {
  if (error is! ApiException) return null;
  if (error.code == 'AI_SUGGESTION_STALE') return appStrings.aiSuggestionStale;
  if (error.code == 'AI_SUGGESTION_DECIDED' || error.statusCode == 404) return appStrings.aiSuggestionDecided;
  return null;
}

/// How a decision went: done, gone (the row leaves with [note]) or failed
/// (the row stays, [note] says why).
class _AiDecision {
  const _AiDecision.done() : ok = true, note = null;
  const _AiDecision.gone(this.note) : ok = true;
  const _AiDecision.failed(this.note) : ok = false;

  /// The suggestion left the queue (decided or gone).
  final bool ok;
  final String? note;
}

/// Asks for what to use instead of the suggestion: another name, folder or
/// tag list. Resolves to (value, label) or null when cancelled.
Future<(Object, String)?> _editAiSuggestion(BuildContext context, NeoCloudController controller, AiSuggestion suggestion) async {
  switch (suggestion.kind) {
    case 'rename':
      final name = await _promptText(
        context,
        title: appStrings.aiEditName,
        label: appStrings.newName,
        confirmLabel: appStrings.aiAccept,
        initialValue: suggestion.toName,
        selectStem: true,
      );
      return name == null ? null : (name, name);
    case 'move':
      final target = await showFolderPicker(context, controller, title: appStrings.aiEditFolder, confirmLabel: appStrings.moveHere, hideVaults: true);
      return target == null ? null : (target.$1, target.$2);
    default:
      final text = await _promptText(
        context,
        title: appStrings.aiEditTags,
        label: appStrings.aiTagsLabel,
        confirmLabel: appStrings.aiAccept,
        initialValue: suggestion.toTags.join(', '),
      );
      if (text == null) return null;
      final tags = text.split(',').map((tag) => tag.trim()).where((tag) => tag.isNotEmpty).toSet().toList();
      return (tags, tags.join(', '));
  }
}

/// Accepts [suggestion] (with [value] in place of its proposal), refreshes
/// the files and offers Undo.
Future<_AiDecision> _acceptAiSuggestion(
  BuildContext context,
  NeoCloudController controller,
  AiSuggestion suggestion, {
  Object? value,
  String? valueLabel,
}) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    await controller.ai.acceptSuggestion(suggestion.id, value: value);
  } catch (error) {
    final gone = _aiGoneNote(error);
    if (gone != null) {
      controller.aiReview.forget(suggestion.id);
      return _AiDecision.gone(gone);
    }
    return _AiDecision.failed(_aiFriendlyError(controller, error));
  }
  controller.aiReview.forget(suggestion.id);
  controller.notifyFilesChanged();
  controller.undo.show(UndoEntry(
    message: _aiAcceptedMessage(suggestion, value, valueLabel),
    onUndo: () => _undoAiSuggestions(controller, messenger, <String>[suggestion.id]),
  ));
  return const _AiDecision.done();
}

Future<_AiDecision> _rejectAiSuggestion(NeoCloudController controller, AiSuggestion suggestion) async {
  try {
    await controller.ai.rejectSuggestion(suggestion.id);
  } catch (error) {
    final gone = _aiGoneNote(error);
    if (gone == null) return _AiDecision.failed(_aiFriendlyError(controller, error));
    controller.aiReview.forget(suggestion.id);
    return _AiDecision.gone(gone);
  }
  controller.aiReview.forget(suggestion.id);
  return const _AiDecision.done();
}

/// Puts accepted suggestions back. One the file moved on from can't be
/// undone; that is said in a snack bar.
Future<void> _undoAiSuggestions(NeoCloudController controller, ScaffoldMessengerState? messenger, List<String> ids) async {
  final failures = await runBatch(ids, (id) => controller.ai.undoSuggestion(id));
  controller.notifyFilesChanged();
  unawaited(controller.aiReview.refreshSuggestions());
  if (failures.isEmpty) return;
  final error = failures.first.error;
  final message = error is ApiException && error.code == 'AI_UNDO_CONFLICT'
      ? appStrings.aiUndoConflict(failures.length)
      : _aiFriendlyError(controller, error);
  messenger?.showSnackBar(SnackBar(content: Text(message)));
}

/// "● High" with the exact confidence in the tooltip.
class _AiTierPill extends StatelessWidget {
  const _AiTierPill({required this.suggestion});

  final AiSuggestion suggestion;

  @override
  Widget build(BuildContext context) {
    final (label, color) = switch (suggestion.tier) {
      'high' => (appStrings.aiTierHigh, _success),
      'low' => (appStrings.aiTierLow, _textMuted),
      _ => (appStrings.aiTierMedium, _warning),
    };
    return Tooltip(
      message: appStrings.aiConfidence((suggestion.confidence * 100).round()),
      child: _StatusPill(label: label, color: color),
    );
  }
}

/// Who proposed it: "AI" (with the model in the tooltip) or "Rule".
class _AiSourceTag extends StatelessWidget {
  const _AiSourceTag({required this.suggestion});

  final AiSuggestion suggestion;

  @override
  Widget build(BuildContext context) {
    final rule = suggestion.fromRule;
    final model = _aiModelName(suggestion.model);
    return Tooltip(
      message: rule ? appStrings.aiSourceRuleHint : (model.isEmpty ? appStrings.aiSourceAi : appStrings.aiSourceModel(model)),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
        decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.tag), border: Border.all(color: _border)),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(rule ? Icons.rule_rounded : Icons.auto_awesome_outlined, size: 12, color: _textSecondary),
            const SizedBox(width: 4),
            Text(
              rule ? appStrings.aiSourceRule : appStrings.aiSourceAi,
              style: TextStyle(color: _textSecondary, fontSize: 11.5, fontWeight: FontWeight.w600),
            ),
          ],
        ),
      ),
    );
  }
}

/// A tag as AI suggests it: neutral, small.
class _AiTagChip extends StatelessWidget {
  const _AiTagChip({required this.label, this.added = false});

  final String label;

  /// New compared with the file's tags.
  final bool added;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(
        color: _bgTertiary,
        borderRadius: BorderRadius.circular(AppRadius.pill),
        border: Border.all(color: added ? _borderLight : _border),
      ),
      child: Text(label, style: TextStyle(color: added ? _textPrimary : _textSecondary, fontSize: 12, fontWeight: FontWeight.w500)),
    );
  }
}

/// What would change: old name → ghost name, folder → folder, or the tags.
class _AiChange extends StatelessWidget {
  const _AiChange({required this.suggestion});

  final AiSuggestion suggestion;

  @override
  Widget build(BuildContext context) {
    final arrow = Padding(
      padding: const EdgeInsets.symmetric(horizontal: 6),
      child: Icon(Icons.arrow_forward_rounded, size: 14, color: _textMuted),
    );
    final oldStyle = TextStyle(color: _textMuted, fontSize: 13.5);
    final newStyle = TextStyle(color: _textPrimary, fontSize: 14, fontWeight: FontWeight.w600);
    switch (suggestion.kind) {
      case 'rename':
        return Wrap(
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            Text(suggestion.fromName, style: oldStyle),
            arrow,
            Text(suggestion.toName, style: newStyle),
          ],
        );
      case 'move':
        final to = suggestion.toFolder;
        return Wrap(
          crossAxisAlignment: WrapCrossAlignment.center,
          runSpacing: 4,
          children: <Widget>[
            Icon(Icons.folder_outlined, size: 15, color: _textMuted),
            const SizedBox(width: 5),
            Text(_aiFolderLabel(suggestion.fromFolder?.path ?? '/'), style: oldStyle),
            arrow,
            Icon(Icons.folder_outlined, size: 15, color: _textSecondary),
            const SizedBox(width: 5),
            Text(_aiFolderLabel(to?.path ?? '/'), style: newStyle),
            if (to != null && to.isNew) ...<Widget>[const SizedBox(width: 8), _SettingsTag(label: appStrings.aiNewFolder)],
          ],
        );
      default:
        final had = suggestion.fromTags.map((tag) => tag.toLowerCase()).toSet();
        return Wrap(
          spacing: 6,
          runSpacing: 6,
          children: <Widget>[
            for (final tag in suggestion.toTags) _AiTagChip(label: tag, added: !had.contains(tag.toLowerCase())),
          ],
        );
    }
  }
}

/// One suggestion with its decision buttons. [showFile] names the file
/// above a move or tag change (a rename names it already).
class _AiSuggestionTile extends StatelessWidget {
  const _AiSuggestionTile({
    super.key,
    required this.suggestion,
    required this.onAccept,
    required this.onEdit,
    required this.onReject,
    this.busy = false,
    this.showFile = true,
  });

  final AiSuggestion suggestion;
  final VoidCallback onAccept;
  final VoidCallback onEdit;
  final VoidCallback onReject;
  final bool busy;
  final bool showFile;

  @override
  Widget build(BuildContext context) {
    final node = suggestion.node;
    final fileName = node?.name ?? suggestion.fromName;
    final details = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        if (showFile && suggestion.kind != 'rename') ...<Widget>[
          Text(fileName, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 14, fontWeight: FontWeight.w500)),
          const SizedBox(height: 6),
        ],
        _AiChange(suggestion: suggestion),
        if (suggestion.reason.isNotEmpty) ...<Widget>[
          const SizedBox(height: 6),
          Text(suggestion.reason, style: TextStyle(color: _textSecondary, fontSize: 12.5, height: 1.4)),
        ],
        const SizedBox(height: 8),
        Wrap(
          spacing: 12,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[_AiTierPill(suggestion: suggestion), _AiSourceTag(suggestion: suggestion)],
        ),
      ],
    );
    final actions = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        TextButton(onPressed: busy ? null : onReject, child: Text(appStrings.aiReject)),
        const SizedBox(width: 4),
        OutlinedButton(onPressed: busy ? null : onEdit, child: Text(appStrings.edit)),
        const SizedBox(width: 8),
        // Gold marks a screen's one main action: in the Review list that is
        // "Accept all high-confidence", so rows use the outlined style.
        if (showFile)
          OutlinedButton(
            onPressed: busy ? null : onAccept,
            child: busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(appStrings.aiAccept),
          )
        else
          FilledButton(
            onPressed: busy ? null : onAccept,
            child: busy ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.aiAccept),
          ),
      ],
    );
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final narrow = constraints.maxWidth < 620;
          final icon = node == null ? Icon(Icons.insert_drive_file_outlined, color: _textMuted) : _FileIcon(node: node, size: 36);
          if (narrow) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[icon, const SizedBox(width: 12), Expanded(child: details)],
                ),
                const SizedBox(height: 12),
                Align(alignment: Alignment.centerRight, child: actions),
              ],
            );
          }
          return Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[icon, const SizedBox(width: 14), Expanded(child: details), const SizedBox(width: 16), actions],
          );
        },
      ),
    );
  }
}

/// The sparkle on a file row with a pending suggestion; it opens the
/// suggestion. [compact] leaves only the icon (grid tiles).
class _AiSuggestionChip extends StatelessWidget {
  const _AiSuggestionChip({required this.onTap, this.compact = false});

  final VoidCallback onTap;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(AppRadius.pill);
    return Tooltip(
      message: appStrings.aiSuggestionChipHint,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          borderRadius: radius,
          onTap: onTap,
          child: Container(
            padding: EdgeInsets.symmetric(horizontal: compact ? 5 : 8, vertical: 2),
            decoration: BoxDecoration(color: _bgTertiary, borderRadius: radius, border: Border.all(color: _border)),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Icon(Icons.auto_awesome_rounded, size: 13, color: _info),
                if (!compact) ...<Widget>[
                  const SizedBox(width: 4),
                  Text(appStrings.aiSuggestionChip, style: TextStyle(color: _textSecondary, fontSize: 11.5, fontWeight: FontWeight.w600)),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Opens [node]'s pending suggestions from its chip: the ghost name (or
/// folder, or tags) with Accept, Edit and Reject; a sheet on phones.
Future<void> showAiSuggestionsFor(BuildContext context, NeoCloudController controller, FileNode node) {
  return showAdaptiveSheet<void>(
    context: context,
    builder: (_) => AiFileSuggestions(controller: controller, node: node),
  );
}

/// The suggestions for one file, decided right there.
class AiFileSuggestions extends StatefulWidget {
  const AiFileSuggestions({super.key, required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  State<AiFileSuggestions> createState() => _AiFileSuggestionsState();
}

class _AiFileSuggestionsState extends State<AiFileSuggestions> {
  late List<AiSuggestion> _items = widget.controller.aiReview.pendingFor(widget.node.id);
  String? _busy;
  String? _note;

  Future<void> _decide(AiSuggestion suggestion, Future<_AiDecision> Function() action) async {
    setState(() {
      _busy = suggestion.id;
      _note = null;
    });
    final decision = await action();
    if (!mounted) return;
    setState(() {
      _busy = null;
      _note = decision.note;
      if (decision.ok) _items = _items.where((item) => item.id != suggestion.id).toList();
    });
    if (_items.isEmpty && decision.note == null) Navigator.of(context).pop();
  }

  Future<void> _edit(AiSuggestion suggestion) async {
    final edited = await _editAiSuggestion(context, widget.controller, suggestion);
    if (edited == null || !mounted) return;
    await _decide(
      suggestion,
      () => _acceptAiSuggestion(context, widget.controller, suggestion, value: edited.$1, valueLabel: edited.$2),
    );
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    // The file as the suggestion knows it, so the ghost reads against the
    // name it would replace.
    final items = <AiSuggestion>[
      for (final item in _items)
        item.node == null
            ? AiSuggestion(
                id: item.id,
                nodeId: item.nodeId,
                kind: item.kind,
                from: item.from,
                to: item.to,
                reason: item.reason,
                confidence: item.confidence,
                tier: item.tier,
                source: item.source,
                model: item.model,
                node: widget.node,
              )
            : item,
    ];
    return _AdaptiveDialog(
      title: Row(
        children: <Widget>[
          Icon(Icons.auto_awesome_rounded, size: 20, color: _info),
          const SizedBox(width: 10),
          Expanded(child: Text(appStrings.aiSuggestionTitle)),
        ],
      ),
      content: SizedBox(
        width: 560,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Row(
              children: <Widget>[
                _FileIcon(node: widget.node, size: 30),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(widget.node.name, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
                ),
              ],
            ),
            const SizedBox(height: 12),
            if (_note != null) ...<Widget>[_InlineNote(message: _note!), const SizedBox(height: 12)],
            if (items.isEmpty)
              _EmptyText(appStrings.aiReviewEmptyTitle)
            else
              _SettingsCardList(
                children: <Widget>[
                  for (final suggestion in items)
                    _AiSuggestionTile(
                      suggestion: suggestion,
                      showFile: false,
                      busy: _busy == suggestion.id,
                      onAccept: () => unawaited(_decide(suggestion, () => _acceptAiSuggestion(context, controller, suggestion))),
                      onEdit: () => unawaited(_edit(suggestion)),
                      onReject: () => unawaited(_decide(suggestion, () => _rejectAiSuggestion(controller, suggestion))),
                    ),
                ],
              ),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () {
            Navigator.of(context).pop();
            controller.setSelectedSection(AppSection.review);
          },
          child: Text(appStrings.aiOpenReview),
        ),
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.close)),
      ],
    );
  }
}
