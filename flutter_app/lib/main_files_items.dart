part of 'main.dart';

/// What every row and tile needs to react to clicks, keys, drags and drops.
class _ItemCallbacks {
  const _ItemCallbacks({
    required this.isSelected,
    required this.onTap,
    required this.onOpen,
    required this.actionsFor,
    this.focusedId,
    this.selecting = false,
    this.onToggleSelected,
    this.onLongPress,
    this.dragSubject,
    this.onDropInto,
    this.onDragActive,
    this.isCut,
    this.showPath = false,
    this.hitFor,
    this.aiPending,
    this.onAiSuggestion,
  });

  final bool Function(String id) isSelected;
  final ValueChanged<FileNode> onTap;
  final ValueChanged<FileNode> onOpen;
  final List<_FileAction> Function(FileNode node) actionsFor;

  /// The keyboard cursor, drawn as a gold ring.
  final String? focusedId;

  /// Some items are selected: checkboxes show on every row.
  final bool selecting;
  final ValueChanged<FileNode>? onToggleSelected;
  final ValueChanged<FileNode>? onLongPress;

  /// What dragging this node moves (the selection, if it is part of it).
  final List<FileNode> Function(FileNode node)? dragSubject;

  /// Items dropped onto a folder.
  final void Function(List<FileNode> nodes, FileNode folder)? onDropInto;

  /// A drag of items started (true) or ended (false).
  final ValueChanged<bool>? onDragActive;

  /// Cut with Ctrl/⌘+X and waiting to be pasted: drawn dimmed.
  final bool Function(String id)? isCut;

  /// Search results show the folder each item is in.
  final bool showPath;

  /// Content search: why a result matched and its snippet.
  final SearchHit? Function(String id)? hitFor;

  /// Whether a file has a pending AI suggestion (the sparkle chip), and
  /// what tapping the chip does.
  final bool Function(String id)? aiPending;
  final ValueChanged<FileNode>? onAiSuggestion;

  bool hasAiSuggestion(String id) => onAiSuggestion != null && (aiPending?.call(id) ?? false);

  bool get canDrag => dragSubject != null && onDropInto != null;
}

/// The folder path a search result shows: "Home / Photos / 2026"; null
/// when the server didn't send one.
String? _displayPath(FileNode node) {
  final raw = node.path;
  if (raw == null) return null;
  final path = raw.split('/').where((segment) => segment.isNotEmpty).toList();
  return <String>[appStrings.home, ...path].join(' / ');
}

/// "today", "yesterday" or the date, for screen readers.
String _spokenDate(DateTime? date) {
  if (date == null) return '';
  final now = DateTime.now();
  final days = DateTime(now.year, now.month, now.day).difference(DateTime(date.year, date.month, date.day)).inDays;
  if (days == 0) return appStrings.filesModifiedToday;
  if (days == 1) return appStrings.filesModifiedYesterday;
  return appStrings.filesModifiedOn(DateFormat.yMMMd(currentAppLanguage.code).format(date));
}

/// What a screen reader says for a row or tile: "Report.pdf, PDF, 2 MB,
/// modified yesterday".
String _describeNode(FileNode node) {
  final parts = <String>[node.name];
  if (node.isFolder) {
    parts.add(appStrings.filesKindFolder);
  } else {
    final dot = node.name.lastIndexOf('.');
    if (dot > 0 && dot < node.name.length - 1) parts.add(node.name.substring(dot + 1).toUpperCase());
    parts.add(formatBytes(node.size));
  }
  final modified = _spokenDate(node.modifiedAt);
  if (modified.isNotEmpty) parts.add(modified);
  if (node.favorite) parts.add(appStrings.filesFavorite);
  if (node.shared) parts.add(appStrings.sharedWithPeople);
  if (node.aiExcluded) parts.add(appStrings.aiFolderExcludedTag);
  return parts.join(', ');
}

/// The gold ring of the keyboard focus, and the dimming of cut items (by
/// color, not by opacity, so nothing behind shows through).
Decoration? _itemForeground({required bool focused, required bool cut, required double radius}) {
  if (!focused && !cut) return null;
  return BoxDecoration(
    color: cut ? _bgCard.withValues(alpha: 0.55) : null,
    borderRadius: BorderRadius.circular(radius),
    border: focused ? Border.all(color: _accent, width: 2) : null,
  );
}

