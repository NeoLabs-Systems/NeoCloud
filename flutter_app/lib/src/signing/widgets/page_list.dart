import 'dart:math' as math;

import 'package:flutter/gestures.dart' show PointerScaleEvent, PointerScrollEvent, PointerSignalEvent;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart' show HardwareKeyboard, LogicalKeyboardKey;

import '../../theme/palette.dart';

/// The overlay of one page: boxes positioned in a `Stack`. [pageSize] is the
/// page's size on screen in pixels; use `geometry.dart` to map the fractions
/// of a field area onto it. The page index is 0-based.
typedef PageOverlayBuilder = List<Widget> Function(BuildContext context, int pageIndex, Size pageSize);

/// What a page shows under its overlay (the PDF image, or a stand-in in
/// tests). Fills a box of [pageSize].
typedef PageContentBuilder = Widget Function(BuildContext context, int pageIndex, Size pageSize);

/// Scrolls and zooms a [SigningPageList] from outside, and tells which page
/// is in view.
class SigningPageController extends ChangeNotifier {
  SigningPageController({double zoom = 1}) : _zoom = zoom.clamp(minZoom, maxZoom).toDouble();

  static const double minZoom = 0.5;
  static const double maxZoom = 4;

  final ScrollController scroll = ScrollController();
  final ScrollController horizontalScroll = ScrollController();

  double _zoom;
  int _currentPage = 0;
  List<double> _offsets = const <double>[];
  List<Size> _pixelSizes = const <Size>[];

  /// 1 = the page as wide as the view (up to the maximum page width).
  double get zoom => _zoom;

  /// The page at the upper third of the view (0-based).
  int get currentPage => _currentPage;

  int get pageCount => _pixelSizes.length;

  /// A page's size on screen right now; null before the first layout.
  Size? pageSize(int index) => index >= 0 && index < _pixelSizes.length ? _pixelSizes[index] : null;

  void setZoom(double value) {
    final next = value.clamp(minZoom, maxZoom).toDouble();
    if (next == _zoom) return;
    _zoom = next;
    notifyListeners();
  }

  void zoomBy(double factor) => setZoom(_zoom * factor);

  void resetZoom() => setZoom(1);

  /// Scrolls so page [index] starts at the top of the view.
  Future<void> scrollToPage(int index, {bool animate = true}) async {
    if (index < 0 || index >= _offsets.length || !scroll.hasClients) return;
    await _scrollTo(_offsets[index], animate);
  }

  /// Scrolls so the box [fractions] of page [page] sits [alignment] of the
  /// way down the view (0 = top, 1 = bottom).
  Future<void> scrollToRect(int page, Rect fractions, {double alignment = 0.35, bool animate = true}) async {
    if (page < 0 || page >= _offsets.length || !scroll.hasClients) return;
    final size = _pixelSizes[page];
    final target = _offsets[page] + fractions.center.dy * size.height - scroll.position.viewportDimension * alignment;
    await _scrollTo(target, animate);
  }

  Future<void> _scrollTo(double offset, bool animate) async {
    final clamped = offset.clamp(0.0, scroll.position.maxScrollExtent).toDouble();
    if (animate && !(WidgetsBinding.instance.platformDispatcher.accessibilityFeatures.disableAnimations)) {
      await scroll.animateTo(clamped, duration: const Duration(milliseconds: 280), curve: Curves.easeOutCubic);
    } else {
      scroll.jumpTo(clamped);
    }
  }

  void _layout(List<double> offsets, List<Size> sizes) {
    _offsets = offsets;
    _pixelSizes = sizes;
  }

  void _scrolled() {
    if (!scroll.hasClients || _offsets.isEmpty) return;
    final probe = scroll.offset + scroll.position.viewportDimension / 3;
    var page = 0;
    for (var index = 0; index < _offsets.length; index++) {
      if (_offsets[index] <= probe) page = index;
    }
    if (page != _currentPage) {
      _currentPage = page;
      notifyListeners();
    }
  }

  @override
  void dispose() {
    scroll.dispose();
    horizontalScroll.dispose();
    super.dispose();
  }
}

/// The pages of a document one under the other: lazy (only the pages in view
/// are built), zoomable, each with an overlay `Stack` on top of its content.
///
/// [pageSizes] are the pages' sizes in any unit (PDF points); only their
/// ratios matter. The width of a page is the view's width minus [padding],
/// at most [maxPageWidth], times the controller's zoom.
class SigningPageList extends StatefulWidget {
  const SigningPageList({
    super.key,
    required this.pageSizes,
    required this.pageContent,
    this.overlayBuilder,
    this.controller,
    this.padding = const EdgeInsets.symmetric(horizontal: 16, vertical: 16),
    this.pageGap = 16,
    this.maxPageWidth = 1000,
    this.zoomWithWheel = true,
  });

  final List<Size> pageSizes;
  final PageContentBuilder pageContent;
  final PageOverlayBuilder? overlayBuilder;
  final SigningPageController? controller;
  final EdgeInsets padding;
  final double pageGap;
  final double maxPageWidth;

  /// Ctrl/Cmd + wheel and trackpad pinch zoom the pages.
  final bool zoomWithWheel;

  @override
  State<SigningPageList> createState() => _SigningPageListState();
}

