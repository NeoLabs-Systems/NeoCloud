import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/models.dart';

/// The pages that are not a folder but keep their own sort and view, as
/// keys of `folder_views` (see docs/api.md); a folder's key is its id.
abstract final class FolderViewKeys {
  static const String root = 'root';
  static const String recent = 'recent';
  static const String favorites = 'favorites';
  static const String shared = 'shared';
  static const String trash = 'trash';
  static const String search = 'search';
}

/// How one folder or page is shown: what it is sorted by, which way, and
/// as a list or a grid.
@immutable
class FolderView {
  const FolderView({required this.sort, required this.dir, required this.view});

  static const Set<String> sorts = <String>{'name', 'size', 'modified', 'type', 'relevance'};

  /// The account's defaults (`sortField`, `sortOrder`, `fileView`).
  factory FolderView.defaultsOf(UserSettings settings) =>
      FolderView(sort: settings.sortField, dir: settings.sortOrder, view: settings.fileView);

  /// A stored entry; null for anything the server would refuse.
  static FolderView? fromJson(Object? json) {
    if (json is! Map) return null;
    final sort = json['sort'];
    final dir = json['dir'];
    final view = json['view'];
    if (sort is! String || !sorts.contains(sort)) return null;
    if (dir != 'asc' && dir != 'desc') return null;
    if (view != 'list' && view != 'grid') return null;
    return FolderView(sort: sort, dir: dir as String, view: view as String);
  }

  final String sort;
  final String dir;
  final String view;

  bool get grid => view == 'grid';

  Map<String, Object?> toJson() => <String, Object?>{'sort': sort, 'dir': dir, 'view': view};

  FolderView copyWith({String? sort, String? dir, String? view}) =>
      FolderView(sort: sort ?? this.sort, dir: dir ?? this.dir, view: view ?? this.view);

  @override
  bool operator ==(Object other) => other is FolderView && other.sort == sort && other.dir == dir && other.view == view;

  @override
  int get hashCode => Object.hash(sort, dir, view);

  @override
  String toString() => 'FolderView($sort $dir $view)';
}

/// Sorts a list that does not come sorted from the server (Recent,
/// Favorites, Trash, Shared with me, search results) the way a folder
/// listing is: folders first, then by [view], then by name. `relevance`
/// keeps the order the list came in; `modified` of an item in the trash is
/// when it was deleted.
List<T> sortedForView<T>(List<T> items, FolderView view, FileNode Function(T item) nodeOf) {
  if (view.sort == 'relevance') return items;
  final direction = view.dir == 'desc' ? -1 : 1;
  int byName(FileNode a, FileNode b) => a.name.toLowerCase().compareTo(b.name.toLowerCase());
  int byField(FileNode a, FileNode b) {
    switch (view.sort) {
      case 'size':
        return a.size.compareTo(b.size);
      case 'modified':
        final left = a.trashedAt ?? a.modifiedAt ?? DateTime.fromMillisecondsSinceEpoch(0);
        final right = b.trashedAt ?? b.modifiedAt ?? DateTime.fromMillisecondsSinceEpoch(0);
        return left.compareTo(right);
      case 'type':
        return (a.mime ?? '').compareTo(b.mime ?? '');
      default:
        return byName(a, b);
    }
  }

  final sorted = List<T>.of(items);
  sorted.sort((left, right) {
    final a = nodeOf(left);
    final b = nodeOf(right);
    if (a.isFolder != b.isFolder) return a.isFolder ? -1 : 1;
    final field = byField(a, b) * direction;
    return field != 0 ? field : byName(a, b);
  });
  return sorted;
}

/// Remembers sort and view per folder and page, for the signed-in account.
///
/// A choice shows at once and is kept on the device; after [debounce] the
/// changed entries go to the server (`folder_views`), which merges them, so
/// they follow the account to the web app and its phones. When the
/// account's settings load, the server's entries replace the device's,
/// except those not sent yet, which are sent again.
class FolderViews extends ChangeNotifier {
  FolderViews({
    required SharedPreferences preferences,
    required Future<void> Function(Map<String, Object?> patch) save,
    this.debounce = const Duration(milliseconds: 800),
  }) : _preferences = preferences,
       _save = save;

