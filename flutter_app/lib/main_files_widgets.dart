part of 'main.dart';

class _FileAction {
  const _FileAction(this.icon, this.label, this.onSelected, {this.danger = false, this.quick = false, this.shortcut});

  final IconData icon;
  final String label;
  final VoidCallback onSelected;
  final bool danger;

  /// Shown as an icon on hover, not only in the menu.
  final bool quick;

  /// Key hint shown in the menu, e.g. "F2".
  final String? shortcut;
}

List<PopupMenuEntry<int>> _menuEntries(List<_FileAction> actions) {
  return <PopupMenuEntry<int>>[
    for (var i = 0; i < actions.length; i++) ...<PopupMenuEntry<int>>[
      if (actions[i].danger && i > 0) const PopupMenuDivider(),
      PopupMenuItem<int>(
        value: i,
        height: 40,
        child: Row(
          children: <Widget>[
            Icon(actions[i].icon, size: 18, color: actions[i].danger ? _danger : _textSecondary),
            const SizedBox(width: 12),
            Expanded(child: Text(actions[i].label, style: TextStyle(fontSize: 13.5, color: actions[i].danger ? _danger : _textPrimary))),
            if (actions[i].shortcut != null) ...<Widget>[
              const SizedBox(width: 16),
              Text(actions[i].shortcut!, style: TextStyle(fontSize: 12, color: _textMuted)),
            ],
          ],
        ),
      ),
    ],
  ];
}

/// The right-click menu, opened where the pointer is.
Future<void> _showContextMenu(BuildContext context, Offset position, List<_FileAction> actions) async {
  final overlay = Overlay.of(context).context.findRenderObject()! as RenderBox;
  final index = await showMenu<int>(
    context: context,
    position: RelativeRect.fromRect(position & const Size(1, 1), Offset.zero & overlay.size),
    items: _menuEntries(actions),
  );
  if (index != null) actions[index].onSelected();
}

class _FileActionsMenu extends StatelessWidget {
  const _FileActionsMenu({required this.actions});

  final List<_FileAction> actions;

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<int>(
      tooltip: appStrings.details,
      icon: Icon(Icons.more_horiz_rounded, color: _textMuted, size: 20),
      onSelected: (index) => actions[index].onSelected(),
      itemBuilder: (context) => _menuEntries(actions),
    );
  }
}

/// A file's type at a glance, on a neutral tile.
class _FileIcon extends StatelessWidget {
  const _FileIcon({required this.node, this.size = 34});

  final FileNode node;
  final double size;

  @override
  Widget build(BuildContext context) {
    final kind = fileKindOf(node);
    final vaultFolder = kind == FileKind.folder && node.inVault;
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: kind == FileKind.folder ? _accentMuted : _bgTertiary,
        borderRadius: BorderRadius.circular(size * 0.28),
        border: Border.all(color: _border),
      ),
      child: Icon(
        vaultFolder ? Icons.lock_rounded : fileKindIcon(kind),
        size: size * 0.52,
        color: kind == FileKind.folder ? _accent : _textSecondary,
      ),
    );
  }
}

