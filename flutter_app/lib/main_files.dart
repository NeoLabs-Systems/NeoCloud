part of 'main.dart';

/// The file browser: one folder at a time with breadcrumbs, list or grid,
/// a keyboard focus model, multi-select, drag & drop to move and to upload,
/// hover quick actions, a right-click menu and undo for everything
/// reversible. The pieces live in main_files_*.dart:
///
/// * this file — the folder's data: loading, paging, search;
/// * main_files_actions.dart — what can be done to items, with undo;
/// * main_files_keyboard.dart — selection, focus, keys and type-ahead;
/// * main_files_view.dart — the page itself on slivers.
class FilesPanel extends StatefulWidget {
  const FilesPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<FilesPanel> createState() => _FilesPanelState();
}

/// Something that went wrong, said with the item's name, and a way to try it again.
class _FilesError {
  const _FilesError(this.message, {this.retry});

  final String message;
  final VoidCallback? retry;
}

/// A folder page holds this many items per request; scrolling loads more.
const int _folderPageSize = 500;

/// Items cut with Ctrl/⌘+X, waiting for Ctrl/⌘+V in another folder.
List<FileNode> _cutNodes = const <FileNode>[];

abstract class _FilesBase extends State<FilesPanel> {
  final TextEditingController searchText = TextEditingController();
  final FocusNode panelFocus = FocusNode(debugLabel: 'files');
  final FocusNode searchFocus = FocusNode(debugLabel: 'files-search');
  final ScrollController scroll = ScrollController();
  final SelectionModel selection = SelectionModel();

  FolderListing? listing;
  List<FileNode> items = const <FileNode>[];
  String? nextCursor;
  bool loadingMore = false;
  List<FileNode>? searchResults;

  /// Why each result matched, by node id, with its snippet.
  Map<String, SearchHit> searchHits = const <String, SearchHit>{};
  _SearchFilters filters = const _SearchFilters();

  /// The "File contents" chip; follows the account's setting until changed.
  bool? _searchContents;
  bool get contentSearchAvailable => controller.fullTextSearch;
  bool get searchContents => contentSearchAvailable && (_searchContents ?? controller.settings.fullTextSearch);
  List<FileNode> recent = const <FileNode>[];
  _FilesError? error;
  bool loading = true;
  bool showSkeleton = false;
  bool dragging = false;
  String? _loadedFolderId;
  Timer? _searchDebounce;
  Timer? _skeletonTimer;
  int _loadGeneration = 0;

  NeoCloudController get controller => widget.controller;

  /// Browsing a folder another account shares with this one (Shared with
  /// me) rather than the account's own files.
  bool get inShare => controller.selectedSection == AppSection.shared;
  String get folderId => inShare ? controller.openSharedFolderId ?? '' : controller.openFolderId ?? 'root';
  String get currentFolderId => listing?.folder.id ?? folderId;

  /// The account's own root. A shared folder also has no parent here (the
  /// owner's folders above it stay hidden), but it is not a root.
  bool get atRoot => !inShare && listing?.folder.parentId == null;

  /// The share the open folder is part of; its first breadcrumb is the
  /// shared folder itself.
  SharedWithMe? get share => inShare ? controller.shareFor(listing?.path.firstOrNull?.id) : null;

  /// Whether items may be added, renamed, moved or deleted here. Read-only
  /// shares only browse and download; the server refuses the rest anyway.
  bool get canWrite => !inShare || (share?.permission.canWrite ?? false);

  /// Opens [id] in the same place this page is: own files or Shared with me
  /// (null: the root, or the list of shares).
  void goToFolder(String? id, {bool replace = false}) {
    if (inShare) {
      controller.openSharedFolder(id, replace: replace);
    } else {
      controller.openFolder(id, replace: replace);
    }
  }

  /// The vault the open folder belongs to, if any.
  String? get vaultId => listing?.folder.vaultId;
  bool get vaultLocked => vaultId != null && !controller.vaults.isUnlocked(vaultId!);

  bool get searching => searchResults != null;
  String get query => searchText.text.trim();

