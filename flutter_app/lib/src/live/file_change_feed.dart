import 'dart:async';

/// Which folders changed elsewhere (another tab, a device, WebDAV, the sync
/// client). Pages refresh when a batch [affects] the folder they show;
/// cross-folder pages (recent, favorites, trash) refresh on any batch.
class FileChangeBatch {
  const FileChangeBatch({this.folderIds = const <String>{}, this.everything = false});

  /// Folders whose listing changed, by id.
  final Set<String> folderIds;

  /// Something changed whose folders aren't known (a reconnect, a move out
  /// of a folder): every page should refresh.
  final bool everything;

  bool affects(String folderId) => everything || folderIds.contains(folderId);
}

/// Collects change notices and hands them out at most once per [delay], as
/// one [FileChangeBatch]. While something [hold]s the feed (a rename dialog,
/// a drag), batches wait and go out when the last hold is released.
class FileChangeFeed {
  FileChangeFeed({this.delay = const Duration(milliseconds: 400)});

  final Duration delay;
  final List<void Function(FileChangeBatch batch)> _listeners = <void Function(FileChangeBatch batch)>[];
  final Set<Object> _holds = <Object>{};
  final Set<String> _pendingFolders = <String>{};
  bool _pendingEverything = false;
  Timer? _timer;

  bool get _hasPending => _pendingEverything || _pendingFolders.isNotEmpty;

  /// Whether a batch is waiting (for the delay or for a hold).
  bool get hasPending => _hasPending;

  void addListener(void Function(FileChangeBatch batch) listener) => _listeners.add(listener);

  void removeListener(void Function(FileChangeBatch batch) listener) => _listeners.remove(listener);

  void addFolders(Iterable<String> folderIds) {
    final before = _pendingFolders.length;
    _pendingFolders.addAll(folderIds.where((id) => id.isNotEmpty));
    if (_pendingFolders.length != before) _schedule();
  }

  void addEverything() {
    _pendingEverything = true;
    _schedule();
  }

  void add(FileChangeBatch batch) {
    if (batch.everything) addEverything();
    addFolders(batch.folderIds);
  }

  /// Keeps batches back until [release] is called with the same [token].
  void hold(Object token) => _holds.add(token);

  void release(Object token) {
    if (_holds.remove(token) && _holds.isEmpty && _hasPending) _schedule();
  }

  /// Fires [delay] after the first notice of a burst, so a steady stream of
  /// changes still refreshes regularly.
  void _schedule() {
    _timer ??= Timer(delay, _flush);
  }

  void _flush() {
    _timer = null;
    if (_holds.isNotEmpty || !_hasPending) return;
    final batch = FileChangeBatch(folderIds: Set<String>.unmodifiable(_pendingFolders), everything: _pendingEverything);
    _pendingFolders.clear();
    _pendingEverything = false;
    for (final listener in List<void Function(FileChangeBatch batch)>.of(_listeners)) {
      listener(batch);
    }
  }

  /// Drops what is waiting, e.g. on sign-out.
  void clear() {
    _timer?.cancel();
    _timer = null;
    _pendingFolders.clear();
    _pendingEverything = false;
    _holds.clear();
  }
}

/// The folders a page of `/api/sync/changes` touched. The journal names a
/// node's parent as it is now, so a move, trash, restore or deletion can't
/// say which folder the node left: those refresh everything.
FileChangeBatch batchFromJournal(List<dynamic> changes) {
  const unknownOrigin = <String>{'move', 'trash', 'restore', 'delete'};
  final folders = <String>{};
  var everything = false;
  for (final raw in changes) {
    if (raw is! Map) continue;
    final node = raw['node'];
    final parentId = node is Map ? node['parentId'] : null;
    if (unknownOrigin.contains(raw['op']) || parentId is! String) {
      everything = true;
    } else {
      folders.add(parentId);
    }
  }
  return FileChangeBatch(folderIds: folders, everything: everything);
}
