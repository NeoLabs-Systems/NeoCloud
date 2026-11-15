import 'dart:ui';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/geometry.dart';
import 'package:neocloud/src/signing/models.dart';

Matcher rectClose(Rect expected) => predicate<Rect>(
  (actual) =>
      (actual.left - expected.left).abs() < 1e-9 &&
      (actual.top - expected.top).abs() < 1e-9 &&
      (actual.right - expected.right).abs() < 1e-9 &&
      (actual.bottom - expected.bottom).abs() < 1e-9,
  'is close to $expected',
);

void main() {
  const page = Size(600, 800);

  group('mapping', () {
    test('fractions and pixels convert both ways', () {
      const fractions = Rect.fromLTWH(0.1, 0.25, 0.5, 0.1);
      final pixels = fractionToPixels(fractions, page);
      expect(pixels, const Rect.fromLTWH(60, 200, 300, 80));
      expect(pixelsToFraction(pixels, page), rectClose(fractions));
      expect(pixelsToFraction(pixels, Size.zero), Rect.zero);
    });

    test('points convert', () {
      expect(fractionToPixelsPoint(const Offset(0.5, 0.5), page), const Offset(300, 400));
      expect(pixelsToFractionPoint(const Offset(150, 200), page), const Offset(0.25, 0.25));
    });

    test('areas and rects convert', () {
      const area = FieldArea(page: 3, x: 0.1, y: 0.2, w: 0.3, h: 0.4);
      expect(areaRect(area), const Rect.fromLTWH(0.1, 0.2, 0.3, 0.4));
      final back = rectToArea(areaRect(area), 3);
      expect(back.page, 3);
      expect(back.w, closeTo(0.3, 1e-9));
      expect(back.h, closeTo(0.4, 1e-9));
    });

    test('a square is not square in fractions on a tall page', () {
      expect(fractionAspect(1, page), closeTo(800 / 600, 1e-9));
      final box = Rect.fromLTWH(0, 0, 0.1, 0.1 / fractionAspect(1, page));
      final px = fractionToPixels(box, page);
      expect(px.width, closeTo(px.height, 1e-9));
    });
  });

  group('clamp, move, snap', () {
    test('clampRect keeps size and pulls inside', () {
      expect(clampRect(const Rect.fromLTWH(0.9, -0.1, 0.3, 0.2)), rectClose(const Rect.fromLTWH(0.7, 0, 0.3, 0.2)));
      expect(clampRect(const Rect.fromLTWH(0.2, 0.2, 0.3, 0.2)), const Rect.fromLTWH(0.2, 0.2, 0.3, 0.2));
    });

    test('clampRect shrinks a box larger than the bounds', () {
      expect(clampRect(const Rect.fromLTWH(0, 0, 2, 3)), const Rect.fromLTWH(0, 0, 1, 1));
    });

    test('moveRect stops at the page edge', () {
      const start = Rect.fromLTWH(0.5, 0.5, 0.2, 0.1);
      expect(moveRect(start, const Offset(1, 1)), rectClose(const Rect.fromLTWH(0.8, 0.9, 0.2, 0.1)));
      expect(moveRect(start, const Offset(-1, -1)), rectClose(const Rect.fromLTWH(0, 0, 0.2, 0.1)));
      expect(moveRect(start, const Offset(0.02, 0.0)), rectClose(const Rect.fromLTWH(0.52, 0.5, 0.2, 0.1)));
    });

    test('grid snapping', () {
      expect(snapValue(0.234, 0.05), closeTo(0.25, 1e-9));
      expect(snapValue(0.234, 0), 0.234);
      final moved = moveRect(const Rect.fromLTWH(0.1, 0.1, 0.2, 0.1), const Offset(0.113, 0.062), gridStep: 0.05);
      expect(moved.left, closeTo(0.2, 1e-9));
      expect(moved.top, closeTo(0.15, 1e-9));
    });

    test('snapToNeighbours lines up edges and reports guides', () {
      const other = Rect.fromLTWH(0.3, 0.5, 0.2, 0.1);
      final guides = <SnapGuide>[];
      final snapped = snapToNeighbours(const Rect.fromLTWH(0.302, 0.2, 0.1, 0.05), <Rect>[other], guides: guides);
      expect(snapped.left, closeTo(0.3, 1e-9));
      expect(snapped.top, 0.2);
      expect(guides.single.vertical, isTrue);
      expect(guides.single.position, closeTo(0.3, 1e-9));
    });

    test('snapToNeighbours leaves distant boxes alone', () {
      const box = Rect.fromLTWH(0.1, 0.1, 0.1, 0.1);
      expect(snapToNeighbours(box, const <Rect>[Rect.fromLTWH(0.6, 0.6, 0.1, 0.1)]), box);
    });

    test('moveGroup keeps the layout at the edge', () {
      final moved = moveGroup(const <Rect>[Rect.fromLTWH(0.1, 0.1, 0.2, 0.1), Rect.fromLTWH(0.5, 0.3, 0.2, 0.1)], const Offset(0.6, 0));
      expect(moved[1].right, closeTo(1, 1e-9));
      expect(moved[1].left - moved[0].left, closeTo(0.4, 1e-9));
    });
  });

  group('resize without aspect lock', () {
    const start = Rect.fromLTWH(0.3, 0.3, 0.2, 0.1);

    test('each handle moves only its edges', () {
      expect(resizeRect(start, ResizeHandle.right, const Offset(0.1, 0.5)), rectClose(const Rect.fromLTRB(0.3, 0.3, 0.6, 0.4)));
      expect(resizeRect(start, ResizeHandle.left, const Offset(-0.1, 0.5)), rectClose(const Rect.fromLTRB(0.2, 0.3, 0.5, 0.4)));
      expect(resizeRect(start, ResizeHandle.top, const Offset(0.5, -0.1)), rectClose(const Rect.fromLTRB(0.3, 0.2, 0.5, 0.4)));
      expect(resizeRect(start, ResizeHandle.bottom, const Offset(0.5, 0.1)), rectClose(const Rect.fromLTRB(0.3, 0.3, 0.5, 0.5)));
      expect(resizeRect(start, ResizeHandle.topLeft, const Offset(-0.1, -0.1)), rectClose(const Rect.fromLTRB(0.2, 0.2, 0.5, 0.4)));
      expect(resizeRect(start, ResizeHandle.topRight, const Offset(0.1, -0.1)), rectClose(const Rect.fromLTRB(0.3, 0.2, 0.6, 0.4)));
      expect(resizeRect(start, ResizeHandle.bottomLeft, const Offset(-0.1, 0.1)), rectClose(const Rect.fromLTRB(0.2, 0.3, 0.5, 0.5)));
      expect(resizeRect(start, ResizeHandle.bottomRight, const Offset(0.1, 0.1)), rectClose(const Rect.fromLTRB(0.3, 0.3, 0.6, 0.5)));
    });

    test('never smaller than the minimum, the opposite edge stays', () {
      final shrunk = resizeRect(start, ResizeHandle.right, const Offset(-0.9, 0), minSize: const Size(0.05, 0.02));
      expect(shrunk.left, closeTo(0.3, 1e-9));
      expect(shrunk.width, closeTo(0.05, 1e-9));
      final fromLeft = resizeRect(start, ResizeHandle.left, const Offset(0.9, 0), minSize: const Size(0.05, 0.02));
      expect(fromLeft.right, closeTo(0.5, 1e-9));
      expect(fromLeft.width, closeTo(0.05, 1e-9));
      final fromTop = resizeRect(start, ResizeHandle.topLeft, const Offset(0.9, 0.9), minSize: const Size(0.05, 0.02));
      expect(fromTop.width, closeTo(0.05, 1e-9));
      expect(fromTop.height, closeTo(0.02, 1e-9));
      expect(fromTop.bottomRight, start.bottomRight);
    });

    test('stays inside the page', () {
      final grown = resizeRect(start, ResizeHandle.bottomRight, const Offset(5, 5));
      expect(grown.right, 1);
      expect(grown.bottom, 1);
      final up = resizeRect(start, ResizeHandle.topLeft, const Offset(-5, -5));
      expect(up.left, 0);
      expect(up.top, 0);
    });

    test('a zero delta changes nothing', () {
      for (final handle in ResizeHandle.values) {
        expect(resizeRect(start, handle, Offset.zero), rectClose(start), reason: handle.name);
      }
    });

    test('grid snapping moves the dragged edge', () {
      final snapped = resizeRect(start, ResizeHandle.right, const Offset(0.133, 0), gridStep: 0.05);
      expect(snapped.right, closeTo(0.65, 1e-9));
    });
  });

  group('resize with aspect lock', () {
    const start = Rect.fromLTWH(0.2, 0.2, 0.2, 0.1);
    const aspect = 2.0;

    double ratio(Rect rect) => rect.width / rect.height;

    test('corners keep the ratio and the opposite corner', () {
      final grown = resizeRect(start, ResizeHandle.bottomRight, const Offset(0.1, 0.0), aspect: aspect);
      expect(ratio(grown), closeTo(aspect, 1e-9));
      expect(grown.topLeft, start.topLeft);
      expect(grown.width, closeTo(0.3, 1e-9));
      final up = resizeRect(start, ResizeHandle.topLeft, const Offset(-0.1, 0), aspect: aspect);
      expect(ratio(up), closeTo(aspect, 1e-9));
      expect(up.bottomRight.dx, closeTo(start.right, 1e-9));
      expect(up.bottomRight.dy, closeTo(start.bottom, 1e-9));
    });

    test('the larger change drives a corner', () {
      final byHeight = resizeRect(start, ResizeHandle.bottomRight, const Offset(0.01, 0.2), aspect: aspect);
      expect(ratio(byHeight), closeTo(aspect, 1e-9));
      expect(byHeight.height, closeTo(0.3, 1e-9));
    });

    test('edges scale the other side around the middle', () {
      final wider = resizeRect(start, ResizeHandle.right, const Offset(0.1, 0), aspect: aspect);
      expect(ratio(wider), closeTo(aspect, 1e-9));
      expect(wider.left, closeTo(start.left, 1e-9));
      expect(wider.center.dy, closeTo(start.center.dy, 1e-9));
      final taller = resizeRect(start, ResizeHandle.bottom, const Offset(0, 0.05), aspect: aspect);
      expect(ratio(taller), closeTo(aspect, 1e-9));
      expect(taller.top, closeTo(start.top, 1e-9));
      expect(taller.center.dx, closeTo(start.center.dx, 1e-9));
    });

    test('is held in by the page and keeps the ratio', () {
      final grown = resizeRect(start, ResizeHandle.bottomRight, const Offset(5, 5), aspect: aspect);
      expect(ratio(grown), closeTo(aspect, 1e-9));
      expect(grown.right, lessThanOrEqualTo(1 + 1e-9));
      expect(grown.bottom, lessThanOrEqualTo(1 + 1e-9));
    });

    test('respects the minimum size', () {
      final shrunk = resizeRect(start, ResizeHandle.bottomRight, const Offset(-5, -5), aspect: aspect, minSize: const Size(0.04, 0.03));
      expect(ratio(shrunk), closeTo(aspect, 1e-9));
      expect(shrunk.width, greaterThanOrEqualTo(0.04 - 1e-9));
      expect(shrunk.height, greaterThanOrEqualTo(0.03 - 1e-9));
    });
  });

  group('hit testing', () {
    const box = Rect.fromLTWH(100, 100, 200, 100);

    test('finds handles, corners first', () {
      expect(hitTestHandle(box, const Offset(100, 100)), ResizeHandle.topLeft);
      expect(hitTestHandle(box, const Offset(300, 200)), ResizeHandle.bottomRight);
      expect(hitTestHandle(box, const Offset(200, 100)), ResizeHandle.top);
      expect(hitTestHandle(box, const Offset(300, 150)), ResizeHandle.right);
      expect(hitTestHandle(box, const Offset(104, 108)), ResizeHandle.topLeft);
      expect(hitTestHandle(box, const Offset(200, 150)), isNull);
      expect(hitTestHandle(box, const Offset(10, 10)), isNull);
    });

    test('a small box offers only corners', () {
      const small = Rect.fromLTWH(100, 100, 40, 30);
      expect(hitTestHandle(small, const Offset(120, 100)), isNull);
      expect(hitTestHandle(small, const Offset(140, 130)), ResizeHandle.bottomRight);
    });

    test('handle positions', () {
      expect(handlePosition(box, ResizeHandle.top), const Offset(200, 100));
      expect(handlePosition(box, ResizeHandle.bottomLeft), const Offset(100, 200));
    });

    test('the top box wins and tolerance grows small ones', () {
      final boxes = <MapEntry<String, Rect>>[
        const MapEntry<String, Rect>('under', Rect.fromLTWH(0, 0, 100, 100)),
        const MapEntry<String, Rect>('over', Rect.fromLTWH(50, 50, 100, 100)),
      ];
      expect(hitTestBoxes(boxes, const Offset(75, 75)), 'over');
      expect(hitTestBoxes(boxes, const Offset(20, 20)), 'under');
      expect(hitTestBoxes(boxes, const Offset(-3, -3)), isNull);
      expect(hitTestBoxes(boxes, const Offset(-3, -3), tolerance: 5), 'under');
    });

    test('pageAtOffset', () {
      const pages = <Rect>[Rect.fromLTWH(0, 0, 100, 100), Rect.fromLTWH(0, 120, 100, 100)];
      expect(pageAtOffset(pages, 50), 0);
      expect(pageAtOffset(pages, 150), 1);
      expect(pageAtOffset(pages, 105), 0);
      expect(pageAtOffset(pages, 115), 1);
      expect(pageAtOffset(const <Rect>[], 5), 0);
    });
  });

  group('placing', () {
    test('placeAt centres and clamps', () {
      expect(placeAt(const Offset(0.5, 0.5), const Size(0.2, 0.1)), rectClose(const Rect.fromLTWH(0.4, 0.45, 0.2, 0.1)));
      expect(placeAt(const Offset(0.99, 0.99), const Size(0.2, 0.1)), rectClose(const Rect.fromLTWH(0.8, 0.9, 0.2, 0.1)));
      expect(placeAt(const Offset(0, 0), const Size(0.2, 0.1)).topLeft, Offset.zero);
    });

    test('rectFromDrag works in any direction and honours the minimum', () {
      expect(rectFromDrag(const Offset(0.5, 0.5), const Offset(0.3, 0.4)), rectClose(const Rect.fromLTRB(0.3, 0.4, 0.5, 0.5)));
      final tiny = rectFromDrag(const Offset(0.5, 0.5), const Offset(0.501, 0.501), minSize: const Size(0.05, 0.02));
      expect(tiny.width, closeTo(0.05, 1e-9));
      expect(tiny.height, closeTo(0.02, 1e-9));
      expect(rectFromDrag(const Offset(0.95, 0.5), const Offset(0.99, 0.51), minSize: const Size(0.1, 0.02)).right, closeTo(1, 1e-9));
    });
  });
}
