part of 'main.dart';

// An item's ⋯ menu. A few actions stand on their own; the rest sit in named
// groups that open as submenus on wide screens (hover, click or arrow keys)
// and as a second page of the bottom sheet on phones. The right-click menu
// is the same menu, opened where the pointer is.

/// One entry of an item's menu: an action, or a named group of actions.
class _FileAction {
  const _FileAction(this.icon, this.label, VoidCallback this.onSelected, {this.danger = false, this.quick = false, this.shortcut})
    : children = const <_FileAction>[];

  /// A submenu. Without actions it is left out; with one, that action takes
  /// its place (see [_menuLayout]).
  const _FileAction.group(this.icon, this.label, this.children) : onSelected = null, danger = false, quick = false, shortcut = null;

  final IconData icon;
  final String label;

  /// Null for a group.
  final VoidCallback? onSelected;
  final List<_FileAction> children;
  final bool danger;

  /// Shown as an icon on hover, not only in the menu.
  final bool quick;

  /// Key hint shown in the menu, e.g. "F2".
  final String? shortcut;

  bool get isGroup => onSelected == null;
}

/// The entries a menu shows: empty groups dropped, a group of one shown as
/// that one action.
List<_FileAction> _menuLayout(List<_FileAction> actions) => <_FileAction>[
  for (final action in actions)
    if (!action.isGroup)
      action
    else if (action.children.length == 1)
      action.children.single
    else if (action.children.length > 1)
      action,
];

/// Every action, groups opened up (for the hover buttons).
Iterable<_FileAction> _leafActions(List<_FileAction> actions) sync* {
  for (final action in actions) {
    if (action.isGroup) {
      yield* _leafActions(action.children);
    } else {
      yield action;
    }
  }
}

const MenuStyle _fileMenuStyle = MenuStyle(minimumSize: WidgetStatePropertyAll<Size>(Size(220, 0)));

ButtonStyle _fileMenuItemStyle({required bool danger}) => ButtonStyle(
  minimumSize: const WidgetStatePropertyAll<Size>(Size(0, 40)),
  foregroundColor: WidgetStatePropertyAll<Color>(danger ? _danger : _textPrimary),
  iconColor: WidgetStatePropertyAll<Color>(danger ? _danger : _textSecondary),
  iconSize: const WidgetStatePropertyAll<double>(18),
  textStyle: const WidgetStatePropertyAll<TextStyle>(TextStyle(fontSize: 13.5)),
);

/// A line before the first group and before destructive actions, so the
/// everyday actions, the groups and Move to trash read as three blocks.
bool _lineBefore(List<_FileAction> entries, int index) {
  if (index == 0) return false;
  final entry = entries[index];
  return entry.danger || (entry.isGroup && !entries[index - 1].isGroup);
}

/// The rows of a wide-screen menu.
List<Widget> _fileMenuChildren(List<_FileAction> actions) {
  final entries = _menuLayout(actions);
  return <Widget>[
    for (final (index, action) in entries.indexed) ...<Widget>[
      if (_lineBefore(entries, index)) Divider(height: 9, color: _border),
      if (action.isGroup)
        SubmenuButton(
          style: _fileMenuItemStyle(danger: false),
          menuStyle: _fileMenuStyle,
          leadingIcon: Icon(action.icon),
          menuChildren: _fileMenuChildren(action.children),
          child: Text(action.label),
        )
      else
        MenuItemButton(
          style: _fileMenuItemStyle(danger: action.danger),
          leadingIcon: Icon(action.icon),
          trailingIcon: action.shortcut == null
              ? null
              : Padding(padding: const EdgeInsets.only(left: 16), child: Text(action.shortcut!, style: TextStyle(fontSize: 12, color: _textMuted))),
          onPressed: action.onSelected,
          child: Text(action.label),
        ),
    ],
  ];
}

/// The ⋯ button: a menu on wide screens, a bottom sheet on phones.
class _FileActionsMenu extends StatelessWidget {
  const _FileActionsMenu({required this.actions, required this.title, this.node});

  final List<_FileAction> actions;

  /// The item's name, heading the phone's action sheet.
  final String title;
  final FileNode? node;

  @override
  Widget build(BuildContext context) {
    if (MediaQuery.sizeOf(context).width < AppBreakpoints.mobile) {
      return IconButton(
        tooltip: appStrings.filesMoreActions,
        icon: Icon(Icons.more_horiz_rounded, color: _textMuted, size: 22),
        onPressed: () => unawaited(_showFileActionSheet(context, title: title, node: node, actions: actions)),
      );
    }
    return _FileMenuButton(actions: actions, tooltip: appStrings.filesMoreActions, icon: Icon(Icons.more_horiz_rounded, color: _textMuted, size: 20));
  }
}