class _SigningPageListState extends State<SigningPageList> {
  SigningPageController? _owned;

  SigningPageController get _controller => widget.controller ?? (_owned ??= SigningPageController());

  @override
  void initState() {
    super.initState();
    _controller.addListener(_zoomed);
    _controller.scroll.addListener(_controller._scrolled);
  }

  @override
  void didUpdateWidget(covariant SigningPageList old) {
    super.didUpdateWidget(old);
    if (old.controller != widget.controller) {
      (old.controller ?? _owned)?.removeListener(_zoomed);
      (old.controller ?? _owned)?.scroll.removeListener((old.controller ?? _owned)!._scrolled);
      _controller.addListener(_zoomed);
      _controller.scroll.addListener(_controller._scrolled);
    }
  }

  @override
  void dispose() {
    _controller.removeListener(_zoomed);
    _controller.scroll.removeListener(_controller._scrolled);
    _owned?.dispose();
    super.dispose();
  }

  late double _lastZoom = _controller.zoom;

  void _zoomed() {
    final controller = _controller;
    final ratio = controller.zoom / _lastZoom;
    _lastZoom = controller.zoom;
    if (ratio != 1 && controller.scroll.hasClients) {
      // The pages grow with the zoom: keep the same place in view.
      final target = controller.scroll.offset * ratio;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (controller.scroll.hasClients) controller.scroll.jumpTo(target.clamp(0.0, controller.scroll.position.maxScrollExtent).toDouble());
      });
    }
    if (mounted) setState(() {});
  }

  void _onSignal(PointerSignalEvent event) {
    if (!widget.zoomWithWheel) return;
    if (event is PointerScaleEvent) {
      _controller.zoomBy(event.scale);
    } else if (event is PointerScrollEvent) {
      final keys = HardwareKeyboard.instance;
      final modifier = keys.isControlPressed || keys.isMetaPressed || keys.logicalKeysPressed.contains(LogicalKeyboardKey.controlLeft);
      if (modifier) _controller.zoomBy(event.scrollDelta.dy < 0 ? 1.1 : 1 / 1.1);
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = paletteFor(Theme.of(context).brightness);
    final controller = _controller;
    return LayoutBuilder(
      builder: (context, constraints) {
        final available = math.max(1.0, constraints.maxWidth - widget.padding.horizontal);
        final fitted = math.min(available, widget.maxPageWidth);
        final pageWidth = fitted * controller.zoom;
        final contentWidth = math.max(constraints.maxWidth, pageWidth + widget.padding.horizontal);

        final sizes = <Size>[
          for (final page in widget.pageSizes) Size(pageWidth, page.width <= 0 ? pageWidth : pageWidth * page.height / page.width),
        ];
        final offsets = <double>[];
        var running = widget.padding.top;
        for (final size in sizes) {
          offsets.add(running);
          running += size.height + widget.pageGap;
        }
        controller._layout(offsets, sizes);

        double extentOf(int index) =>
            sizes[index].height + (index == 0 ? widget.padding.top : 0) + (index == sizes.length - 1 ? widget.padding.bottom : widget.pageGap);

        Widget list = ListView.builder(
          controller: controller.scroll,
          itemCount: sizes.length,
          itemExtentBuilder: (index, _) => extentOf(index),
          itemBuilder: (context, index) {
            final size = sizes[index];
            return Padding(
              padding: EdgeInsets.only(
                top: index == 0 ? widget.padding.top : 0,
                bottom: index == sizes.length - 1 ? widget.padding.bottom : widget.pageGap,
                left: (contentWidth - size.width) / 2,
                right: (contentWidth - size.width) / 2,
              ),
              child: _PageFrame(
                key: ValueKey<int>(index),
                size: size,
                palette: palette,
                content: widget.pageContent(context, index, size),
                overlay: widget.overlayBuilder?.call(context, index, size) ?? const <Widget>[],
              ),
            );
          },
        );
        list = SizedBox(width: contentWidth, child: list);
        if (contentWidth > constraints.maxWidth) {
          list = SingleChildScrollView(controller: controller.horizontalScroll, scrollDirection: Axis.horizontal, child: list);
        }
        return Listener(onPointerSignal: _onSignal, child: list);
      },
    );
  }
}

/// A page: white sheet with a soft edge, content, then the overlay.
class _PageFrame extends StatelessWidget {
  const _PageFrame({super.key, required this.size, required this.palette, required this.content, required this.overlay});

  final Size size;
  final NeoCloudPalette palette;
  final Widget content;
  final List<Widget> overlay;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size.width,
      height: size.height,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: Colors.white,
          boxShadow: <BoxShadow>[BoxShadow(color: palette.shadow, blurRadius: 12, offset: const Offset(0, 3))],
        ),
        // Paper is white in every theme: whatever is written or drawn on it
        // defaults to dark ink, never to the dark theme's light text.
        child: DefaultTextStyle.merge(
          style: const TextStyle(color: Color(0xDD000000)),
          child: IconTheme.merge(
            data: const IconThemeData(color: Color(0xDD000000)),
            child: Stack(
              clipBehavior: Clip.none,
              fit: StackFit.expand,
              children: <Widget>[
                RepaintBoundary(child: content),
                ...overlay,
              ],
            ),
          ),
        ),
      ),
    );
  }
}
