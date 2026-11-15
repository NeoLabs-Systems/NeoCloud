import 'dart:async';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/signing.dart';

Future<ui.Image> decode(Uint8List png) async {
  final codec = await ui.instantiateImageCodec(png);
  return (await codec.getNextFrame()).image;
}

/// Alpha of the pixel at ([x], [y]).
Future<int> alphaAt(ui.Image image, int x, int y) async {
  final data = (await image.toByteData(format: ui.ImageByteFormat.rawRgba))!;
  return data.getUint8((y * image.width + x) * 4 + 3);
}

Future<int> inkPixels(ui.Image image) async {
  final data = (await image.toByteData(format: ui.ImageByteFormat.rawRgba))!;
  var count = 0;
  for (var offset = 3; offset < data.lengthInBytes; offset += 4) {
    if (data.getUint8(offset) > 0) count++;
  }
  return count;
}

bool isPng(Uint8List bytes) => bytes.length > 8 && bytes[0] == 0x89 && bytes[1] == 0x50 && bytes[2] == 0x4e && bytes[3] == 0x47;

Widget host(Widget child, {Size size = const Size(700, 900)}) => MaterialApp(
  home: Scaffold(body: Center(child: SizedBox(width: size.width, height: size.height, child: child))),
);

void main() {
  group('SignaturePadController', () {
    test('tracks strokes, undo and clear', () {
      final controller = SignaturePadController();
      expect(controller.isEmpty, isTrue);
      controller.beginStroke(const Offset(10, 10));
      controller.extendStroke(const Offset(10.2, 10.2));
      expect(controller.strokes.single, hasLength(1), reason: 'sub-pixel moves are dropped');
      controller.extendStroke(const Offset(30, 20));
      controller.beginStroke(const Offset(50, 50));
      expect(controller.strokes, hasLength(2));
      controller.undo();
      expect(controller.strokes, hasLength(1));
      controller.clear();
      expect(controller.isEmpty, isTrue);
      controller.undo();
      controller.extendStroke(const Offset(1, 1));
      expect(controller.isEmpty, isTrue);
    });

    testWidgets('exports a transparent PNG trimmed to the ink', (tester) async {
      final controller = SignaturePadController();
      await tester.runAsync(() async {
        expect(await controller.exportPng(), isNull, reason: 'nothing drawn yet');
        controller.beginStroke(const Offset(50, 50));
        for (var step = 1; step <= 10; step++) {
          controller.extendStroke(Offset(50 + step * 10, 50 + step * 3));
        }
        final png = await controller.exportPng();
        expect(png, isNotNull);
        expect(isPng(png!), isTrue);
        final image = await decode(png);
        // 100 x 30 px of ink plus padding, drawn at 3x.
        expect(image.width, inInclusiveRange(300, 400));
        expect(image.height, inInclusiveRange(120, 220));
        expect(image.width, lessThanOrEqualTo(maxSignatureWidth));
        expect(await alphaAt(image, 0, 0), 0, reason: 'background is transparent');
        expect(await inkPixels(image), greaterThan(200));
      });
    });

    testWidgets('a huge signature is scaled down to the limit', (tester) async {
      final controller = SignaturePadController();
      await tester.runAsync(() async {
        controller.beginStroke(const Offset(0, 0));
        controller.extendStroke(const Offset(2000, 40));
        controller.extendStroke(const Offset(4000, 0));
        final image = await decode((await controller.exportPng())!);
        expect(image.width, lessThanOrEqualTo(maxSignatureWidth));
        expect(image.height, lessThanOrEqualTo(maxSignatureHeight));
      });
    });

    testWidgets('a single tap is a dot', (tester) async {
      final controller = SignaturePadController()..beginStroke(const Offset(20, 20));
      await tester.runAsync(() async {
        final png = await controller.exportPng();
        expect(png, isNotNull);
        expect(await inkPixels(await decode(png!)), greaterThan(0));
      });
    });
  });

  group('SignaturePad widget', () {
    testWidgets('draws what the pointer does and shows the hint until then', (tester) async {
      final controller = SignaturePadController();
      await tester.pumpWidget(host(SignaturePad(controller: controller), size: const Size(400, 200)));
      expect(controller.isEmpty, isTrue);
      final gesture = await tester.startGesture(tester.getCenter(find.byType(SignaturePad)));
      await gesture.moveBy(const Offset(40, 10));
      await gesture.moveBy(const Offset(40, -10));
      await gesture.up();
      await tester.pump();
      expect(controller.strokes, hasLength(1));
      expect(controller.strokes.single.length, greaterThanOrEqualTo(3));
      // A second finger is ignored while the first is down.
      final first = await tester.startGesture(tester.getCenter(find.byType(SignaturePad)) + const Offset(-100, 40), pointer: 1);
      final second = await tester.startGesture(tester.getCenter(find.byType(SignaturePad)) + const Offset(-90, 50), pointer: 2);
      await second.moveBy(const Offset(30, 0));
      await first.moveBy(const Offset(30, 0));
      await first.up();
      await second.up();
      expect(controller.strokes, hasLength(2));
    });
  });

  group('typed signature', () {
    testWidgets('renders text into a PNG for every bundled font', (tester) async {
      await tester.runAsync(() async {
        for (final font in signatureFonts) {
          final png = await renderTypedPng('Anna Schmidt', fontFamily: font.family);
          expect(png, isNotNull, reason: font.family);
          expect(isPng(png!), isTrue);
          final image = await decode(png);
          expect(image.width, lessThanOrEqualTo(maxSignatureWidth));
          expect(image.height, lessThanOrEqualTo(maxSignatureHeight));
          expect(await alphaAt(image, 0, 0), 0);
          expect(await inkPixels(image), greaterThan(100));
        }
        expect(await renderTypedPng('   ', fontFamily: 'Caveat'), isNull);
      });
    });

    testWidgets('the controller exports with the chosen style', (tester) async {
      final controller = TypedSignatureController(text: 'Bob');
      expect(controller.isEmpty, isFalse);
      controller.fontIndex = 2;
      expect(controller.font.family, 'GreatVibes');
      controller.fontIndex = 99;
      expect(controller.fontIndex, 2);
      await tester.runAsync(() async => expect(await controller.exportPng(), isNotNull));
      controller.text = ' ';
      expect(controller.isEmpty, isTrue);
    });

    testWidgets('the editor previews each style and picks one', (tester) async {
      final controller = TypedSignatureController();
      await tester.pumpWidget(host(Material(child: TypedSignatureEditor(controller: controller, autofocus: false))));
      await tester.enterText(find.byType(TextField), 'Carla');
      await tester.pump();
      expect(controller.text, 'Carla');
      expect(find.text('Carla'), findsNWidgets(signatureFonts.length + 1));
      await tester.tap(find.byKey(const ValueKey<String>('signature-style-3')));
      expect(controller.fontIndex, 3);
    });
  });

  group('SignaturePanel', () {
    Future<SignatureResult?> run(WidgetTester tester, Future<void> Function() act, {Future<Uint8List?> Function()? pick, bool offerInitials = false}) async {
      SignatureResult? result;
      await tester.pumpWidget(
        host(
          Material(
            child: SingleChildScrollView(
              child: SignaturePanel(
                offerInitials: offerInitials,
                initialName: 'Dana',
                pickImage: pick ?? () async => null,
                onCancel: () {},
                onResult: (value) => result = value,
              ),
            ),
          ),
        ),
      );
      await act();
      return result;
    }

    testWidgets('Apply is off until something is drawn, then returns a drawn PNG', (tester) async {
      SignatureResult? result;
      await tester.pumpWidget(
        host(Material(child: SingleChildScrollView(child: SignaturePanel(onCancel: () {}, onResult: (value) => result = value)))),
      );
      final apply = find.widgetWithText(FilledButton, 'Apply');
      expect(tester.widget<FilledButton>(apply).onPressed, isNull);
      final pad = find.byType(SignaturePad);
      final gesture = await tester.startGesture(tester.getCenter(pad));
      await gesture.moveBy(const Offset(60, 20));
      await gesture.moveBy(const Offset(40, -30));
      await gesture.up();
      await tester.pump();
      expect(tester.widget<FilledButton>(apply).onPressed, isNotNull);
      await tester.runAsync(() async {
        await tester.tap(apply);
        await Future<void>.delayed(const Duration(milliseconds: 300));
      });
      await tester.pump();
      expect(result, isNotNull);
      expect(result!.method, SignatureMethod.drawn);
      expect(isPng(result!.png), isTrue);
      expect(result!.typedText, isNull);
    });

    testWidgets('the Type tab returns a typed PNG with the name', (tester) async {
      final result = await run(tester, () async {
        await tester.tap(find.text('Type'));
        await tester.pumpAndSettle();
        expect(find.widgetWithText(TextField, 'Dana'), findsOneWidget);
        await tester.runAsync(() async {
          await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
          await Future<void>.delayed(const Duration(milliseconds: 300));
        });
        await tester.pump();
      });
      expect(result, isNotNull);
      expect(result!.method, SignatureMethod.typed);
      expect(result.typedText, 'Dana');
    });

    testWidgets('the Upload tab takes a picked picture', (tester) async {
      final source = await tester.runAsync(() async {
        final controller = SignaturePadController()
          ..beginStroke(const Offset(5, 5))
          ..extendStroke(const Offset(80, 30));
        return controller.exportPng();
      });
      final result = await run(
        tester,
        () async {
          await tester.tap(find.text('Upload'));
          await tester.pumpAndSettle();
          await tester.runAsync(() async {
            await tester.tap(find.text('Choose a picture'));
            await Future<void>.delayed(const Duration(milliseconds: 300));
          });
          await tester.pump();
          await tester.runAsync(() async {
            await tester.tap(find.widgetWithText(FilledButton, 'Apply'));
            await Future<void>.delayed(const Duration(milliseconds: 100));
          });
          await tester.pump();
        },
        pick: () async => source,
      );
      expect(result, isNotNull);
      expect(result!.method, SignatureMethod.uploaded);
      expect(isPng(result.png), isTrue);
    });

    testWidgets('an unreadable picture shows an error and stays off', (tester) async {
      await run(
        tester,
        () async {
          await tester.tap(find.text('Upload'));
          await tester.pumpAndSettle();
          await tester.runAsync(() async {
            await tester.tap(find.text('Choose a picture'));
            await Future<void>.delayed(const Duration(milliseconds: 300));
          });
          await tester.pump();
          expect(find.textContaining("couldn't be read"), findsOneWidget);
          expect(tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Apply')).onPressed, isNull);
        },
        pick: () async => Uint8List.fromList(<int>[1, 2, 3]),
      );
    });

    testWidgets('offers initials reuse only when asked', (tester) async {
      await run(tester, () async => expect(find.text('Use this for my initials too'), findsNothing));
      await run(tester, () async => expect(find.text('Use this for my initials too'), findsOneWidget), offerInitials: true);
    });
  });

  group('FieldBox', () {
    Widget box({
      bool selected = false,
      bool hasError = false,
      bool readonly = false,
      bool filled = false,
      bool highlighted = false,
      bool hovered = false,
      void Function(ResizeHandle, Offset)? onResize,
      ValueChanged<ResizeHandle>? onStart,
      VoidCallback? onEnd,
      String? label = 'Signature',
      Widget? child,
    }) => host(
      Padding(
        padding: const EdgeInsets.all(40),
        child: FieldBox(
          color: partyColor(1),
          icon: Icons.draw_rounded,
          label: label,
          selected: selected,
          hasError: hasError,
          readonly: readonly,
          filled: filled,
          highlighted: highlighted,
          hovered: hovered,
          onResizeUpdate: onResize,
          onResizeStart: onStart,
          onResizeEnd: onEnd,
          child: child,
        ),
      ),
      size: const Size(300, 160),
    );

    BoxDecoration decoration(WidgetTester tester) {
      final boxes = tester.widgetList<DecoratedBox>(find.descendant(of: find.byType(FieldBox), matching: find.byType(DecoratedBox)));
      return boxes.map((b) => b.decoration).whereType<BoxDecoration>().firstWhere((d) => d.border != null && d.borderRadius == BorderRadius.circular(3));
    }

    testWidgets('shows its label chip and icon', (tester) async {
      await tester.pumpWidget(box());
      expect(find.text('Signature'), findsOneWidget);
      expect(find.byIcon(Icons.draw_rounded), findsWidgets);
      await tester.pumpWidget(box(label: null));
      expect(find.text('Signature'), findsNothing);
    });

    testWidgets('unselected has no handles; selected has all eight', (tester) async {
      await tester.pumpWidget(box(onResize: (_, _) {}));
      expect(find.byKey(const ValueKey<String>('field-handle-topLeft')), findsNothing);
      await tester.pumpWidget(box(selected: true, onResize: (_, _) {}));
      for (final handle in ResizeHandle.values) {
        expect(find.byKey(ValueKey<String>('field-handle-${handle.name}')), findsOneWidget, reason: handle.name);
      }
    });

    testWidgets('readonly or without a resize callback shows no handles', (tester) async {
      await tester.pumpWidget(box(selected: true, readonly: true, onResize: (_, _) {}));
      expect(find.byKey(const ValueKey<String>('field-handle-right')), findsNothing);
      await tester.pumpWidget(box(selected: true));
      expect(find.byKey(const ValueKey<String>('field-handle-right')), findsNothing);
    });

    testWidgets('dragging a handle reports the handle and the delta', (tester) async {
      final updates = <(ResizeHandle, Offset)>[];
      ResizeHandle? started;
      var ended = 0;
      await tester.pumpWidget(box(selected: true, onResize: (h, d) => updates.add((h, d)), onStart: (h) => started = h, onEnd: () => ended++));
      final corner = tester.getBottomRight(find.byType(FieldBox)) - const Offset(4, 4);
      await tester.dragFrom(corner, const Offset(30, 20));
      await tester.pump();
      expect(started, ResizeHandle.bottomRight);
      expect(ended, 1);
      expect(updates, isNotEmpty);
      expect(updates.every((u) => u.$1 == ResizeHandle.bottomRight), isTrue);
      final total = updates.fold<Offset>(Offset.zero, (sum, u) => sum + u.$2);
      expect(total.dx, closeTo(30, 20));
      expect(total.dy, greaterThan(0));
    });

    testWidgets('states change the border and the tint', (tester) async {
      final color = partyColor(1);
      await tester.pumpWidget(box());
      final normal = decoration(tester);
      expect((normal.border! as Border).top.width, 1.2);
      expect((normal.border! as Border).top.color, color);

      await tester.pumpWidget(box(selected: true));
      expect((decoration(tester).border! as Border).top.width, 2);
      expect(decoration(tester).color!.a, greaterThan(normal.color!.a));

      await tester.pumpWidget(box(hovered: true));
      expect(decoration(tester).color!.a, greaterThan(normal.color!.a));

      await tester.pumpWidget(box(filled: true));
      expect(decoration(tester).color!.a, lessThan(normal.color!.a));

      await tester.pumpWidget(box(hasError: true));
      final error = decoration(tester);
      expect((error.border! as Border).top.color, isNot(color));
      expect((error.border! as Border).top.width, 2);

      await tester.pumpWidget(box(readonly: true));
      expect((decoration(tester).border! as Border).top.color.a, lessThan(1));

      await tester.pumpWidget(box(highlighted: true));
      expect(decoration(tester).boxShadow, isNotEmpty);
    });

    testWidgets('shows its content instead of the icon', (tester) async {
      await tester.pumpWidget(box(child: const Text('Anna'), filled: true));
      expect(find.text('Anna'), findsOneWidget);
    });
  });

  group('party colours', () {
    test('are distinct, wrap darker past the palette, and round-trip hex', () {
      expect(partyPalette.toSet(), hasLength(partyPalette.length));
      expect(partyColor(0), partyPalette.first);
      expect(partyColor(partyPalette.length), isNot(partyPalette.first));
      expect(partyColor(-3), partyPalette.first);
      expect(parseHexColor(colorToHex(partyColor(3))), partyColor(3));
      expect(colorToHex(const Color(0xFF0A0B0C)), '#0a0b0c');
      expect(parseHexColor('nope'), isNull);
      expect(parseHexColor(null), isNull);
      expect(resolvePartyColor('#ff0000', 4), const Color(0xFFFF0000));
      expect(resolvePartyColor('', 4), partyColor(4));
    });

    test('text on a colour is readable', () {
      for (final color in partyPalette) {
        final on = onPartyColor(color);
        expect(on == Colors.white || on == const Color(0xFF111111), isTrue);
      }
    });
  });

  group('SigningPageList', () {
    const sizes = <Size>[Size(595, 842), Size(842, 595), Size(595, 842)];

    Widget list({SigningPageController? controller, PageOverlayBuilder? overlay, Size size = const Size(700, 600)}) => host(
      SigningPageList(
        pageSizes: sizes,
        controller: controller,
        pageContent: (context, index, size) => ColoredBox(color: Colors.white, child: Center(child: Text('content $index'))),
        overlayBuilder: overlay,
      ),
      size: size,
    );

    testWidgets('lays pages out at the view width in their own ratio, lazily', (tester) async {
      final seen = <int, Size>{};
      await tester.pumpWidget(
        list(
          overlay: (context, index, size) {
            seen[index] = size;
            return const <Widget>[];
          },
        ),
      );
      expect(seen[0]!.width, 700 - 32);
      expect(seen[0]!.height, closeTo((700 - 32) * 842 / 595, 1e-6));
      expect(find.text('content 0'), findsOneWidget);
      expect(find.text('content 2'), findsNothing, reason: 'page 3 is far below the fold');
    });

    testWidgets('overlay boxes sit where the fractions say', (tester) async {
      await tester.pumpWidget(
        list(
          overlay: (context, index, size) => index != 0
              ? const <Widget>[]
              : <Widget>[
                  Positioned.fromRect(
                    rect: fractionToPixels(const Rect.fromLTWH(0.5, 0.25, 0.25, 0.1), size),
                    child: Container(key: const ValueKey<String>('box'), color: Colors.red),
                  ),
                ],
        ),
      );
      final page = tester.getRect(find.ancestor(of: find.text('content 0'), matching: find.byType(ColoredBox)).first);
      final box = tester.getRect(find.byKey(const ValueKey<String>('box')));
      expect((box.left - page.left) / page.width, closeTo(0.5, 0.01));
      expect((box.top - page.top) / page.height, closeTo(0.25, 0.01));
      expect(box.width / page.width, closeTo(0.25, 0.01));
    });

    testWidgets('the controller scrolls to a page and reports the current one', (tester) async {
      final controller = SigningPageController();
      await tester.pumpWidget(list(controller: controller));
      expect(controller.currentPage, 0);
      expect(controller.pageCount, 3);
      expect(controller.pageSize(1)!.width, 700 - 32);
      expect(controller.pageSize(9), isNull);
      unawaited(controller.scrollToPage(2, animate: false));
      await tester.pumpAndSettle();
      expect(controller.currentPage, 2);
      expect(find.text('content 2'), findsOneWidget);
      unawaited(controller.scrollToPage(0, animate: false));
      await tester.pumpAndSettle();
      expect(controller.currentPage, 0);
    });

    testWidgets('scrollToRect brings a box into view', (tester) async {
      final controller = SigningPageController();
      await tester.pumpWidget(
        list(
          controller: controller,
          overlay: (context, index, size) => index != 2
              ? const <Widget>[]
              : <Widget>[
                  Positioned.fromRect(
                    rect: fractionToPixels(const Rect.fromLTWH(0.1, 0.8, 0.3, 0.05), size),
                    child: Container(key: const ValueKey<String>('far'), color: Colors.red),
                  ),
                ],
        ),
      );
      expect(find.byKey(const ValueKey<String>('far')), findsNothing);
      unawaited(controller.scrollToRect(2, const Rect.fromLTWH(0.1, 0.8, 0.3, 0.05), animate: false));
      await tester.pumpAndSettle();
      final box = tester.getRect(find.byKey(const ValueKey<String>('far')));
      expect(box.top, greaterThan(0));
      expect(box.bottom, lessThan(900));
    });

    testWidgets('zoom widens the pages and scrolls sideways', (tester) async {
      final controller = SigningPageController();
      await tester.pumpWidget(list(controller: controller));
      final before = controller.pageSize(0)!;
      controller.setZoom(2);
      await tester.pumpAndSettle();
      expect(controller.pageSize(0)!.width, closeTo(before.width * 2, 1e-6));
      expect(find.byType(SingleChildScrollView), findsOneWidget);
      controller.setZoom(100);
      expect(controller.zoom, SigningPageController.maxZoom);
      controller.setZoom(0.01);
      expect(controller.zoom, SigningPageController.minZoom);
      controller.resetZoom();
      await tester.pumpAndSettle();
      expect(controller.pageSize(0)!.width, closeTo(before.width, 1e-6));
      expect(find.byType(SingleChildScrollView), findsNothing);
    });

    testWidgets('a wide window caps the page width', (tester) async {
      final controller = SigningPageController();
      await tester.pumpWidget(
        host(
          SigningPageList(
            pageSizes: sizes,
            controller: controller,
            maxPageWidth: 500,
            pageContent: (context, index, size) => const SizedBox(),
          ),
          size: const Size(1200, 600),
        ),
      );
      expect(controller.pageSize(0)!.width, 500);
    });
  });
}
