import 'dart:math' as math;
import 'dart:ui';

import 'models.dart';

/// Geometry of fields on a page, shared by the editor and the signer view.
///
/// Two spaces are used:
/// * **fractions**: 0..1 of the page as displayed, origin top-left (what the
///   server stores, see [FieldArea]);
/// * **pixels**: the page as it is laid out on screen.
///
/// Move and resize work in fractions, so the result is independent of the
/// zoom. Where a pixel ratio matters (an aspect lock) the caller passes the
/// ratio converted with [fractionAspect].

/// The whole page in fractions.
const Rect unitRect = Rect.fromLTWH(0, 0, 1, 1);

/// A box that is [FieldArea]-sized in pixels on a page of [page] size.
Rect fractionToPixels(Rect fractions, Size page) =>
    Rect.fromLTWH(fractions.left * page.width, fractions.top * page.height, fractions.width * page.width, fractions.height * page.height);

Rect pixelsToFraction(Rect pixels, Size page) {
  if (page.isEmpty) return Rect.zero;
  return Rect.fromLTWH(pixels.left / page.width, pixels.top / page.height, pixels.width / page.width, pixels.height / page.height);
}

Offset fractionToPixelsPoint(Offset fractions, Size page) => Offset(fractions.dx * page.width, fractions.dy * page.height);

Offset pixelsToFractionPoint(Offset pixels, Size page) =>
    page.isEmpty ? Offset.zero : Offset(pixels.dx / page.width, pixels.dy / page.height);

Rect areaRect(FieldArea area) => Rect.fromLTWH(area.x, area.y, area.w, area.h);

FieldArea rectToArea(Rect fractions, int page) =>
    FieldArea(page: page, x: fractions.left, y: fractions.top, w: fractions.width, h: fractions.height);

/// The aspect ratio (width / height) a box of [pixelAspect] has in fraction
/// space on a page of [page] size. A square (1.0) is not square in fractions
/// on an A4 page.
double fractionAspect(double pixelAspect, Size page) => pixelAspect * page.height / page.width;

// Clamping

/// [rect] moved and shrunk so it lies inside [bounds]. The size is kept when
/// it fits; a box larger than [bounds] shrinks to it.
Rect clampRect(Rect rect, {Rect bounds = unitRect}) {
  final width = math.min(rect.width, bounds.width);
  final height = math.min(rect.height, bounds.height);
  final left = rect.left.clamp(bounds.left, bounds.right - width).toDouble();
  final top = rect.top.clamp(bounds.top, bounds.bottom - height).toDouble();
  return Rect.fromLTWH(left, top, width, height);
}

// Snapping

/// [value] rounded to the nearest multiple of [step]; unchanged for a step of 0.
double snapValue(double value, double step) => step <= 0 ? value : (value / step).round() * step;

/// A box with its top-left corner on the grid.
Rect snapRectToGrid(Rect rect, double step) =>
    Rect.fromLTWH(snapValue(rect.left, step), snapValue(rect.top, step), rect.width, rect.height);

/// Moves [rect] so an edge or the centre lines up with one of [others] when
/// closer than [threshold] (in fractions). Horizontal and vertical are
/// decided independently; [guides] receives the lines that matched, for
/// drawing.
Rect snapToNeighbours(Rect rect, Iterable<Rect> others, {double threshold = 0.006, List<SnapGuide>? guides}) {
  double? bestX;
  double? bestY;
  var distX = threshold;
  var distY = threshold;
  double? guideX;
  double? guideY;
  final mine = <double>[rect.left, rect.center.dx, rect.right];
  final mineY = <double>[rect.top, rect.center.dy, rect.bottom];
  for (final other in others) {
    for (final target in <double>[other.left, other.center.dx, other.right]) {
      for (final candidate in mine) {
        final distance = (target - candidate).abs();
        if (distance < distX) {
          distX = distance;
          bestX = target - candidate;
          guideX = target;
        }
      }
    }
    for (final target in <double>[other.top, other.center.dy, other.bottom]) {
      for (final candidate in mineY) {
        final distance = (target - candidate).abs();
        if (distance < distY) {
          distY = distance;
          bestY = target - candidate;
          guideY = target;
        }
      }
    }
  }
  if (guideX != null) guides?.add(SnapGuide(vertical: true, position: guideX));
  if (guideY != null) guides?.add(SnapGuide(vertical: false, position: guideY));
  return rect.shift(Offset(bestX ?? 0, bestY ?? 0));
}

/// A line the snapping matched: a vertical one at x = [position] or a
/// horizontal one at y = [position] (fractions).
class SnapGuide {
  const SnapGuide({required this.vertical, required this.position});

  final bool vertical;
  final double position;
}

// Move

