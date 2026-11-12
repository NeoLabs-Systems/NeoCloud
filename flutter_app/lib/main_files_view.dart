part of 'main.dart';

class _FilesPanelState extends _FilesBase with _FilesActions, _FilesKeyboard {
  @override
  void initState() {
    super.initState();
    _filesPaletteCommands = _paletteCommands;
    searchFocus.addListener(_onSearchFocus);
  }

  @override
  void dispose() {
    if (_filesPaletteCommands == _paletteCommands) _filesPaletteCommands = null;
    searchFocus.removeListener(_onSearchFocus);
    super.dispose();
  }

  void _onSearchFocus() => setState(() {});

  bool get _compact => MediaQuery.sizeOf(context).width < 760;

  Future<void> _setView(String view) => controller.updateSettings(<String, Object?>{'fileView': view});

  Future<void> _setSort(String field, {String? order}) async {
    final settings = controller.settings;
    final nextOrder = order ?? (settings.sortField == field && settings.sortOrder == 'asc' ? 'desc' : 'asc');
    await controller.updateSettings(<String, Object?>{'sortField': field, 'sortOrder': nextOrder});
    await load(quiet: true);
  }

  /// What the command palette offers while the Files page is open.
  List<_PaletteCommand> _paletteCommands() {
    final targets = selectedNodes.isNotEmpty ? selectedNodes : <FileNode>[?focusedNode];
    return <_PaletteCommand>[
      if (!vaultLocked && canWrite) ...<_PaletteCommand>[
        _PaletteCommand(Icons.upload_rounded, appStrings.uploadFiles, () => unawaited(pickAndUpload())),
        _PaletteCommand(Icons.create_new_folder_outlined, appStrings.newFolder, () => unawaited(newFolder())),
      ],
      _PaletteCommand(Icons.search_rounded, appStrings.paletteSearchFiles, searchFocus.requestFocus, shortcut: '/'),
      _PaletteCommand(
        gridView ? Icons.view_list_rounded : Icons.grid_view_rounded,
        gridView ? appStrings.fileViewList : appStrings.fileViewGrid,
        () => unawaited(_setView(gridView ? 'list' : 'grid')),
      ),
      if (targets.isNotEmpty && !inShare) ...<_PaletteCommand>[
        _PaletteCommand(
          targets.first.favorite ? Icons.star_rounded : Icons.star_outline_rounded,
          targets.first.favorite ? appStrings.filesUnstar : appStrings.filesStar,
          () => unawaited(toggleFavorite(targets)),
        ),
        _PaletteCommand(Icons.drive_file_move_outline, appStrings.moveTo, () => unawaited(move(targets))),
        _PaletteCommand(Icons.delete_outline_rounded, appStrings.moveToTrash, () => unawaited(trash(targets)), shortcut: 'Del'),
      ],
      if (_cutNodes.isNotEmpty && canWrite) _PaletteCommand(Icons.content_paste_rounded, appStrings.filesPasteHere(_cutNodes.length), () => unawaited(paste()), shortcut: _shortcutLabel('V')),
    ];
  }

  _ItemCallbacks get _callbacks => _ItemCallbacks(
    isSelected: selection.isSelected,
    onTap: onItemTap,
    onOpen: open,
    actionsFor: actionsFor,
    focusedId: showFocusRing ? selection.focusedId : null,
    selecting: !selection.isEmpty,
    onToggleSelected: toggleSelected,
    onLongPress: onItemLongPress,
    dragSubject: canWrite ? subjectFor : null,
    onDropInto: canWrite ? (nodes, folder) => unawaited(moveTo(nodes, folder.id, folder.name)) : null,
    onDragActive: (active) => holdLiveUpdates(dragHold, active),
    isCut: (id) => _cutNodes.any((node) => node.id == id),
    showPath: searching,
    hitFor: searchHits.isEmpty ? null : (id) => searchHits[id],
    aiPending: (id) => controller.aiReview.pendingFor(id).isNotEmpty,
    onAiSuggestion: controller.aiReview.enabled && !inShare ? (node) => unawaited(showAiSuggestionsFor(context, controller, node)) : null,
  );

  Widget _newMenu() {
    return MenuAnchor(
      alignmentOffset: const Offset(0, 6),
      menuChildren: <Widget>[
        MenuItemButton(leadingIcon: const Icon(Icons.create_new_folder_outlined, size: 18), onPressed: () => unawaited(newFolder()), child: Text(appStrings.newFolder)),
        MenuItemButton(leadingIcon: const Icon(Icons.upload_file_outlined, size: 18), onPressed: () => unawaited(pickAndUpload()), child: Text(appStrings.uploadFiles)),
        if (vaultId == null && !inShare) ...<Widget>[
          const Divider(),
          MenuItemButton(leadingIcon: const Icon(Icons.lock_outline_rounded, size: 18), onPressed: () => unawaited(newVault()), child: Text(appStrings.newVault)),
        ],
      ],
      builder: (context, menu, _) => OutlinedButton.icon(
        onPressed: () => menu.isOpen ? menu.close() : menu.open(),
        icon: const Icon(Icons.add_rounded, size: 18),
        label: Text(appStrings.newMenu),
      ),
    );
  }