/// An icon button that opens [actions] as a menu below it.
class _FileMenuButton extends StatelessWidget {
  const _FileMenuButton({required this.actions, required this.tooltip, required this.icon});

  final List<_FileAction> actions;
  final String tooltip;
  final Widget icon;

  @override
  Widget build(BuildContext context) {
    return MenuAnchor(
      style: _fileMenuStyle,
      menuChildren: _fileMenuChildren(actions),
      builder: (context, menu, _) => IconButton(
        tooltip: tooltip,
        icon: icon,
        onPressed: () => menu.isOpen ? menu.close() : menu.open(),
      ),
    );
  }
}

/// Opens the item's menu where it is right-clicked.
class _FileContextMenu extends StatefulWidget {
  const _FileContextMenu({required this.actions, required this.child});

  final List<_FileAction> actions;
  final Widget child;

  @override
  State<_FileContextMenu> createState() => _FileContextMenuState();
}

class _FileContextMenuState extends State<_FileContextMenu> {
  final MenuController _menu = MenuController();

  @override
  Widget build(BuildContext context) {
    return MenuAnchor(
      controller: _menu,
      style: _fileMenuStyle,
      menuChildren: _fileMenuChildren(widget.actions),
      child: GestureDetector(
        onSecondaryTapDown: (details) => _menu.open(position: details.localPosition),
        child: widget.child,
      ),
    );
  }
}

/// A file's actions on a phone: a bottom sheet with the item on top and one
/// full-width row per action; a group opens as the sheet's second page.
Future<void> _showFileActionSheet(BuildContext context, {required String title, FileNode? node, required List<_FileAction> actions}) async {
  final chosen = await showModalBottomSheet<_FileAction>(
    context: context,
    useSafeArea: true,
    isScrollControlled: true,
    showDragHandle: true,
    backgroundColor: _bgCard,
    builder: (_) => _FileActionSheet(title: title, node: node, actions: actions),
  );
  chosen?.onSelected?.call();
}

class _FileActionSheet extends StatefulWidget {
  const _FileActionSheet({required this.title, required this.node, required this.actions});

  final String title;
  final FileNode? node;
  final List<_FileAction> actions;

  @override
  State<_FileActionSheet> createState() => _FileActionSheetState();
}

class _FileActionSheetState extends State<_FileActionSheet> {
  /// The groups opened, innermost last.
  final List<_FileAction> _opened = <_FileAction>[];

  void _back() => setState(_opened.removeLast);

  Widget _header() {
    final group = _opened.lastOrNull;
    if (group == null) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
        child: Row(
          children: <Widget>[
            if (widget.node != null) ...<Widget>[_FileIcon(node: widget.node!, size: 36), const SizedBox(width: 12)],
            Expanded(
              child: Text(widget.title, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 15, fontWeight: FontWeight.w600)),
            ),
          ],
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 0, 16, 6),
      child: Row(
        children: <Widget>[
          IconButton(tooltip: MaterialLocalizations.of(context).backButtonTooltip, onPressed: _back, icon: const Icon(Icons.arrow_back_rounded)),
          const SizedBox(width: 4),
          Expanded(
            child: Semantics(
              header: true,
              child: Text(group.label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 15, fontWeight: FontWeight.w600)),
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final entries = _menuLayout(_opened.lastOrNull?.children ?? widget.actions);
    // Back (gesture or button) leaves a group before it closes the sheet.
    return PopScope(
      canPop: _opened.isEmpty,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _back();
      },
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(8, 0, 8, 12 + MediaQuery.viewPaddingOf(context).bottom),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            _header(),
            Divider(height: 1, color: _border),
            const SizedBox(height: 6),
            for (final (index, action) in entries.indexed) ...<Widget>[
              if (_lineBefore(entries, index)) Divider(height: 13, color: _border, indent: 16, endIndent: 16),
              ListTile(
                minTileHeight: 52,
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control)),
                leading: Icon(action.icon, size: 22, color: action.danger ? _danger : _textSecondary),
                title: Text(action.label, style: TextStyle(fontSize: 15, color: action.danger ? _danger : _textPrimary)),
                trailing: action.isGroup ? Icon(Icons.chevron_right_rounded, color: _textMuted) : null,
                onTap: action.isGroup ? () => setState(() => _opened.add(action)) : () => Navigator.of(context).pop(action),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