  final SharedPreferences _preferences;
  final Future<void> Function(Map<String, Object?> patch) _save;
  final Duration debounce;

  String? _storageKey;
  Map<String, FolderView> _views = <String, FolderView>{};

  /// Changed on this device and not yet confirmed by the server.
  final Map<String, FolderView> _pending = <String, FolderView>{};
  Timer? _timer;

  /// The id of the account's root folder, once known: opened by id (from
  /// "Show in folder") it shares the `root` entry.
  String? rootId;

  /// The entry for a folder id or a page key.
  String keyFor(String folderId) => folderId == rootId ? FolderViewKeys.root : folderId;

  /// Loads what this device remembers for [userId].
  void open(String userId) {
    _timer?.cancel();
    _storageKey = 'files.folderViews.$userId';
    _views = <String, FolderView>{};
    _pending.clear();
    rootId = null;
    final text = _preferences.getString(_storageKey!);
    if (text != null) {
      try {
        final stored = jsonDecode(text);
        if (stored is Map) {
          _views = _parse(stored['views']);
          _pending.addAll(_parse(stored['pending']));
        }
      } catch (_) {
        // A cache nobody can read starts empty; the server has the rest.
      }
    }
    if (_pending.isNotEmpty) _schedule();
    notifyListeners();
  }

  /// Signing out: unsent changes stay on the device for the next sign-in.
  void close() {
    _timer?.cancel();
    _storageKey = null;
    _views = <String, FolderView>{};
    _pending.clear();
    rootId = null;
  }

  /// What [key] remembers, or null for "the account's defaults".
  FolderView? operator [](String key) => _views[keyFor(key)];

  /// What [key] shows: its own choice, else [fallback].
  FolderView viewFor(String key, FolderView fallback) => this[key] ?? fallback;

  /// Remembers [view] for [key] and sends it after [debounce].
  void set(String key, FolderView view) {
    final entry = keyFor(key);
    if (_views[entry] == view) return;
    _views[entry] = view;
    _pending[entry] = view;
    _persist();
    _schedule();
    notifyListeners();
  }

  /// The server's entries (`settings.folder_views`) win, except for those
  /// this device has not sent yet.
  void reconcile(Map<String, Object?> server) {
    if (_storageKey == null) return;
    final next = _parse(server)..addAll(_pending);
    if (mapEquals(next, _views)) return;
    _views = next;
    _persist();
    notifyListeners();
  }

  /// Sends what changed now instead of after [debounce].
  Future<void> flush() async {
    _timer?.cancel();
    if (_pending.isEmpty || _storageKey == null) return;
    final storageKey = _storageKey;
    final batch = Map<String, FolderView>.of(_pending);
    try {
      await _save(<String, Object?>{for (final entry in batch.entries) entry.key: entry.value.toJson()});
    } catch (_) {
      // Kept as pending; sent with the next change or the next sign-in.
      return;
    }
    if (storageKey != _storageKey) return;
    // A choice made while this was on its way stays pending.
    batch.forEach((key, view) {
      if (_pending[key] == view) _pending.remove(key);
    });
    _persist();
  }

  void _schedule() {
    _timer?.cancel();
    _timer = Timer(debounce, () => unawaited(flush()));
  }

  void _persist() {
    final key = _storageKey;
    if (key == null) return;
    unawaited(_preferences.setString(key, jsonEncode(<String, Object?>{
      'views': <String, Object?>{for (final entry in _views.entries) entry.key: entry.value.toJson()},
      'pending': <String, Object?>{for (final entry in _pending.entries) entry.key: entry.value.toJson()},
    })));
  }

  static Map<String, FolderView> _parse(Object? json) {
    final views = <String, FolderView>{};
    if (json is! Map) return views;
    for (final entry in json.entries) {
      final view = FolderView.fromJson(entry.value);
      if (entry.key is String && view != null) views[entry.key as String] = view;
    }
    return views;
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }
}