  /// The open folder's entry in [FolderViews]: its id, `root`, or `shared`
  /// for the list of shares.
  String get folderViewKey => inShare && folderId.isEmpty ? FolderViewKeys.shared : controller.folderViews.keyFor(folderId);

  /// How the open folder is sorted and shown: its own choice, else the
  /// account's defaults.
  FolderView get listingView => controller.folderViews.viewFor(folderViewKey, FolderView.defaultsOf(controller.settings));

  /// Search results keep their own choice; by default they stay in the
  /// order of relevance.
  FolderView get searchView => controller.folderViews.viewFor(
    FolderViewKeys.search,
    FolderView.defaultsOf(controller.settings).copyWith(sort: 'relevance', dir: 'desc'),
  );

  /// What the page shows now: the search's view while searching.
  FolderView get folderView => searching ? searchView : listingView;

  /// The view the items on screen were (or are being) loaded with.
  FolderView? _loadedView;

  List<FileNode>? _sortedFrom;
  FolderView? _sortedBy;
  List<FileNode> _sortedResults = const <FileNode>[];

  /// The folder's items as the server sorted them, or the search results
  /// sorted here.
  List<FileNode> get visibleItems {
    final results = searchResults;
    if (results == null) return items;
    final view = searchView;
    if (!identical(results, _sortedFrom) || view != _sortedBy) {
      _sortedFrom = results;
      _sortedBy = view;
      _sortedResults = sortedForView(results, view, (node) => node);
    }
    return _sortedResults;
  }
  List<FileNode> get selectedNodes => visibleItems.where((node) => selection.isSelected(node.id)).toList();

  String get folderName {
    final current = listing;
    if (current == null) return inShare ? '' : appStrings.home;
    if (atRoot) return appStrings.home;
    if (current.folder.vaultId != null && current.path.isNotEmpty && current.path.last.id == current.folder.id) {
      return current.path.last.name;
    }
    return current.folder.name;
  }

  @override
  void initState() {
    super.initState();
    controller.filesRevision.addListener(reload);
    controller.addListener(_onControllerChanged);
    controller.aiReview.addListener(_onAiChanged);
    controller.vaults.addListener(reload);
    controller.fileChanges.addListener(_onFilesChangedElsewhere);
    controller.folderViews.addListener(_onFolderViewsChanged);
    scroll.addListener(_onScroll);
    unawaited(load());
  }

  @override
  void dispose() {
    controller.filesRevision.removeListener(reload);
    controller.removeListener(_onControllerChanged);
    controller.aiReview.removeListener(_onAiChanged);
    controller.vaults.removeListener(reload);
    controller.folderViews.removeListener(_onFolderViewsChanged);
    controller.fileChanges
      ..removeListener(_onFilesChangedElsewhere)
      ..release(renameHold)
      ..release(dragHold);
    _searchDebounce?.cancel();
    _skeletonTimer?.cancel();
    searchText.dispose();
    panelFocus.dispose();
    searchFocus.dispose();
    scroll.dispose();
    super.dispose();
  }

  void _onControllerChanged() {
    if (folderId == _loadedFolderId) {
      // The account's defaults arrived and this folder has no choice of its own.
      if (_sortChanged) reload();
      return;
    }
    selection
      ..clear()
      ..setItems(const <String>[]);
    if (searching) clearSearch();
    unawaited(load());
  }

  void reload() => unawaited(load(quiet: true));

  /// A choice made here, or another device's arriving with the settings:
  /// the folder is loaded again only when its sort changed.
  void _onFolderViewsChanged() {
    if (!mounted) return;
    if (_sortChanged) {
      reload();
    } else {
      setState(() {});
    }
  }

  /// Whether the open folder now sorts differently from how it was loaded.
  bool get _sortChanged {
    final loaded = _loadedView;
    final view = listingView;
    return loaded != null && (loaded.sort != view.sort || loaded.dir != view.dir);
  }

  /// Suggestions came or went: the chips on the rows follow.
  void _onAiChanged() {
    if (mounted) setState(() {});
  }

