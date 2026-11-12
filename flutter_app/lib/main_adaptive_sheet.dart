part of 'main.dart';

// Dialogs on wide screens, NeoAgent-style bottom sheets on phones. A caller
// shows its dialog with [showAdaptiveSheet] and builds an [_AdaptiveDialog]
// (title, content, actions, like AlertDialog); the dialog lays itself out
// for the route it is in, so each widget exists once.

/// Phones get sheets: a narrow window, or an Android or iOS phone held
/// sideways.
bool _usesSheets(BuildContext context) {
  final size = MediaQuery.sizeOf(context);
  if (size.width < AppBreakpoints.mobile) return true;
  final mobileOs = !kIsWeb && (defaultTargetPlatform == TargetPlatform.android || defaultTargetPlatform == TargetPlatform.iOS);
  return mobileOs && size.shortestSide < 600;
}

/// [showDialog] on wide screens, a modal bottom sheet on phones. With
/// [dismissible] false neither a tap outside nor a drag closes it.
Future<T?> showAdaptiveSheet<T>({required BuildContext context, required WidgetBuilder builder, bool dismissible = true}) {
  if (!_usesSheets(context)) {
    return showDialog<T>(context: context, barrierDismissible: dismissible, builder: builder);
  }
  return showModalBottomSheet<T>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    showDragHandle: dismissible,
    isDismissible: dismissible,
    enableDrag: dismissible,
    backgroundColor: _bgCard,
    builder: builder,
  );
}

/// AlertDialog's shape, or a bottom sheet's when shown as one: title on
/// top, the content scrolling above the keyboard, the actions full width
/// with the primary one (the last) first.
class _AdaptiveDialog extends StatelessWidget {
  const _AdaptiveDialog({this.icon, required this.title, required this.content, this.actions = const <Widget>[]});

  final Widget? icon;
  final Widget title;
  final Widget content;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    if (ModalRoute.of(context) is! ModalBottomSheetRoute) {
      return AlertDialog(icon: icon, title: title, content: content, actions: actions);
    }
    final dragHandle = (ModalRoute.of(context)! as ModalBottomSheetRoute).showDragHandle ?? false;
    // Above the keyboard when it is up, else above the home indicator (the
    // sheet's own safe area leaves the bottom edge alone).
    final bottom = math.max(MediaQuery.viewInsetsOf(context).bottom, MediaQuery.viewPaddingOf(context).bottom);
    return Padding(
      padding: EdgeInsets.only(bottom: bottom),
      child: Padding(
        padding: EdgeInsets.fromLTRB(AppSpacing.lg, dragHandle ? 0 : AppSpacing.lg, AppSpacing.lg, AppSpacing.md),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (icon != null) ...<Widget>[
              IconTheme.merge(data: IconThemeData(color: _accent, size: 32), child: icon!),
              const SizedBox(height: AppSpacing.sm),
            ],
            DefaultTextStyle.merge(style: _displayTitleStyle(20), child: title),
            const SizedBox(height: AppSpacing.md),
            Flexible(child: SingleChildScrollView(child: content)),
            if (actions.isNotEmpty) ...<Widget>[
              const SizedBox(height: AppSpacing.lg),
              for (final (index, action) in actions.reversed.indexed) ...<Widget>[
                if (index > 0) const SizedBox(height: AppSpacing.xs),
                SizedBox(height: 48, child: action),
              ],
            ],
          ],
        ),
      ),
    );
  }
}
