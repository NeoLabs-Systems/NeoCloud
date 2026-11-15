import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../theme/palette.dart';
import '../geometry.dart';
import 'party_colors.dart';

/// One box on a page, as the editor and the signer see it: the party's
/// colour, a small label chip with the type icon, and states.
///
/// It fills the space it is given (position it in the page's overlay `Stack`
/// with the rect from `geometry.dart`) and draws its handles just outside
/// that space. It does no dragging of the box itself; wrap it in a
/// `GestureDetector` for that. Handle drags are reported through [onResize*].
class FieldBox extends StatelessWidget {
  const FieldBox({
    super.key,
    required this.color,
    required this.icon,
    this.label,
    this.child,
    this.selected = false,
    this.hovered = false,
    this.filled = false,
    this.hasError = false,
    this.readonly = false,
    this.highlighted = false,
    this.showHandles = true,
    this.showLabel = true,
    this.handleHitSize = 24,
    this.onResizeStart,
    this.onResizeUpdate,
    this.onResizeEnd,
  });

  /// The party's colour ([partyColor]).
  final Color color;
  final IconData icon;

  /// Name of the field, or its type, on the chip above the box.
  final String? label;

  /// What the box shows inside: the typed text, the signature image, …
  final Widget? child;

  /// The one being edited: strong border and (with [showHandles]) 8 handles.
  final bool selected;
  final bool hovered;

  /// A value is in: the box shows a calm outline instead of the tint.
  final bool filled;

  /// The value is missing or wrong (signing view).
  final bool hasError;

  /// Another party's box, or a locked one: dimmed, no handles.
  final bool readonly;

  /// The box to fill in next (signing view): a thicker outline.
  final bool highlighted;

  final bool showHandles;
  final bool showLabel;

  /// Side of the touch target around a handle; use 32+ on phones.
  final double handleHitSize;

  final ValueChanged<ResizeHandle>? onResizeStart;

  /// [delta] is the pointer's move since the last call, in pixels.
  final void Function(ResizeHandle handle, Offset delta)? onResizeUpdate;
  final VoidCallback? onResizeEnd;