  Widget _sortMenu() {
    final settings = controller.settings;
    final fields = <(String, String)>[
      ('name', appStrings.sortName),
      ('modified', appStrings.sortModified),
      ('size', appStrings.sortSize),
      ('type', appStrings.sortType),
    ];
    return MenuAnchor(
      alignmentOffset: const Offset(0, 6),
      menuChildren: <Widget>[
        for (final (field, label) in fields)
          MenuItemButton(
            leadingIcon: Icon(settings.sortField == field ? Icons.check_rounded : null, size: 18),
            onPressed: () => unawaited(_setSort(field, order: settings.sortOrder)),
            child: Text(label),
          ),
        const Divider(),
        for (final (order, label) in <(String, String)>[('asc', appStrings.sortAscending), ('desc', appStrings.sortDescending)])
          MenuItemButton(
            leadingIcon: Icon(settings.sortOrder == order ? Icons.check_rounded : null, size: 18),
            onPressed: () => unawaited(_setSort(settings.sortField, order: order)),
            child: Text(label),
          ),
      ],
      builder: (context, menu, _) => IconButton(
        tooltip: appStrings.sortBy,
        onPressed: () => menu.isOpen ? menu.close() : menu.open(),
        icon: const Icon(Icons.sort_rounded, size: 20),
      ),
    );
  }