class _FileRow extends StatefulWidget {
  const _FileRow({super.key, required this.node, required this.callbacks, required this.wide, required this.first, required this.last});

  final FileNode node;
  final _ItemCallbacks callbacks;
  final bool wide;
  final bool first;
  final bool last;

  @override
  State<_FileRow> createState() => _FileRowState();
}

class _FileRowState extends State<_FileRow> {
  bool _hovering = false;

  @override
  Widget build(BuildContext context) {
    final node = widget.node;
    final callbacks = widget.callbacks;
    final selected = callbacks.isSelected(node.id);
    final actions = callbacks.actionsFor(node);
    final showCheckbox = callbacks.onToggleSelected != null && (_hovering || callbacks.selecting);
    final locale = currentAppLanguage.code;
    final meta = node.isFolder ? '—' : formatBytes(node.size);
    final mouse = _usesMouse;
    final focused = callbacks.focusedId == node.id;
    final radius = BorderRadius.vertical(
      top: Radius.circular(widget.first ? AppRadius.card : 0),
      bottom: Radius.circular(widget.last ? AppRadius.card : 0),
    );
    final path = callbacks.showPath ? _displayPath(node) : null;
    final date = formatDate(node.modifiedAt, locale);
    // A folder has no size worth a dash on the phone's second line.
    final secondLine = path ?? (widget.wide ? null : (node.isFolder ? date : '$meta · $date'));
    final hit = callbacks.hitFor?.call(node.id);
    final snippet = hit?.snippet;
    return _DragDrop(
      node: node,
      callbacks: callbacks,
      builder: (context, dropHover) => MouseRegion(
        onEnter: (_) => setState(() => _hovering = true),
        onExit: (_) => setState(() => _hovering = false),
        child: GestureDetector(
          onSecondaryTapDown: (details) => unawaited(_showContextMenu(context, details.globalPosition, actions)),
          child: Container(
            foregroundDecoration: _itemForeground(focused: focused, cut: callbacks.isCut?.call(node.id) ?? false, radius: 6),
            decoration: BoxDecoration(border: widget.first ? null : Border(top: BorderSide(color: _border))),
            child: Material(
              borderRadius: radius,
              color: dropHover
                  ? _accentMuted
                  : (selected ? _accentMuted : (_hovering ? _bgTertiary.withValues(alpha: 0.6) : Colors.transparent)),
              child: InkWell(
                borderRadius: radius,
                onTap: () => callbacks.onTap(node),
                onDoubleTap: mouse ? () => callbacks.onOpen(node) : null,
                onLongPress: callbacks.onLongPress == null || mouse ? null : () => callbacks.onLongPress!(node),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(10, 7, 8, 7),
                  child: Row(
                    children: <Widget>[
                      SizedBox(
                        width: 40,
                        height: 40,
                        child: showCheckbox
                            ? Checkbox(value: selected, onChanged: (_) => callbacks.onToggleSelected!(node))
                            : Center(child: _FileIcon(node: node)),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Semantics(
                          label: _describeNode(node),
                          selected: selected,
                          button: true,
                          excludeSemantics: true,
                          child: Column(
                            mainAxisAlignment: MainAxisAlignment.center,
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Row(
                                children: <Widget>[
                                  Flexible(
                                    child: Text(
                                      node.name,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 14),
                                    ),
                                  ),
                                  if (node.favorite) ...<Widget>[const SizedBox(width: 6), Icon(Icons.star_rounded, size: 15, color: _accent)],
                                  if (node.shared) ...<Widget>[const SizedBox(width: 6), const _SharedMark(size: 15)],
                                  if (callbacks.hasAiSuggestion(node.id)) ...<Widget>[
                                    const SizedBox(width: 8),
                                    _AiSuggestionChip(onTap: () => callbacks.onAiSuggestion!(node)),
                                  ],
                                  if (hit != null) ...<Widget>[const SizedBox(width: 8), _MatchBadge(match: hit.match)],
                                ],
                              ),
                              // A content match shows where the words are; its folder is in the tooltip.
                              if (snippet != null) ...<Widget>[
                                const SizedBox(height: 2),
                                Tooltip(message: secondLine ?? '', child: _SnippetText(snippet: snippet)),
                              ] else if (secondLine != null) ...<Widget>[
                                const SizedBox(height: 2),
                                Text(secondLine, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 12.5)),
                              ],
                            ],
                          ),
                        ),
                      ),
                      if (widget.wide) ...<Widget>[
                        SizedBox(width: 100, child: Text(meta, style: TextStyle(color: _textSecondary, fontSize: 13))),
                        SizedBox(width: 150, child: Text(formatDate(node.modifiedAt, locale), style: TextStyle(color: _textSecondary, fontSize: 13))),
                      ],
                      // Quick actions appear on hover, so the row stays calm.
                      SizedBox(
                        width: widget.wide ? 120 : 0,
                        child: widget.wide && _hovering && !callbacks.selecting
                            ? Row(
                                mainAxisAlignment: MainAxisAlignment.end,
                                children: <Widget>[
                                  for (final action in actions.where((a) => a.quick).take(3))
                                    IconButton(
                                      tooltip: action.label,
                                      visualDensity: VisualDensity.compact,
                                      onPressed: action.onSelected,
                                      icon: Icon(action.icon, size: 18),
                                    ),
                                ],
                              )
                            : null,
                      ),
                      _FileActionsMenu(actions: actions, title: node.name, node: node),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _FileTile extends StatelessWidget {
  const _FileTile({super.key, required this.node, required this.callbacks, required this.controller});

  final FileNode node;
  final _ItemCallbacks callbacks;
  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final selected = callbacks.isSelected(node.id);
    final actions = callbacks.actionsFor(node);
    final mouse = _usesMouse;
    return _DragDrop(
      node: node,
      callbacks: callbacks,
      builder: (context, dropHover) => GestureDetector(
        onSecondaryTapDown: (details) => unawaited(_showContextMenu(context, details.globalPosition, actions)),
        child: Container(
          foregroundDecoration: _itemForeground(
            focused: callbacks.focusedId == node.id,
            cut: callbacks.isCut?.call(node.id) ?? false,
            radius: AppRadius.card,
          ),
          child: Material(
            color: selected || dropHover ? _accentMuted : _bgCard,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(AppRadius.card),
              side: BorderSide(color: selected || dropHover ? _accent.withValues(alpha: 0.5) : _border),
            ),
            clipBehavior: Clip.antiAlias,
            child: InkWell(
              onTap: () => callbacks.onTap(node),
              onDoubleTap: mouse ? () => callbacks.onOpen(node) : null,
              onLongPress: callbacks.onLongPress == null || mouse ? null : () => callbacks.onLongPress!(node),
              child: Padding(
                padding: const EdgeInsets.fromLTRB(8, 4, 2, 12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        if (callbacks.onToggleSelected != null)
                          Checkbox(value: selected, onChanged: (_) => callbacks.onToggleSelected!(node))
                        else
                          const SizedBox(height: 40),
                        const Spacer(),
                        if (node.favorite) Icon(Icons.star_rounded, size: 16, color: _accent),
                        if (node.shared) const _SharedMark(size: 16),
                        if (callbacks.hasAiSuggestion(node.id)) ...<Widget>[
                          const SizedBox(width: 4),
                          _AiSuggestionChip(compact: true, onTap: () => callbacks.onAiSuggestion!(node)),
                        ],
                        _FileActionsMenu(actions: actions, title: node.name, node: node),
                      ],
                    ),
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.only(right: 6),
                        child: _FilePreviewTile(node: node, controller: controller),
                      ),
                    ),
                    const SizedBox(height: 10),
                    Padding(
                      padding: const EdgeInsets.only(left: 4, right: 6),
                      child: Semantics(
                        label: _describeNode(node),
                        selected: selected,
                        button: true,
                        excludeSemantics: true,
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: <Widget>[
                            Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 13)),
                            const SizedBox(height: 2),
                            if (callbacks.hitFor?.call(node.id)?.snippet case final snippet?)
                              _SnippetText(snippet: snippet, fontSize: 12)
                            else
                              Text(
                              (callbacks.showPath ? _displayPath(node) : null) ??
                                  (node.isFolder ? formatDate(node.modifiedAt, currentAppLanguage.code) : formatBytes(node.size)),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(color: _textMuted, fontSize: 12),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// An image's thumbnail, decoded at the size it is shown, or its type icon
/// for everything else (and while the thumbnail loads).
class _FilePreviewTile extends StatelessWidget {
  const _FilePreviewTile({required this.node, required this.controller});

  final FileNode node;
  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final icon = Center(child: _FileIcon(node: node, size: 56));
    if (fileKindOf(node) != FileKind.image) return icon;
    return LayoutBuilder(
      builder: (context, constraints) {
        final ratio = MediaQuery.devicePixelRatioOf(context);
        final width = constraints.maxWidth.isFinite ? (constraints.maxWidth * ratio).round() : null;
        return FutureBuilder<Uint8List?>(
          future: controller.thumbnailFor(node),
          builder: (context, snapshot) {
            final bytes = snapshot.data;
            if (bytes == null) return icon;
            return ClipRRect(
              borderRadius: BorderRadius.circular(AppRadius.control),
              child: SizedBox.expand(
                child: Image.memory(bytes, fit: BoxFit.cover, cacheWidth: width, gaplessPlayback: true, errorBuilder: (_, _, _) => icon),
              ),
            );
          },
        );
      },
    );
  }
}

/// The list as slivers inside one card: a sort header, then fixed-height
/// rows built only while visible.
Widget _fileListSliver({
  required List<FileNode> items,
  required _ItemCallbacks callbacks,
  required bool wide,
  required Key startKey,
  Widget? header,
}) {
  return DecoratedSliver(
    decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: _border)),
    sliver: SliverMainAxisGroup(
      slivers: <Widget>[
        if (header != null) SliverToBoxAdapter(child: header),
        SliverToBoxAdapter(key: startKey, child: const SizedBox.shrink()),
        SliverFixedExtentList(
          itemExtent: _rowExtent,
          delegate: SliverChildBuilderDelegate(
            (context, index) => _FileRow(
              key: ValueKey<String>(items[index].id),
              node: items[index],
              callbacks: callbacks,
              wide: wide,
              first: index == 0 && header == null,
              last: index == items.length - 1,
            ),
            childCount: items.length,
          ),
        ),
      ],
    ),
  );
}

/// The sort header above the list: name, size, modified.
class _ListHeader extends StatelessWidget {
  const _ListHeader({required this.sortField, required this.sortOrder, required this.onSort, required this.allSelected, required this.someSelected, required this.onSelectAll});

  final String sortField;
  final String sortOrder;
  final ValueChanged<String> onSort;
  final bool allSelected;
  final bool someSelected;
  final VoidCallback onSelectAll;

  Widget _sortHeader(String field, String label) {
    final active = sortField == field;
    final arrow = sortOrder == 'desc' ? Icons.arrow_downward_rounded : Icons.arrow_upward_rounded;
    return InkWell(
      onTap: () => onSort(field),
      borderRadius: BorderRadius.circular(AppRadius.tag),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(label, style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: active ? _textPrimary : _textMuted)),
            if (active) ...<Widget>[const SizedBox(width: 4), Icon(arrow, size: 13, color: _textPrimary)],
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(10, 6, 168, 6),
      decoration: BoxDecoration(border: Border(bottom: BorderSide(color: _border))),
      child: Row(
        children: <Widget>[
          SizedBox(
            width: 40,
            child: Checkbox(
              tristate: true,
              value: allSelected ? true : (someSelected ? null : false),
              onChanged: (_) => onSelectAll(),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(child: _sortHeader('name', appStrings.columnName)),
          SizedBox(width: 100, child: _sortHeader('size', appStrings.columnSize)),
          SizedBox(width: 150, child: _sortHeader('modified', appStrings.columnModified)),
        ],
      ),
    );
  }
}

/// Marks an item the account shared with other people on the server.
class _SharedMark extends StatelessWidget {
  const _SharedMark({required this.size});

  final double size;

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: appStrings.sharedWithPeople,
      child: Icon(Icons.people_alt_outlined, size: size, color: _textMuted),
    );
  }
}
