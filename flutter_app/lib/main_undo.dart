part of 'main.dart';

/// The floating "3 items moved to trash · Undo" bar at the bottom center.
/// It never shifts the page: it floats, keeps clear of the upload panel's
/// corner, and the file list keeps the focused row above it. Hovering
/// pauses its 10 s clock.
class _UndoBar extends StatelessWidget {
  const _UndoBar({required this.center});

  final UndoCenter center;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: center,
      builder: (context, _) {
        final entry = center.current;
        return AnimatedSwitcher(
          duration: const Duration(milliseconds: 180),
          switchInCurve: Curves.easeOutCubic,
          switchOutCurve: Curves.easeIn,
          transitionBuilder: (child, animation) => FadeTransition(
            opacity: animation,
            child: SlideTransition(position: Tween<Offset>(begin: const Offset(0, 0.3), end: Offset.zero).animate(animation), child: child),
          ),
          child: entry == null ? const SizedBox.shrink() : _UndoCard(key: ObjectKey(entry), entry: entry, center: center),
        );
      },
    );
  }
}

class _UndoCard extends StatelessWidget {
  const _UndoCard({super.key, required this.entry, required this.center});

  final UndoEntry entry;
  final UndoCenter center;

  @override
  Widget build(BuildContext context) {
    final width = math.min(520.0, MediaQuery.sizeOf(context).width - 32);
    return MouseRegion(
      onEnter: (_) => center.pause(),
      onExit: (_) => center.resume(),
      child: Semantics(
        liveRegion: true,
        container: true,
        child: Material(
          color: _textPrimary,
          borderRadius: BorderRadius.circular(AppRadius.control),
          elevation: 0,
          child: Container(
            constraints: BoxConstraints(maxWidth: width, minHeight: 48),
            padding: const EdgeInsets.fromLTRB(16, 4, 4, 4),
            decoration: BoxDecoration(borderRadius: BorderRadius.circular(AppRadius.control), boxShadow: _softPanelShadow),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Flexible(
                  child: Text(
                    entry.message,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: _bgPrimary, fontSize: 13.5, fontWeight: FontWeight.w500),
                  ),
                ),
                const SizedBox(width: 8),
                if (entry.onUndo != null)
                  TextButton(
                    onPressed: () => unawaited(center.undo()),
                    style: TextButton.styleFrom(foregroundColor: _bgPrimary, textStyle: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5, decoration: TextDecoration.underline)),
                    child: Text(appStrings.undoAction),
                  ),
                IconButton(
                  tooltip: appStrings.close,
                  onPressed: center.dismiss,
                  icon: Icon(Icons.close_rounded, size: 18, color: _bgPrimary.withValues(alpha: 0.8)),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