/// [start] shifted by [delta] (fractions), kept inside [bounds], with its
/// top-left optionally snapped to a grid of [gridStep].
Rect moveRect(Rect start, Offset delta, {Rect bounds = unitRect, double gridStep = 0}) {
  var moved = start.shift(delta);
  if (gridStep > 0) moved = snapRectToGrid(moved, gridStep);
  return clampRect(moved, bounds: bounds);
}

// Resize

/// The 8 handles of a selected box, clockwise from the top-left.
enum ResizeHandle {
  topLeft,
  top,
  topRight,
  right,
  bottomRight,
  bottom,
  bottomLeft,
  left;

  bool get movesLeft => this == topLeft || this == left || this == bottomLeft;
  bool get movesRight => this == topRight || this == right || this == bottomRight;
  bool get movesTop => this == topLeft || this == top || this == topRight;
  bool get movesBottom => this == bottomLeft || this == bottom || this == bottomRight;
  bool get isCorner => this == topLeft || this == topRight || this == bottomLeft || this == bottomRight;
}

/// Where each handle sits on [rect].
Offset handlePosition(Rect rect, ResizeHandle handle) {
  final x = handle.movesLeft ? rect.left : (handle.movesRight ? rect.right : rect.center.dx);
  final y = handle.movesTop ? rect.top : (handle.movesBottom ? rect.bottom : rect.center.dy);
  return Offset(x, y);
}

/// The handle under [point] (pixels) of a box [rect] (pixels), or null.
/// [hitSize] is the side of the touch target around each handle; corners win
/// over edges. Handles are only offered when the box is big enough that they
/// don't overlap: a tiny box keeps its corners.
ResizeHandle? hitTestHandle(Rect rect, Offset point, {double hitSize = 24}) {
  final half = hitSize / 2;
  final roomForEdges = rect.width > hitSize * 2 && rect.height > hitSize * 2;
  const order = <ResizeHandle>[
    ResizeHandle.topLeft,
    ResizeHandle.topRight,
    ResizeHandle.bottomRight,
    ResizeHandle.bottomLeft,
    ResizeHandle.top,
    ResizeHandle.right,
    ResizeHandle.bottom,
    ResizeHandle.left,
  ];
  for (final handle in order) {
    if (!handle.isCorner && !roomForEdges) continue;
    final at = handlePosition(rect, handle);
    if ((point.dx - at.dx).abs() <= half && (point.dy - at.dy).abs() <= half) return handle;
  }
  return null;
}

/// [start] with [handle] dragged by [delta], all in fractions.
///
/// * The box never gets smaller than [minSize] nor leaves [bounds].
/// * With [aspect] (width / height in fraction space, see [fractionAspect])
///   the ratio is kept: a corner drives both sides from the larger move, an
///   edge scales the other side around its middle.
/// * [gridStep] snaps the moving edges.
Rect resizeRect(
  Rect start,
  ResizeHandle handle,
  Offset delta, {
  Size minSize = const Size(0.02, 0.012),
  Rect bounds = unitRect,
  double? aspect,
  double gridStep = 0,
}) {
  if (aspect != null && aspect > 0) {
    return _resizeKeepingAspect(start, handle, delta, minSize: minSize, bounds: bounds, aspect: aspect, gridStep: gridStep);
  }
  var left = start.left;
  var top = start.top;
  var right = start.right;
  var bottom = start.bottom;
  if (handle.movesLeft) left += delta.dx;
  if (handle.movesRight) right += delta.dx;
  if (handle.movesTop) top += delta.dy;
  if (handle.movesBottom) bottom += delta.dy;
  if (gridStep > 0) {
    if (handle.movesLeft) left = snapValue(left, gridStep);
    if (handle.movesRight) right = snapValue(right, gridStep);
    if (handle.movesTop) top = snapValue(top, gridStep);
    if (handle.movesBottom) bottom = snapValue(bottom, gridStep);
  }
  // Bounds first, then the minimum size against the fixed edge.
  left = math.max(left, bounds.left);
  top = math.max(top, bounds.top);
  right = math.min(right, bounds.right);
  bottom = math.min(bottom, bounds.bottom);
  if (handle.movesLeft) left = math.min(left, right - minSize.width);
  if (handle.movesRight) right = math.max(right, left + minSize.width);
  if (handle.movesTop) top = math.min(top, bottom - minSize.height);
  if (handle.movesBottom) bottom = math.max(bottom, top + minSize.height);
  return Rect.fromLTRB(left, top, right, bottom);
}

