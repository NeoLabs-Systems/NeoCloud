part of 'main.dart';

// Files › Review: the AI suggestions waiting for a decision, one tab per
// kind (names, folders, tags). High-confidence ones can be accepted in one
// go, low-confidence ones stay hidden until asked for, and rule proposals
// ("Always move these here?") lead the page. Each AI tab can also be
// accepted or rejected as a whole from the ⋯ menu. The Duplicates tab
// (main_ai_duplicates.dart) needs no AI and is the whole page while AI is
// off. On phones it is the same page in the phone frame; editing opens
// sheets.

/// The kinds of suggestion, by their API name, with their tab label.
List<(String, String)> get _aiReviewTabs => <(String, String)>[
  ('rename', appStrings.aiReviewTabNames),
  ('move', appStrings.aiReviewTabFolders),
  ('tags', appStrings.aiReviewTabTags),
];

class AiReviewPage extends StatefulWidget {
  const AiReviewPage({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AiReviewPage> createState() => _AiReviewPageState();
}

class _AiReviewPageState extends State<AiReviewPage> with _ReviewDuplicates {
  static const int _pageSize = 50;

  String _kind = 'rename';
  bool _showLow = false;
  List<AiSuggestion>? _items;
  int? _nextOffset;
  List<AiRuleProposal> _proposals = const <AiRuleProposal>[];
  String? _error;

  /// A short word about the last decision (a suggestion that went stale).
  String? _note;
  final Set<String> _busy = <String>{};
  bool _acceptingHigh = false;
  bool _bulkBusy = false;
  AiSuggestionCounts? _seenCounts;
  int _generation = 0;

  @override
  NeoCloudController get _controller => widget.controller;
  AiReviewState get _review => _controller.aiReview;

  @override
  void initState() {
    super.initState();
    _seenCounts = _review.counts;
    if (!_review.enabled) _kind = _duplicatesTab;
    _review.addListener(_onReviewChanged);
    unawaited(_load());
    unawaited(_loadDuplicates());
  }

  @override
  void dispose() {
    _disposeDuplicates();
    _review.removeListener(_onReviewChanged);
    super.dispose();
  }

  /// Counts moved (another tab, a finished job): read the list again,
  /// unless a decision of this page is still under way.
  void _onReviewChanged() {
    final counts = _review.counts;
    if (counts == _seenCounts) return;
    _seenCounts = counts;
    if (_onDuplicates) {
      // AI switched on or off: the page shows other tabs.
      setState(() {});
      return;
    }
    if (_busy.isEmpty && !_acceptingHigh && !_bulkBusy) unawaited(_load(quiet: true));
  }

  bool get _onDuplicates => _kind == _duplicatesTab || !_review.enabled;

  Future<void> _load({bool quiet = false}) async {
    if (_onDuplicates) return _loadDuplicates(quiet: quiet);
    final generation = ++_generation;
    if (!quiet) {
      setState(() {
        _items = null;
        _error = null;
      });
    }
    try {
      final page = await _controller.ai.suggestions(kind: _kind, tier: _showLow ? 'all' : null, limit: _pageSize);
      List<AiRuleProposal> proposals;
      try {
        proposals = await _controller.ai.ruleProposals();
      } catch (_) {
        proposals = _proposals;
      }
      if (!mounted || generation != _generation) return;
      setState(() {
        _items = page.items;
        _nextOffset = page.nextOffset;
        _proposals = proposals;
        _error = null;
      });
      _seenCounts = page.counts;
      _review.applyCounts(page.counts);
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = _aiFriendlyError(_controller, error));
    }
  }

  Future<void> _loadMore() async {
    final offset = _nextOffset;
    if (offset == null) return;
    final generation = _generation;
    try {
      final page = await _controller.ai.suggestions(kind: _kind, tier: _showLow ? 'all' : null, limit: _pageSize, offset: offset);
      if (!mounted || generation != _generation) return;
      final known = (_items ?? const <AiSuggestion>[]).map((item) => item.id).toSet();
      setState(() {
        _items = <AiSuggestion>[...?_items, ...page.items.where((item) => !known.contains(item.id))];
        _nextOffset = page.nextOffset;
      });
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    }
  }

  void _openTab(String kind) {
    if (kind == _kind) return;
    setState(() {
      _kind = kind;
      _note = null;
    });
    unawaited(_load(quiet: kind == _duplicatesTab));
  }

  void _toggleLow() {
    setState(() => _showLow = !_showLow);
    unawaited(_load());
  }

  /// Takes the row out at once; puts it back if the server refused for a
  /// reason other than the suggestion being gone.
  Future<void> _decide(AiSuggestion suggestion, Future<_AiDecision> Function() action) async {
    setState(() {
      _busy.add(suggestion.id);
      _note = null;
    });
    final decision = await action();
    if (!mounted) return;
    setState(() {
      _busy.remove(suggestion.id);
      if (decision.ok) _items = _items?.where((item) => item.id != suggestion.id).toList();
      _note = decision.note;
    });
    if (decision.ok) {
      unawaited(_review.refreshSuggestions());
      // An accepted move may have made a rule proposal.
      if (suggestion.kind == 'move' && decision.note == null) unawaited(_refreshProposals());
    }
  }

  Future<void> _refreshProposals() async {
    try {
      final proposals = await _controller.ai.ruleProposals();
      if (mounted) setState(() => _proposals = proposals);
    } catch (_) {
      // Shown on the next load.
    }
  }

  void _accept(AiSuggestion suggestion) => unawaited(_decide(suggestion, () => _acceptAiSuggestion(context, _controller, suggestion)));

  void _reject(AiSuggestion suggestion) => unawaited(_decide(suggestion, () => _rejectAiSuggestion(_controller, suggestion)));

  Future<void> _edit(AiSuggestion suggestion) async {
    final edited = await _editAiSuggestion(context, _controller, suggestion);
    if (edited == null || !mounted) return;
    await _decide(suggestion, () => _acceptAiSuggestion(context, _controller, suggestion, value: edited.$1, valueLabel: edited.$2));
  }

  /// Every pending high-confidence suggestion, after a confirmation. Undo
  /// puts back all of them that are still as AI left them.
  Future<void> _acceptAllHigh() async {
    final count = _review.counts.high;
    if (count == 0 || _acceptingHigh) return;
    final confirmed = await showAdaptiveSheet<bool>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(appStrings.aiAcceptHighTitle(count)),
        content: SizedBox(width: 420, child: Text(appStrings.aiAcceptHighBody, style: TextStyle(color: _textSecondary, height: 1.45))),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.aiAccept)),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    setState(() {
      _acceptingHigh = true;
      _note = null;
    });
    try {
      final ids = await _pendingHighIds();
      final result = await _controller.ai.acceptHigh();
      final accepted = ids.where((id) => !result.failed.containsKey(id)).toList();
      _controller.notifyFilesChanged();
      if (result.accepted > 0) {
        _controller.undo.show(UndoEntry(
          message: appStrings.aiAcceptedMany(result.accepted),
          onUndo: accepted.isEmpty ? null : () => _undoAiSuggestions(_controller, messenger, accepted),
        ));
      }
      if (mounted && result.failed.isNotEmpty) setState(() => _note = appStrings.aiAcceptHighFailed(result.failed.length));
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    } finally {
      if (mounted) setState(() => _acceptingHigh = false);
    }
    await _review.refreshSuggestions();
    if (mounted) await _load(quiet: true);
  }

  /// The ids accept-high will take, so Undo can name them.
  Future<List<String>> _pendingHighIds() async {
    final ids = <String>[];
    int? offset = 0;
    while (offset != null && ids.length < 1000) {
      final page = await _controller.ai.suggestions(tier: 'high', limit: 200, offset: offset);
      ids.addAll(page.items.map((item) => item.id));
      offset = page.nextOffset;
    }
    return ids;
  }

  /// Every suggestion the active tab shows (all pages, low-confidence
  /// ones only while they are shown).
  Future<List<String>> _tabIds() async {
    final ids = <String>[];
    int? offset = 0;
    while (offset != null && ids.length < 1000) {
      final page = await _controller.ai.suggestions(kind: _kind, tier: _showLow ? 'all' : null, limit: 200, offset: offset);
      ids.addAll(page.items.map((item) => item.id));
      offset = page.nextOffset;
    }
    return ids.take(1000).toList();
  }

  /// "Accept all" / "Reject all" on the active tab, after a confirmation
  /// with the count. Accepting offers one Undo for the whole batch.
  Future<void> _decideTab(String action) async {
    if (_bulkBusy || _acceptingHigh) return;
    final accept = action == 'accept';
    setState(() {
      _bulkBusy = true;
      _note = null;
    });
    List<String> ids;
    try {
      ids = await _tabIds();
    } catch (error) {
      if (mounted) {
        setState(() {
          _bulkBusy = false;
          _error = _aiFriendlyError(_controller, error);
        });
      }
      return;
    }
    if (!mounted) return;
    if (ids.isEmpty) {
      setState(() => _bulkBusy = false);
      return;
    }
    final confirmed = await showAdaptiveSheet<bool>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(accept ? appStrings.aiAcceptAllTitle(ids.length) : appStrings.aiRejectAllTitle(ids.length)),
        content: SizedBox(
          width: 420,
          child: Text(accept ? appStrings.aiAcceptAllBody : appStrings.aiRejectAllBody, style: TextStyle(color: _textSecondary, height: 1.45)),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(accept ? appStrings.aiAccept : appStrings.aiReject)),
        ],
      ),
    );
    if (confirmed != true || !mounted) {
      if (mounted) setState(() => _bulkBusy = false);
      return;
    }
    final messenger = ScaffoldMessenger.maybeOf(context);
    try {
      final result = await _controller.ai.bulkDecide(ids, action);
      final decided = ids.where((id) => !result.failed.containsKey(id)).toList();
      for (final id in decided) {
        _controller.aiReview.forget(id);
      }
      if (accept) {
        _controller.notifyFilesChanged();
        if (result.done > 0) {
          _controller.undo.show(UndoEntry(
            message: appStrings.aiAcceptedMany(result.done),
            onUndo: decided.isEmpty ? null : () => _undoAiSuggestions(_controller, messenger, decided),
          ));
        }
      } else if (result.done > 0) {
        messenger?.showSnackBar(SnackBar(content: Text(appStrings.aiRejectedMany(result.done))));
      }
      if (mounted && result.failed.isNotEmpty) setState(() => _note = appStrings.aiAcceptHighFailed(result.failed.length));
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    } finally {
      if (mounted) setState(() => _bulkBusy = false);
    }
    await _review.refreshSuggestions();
    if (accept && _kind == 'move') unawaited(_refreshProposals());
    if (mounted) await _load(quiet: true);
  }

  Future<void> _createRule(AiRuleProposal proposal) async {
    try {
      await _controller.ai.acceptProposal(proposal.id);
      if (!mounted) return;
      setState(() => _proposals = _proposals.where((item) => item.id != proposal.id).toList());
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.aiRuleCreated)));
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    }
  }

  Future<void> _dismissProposal(AiRuleProposal proposal) async {
    setState(() => _proposals = _proposals.where((item) => item.id != proposal.id).toList());
    try {
      await _controller.ai.dismissProposal(proposal.id);
    } catch (_) {
      // It comes back on the next load.
    }
  }

  /// The gold "Accept all high-confidence" and, beside it, the ⋯ menu with
  /// Accept all / Reject all for the active tab.
  Widget? _aiActions(AiSuggestionCounts counts) {
    final hasItems = (_items ?? const <AiSuggestion>[]).isNotEmpty;
    if (counts.high == 0 && !hasItems) return null;
    final busy = _acceptingHigh || _bulkBusy;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        if (counts.high > 0)
          FilledButton.icon(
            onPressed: busy ? null : () => unawaited(_acceptAllHigh()),
            icon: _acceptingHigh
                ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent))
                : const Icon(Icons.done_all_rounded, size: 18),
            label: Text(appStrings.aiAcceptHigh(counts.high)),
          ),
        if (hasItems) ...<Widget>[
          const SizedBox(width: 6),
          PopupMenuButton<String>(
            enabled: !busy,
            tooltip: appStrings.aiMoreActions,
            icon: _bulkBusy
                ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _textMuted))
                : Icon(Icons.more_horiz_rounded, color: _textSecondary),
            onSelected: (action) => unawaited(_decideTab(action)),
            itemBuilder: (context) => <PopupMenuEntry<String>>[
              PopupMenuItem<String>(value: 'accept', child: Text(appStrings.aiAcceptAllTab)),
              PopupMenuItem<String>(value: 'reject', child: Text(appStrings.aiRejectAllTab)),
            ],
          ),
        ],
      ],
    );
  }

  Widget _tabs(AiSuggestionCounts counts, {required bool aiOn}) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        if (aiOn)
          for (final (kind, label) in _aiReviewTabs)
            _AiReviewTab(label: label, count: counts.ofKind(kind), active: kind == _kind, onTap: () => _openTab(kind)),
        _AiReviewTab(
          label: appStrings.duplicatesTab,
          count: _duplicateGroupCount,
          active: _onDuplicates,
          onTap: () => _openTab(_duplicatesTab),
        ),
        if (aiOn && !_onDuplicates && (counts.hiddenLow > 0 || _showLow))
          TextButton.icon(
            onPressed: _toggleLow,
            icon: Icon(_showLow ? Icons.visibility_off_outlined : Icons.visibility_outlined, size: 17),
            label: Text(_showLow ? appStrings.aiHideLow : appStrings.aiShowLow(counts.hiddenLow)),
          ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final aiOn = _review.enabled;
    final counts = _review.counts;
    if (_onDuplicates) {
      return _PageScaffold(
        onRefresh: _loadDuplicates,
        lazy: true,
        children: <Widget>[
          _PageTitle(title: appStrings.aiReviewTitle, subtitle: aiOn ? appStrings.aiReviewSubtitle : appStrings.duplicatesSubtitle),
          _tabs(counts, aiOn: aiOn),
          ..._duplicateChildren(),
        ],
      );
    }
    final items = _items;
    return _PageScaffold(
      onRefresh: _load,
      children: <Widget>[
        _PageTitle(
          title: appStrings.aiReviewTitle,
          subtitle: appStrings.aiReviewSubtitle,
          trailing: _aiActions(counts),
        ),
        for (final proposal in _proposals)
          _AiRuleProposalCard(
            proposal: proposal,
            onCreate: () => unawaited(_createRule(proposal)),
            onDismiss: () => unawaited(_dismissProposal(proposal)),
          ),
        _tabs(counts, aiOn: aiOn),
        if (_note != null) _Notice(message: _note!, color: _textSecondary, icon: Icons.info_outline_rounded, onDismiss: () => setState(() => _note = null)),
        if (_error != null)
          _LoadError(
            message: _error!,
            onRetry: () {
              setState(() => _error = null);
              unawaited(_load());
            },
          ),
        if (items == null && _error == null) const _LoadingPlaceholder(),
        if (items != null && items.isEmpty)
          _EmptyCard(
            icon: Icons.auto_awesome_outlined,
            title: appStrings.aiReviewEmptyTitle,
            subtitle: appStrings.aiReviewEmptyBody,
            action: !_showLow && counts.hiddenLow > 0
                ? OutlinedButton(onPressed: _toggleLow, child: Text(appStrings.aiShowLow(counts.hiddenLow)))
                : null,
          ),
        if (items != null && items.isNotEmpty)
          _EntranceMotion(
            child: _SettingsCardList(
              children: <Widget>[
                for (final suggestion in items)
                  _AiSuggestionTile(
                    key: ValueKey<String>(suggestion.id),
                    suggestion: suggestion,
                    busy: _busy.contains(suggestion.id),
                    onAccept: () => _accept(suggestion),
                    onEdit: () => unawaited(_edit(suggestion)),
                    onReject: () => _reject(suggestion),
                  ),
              ],
            ),
          ),
        if (_nextOffset != null)
          Center(child: OutlinedButton(onPressed: () => unawaited(_loadMore()), child: Text(appStrings.aiLoadMore))),
      ],
    );
  }
}