/// The floating label under the pointer while items are dragged.
Widget _dragFeedback(List<FileNode> nodes) {
  return Material(
    color: Colors.transparent,
    child: Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: _bgSecondary,
        borderRadius: BorderRadius.circular(AppRadius.control),
        border: Border.all(color: _accent.withValues(alpha: 0.5)),
        boxShadow: _softPanelShadow,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          _FileIcon(node: nodes.first, size: 24),
          const SizedBox(width: 10),
          Text(
            nodes.length == 1 ? nodes.first.name : appStrings.dragItems(nodes.length),
            style: TextStyle(color: _textPrimary, fontSize: 13, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    ),
  );
}

/// Makes [child] draggable (as the node's drag subject) and, for folders, a
/// drop target. Touch screens start a drag with a long press so scrolling
/// keeps working.
class _DragDrop extends StatelessWidget {
  const _DragDrop({required this.node, required this.callbacks, required this.builder});

  final FileNode node;
  final _ItemCallbacks callbacks;
  final Widget Function(BuildContext context, bool dropHover) builder;

  @override
  Widget build(BuildContext context) {
    if (!callbacks.canDrag) return builder(context, false);
    final subject = callbacks.dragSubject!(node);
    Widget target = node.isFolder
        ? DragTarget<List<FileNode>>(
            onWillAcceptWithDetails: (details) => !details.data.any((n) => n.id == node.id),
            onAcceptWithDetails: (details) => callbacks.onDropInto!(details.data, node),
            builder: (context, candidates, _) => builder(context, candidates.isNotEmpty),
          )
        : builder(context, false);
    // On touch a long press selects; moving goes through the selection bar.
    if (!_usesMouse) return target;
    // Dimmed by color, not Opacity, which would need its own layer.
    final dimmed = Container(
      foregroundDecoration: BoxDecoration(color: _bgCard.withValues(alpha: 0.6), borderRadius: BorderRadius.circular(AppRadius.control)),
      child: builder(context, false),
    );
    return Draggable<List<FileNode>>(
      data: subject,
      feedback: _dragFeedback(subject),
      childWhenDragging: dimmed,
      dragAnchorStrategy: pointerDragAnchorStrategy,
      child: target,
    );
  }
}

class _Breadcrumbs extends StatelessWidget {
  const _Breadcrumbs({required this.path, required this.onOpen, this.onDropItems});

  final List<PathSegment> path;
  final ValueChanged<String?> onOpen;

  /// Items dropped onto a crumb move into that folder.
  final void Function(List<FileNode> nodes, String folderId, String folderName)? onDropItems;

  @override
  Widget build(BuildContext context) {
    final crumbs = path.isEmpty ? <PathSegment>[const PathSegment(id: 'root', name: '')] : path;
    return Wrap(
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        for (var i = 0; i < crumbs.length; i++) ...<Widget>[
          if (i > 0) Icon(Icons.chevron_right_rounded, size: 18, color: _textMuted),
          DragTarget<List<FileNode>>(
            onWillAcceptWithDetails: (details) => onDropItems != null && i < crumbs.length - 1,
            onAcceptWithDetails: (details) => onDropItems!(details.data, crumbs[i].id, i == 0 ? appStrings.home : crumbs[i].name),
            builder: (context, candidates, _) => Container(
              decoration: BoxDecoration(
                color: candidates.isNotEmpty ? _accentMuted : Colors.transparent,
                borderRadius: BorderRadius.circular(AppRadius.control),
                border: Border.all(color: candidates.isNotEmpty ? _accent : Colors.transparent),
              ),
              child: TextButton.icon(
                onPressed: i == crumbs.length - 1 ? null : () => onOpen(i == 0 ? null : crumbs[i].id),
                style: TextButton.styleFrom(
                  foregroundColor: _textSecondary,
                  disabledForegroundColor: _textPrimary,
                  padding: const EdgeInsets.symmetric(horizontal: 8),
                  minimumSize: const Size(0, 34),
                ),
                icon: i == 0 ? const Icon(Icons.home_outlined, size: 17) : const SizedBox.shrink(),
                label: Text(i == 0 ? appStrings.home : crumbs[i].name, style: const TextStyle(fontWeight: FontWeight.w600)),
              ),
            ),
          ),
        ],
      ],
    );
  }
}

/// The actions for the selected items. Wide screens show it in place of
/// the toolbar (same height, so nothing jumps); phones pin it to the bottom.
class _SelectionBar extends StatelessWidget {
  const _SelectionBar({
    required this.count,
    required this.onClear,
    required this.onDownload,
    required this.onMove,
    required this.onTrash,
    required this.onFavorite,
    this.compact = false,
    this.iconsOnly = false,
  });