  @override
  Widget build(BuildContext context) {
    final palette = paletteFor(Theme.of(context).brightness);
    final borderColor = hasError ? palette.danger : color;
    final double borderWidth = selected || highlighted || hasError ? 2 : 1.2;
    final double fillAlpha = filled && !selected ? 0.05 : (hovered || selected ? 0.26 : 0.16);
    final handles = selected && showHandles && !readonly && (onResizeUpdate != null);

    final box = DecoratedBox(
      decoration: BoxDecoration(
        color: color.withValues(alpha: readonly ? fillAlpha * 0.6 : fillAlpha),
        borderRadius: BorderRadius.circular(3),
        border: Border.all(color: borderColor.withValues(alpha: readonly ? 0.55 : 1), width: borderWidth),
        boxShadow: highlighted ? <BoxShadow>[BoxShadow(color: color.withValues(alpha: 0.45), blurRadius: 10, spreadRadius: 1)] : null,
      ),
      child: Stack(
        fit: StackFit.expand,
        children: <Widget>[
          if (child != null) Padding(padding: const EdgeInsets.all(2), child: child),
          if (child == null)
            Center(child: Icon(icon, size: 16, color: color.withValues(alpha: readonly ? 0.5 : 0.9))),
        ],
      ),
    );

    return Semantics(
      container: true,
      label: label,
      selected: selected,
      child: Stack(
        clipBehavior: Clip.none,
        fit: StackFit.expand,
        children: <Widget>[
          Positioned.fill(child: box),
          if (showLabel && label != null && label!.isNotEmpty)
            Positioned(left: 0, bottom: null, top: -20, child: _Chip(color: borderColor, icon: icon, label: label!, dim: readonly)),
          if (handles)
            for (final handle in ResizeHandle.values)
              _HandleSpot(
                key: ValueKey<String>('field-handle-${handle.name}'),
                handle: handle,
                color: color,
                hitSize: handleHitSize,
                onStart: onResizeStart,
                onUpdate: onResizeUpdate!,
                onEnd: onResizeEnd,
              ),
        ],
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  const _Chip({required this.color, required this.icon, required this.label, required this.dim});

  final Color color;
  final IconData icon;
  final String label;
  final bool dim;

  @override
  Widget build(BuildContext context) {
    final on = onPartyColor(color);
    return IgnorePointer(
      child: Container(
        height: 18,
        constraints: const BoxConstraints(maxWidth: 180),
        padding: const EdgeInsets.symmetric(horizontal: 5),
        decoration: BoxDecoration(color: color.withValues(alpha: dim ? 0.6 : 1), borderRadius: BorderRadius.circular(4)),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(icon, size: 11, color: on),
            const SizedBox(width: 3),
            Flexible(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: on, fontSize: 10.5, fontWeight: FontWeight.w600, height: 1.1),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _HandleSpot extends StatelessWidget {
  const _HandleSpot({
    super.key,
    required this.handle,
    required this.color,
    required this.hitSize,
    required this.onStart,
    required this.onUpdate,
    required this.onEnd,
  });

  final ResizeHandle handle;
  final Color color;
  final double hitSize;
  final ValueChanged<ResizeHandle>? onStart;
  final void Function(ResizeHandle handle, Offset delta) onUpdate;
  final VoidCallback? onEnd;

  static const double _dot = 10;

  MouseCursor get _cursor {
    switch (handle) {
      case ResizeHandle.top:
      case ResizeHandle.bottom:
        return SystemMouseCursors.resizeUpDown;
      case ResizeHandle.left:
      case ResizeHandle.right:
        return SystemMouseCursors.resizeLeftRight;
      case ResizeHandle.topLeft:
      case ResizeHandle.bottomRight:
        return SystemMouseCursors.resizeUpLeftDownRight;
      case ResizeHandle.topRight:
      case ResizeHandle.bottomLeft:
        return SystemMouseCursors.resizeUpRightDownLeft;
    }
  }

  @override
  Widget build(BuildContext context) {
    // A hit area centred on the handle's point, which lies on the border of
    // the box this widget is stacked over.
    final alignX = handle.movesLeft ? -1.0 : (handle.movesRight ? 1.0 : 0.0);
    final alignY = handle.movesTop ? -1.0 : (handle.movesBottom ? 1.0 : 0.0);
    return Positioned.fill(
      child: LayoutBuilder(
        builder: (context, constraints) {
          // A small box keeps its corners only: edge handles would cover the
          // middle, where a drag has to move the box, and a handle never takes
          // more than a third of a side.
          final roomy = constraints.maxWidth >= hitSize * 3 && constraints.maxHeight >= hitSize * 3;
          if (!handle.isCorner && !roomy) return const SizedBox.shrink();
          final hit = math.min(hitSize, math.min(constraints.maxWidth, constraints.maxHeight) / 2.2);
          final at = Offset((alignX + 1) / 2 * constraints.maxWidth, (alignY + 1) / 2 * constraints.maxHeight);
          return Stack(
            clipBehavior: Clip.none,
            children: <Widget>[
              Positioned(
                left: at.dx - _dot / 2,
                top: at.dy - _dot / 2,
                child: IgnorePointer(
                  child: Container(
                    width: _dot,
                    height: _dot,
                    decoration: BoxDecoration(
                      color: Colors.white,
                      border: Border.all(color: color, width: 2),
                      borderRadius: BorderRadius.circular(handle.isCorner ? 2 : 5),
                    ),
                  ),
                ),
              ),
              // Pointer hits outside a box never reach it, so the touch target
              // is kept inside the box; the dot is still drawn on the corner.
              Positioned(
                left: (at.dx - hit / 2).clamp(0.0, math.max(0.0, constraints.maxWidth - hit)).toDouble(),
                top: (at.dy - hit / 2).clamp(0.0, math.max(0.0, constraints.maxHeight - hit)).toDouble(),
                width: hit,
                height: hit,
                child: MouseRegion(
                  cursor: _cursor,
                  child: GestureDetector(
                    behavior: HitTestBehavior.opaque,
                    onPanStart: (_) => onStart?.call(handle),
                    onPanUpdate: (details) => onUpdate(handle, details.delta),
                    onPanEnd: (_) => onEnd?.call(),
                    onPanCancel: () => onEnd?.call(),
                    child: const SizedBox.expand(),
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}