/// A tab of Review: NeoRouter's pill chip with the count beside the label.
class _AiReviewTab extends StatelessWidget {
  const _AiReviewTab({required this.label, required this.count, required this.active, required this.onTap});

  final String label;
  final int count;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(AppRadius.pill);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
          decoration: BoxDecoration(
            borderRadius: radius,
            color: active ? _bgTertiary : Colors.transparent,
            border: Border.all(color: active ? _borderLight : _border),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Text(label, style: GoogleFonts.geist(fontSize: 13, fontWeight: FontWeight.w600, color: active ? _textPrimary : _textSecondary)),
              if (count > 0) ...<Widget>[
                const SizedBox(width: 7),
                Text('$count', style: GoogleFonts.geist(fontSize: 12, fontWeight: FontWeight.w600, color: _textMuted)),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// "Always move Telekom invoices to Finance / Telekom?" with Create rule
/// and Dismiss.
class _AiRuleProposalCard extends StatelessWidget {
  const _AiRuleProposalCard({required this.proposal, required this.onCreate, required this.onDismiss});

  final AiRuleProposal proposal;
  final VoidCallback onCreate;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(
          appStrings.aiRuleProposalTitle(_aiDocTypeLabel(proposal.type), proposal.issuer, _aiFolderLabel(proposal.targetPath)),
          style: TextStyle(color: _textPrimary, fontSize: 14.5, fontWeight: FontWeight.w600, height: 1.35),
        ),
        const SizedBox(height: 4),
        Text(appStrings.aiRuleProposalBody(proposal.accepts), style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4)),
      ],
    );
    final actions = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        TextButton(onPressed: onDismiss, child: Text(appStrings.aiDismiss)),
        const SizedBox(width: 8),
        FilledButton(onPressed: onCreate, child: Text(appStrings.aiCreateRule)),
      ],
    );
    return _AppCard(
      padding: const EdgeInsets.fromLTRB(18, 16, 16, 16),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final icon = Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(10), border: Border.all(color: _border)),
            child: Icon(Icons.rule_rounded, size: 18, color: _textSecondary),
          );
          if (constraints.maxWidth < 620) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Row(crossAxisAlignment: CrossAxisAlignment.start, children: <Widget>[icon, const SizedBox(width: 12), Expanded(child: text)]),
                const SizedBox(height: 12),
                Align(alignment: Alignment.centerRight, child: actions),
              ],
            );
          }
          return Row(
            children: <Widget>[icon, const SizedBox(width: 14), Expanded(child: text), const SizedBox(width: 16), actions],
          );
        },
      ),
    );
  }
}