  final int count;
  final VoidCallback onClear;
  final VoidCallback onDownload;
  final VoidCallback onMove;
  final VoidCallback onTrash;
  final VoidCallback onFavorite;
  final bool compact;

  /// Narrow content: actions as icons with tooltips, so the bar never overflows.
  final bool iconsOnly;

  @override
  Widget build(BuildContext context) {
    Widget action(IconData icon, String label, VoidCallback onPressed, {Color? color}) => compact || iconsOnly
        ? IconButton(tooltip: label, onPressed: onPressed, color: color, icon: Icon(icon, size: 20))
        : TextButton.icon(
            onPressed: onPressed,
            style: color == null ? null : TextButton.styleFrom(foregroundColor: color),
            icon: Icon(icon, size: 18),
            label: Text(label),
          );
    return Semantics(
      container: true,
      liveRegion: true,
      child: Container(
        height: 48,
        padding: const EdgeInsets.symmetric(horizontal: 6),
        decoration: BoxDecoration(
          color: compact ? _bgSecondary : _accentMuted,
          borderRadius: BorderRadius.circular(compact ? AppRadius.panel : AppRadius.control),
          border: Border.all(color: compact ? _border : _accent.withValues(alpha: 0.3)),
          boxShadow: compact ? _softPanelShadow : null,
        ),
        child: Row(
          children: <Widget>[
            IconButton(tooltip: appStrings.clear, onPressed: onClear, icon: const Icon(Icons.close_rounded, size: 18)),
            Text(appStrings.selectedCount(count), style: TextStyle(fontWeight: FontWeight.w600, color: _textPrimary, fontSize: 13.5)),
            const Spacer(),
            action(Icons.star_outline_rounded, appStrings.filesStar, onFavorite),
            action(Icons.download_rounded, appStrings.download, onDownload),
            action(Icons.drive_file_move_outline, appStrings.moveTo, onMove),
            action(Icons.delete_outline_rounded, appStrings.moveToTrash, onTrash, color: _danger),
          ],
        ),
      ),
    );
  }
}

/// The files opened most recently, one click away, at the top of Home.
class _QuickAccess extends StatelessWidget {
  const _QuickAccess({required this.files, required this.controller, required this.onOpen});

  final List<FileNode> files;
  final NeoCloudController controller;
  final ValueChanged<FileNode> onOpen;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(padding: const EdgeInsets.fromLTRB(2, 0, 0, 10), child: Text(appStrings.filesQuickAccess, style: _sectionTitleStyle())),
        SizedBox(
          height: 132,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            itemCount: files.length,
            separatorBuilder: (_, _) => const SizedBox(width: 12),
            itemBuilder: (context, index) {
              final file = files[index];
              return SizedBox(
                width: 176,
                child: _AppCard(
                  padding: const EdgeInsets.all(10),
                  onTap: () => onOpen(file),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Expanded(child: _FilePreviewTile(node: file, controller: controller)),
                      const SizedBox(height: 8),
                      Text(file.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 13)),
                      Text(_displayPath(file) ?? formatDate(file.modifiedAt, currentAppLanguage.code), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 12)),
                    ],
                  ),
                ),
              );
            },
          ),
        ),
      ],
    );
  }
}

class _DropOverlay extends StatelessWidget {
  const _DropOverlay({required this.folder});

  final String folder;

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: Container(
        margin: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: _bgPrimary.withValues(alpha: 0.86),
          borderRadius: BorderRadius.circular(AppRadius.panel),
          border: Border.all(color: _accent, width: 1.5),
        ),
        child: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Container(
                width: 56,
                height: 56,
                decoration: BoxDecoration(color: _accentMuted, shape: BoxShape.circle),
                child: Icon(Icons.cloud_upload_outlined, size: 26, color: _accent),
              ),
              const SizedBox(height: 14),
              Text(appStrings.dropToUpload(folder), style: _displayTitleStyle(18)),
            ],
          ),
        ),
      ),
    );
  }
}