Rect _resizeKeepingAspect(
  Rect start,
  ResizeHandle handle,
  Offset delta, {
  required Size minSize,
  required Rect bounds,
  required double aspect,
  required double gridStep,
}) {
  // The fixed point: the opposite corner, or the middle of the opposite edge.
  final anchorX = handle.movesLeft ? start.right : (handle.movesRight ? start.left : start.center.dx);
  final anchorY = handle.movesTop ? start.bottom : (handle.movesBottom ? start.top : start.center.dy);

  double width;
  double height;
  if (handle.isCorner) {
    final rawWidth = start.width + (handle.movesRight ? delta.dx : -delta.dx);
    final rawHeight = start.height + (handle.movesBottom ? delta.dy : -delta.dy);
    // The larger relative change wins, so the box follows the pointer.
    final byWidth = rawWidth / start.width >= rawHeight / start.height;
    width = byWidth ? rawWidth : rawHeight * aspect;
    height = width / aspect;
  } else if (handle.movesLeft || handle.movesRight) {
    width = start.width + (handle.movesRight ? delta.dx : -delta.dx);
    height = width / aspect;
  } else {
    height = start.height + (handle.movesBottom ? delta.dy : -delta.dy);
    width = height * aspect;
  }
  if (gridStep > 0) {
    width = snapValue(width, gridStep);
    height = width / aspect;
  }

  // The most room the anchor leaves in the direction of growth; around a
  // centred anchor the box grows both ways.
  double room(bool towardsLow, bool towardsHigh, double anchor, double low, double high) {
    if (towardsLow) return anchor - low;
    if (towardsHigh) return high - anchor;
    return 2 * math.min(anchor - low, high - anchor);
  }

  final roomX = room(handle.movesLeft, handle.movesRight, anchorX, bounds.left, bounds.right);
  final roomY = room(handle.movesTop, handle.movesBottom, anchorY, bounds.top, bounds.bottom);
  final maxWidth = math.min(roomX, roomY * aspect);
  final minWidth = math.max(minSize.width, minSize.height * aspect);
  width = width.clamp(math.min(minWidth, maxWidth), maxWidth).toDouble();
  height = width / aspect;

  final double left;
  final double top;
  if (handle.movesLeft) {
    left = anchorX - width;
  } else if (handle.movesRight) {
    left = anchorX;
  } else {
    left = anchorX - width / 2;
  }
  if (handle.movesTop) {
    top = anchorY - height;
  } else if (handle.movesBottom) {
    top = anchorY;
  } else {
    top = anchorY - height / 2;
  }
  return Rect.fromLTWH(left, top, width, height);
}

// Hit testing

/// The box under [point] among [boxes] (all in the same space), or null. The
/// last box in the list is on top and wins. [tolerance] grows each box so a
/// small one is easy to hit with a finger.
String? hitTestBoxes(List<MapEntry<String, Rect>> boxes, Offset point, {double tolerance = 0}) {
  for (var index = boxes.length - 1; index >= 0; index--) {
    if (boxes[index].value.inflate(tolerance).contains(point)) return boxes[index].key;
  }
  return null;
}

/// The index of the page whose box (in a column, top to bottom) holds the
/// vertical position [dy]; the nearest one for a point in a gap.
int pageAtOffset(List<Rect> pageRects, double dy) {
  if (pageRects.isEmpty) return 0;
  var best = 0;
  var bestDistance = double.infinity;
  for (var index = 0; index < pageRects.length; index++) {
    final rect = pageRects[index];
    if (dy >= rect.top && dy <= rect.bottom) return index;
    final distance = dy < rect.top ? rect.top - dy : dy - rect.bottom;
    if (distance < bestDistance) {
      bestDistance = distance;
      best = index;
    }
  }
  return best;
}

// Placing

/// A box of [size] centred on [center], kept inside [bounds]: "tap the page
/// to place a field".
Rect placeAt(Offset center, Size size, {Rect bounds = unitRect}) =>
    clampRect(Rect.fromCenter(center: center, width: size.width, height: size.height), bounds: bounds);

/// The box spanned by a drag from [a] to [b], at least [minSize], inside
/// [bounds]: "draw the field on the page".
Rect rectFromDrag(Offset a, Offset b, {Size minSize = const Size(0.02, 0.012), Rect bounds = unitRect}) {
  final raw = Rect.fromPoints(a, b);
  final width = math.max(raw.width, minSize.width);
  final height = math.max(raw.height, minSize.height);
  return clampRect(Rect.fromLTWH(raw.left, raw.top, width, height), bounds: bounds);
}

/// [rects] shifted by [delta] and clamped as one group, so copies pasted
/// next to each other keep their layout.
List<Rect> moveGroup(List<Rect> rects, Offset delta, {Rect bounds = unitRect}) {
  if (rects.isEmpty) return const <Rect>[];
  final union = rects.reduce((a, b) => a.expandToInclude(b));
  final moved = clampRect(union.shift(delta), bounds: bounds);
  final shift = moved.topLeft - union.topLeft;
  return rects.map((rect) => rect.shift(shift)).toList(growable: false);
}