  /// The open folder is excluded from AI, itself or through a folder above.
  bool get aiExcludedHere {
    final current = listing;
    if (current == null) return false;
    return current.folder.aiExcluded || controller.aiReview.excludesAny(current.path.map((segment) => segment.id));
  }

  /// Whether AI may be asked about [node]: AI runs for the account, the node
  /// is the account's own, outside vaults and excluded folders.
  bool aiAllowedFor(FileNode node) {
    if (!controller.aiReview.enabled || inShare || node.inVault || node.trashedAt != null) return false;
    if (node.isFolder && node.aiExcluded) return false;
    // Search results come from anywhere; the server checks their folders.
    return searching || !aiExcludedHere;
  }

  /// Another tab, device or client changed this folder: reload in place,
  /// keeping the scroll position and the selection.
  void _onFilesChangedElsewhere(FileChangeBatch batch) {
    if (batch.affects(currentFolderId)) reload();
  }

  /// While a rename or a drag is under way, changes from elsewhere wait, so
  /// the list doesn't move under the pointer.
  final Object renameHold = Object();
  final Object dragHold = Object();

  void holdLiveUpdates(Object token, bool busy) {
    if (busy) {
      controller.fileChanges.hold(token);
    } else {
      controller.fileChanges.release(token);
    }
  }

  /// Applies [change] to the loaded items and search results alike, for
  /// changes made before the server confirms them.
  void updateItems(List<FileNode> Function(List<FileNode> items) change) {
    setState(() {
      items = change(items);
      final results = searchResults;
      if (results != null) searchResults = change(results);
      syncSelection();
    });
  }

  void syncSelection() => selection.setItems(visibleItems.map((node) => node.id).toList());

  /// Loads the open folder. A quiet reload (after a change) keeps what is on
  /// screen and fetches as many items as were loaded, so nothing jumps.
  Future<void> load({bool quiet = false}) async {
    final target = folderId;
    final generation = ++_loadGeneration;
    _loadedFolderId = target;
    if (!quiet) {
      _skeletonTimer?.cancel();
      setState(() {
        loading = true;
        showSkeleton = false;
      });
      // A fast folder never flashes a placeholder.
      _skeletonTimer = Timer(const Duration(milliseconds: 300), () {
        if (mounted && loading) setState(() => showSkeleton = true);
      });
    }
    final limit = quiet ? items.length.clamp(_folderPageSize, 2000) : _folderPageSize;
    final view = listingView;
    _loadedView = view;
    try {
      final page = await _revealed(await controller.client.listFolder(
        target,
        sort: view.sort,
        order: view.dir,
        limit: limit,
      ));
      final latest = !inShare && page.folder.parentId == null ? await _recentFiles() : const <FileNode>[];
      if (!mounted || generation != _loadGeneration) return;
      if (!inShare && page.folder.parentId == null && controller.folderViews.rootId != page.folder.id) {
        // The root opened by its id shares the `root` entry; sorted otherwise, load again.
        controller.folderViews.rootId = page.folder.id;
        if (listingView != view) {
          unawaited(load(quiet: quiet));
          return;
        }
      }
      if (!inShare) controller.aiReview.noteFolders(<FileNode>[page.folder, ...page.items]);
      _skeletonTimer?.cancel();
      setState(() {
        listing = page;
        items = page.items;
        nextCursor = page.nextCursor;
        recent = latest;
        if (error?.retry == null) error = null;
        loading = false;
        showSkeleton = false;
        syncSelection();
      });
      WidgetsBinding.instance.addPostFrameCallback((_) => _onScroll());
    } catch (failure) {
      if (!mounted || generation != _loadGeneration) return;
      _skeletonTimer?.cancel();
      // A folder that vanished (trashed elsewhere, or no longer shared)
      // falls back to the root or the list of shares.
      if (failure is ApiException && failure.statusCode == 404 && target != 'root') {
        goToFolder(null, replace: true);
        return;
      }
      setState(() {
        error = _FilesError(controller.friendlyError(failure), retry: () => unawaited(load()));
        loading = false;
        showSkeleton = false;
      });
    }
    if (query.isNotEmpty || filters.active) await runSearch();
  }