  Widget _toolbar() {
    final search = SizedBox(
      width: _compact ? double.infinity : 280,
      child: TextField(
        controller: searchText,
        focusNode: searchFocus,
        onChanged: onSearchChanged,
        textInputAction: TextInputAction.search,
        decoration: InputDecoration(
          hintText: appStrings.searchFiles,
          prefixIcon: Icon(Icons.search, size: 18, color: _textMuted),
          suffixIcon: searchText.text.isEmpty && !filters.active
              ? null
              : IconButton(tooltip: appStrings.clear, onPressed: clearSearch, icon: Icon(Icons.close, size: 18, color: _textMuted)),
        ),
      ),
    );
    final controls = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        _sortMenu(),
        const SizedBox(width: 4),
        SegmentedButton<String>(
          segments: <ButtonSegment<String>>[
            ButtonSegment<String>(value: 'list', icon: const Icon(Icons.view_list_rounded, size: 18), tooltip: appStrings.fileViewList),
            ButtonSegment<String>(value: 'grid', icon: const Icon(Icons.grid_view_rounded, size: 18), tooltip: appStrings.fileViewGrid),
          ],
          selected: <String>{gridView ? 'grid' : 'list'},
          showSelectedIcon: false,
          onSelectionChanged: (choice) => unawaited(_setView(choice.first)),
        ),
      ],
    );
    final current = listing;
    final crumbs = current == null || searching
        ? Text(searching ? appStrings.searchResultsCount(visibleItems.length) : '', style: _sectionTitleStyle())
        : inShare
        // The list of shares stands where Home is; the shared folder follows.
        ? _Breadcrumbs(
            path: <PathSegment>[const PathSegment(id: '', name: ''), ...current.path],
            onOpen: goToFolder,
            homeLabel: appStrings.navSharedWithMe,
            homeIcon: Icons.folder_shared_outlined,
            onDropItems: canWrite ? (nodes, id, name) => unawaited(moveTo(nodes, id, name)) : null,
          )
        : _Breadcrumbs(path: current.path, onOpen: controller.openFolder, onDropItems: (nodes, id, name) => unawaited(moveTo(nodes, id, name)));
    // Search covers the account's own files only, so a shared folder has none.
    if (_compact) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          // Sort and view sit beside the path, so the list starts higher.
          Row(children: <Widget>[Expanded(child: crumbs), const SizedBox(width: 8), controls]),
          if (!inShare) ...<Widget>[const SizedBox(height: 10), search],
        ],
      );
    }
    return SizedBox(
      height: 48,
      child: Row(children: <Widget>[Expanded(child: crumbs), if (!inShare) ...<Widget>[const SizedBox(width: 12), search], const SizedBox(width: 8), controls]),
    );
  }

  Widget _selectionBar({required bool compact}) {
    final nodes = selectedNodes;
    return _SelectionBar(
      count: nodes.length,
      compact: compact,
      iconsOnly: MediaQuery.sizeOf(context).width < 1180,
      onClear: clearSelection,
      onDownload: () => download(nodes),
      onMove: inShare ? null : () => unawaited(move(nodes)),
      onTrash: canWrite ? () => unawaited(trash(nodes)) : null,
      onFavorite: inShare ? null : () => unawaited(toggleFavorite(nodes)),
      aiActions: aiSelectionActions(nodes),
    );
  }

  Widget _title() {
    return _PageTitle(
      title: folderName,
      subtitle: atRoot ? appStrings.filesSubtitle : (vaultId != null ? appStrings.vaultSubtitle : _shareSubtitle()),
      trailing: vaultLocked || !canWrite
          ? null
          : Wrap(
              spacing: 8,
              runSpacing: 8,
              children: <Widget>[
                if (vaultId != null)
                  OutlinedButton.icon(
                    onPressed: () => controller.vaults.lock(vaultId!),
                    icon: const Icon(Icons.lock_outline_rounded, size: 18),
                    label: Text(appStrings.vaultLock),
                  ),
                _newMenu(),
                FilledButton.icon(onPressed: () => unawaited(pickAndUpload()), icon: const Icon(Icons.upload_rounded, size: 18), label: Text(appStrings.upload)),
              ],
            ),
    );
  }

  /// Who shares the open folder and what this account may do in it.
  String _shareSubtitle() {
    final current = share;
    if (current == null) return '';
    return current.permission.canWrite ? appStrings.sharedByCanEdit(current.ownerName) : appStrings.sharedByReadOnly(current.ownerName);
  }

  /// The items, or what stands in for them: placeholders, the vault's
  /// unlock card, an empty folder or an empty search.
  List<Widget> _contentSlivers(double width) {
    const spacing = 12.0;
    gridColumns = math.max(1, (width / (200 + spacing)).ceil());
    final tileWidth = (width - (gridColumns - 1) * spacing) / gridColumns;
    final tileExtent = tileWidth / 0.9;
    gridRowExtent = tileExtent + spacing;
    if (loading && listing == null) {
      return <Widget>[if (showSkeleton) _skeletonSliver(grid: gridView, columns: gridColumns, tileExtent: tileExtent)];
    }
    if (vaultLocked && !searching) {
      return <Widget>[SliverToBoxAdapter(child: _VaultUnlockCard(controller: controller, vaultId: vaultId!, folderName: folderName))];
    }
    final items = visibleItems;
    if (items.isEmpty) {
      if (searching) {
        return <Widget>[
          SliverToBoxAdapter(
            child: _SearchEmpty(query: query, filters: filters, onFilters: setFilters, onOpenTrash: () => controller.setSelectedSection(AppSection.trash)),
          ),
        ];
      }
      // An empty Home shows the welcome line instead, while it is on.
      if (listing == null || (atRoot && _WelcomeBanner.visibleFor(controller))) return const <Widget>[];
      return <Widget>[
        SliverToBoxAdapter(
          child: _EmptyCard(
            icon: Icons.cloud_upload_outlined,
            title: appStrings.emptyFolderTitle,
            subtitle: appStrings.emptyFolderSubtitle,
            action: canWrite
                ? Wrap(
                    alignment: WrapAlignment.center,
                    spacing: 8,
                    runSpacing: 8,
                    children: <Widget>[
                      OutlinedButton.icon(onPressed: () => unawaited(newFolder()), icon: const Icon(Icons.create_new_folder_outlined, size: 18), label: Text(appStrings.newFolder)),
                      FilledButton.icon(onPressed: () => unawaited(pickAndUpload()), icon: const Icon(Icons.upload_rounded, size: 18), label: Text(appStrings.uploadFiles)),
                    ],
                  )
                : null,
          ),
        ),
      ];
    }
    final callbacks = _callbacks;
    if (gridView) {
      return <Widget>[
        SliverToBoxAdapter(key: itemsStartKey, child: const SizedBox.shrink()),
        SliverGrid(
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: gridColumns,
            mainAxisSpacing: spacing,
            crossAxisSpacing: spacing,
            mainAxisExtent: tileExtent,
          ),
          delegate: SliverChildBuilderDelegate(
            (context, index) => _FileTile(key: ValueKey<String>(items[index].id), node: items[index], callbacks: callbacks, controller: controller),
            childCount: items.length,
          ),
        ),
      ];
    }
    final wide = !_compact;
    final settings = controller.settings;
    return <Widget>[
      _fileListSliver(
        items: items,
        callbacks: callbacks,
        wide: wide,
        startKey: itemsStartKey,
        header: wide && !searching
            ? _ListHeader(
                sortField: settings.sortField,
                sortOrder: settings.sortOrder,
                onSort: (field) => unawaited(_setSort(field)),
                allSelected: selection.selected.length == items.length,
                someSelected: !selection.isEmpty,
                onSelectAll: () => selection.selected.length == items.length ? clearSelection() : selectAll(),
              )
            : null,
      ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final compact = _compact;
    final selecting = !selection.isEmpty;
    final showChips = searching || searchFocus.hasFocus || filters.active;
    final firstRun = atRoot && !loading && !searching && items.isEmpty && listing != null && _WelcomeBanner.visibleFor(controller);
    Widget box(Widget child, {double gap = 16}) => SliverToBoxAdapter(child: Padding(padding: EdgeInsets.only(bottom: gap), child: child));
    return PopScope(
      canPop: !selecting,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) clearSelection();
      },
      child: Focus(
        focusNode: panelFocus,
        autofocus: true,
        onKeyEvent: onKey,
        child: DropTarget(
          enable: canWrite,
          onDragEntered: (_) {
            holdLiveUpdates(dragHold, true);
            setState(() => dragging = true);
          },
          onDragExited: (_) {
            holdLiveUpdates(dragHold, false);
            setState(() => dragging = false);
          },
          onDragDone: (details) {
            holdLiveUpdates(dragHold, false);
            setState(() => dragging = false);
            if (!vaultLocked) unawaited(uploadDropped(details.files));
          },
          child: Stack(
            children: <Widget>[
              RefreshIndicator(
                onRefresh: load,
                child: LayoutBuilder(
                  builder: (context, constraints) {
                    final padding = _pagePadding(context);
                    final available = constraints.maxWidth - padding.horizontal;
                    final inner = math.min(available, kContentMaxWidth);
                    final side = (constraints.maxWidth - inner) / 2;
                    return CustomScrollView(
                      controller: scroll,
                      slivers: <Widget>[
                        SliverPadding(
                          padding: EdgeInsets.fromLTRB(side, padding.top, side, padding.bottom),
                          sliver: SliverMainAxisGroup(
                            slivers: <Widget>[
                              SliverToBoxAdapter(child: _title()),
                              if (vaultId != null && !vaultLocked && !searching) box(_VaultBanner(controller: controller, vaultId: vaultId!, folderName: folderName)),
                              if (atRoot && recent.isNotEmpty && !searching)
                                box(_QuickAccess(
                                  files: recent,
                                  controller: controller,
                                  onOpen: (node) => unawaited(showFileViewer(context, controller, recent, recent.indexOf(node))),
                                )),
                              if (firstRun)
                                box(_WelcomeBanner(controller: controller, onClose: () => setState(() {}))),
                              box(selecting && !compact ? _selectionBar(compact: false) : _toolbar(), gap: showChips ? 10 : 16),
                              if (showChips)
                                box(_SearchChips(
                                  filters: filters,
                                  onChanged: setFilters,
                                  canLimitToFolder: listing != null && !atRoot,
                                  contents: contentSearchAvailable ? searchContents : null,
                                  onContentsChanged: setSearchContents,
                                )),
                              if (error != null)
                                box(_ActionErrorNotice(error: error!, onDismiss: () => setError(null))),
                              ..._contentSlivers(inner),
                              if (loadingMore)
                                const SliverToBoxAdapter(
                                  child: Padding(padding: EdgeInsets.all(20), child: Center(child: SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)))),
                                ),
                              if (visibleItems.isNotEmpty && !compact)
                                SliverToBoxAdapter(
                                  child: Padding(
                                    padding: const EdgeInsets.only(top: 16),
                                    child: Text(appStrings.filesShortcutsHint, textAlign: TextAlign.center, style: TextStyle(color: _textMuted, fontSize: 12)),
                                  ),
                                ),
                              const SliverToBoxAdapter(child: SizedBox(height: 96)),
                            ],
                          ),
                        ),
                      ],
                    );
                  },
                ),
              ),
              if (selecting && compact) Positioned(left: 12, right: 12, bottom: 12, child: SafeArea(child: _selectionBar(compact: true))),
              if (dragging) Positioned.fill(child: _DropOverlay(folder: folderName)),
            ],
          ),
        ),
      ),
    );
  }
}

/// An inline error that names what failed, with Retry when it can be retried.
class _ActionErrorNotice extends StatelessWidget {
  const _ActionErrorNotice({required this.error, required this.onDismiss});

  final _FilesError error;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final retry = error.retry;
    return Semantics(
      liveRegion: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          _InlineError(message: error.message, onDismiss: onDismiss),
          if (retry != null) ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            OutlinedButton.icon(
              onPressed: () {
                onDismiss();
                retry();
              },
              icon: const Icon(Icons.refresh, size: 16),
              label: Text(appStrings.retry),
            ),
          ],
        ],
      ),
    );
  }
}
