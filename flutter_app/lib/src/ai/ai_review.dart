import 'dart:async';

import 'package:flutter/foundation.dart';

import '../api/backend_client.dart';
import '../api/models.dart';

/// What the app keeps about AI for the signed-in account: whether it runs
/// at all ([enabled]: a model is there and the person opted in), the review
/// counts for the sidebar badge, the pending suggestions by file for the
/// chips in the file list, and summaries already fetched.
///
/// The counts arrive with the socket's `ai:suggestions` on the web
/// ([applyCounts]) and by polling on phones ([poll]); a change reloads the
/// pending list.
class AiReviewState extends ChangeNotifier {
  AiReviewState(this._api, {this.followInterval = const Duration(seconds: 2), this.followLimit = 90});

  final AiApi _api;

  /// How often a requested batch is asked about, and how many times at most.
  final Duration followInterval;
  final int followLimit;

  AiStatus? _status;
  AiSuggestionCounts _counts = const AiSuggestionCounts();
  Map<String, List<AiSuggestion>> _byNode = const <String, List<AiSuggestion>>{};
  final Map<String, AiSummary> _summaries = <String, AiSummary>{};
  final Set<String> _excludedFolders = <String>{};
  Timer? _reload;

  /// Bumped on sign-out, so answers for the previous account are dropped.
  int _generation = 0;
  bool _disposed = false;

  AiStatus? get status => _status;

  /// AI can run for this account: the admin turned it on, a model is there
  /// and the person opted in. Everything AI in the file pages hides
  /// otherwise.
  bool get enabled {
    final status = _status;
    return status != null && status.available && status.optedIn;
  }

  AiSuggestionCounts get counts => _counts;

  /// The pending (high and medium) suggestions for one file.
  List<AiSuggestion> pendingFor(String nodeId) => _byNode[nodeId] ?? const <AiSuggestion>[];

  /// Asks again whether AI runs, then for the suggestions. A server without
  /// AI, or any failure, leaves it off.
  Future<void> refresh() async {
    final generation = _generation;
    AiStatus? status;
    try {
      status = await _api.status();
    } catch (_) {
      status = null;
    }
    if (generation != _generation || _disposed) return;
    _status = status;
    if (!enabled) {
      _counts = const AiSuggestionCounts();
      _byNode = const <String, List<AiSuggestion>>{};
      notifyListeners();
      return;
    }
    notifyListeners();
    await refreshSuggestions();
  }

  /// The counts and the pending suggestions behind the file chips.
  Future<void> refreshSuggestions() async {
    if (!enabled) return;
    final generation = _generation;
    try {
      final page = await _api.suggestions(limit: 200);
      if (generation != _generation || _disposed) return;
      final byNode = <String, List<AiSuggestion>>{};
      for (final suggestion in page.items) {
        (byNode[suggestion.nodeId] ??= <AiSuggestion>[]).add(suggestion);
      }
      _counts = page.counts;
      _byNode = byNode;
      notifyListeners();
    } catch (_) {
      // Kept as it was until the next change.
    }
  }

  /// New counts from the socket or a poll; when they moved, the pending
  /// list is fetched again (once, shortly after).
  void applyCounts(AiSuggestionCounts counts) {
    if (!enabled || counts == _counts) return;
    _counts = counts;
    notifyListeners();
    _reload?.cancel();
    _reload = Timer(const Duration(milliseconds: 400), () => unawaited(refreshSuggestions()));
  }

  /// Phones have no socket: the counts are asked for with each poll.
  Future<void> poll() async {
    if (!enabled) return;
    final generation = _generation;
    try {
      final counts = await _api.suggestionCounts();
      if (generation == _generation && !_disposed) applyCounts(counts);
    } catch (_) {
      // The next poll tries again.
    }
  }

  /// A suggestion was decided in this app: its chip goes at once.
  void forget(String suggestionId) {
    var changed = false;
    final next = <String, List<AiSuggestion>>{};
    for (final entry in _byNode.entries) {
      final kept = entry.value.where((suggestion) => suggestion.id != suggestionId).toList();
      if (kept.length != entry.value.length) changed = true;
      if (kept.isNotEmpty) next[entry.key] = kept;
    }
    if (!changed) return;
    _byNode = next;
    notifyListeners();
  }

  /// Follows a requested batch until its jobs are done, then shows what
  /// they suggested. Resolves to the last state seen (null if unknown).
  Future<AiJobBatch?> follow(String batchId, {void Function(AiJobStatus status)? onProgress}) async {
    final generation = _generation;
    AiJobBatch? last;
    for (var attempt = 0; attempt < followLimit; attempt++) {
      if (attempt > 0) await Future<void>.delayed(followInterval);
      if (generation != _generation || _disposed) return last;
      try {
        final status = await _api.jobStatus(batchId: batchId);
        onProgress?.call(status);
        last = status.batches.where((batch) => batch.id == batchId).firstOrNull ?? status.batches.firstOrNull;
        if (last != null && last.finished) break;
        // Jobs that wait for the budget or the switches won't finish now.
        if (status.paused != null) break;
      } catch (_) {
        break;
      }
    }
    if (generation == _generation && !_disposed) await refreshSuggestions();
    return last;
  }

  /// Remembers which of [nodes] are folders excluded from AI. The flag sits
  /// on the folder itself and covers everything below it, so a folder
  /// opened inside one is known to be excluded too ([excludesAny]).
  void noteFolders(Iterable<FileNode> nodes) {
    for (final node in nodes.where((node) => node.isFolder)) {
      if (node.aiExcluded) {
        _excludedFolders.add(node.id);
      } else {
        _excludedFolders.remove(node.id);
      }
    }
  }

  /// Whether any of [folderIds] (a breadcrumb path) is excluded from AI.
  bool excludesAny(Iterable<String> folderIds) => folderIds.any(_excludedFolders.contains);

  /// A summary fetched for [node]'s current content, if any.
  AiSummary? summaryFor(FileNode node) => _summaries['${node.id}:${node.etag}'];

  void keepSummary(FileNode node, AiSummary summary) => _summaries['${node.id}:${node.etag}'] = summary;

  /// Signed out: forgets everything.
  void clear() {
    _generation += 1;
    _reload?.cancel();
    _status = null;
    _counts = const AiSuggestionCounts();
    _byNode = const <String, List<AiSuggestion>>{};
    _summaries.clear();
    _excludedFolders.clear();
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _reload?.cancel();
    super.dispose();
  }
}