  /// Quick Access: the files opened most recently.
  Future<List<FileNode>> _recentFiles() async {
    try {
      final files = await controller.client.recent(by: 'opened');
      return files.where((node) => !node.inVault && !node.isFolder).take(6).toList();
    } catch (_) {
      return const <FileNode>[];
    }
  }

  /// The next page, when the list nears its end.
  Future<void> loadMore() async {
    final cursor = nextCursor;
    if (cursor == null || loadingMore || searching) return;
    final target = folderId;
    final generation = _loadGeneration;
    setState(() => loadingMore = true);
    try {
      final view = _loadedView ?? listingView;
      final page = await _revealed(await controller.client.listFolder(
        target,
        sort: view.sort,
        order: view.dir,
        limit: _folderPageSize,
        cursor: cursor,
      ));
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        final known = items.map((node) => node.id).toSet();
        items = <FileNode>[...items, ...page.items.where((node) => !known.contains(node.id))];
        nextCursor = page.nextCursor;
        loadingMore = false;
        syncSelection();
      });
    } catch (failure) {
      if (!mounted) return;
      setState(() {
        loadingMore = false;
        error = _FilesError(controller.friendlyError(failure), retry: () => unawaited(loadMore()));
      });
    }
  }

  void _onScroll() {
    if (!scroll.hasClients || nextCursor == null) return;
    if (scroll.position.extentAfter < 800) unawaited(loadMore());
  }

  /// A vault folder's listing with names decrypted; a locked vault shows
  /// nothing until it is unlocked.
  Future<FolderListing> _revealed(FolderListing page) async {
    final vault = page.folder.vaultId;
    final vaults = controller.vaults;
    if (vault != null && !vaults.isUnlocked(vault)) {
      return FolderListing(folder: page.folder, path: page.path, items: const <FileNode>[]);
    }
    return FolderListing(
      folder: page.folder,
      path: vault == null ? page.path : await vaults.revealPath(page.path, vault),
      items: await vaults.reveal(page.items),
      nextCursor: page.nextCursor,
    );
  }

  // Search

  Future<void> runSearch() async {
    final text = query;
    final current = filters;
    if (text.isEmpty && !current.active) {
      setState(() {
        searchResults = null;
        syncSelection();
      });
      return;
    }
    try {
      final contents = searchContents && text.isNotEmpty;
      final hits = await controller.client.searchHits(
        text,
        content: contents,
        type: current.type,
        after: current.modifiedAfter(DateTime.now()),
        minSize: current.minSize,
        parentId: current.thisFolder ? currentFolderId : null,
        limit: 200,
      );
      final results = await controller.vaults.reveal(hits.map((hit) => hit.node).toList());
      if (!mounted || query != text || filters != current || contents != (searchContents && text.isNotEmpty)) return;
      setState(() {
        searchResults = results;
        searchHits = contents ? <String, SearchHit>{for (final hit in hits) hit.node.id: hit} : const <String, SearchHit>{};
        syncSelection();
      });
    } catch (failure) {
      if (mounted) setState(() => error = _FilesError(controller.friendlyError(failure), retry: () => unawaited(runSearch())));
    }
  }

  void onSearchChanged(String _) {
    setState(() {});
    _searchDebounce?.cancel();
    _searchDebounce = Timer(const Duration(milliseconds: 250), () => unawaited(runSearch()));
  }

  void setFilters(_SearchFilters next) {
    setState(() => filters = next);
    unawaited(runSearch());
  }

  void setSearchContents(bool value) {
    setState(() => _searchContents = value);
    unawaited(runSearch());
  }

  void clearSearch() {
    _searchDebounce?.cancel();
    searchText.clear();
    setState(() {
      filters = const _SearchFilters();
      searchResults = null;
      searchHits = const <String, SearchHit>{};
      syncSelection();
    });
  }

  void setError(_FilesError? next) => setState(() => error = next);

  /// Runs [action], showing what went wrong inline.
  Future<void> guard(Future<void> Function() action) async {
    try {
      await action();
    } catch (failure) {
      if (mounted) setError(_FilesError(controller.friendlyError(failure)));
    }
  }
}
