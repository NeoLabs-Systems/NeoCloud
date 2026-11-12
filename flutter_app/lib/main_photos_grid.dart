part of 'main.dart';

// The Photos grid. Every row's height is known from the day counts
// (PhotoGridLayout), so the list is one lazily built ListView with fixed
// extents: no measuring, no jumps, and the scrubber can go anywhere at
// once. Thumbnails come from PhotoThumbnailCache; rows just beyond the
// screen are built ahead (cacheExtent) and the next screens are prefetched
// in the direction of the scroll.

class PhotoGrid extends StatefulWidget {
  const PhotoGrid({
    super.key,
    required this.timeline,
    required this.thumbnails,
    required this.selected,
    required this.onOpen,
    required this.onToggle,
    required this.onSelect,
    required this.onRefresh,
    required this.onZoom,
  });

  final PhotoTimeline timeline;
  final PhotoThumbnailCache thumbnails;
  final Set<String> selected;
  final ValueChanged<int> onOpen;
  final ValueChanged<int> onToggle;
  final ValueChanged<Set<String>> onSelect;
  final Future<void> Function() onRefresh;
  final ValueChanged<PhotoZoom> onZoom;

  @override
  State<PhotoGrid> createState() => _PhotoGridState();
}

class _PhotoGridState extends State<PhotoGrid> {
  late final ScrollController _scroll = ScrollController(initialScrollOffset: widget.timeline.savedOffset);
  PhotoGridLayout? _layout;
  List<PhotoDayCount>? _layoutDays;
  double _layoutWidth = 0;
  double _viewport = 0;

  /// The photo to keep in view after the zoom changed.
  int? _zoomAnchor;

  // Pinching: the two pointers and their distance when the pinch started.
  final Map<int, Offset> _pointers = <int, Offset>{};
  double? _pinchStart;
  bool _pinchUsed = false;

  // Drag-select: where it started, what was selected before, and the
  // finger's last place (auto-scrolling near the edges re-reads it).
  int? _dragAnchor;
  Set<String> _dragBase = const <String>{};
  Offset? _dragAt;
  Timer? _autoScroll;

  // The scrubber shows while scrolling and while held.
  bool _scrubberShown = false;
  bool _scrubbing = false;
  Timer? _scrubberTimer;
  double _lastOffset = 0;

  PhotoTimeline get timeline => widget.timeline;
  bool get _selecting => widget.selected.isNotEmpty;

  @override
  void dispose() {
    if (_scroll.hasClients) timeline.savedOffset = _scroll.offset;
    _autoScroll?.cancel();
    _scrubberTimer?.cancel();
    _scroll.dispose();
    super.dispose();
  }

  PhotoGridLayout _layoutFor(double width) {
    final days = timeline.summary.days;
    final layout = _layout;
    if (layout != null && identical(days, _layoutDays) && width == _layoutWidth && layout.zoom == timeline.zoom) return layout;
    final anchor = layout != null && _scroll.hasClients ? layout.itemAtOffset(_scroll.offset + 1) : null;
    final next = PhotoGridLayout.build(days, zoom: timeline.zoom, width: width);
    _layout = next;
    _layoutDays = days;
    _layoutWidth = width;
    // Keep the same photo at the top when the zoom or the width changes.
    _zoomAnchor ??= anchor;
    final keep = _zoomAnchor;
    if (keep != null && layout != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_scroll.hasClients || _layout != next) return;
        _zoomAnchor = null;
        final target = next.rows.isEmpty ? 0.0 : next.rows[math.max(0, next.rowOfItem(keep) - 1)].offset;
        _scroll.jumpTo(target.clamp(0.0, _scroll.position.maxScrollExtent));
      });
    } else {
      _zoomAnchor = null;
    }
    return next;
  }

  void _zoomTo(PhotoZoom? zoom) {
    if (zoom == null || zoom == timeline.zoom) return;
    final layout = _layout;
    if (layout != null && _scroll.hasClients) _zoomAnchor = layout.itemAtOffset(_scroll.offset + 1);
    widget.onZoom(zoom);
  }

  // --- Pinch and ctrl + wheel ------------------------------------------------------

  void _pointerDown(PointerDownEvent event) {
    _pointers[event.pointer] = event.localPosition;
    if (_pointers.length == 2) {
      final points = _pointers.values.toList();
      _pinchStart = (points[0] - points[1]).distance;
      _pinchUsed = false;
    }
  }

  void _pointerMove(PointerMoveEvent event) {
    if (!_pointers.containsKey(event.pointer)) return;
    _pointers[event.pointer] = event.localPosition;
    final start = _pinchStart;
    if (_pointers.length != 2 || start == null || start <= 0 || _pinchUsed) return;
    final points = _pointers.values.toList();
    final ratio = (points[0] - points[1]).distance / start;
    if (ratio > 1.25) {
      _pinchUsed = true;
      _zoomTo(timeline.zoom.bigger);
    } else if (ratio < 0.8) {
      _pinchUsed = true;
      _zoomTo(timeline.zoom.denser);
    }
  }

  void _pointerUp(PointerEvent event) {
    _pointers.remove(event.pointer);
    if (_pointers.length < 2) _pinchStart = null;
  }

  void _pointerSignal(PointerSignalEvent event) {
    if (event is PointerScaleEvent) {
      if (event.scale > 1.05) _zoomTo(timeline.zoom.bigger);
      if (event.scale < 0.95) _zoomTo(timeline.zoom.denser);
    } else if (event is PointerScrollEvent && HardwareKeyboard.instance.isControlPressed) {
      _zoomTo(event.scrollDelta.dy < 0 ? timeline.zoom.bigger : timeline.zoom.denser);
    }
  }

  // --- Scrolling: loading ahead and the scrubber ------------------------------------

  bool _onScroll(ScrollNotification notification) {
    if (notification.depth != 0) return false;
    final layout = _layout;
    if (layout == null) return false;
    final offset = notification.metrics.pixels;
    final down = offset >= _lastOffset;
    _lastOffset = offset;
    _prefetch(layout, offset, notification.metrics.viewportDimension, down);
    if (notification is ScrollUpdateNotification && layout.totalExtent > _viewport * 4) _showScrubber();
    return false;
  }

  /// The next two screens in the direction of the scroll: their page of
  /// photos and their thumbnails, behind what is on screen.
  void _prefetch(PhotoGridLayout layout, double offset, double viewport, bool down) {
    final first = layout.itemAtOffset(offset);
    final last = layout.itemAtOffset(offset + viewport);
    final span = math.max(layout.columns * 4, last - first);
    final from = down ? last : math.max(0, first - span * 2);
    final to = down ? math.min(layout.totalItems, last + span * 2) : first;
    timeline.ensureLoaded(down ? to - 1 : from);
    for (var index = from; index < to; index += 1) {
      final item = timeline.itemAt(index);
      if (item != null && !item.isVideo) widget.thumbnails.prefetch(item.id, item.etag);
    }
  }

  void _showScrubber() {
    _scrubberTimer?.cancel();
    if (!_scrubberShown) setState(() => _scrubberShown = true);
    _scrubberTimer = Timer(const Duration(milliseconds: 1500), () {
      if (mounted && !_scrubbing) setState(() => _scrubberShown = false);
    });
  }

  void _scrubTo(double fraction) {
    if (!_scroll.hasClients) return;
    final max = _scroll.position.maxScrollExtent;
    _scroll.jumpTo((fraction.clamp(0.0, 1.0) * max).toDouble());
  }

  // --- Selecting by long press and drag -----------------------------------------------

  int? _itemAt(Offset local) => _layout?.itemAt(local.dx, local.dy + (_scroll.hasClients ? _scroll.offset : 0));

  void _dragStart(LongPressStartDetails details) {
    final index = _itemAt(details.localPosition);
    final item = index == null ? null : timeline.itemAt(index);
    if (index == null || item == null) return;
    HapticFeedback.selectionClick();
    _dragAnchor = index;
    _dragAt = details.localPosition;
    // Starting on a selected photo deselects the range instead.
    final adding = !widget.selected.contains(item.id);
    _dragBase = Set<String>.of(widget.selected);
    _dragAdding = adding;
    _applyDrag(index);
  }

  bool _dragAdding = true;

  void _applyDrag(int current) {
    final anchor = _dragAnchor;
    if (anchor == null) return;
    final low = math.min(anchor, current);
    final high = math.max(anchor, current);
    final next = Set<String>.of(_dragBase);
    for (var index = low; index <= high; index += 1) {
      final item = timeline.itemAt(index);
      if (item == null) continue;
      if (_dragAdding) {
        next.add(item.id);
      } else {
        next.remove(item.id);
      }
    }
    widget.onSelect(next);
  }

  void _dragMove(LongPressMoveUpdateDetails details) {
    if (_dragAnchor == null) return;
    _dragAt = details.localPosition;
    final index = _itemAt(details.localPosition);
    if (index != null) _applyDrag(index);
    _autoScroll ??= Timer.periodic(const Duration(milliseconds: 16), (_) => _autoScrollTick());
  }

  /// Near the top or bottom edge while drag-selecting: scroll and select on.
  void _autoScrollTick() {
    final at = _dragAt;
    if (at == null || !_scroll.hasClients) return;
    const edge = 56.0;
    double speed = 0;
    if (at.dy < edge) speed = -(edge - at.dy) / 2;
    if (at.dy > _viewport - edge) speed = (at.dy - (_viewport - edge)) / 2;
    if (speed == 0) return;
    final position = _scroll.position;
    final target = (position.pixels + speed).clamp(0.0, position.maxScrollExtent).toDouble();
    if (target == position.pixels) return;
    _scroll.jumpTo(target);
    final index = _itemAt(at);
    if (index != null) _applyDrag(index);
  }

  void _dragEnd() {
    _dragAnchor = null;
    _dragAt = null;
    _autoScroll?.cancel();
    _autoScroll = null;
  }

  void _selectSection(PhotoSection section) {
    final ids = <String>[];
    for (var index = section.firstItem; index < section.firstItem + section.count; index += 1) {
      timeline.ensureLoaded(index);
      final item = timeline.itemAt(index);
      if (item != null) ids.add(item.id);
    }
    final all = ids.every(widget.selected.contains);
    final next = Set<String>.of(widget.selected);
    if (all) {
      next.removeAll(ids);
    } else {
      next.addAll(ids);
    }
    widget.onSelect(next);
  }

  // --- Building ---------------------------------------------------------------------

  Widget _row(PhotoGridLayout layout, int index) {
    final row = layout.rows[index];
    if (row.isHeader) return _header(layout, layout.sections[row.section]);
    final tiles = <Widget>[];
    for (var column = 0; column < row.itemCount; column += 1) {
      final itemIndex = row.firstItem + column;
      timeline.ensureLoaded(itemIndex);
      final item = timeline.itemAt(itemIndex);
      if (column > 0) tiles.add(SizedBox(width: layout.spacing));
      tiles.add(SizedBox(
        width: layout.tileExtent,
        height: layout.tileExtent,
        child: _PhotoTile(
          key: ValueKey<String>(item?.id ?? 'slot-$itemIndex'),
          item: item,
          extent: layout.tileExtent,
          zoom: layout.zoom,
          thumbnails: widget.thumbnails,
          selecting: _selecting,
          selected: item != null && widget.selected.contains(item.id),
          onTap: () => widget.onOpen(itemIndex),
        ),
      ));
    }
    return Padding(
      padding: EdgeInsets.only(bottom: layout.spacing),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: tiles),
    );
  }

  Widget _header(PhotoGridLayout layout, PhotoSection section) {
    var all = _selecting;
    if (all) {
      for (var index = section.firstItem; index < section.firstItem + section.count; index += 1) {
        final item = timeline.itemAt(index);
        if (item == null || !widget.selected.contains(item.id)) {
          all = false;
          break;
        }
      }
    }
    return _PhotoSectionHeader(
      label: _photoSectionLabel(layout.zoom, section.key),
      large: layout.zoom != PhotoZoom.day,
      selecting: _selecting,
      allSelected: all,
      onSelectAll: () => _selectSection(section),
    );
  }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        _viewport = constraints.maxHeight;
        final layout = _layoutFor(constraints.maxWidth);
        final showScrubber = layout.totalExtent > constraints.maxHeight * 4;
        return Listener(
          onPointerDown: _pointerDown,
          onPointerMove: _pointerMove,
          onPointerUp: _pointerUp,
          onPointerCancel: _pointerUp,
          onPointerSignal: _pointerSignal,
          child: Stack(
            children: <Widget>[
              NotificationListener<ScrollNotification>(
                onNotification: _onScroll,
                child: RefreshIndicator(
                  onRefresh: widget.onRefresh,
                  child: GestureDetector(
                    onLongPressStart: _dragStart,
                    onLongPressMoveUpdate: _dragMove,
                    onLongPressEnd: (_) => _dragEnd(),
                    onLongPressCancel: _dragEnd,
                    child: ListView.builder(
                      key: const ValueKey<String>('photos-grid'),
                      controller: _scroll,
                      physics: const AlwaysScrollableScrollPhysics(),
                      // Rows just beyond the screen are built (and their
                      // thumbnails asked for) before they scroll in.
                      cacheExtent: constraints.maxHeight,
                      itemCount: layout.rows.length,
                      itemExtentBuilder: (index, _) => index < layout.rows.length ? layout.rows[index].extent : null,
                      itemBuilder: (context, index) => _row(layout, index),
                    ),
                  ),
                ),
              ),
              _StickyPhotoHeader(scroll: _scroll, layout: layout, builder: (section) => _header(layout, section)),
              if (showScrubber)
                Positioned(
                  top: 0,
                  bottom: 0,
                  right: 0,
                  width: 140,
                  child: _PhotoScrubber(
                    scroll: _scroll,
                    layout: layout,
                    visible: _scrubberShown || _scrubbing,
                    onStart: () => setState(() => _scrubbing = true),
                    onEnd: () {
                      setState(() => _scrubbing = false);
                      _showScrubber();
                    },
                    onScrub: _scrubTo,
                  ),
                ),
            ],
          ),
        );
      },
    );
  }
}

/// The header of the section at the top of the screen stays there until
/// the next one pushes it up.
class _StickyPhotoHeader extends StatelessWidget {
  const _StickyPhotoHeader({required this.scroll, required this.layout, required this.builder});

  final ScrollController scroll;
  final PhotoGridLayout layout;
  final Widget Function(PhotoSection section) builder;

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: scroll,
      builder: (context, _) {
        if (!scroll.hasClients || layout.sections.isEmpty) return const SizedBox.shrink();
        final offset = scroll.offset;
        if (offset <= 0) return const SizedBox.shrink();
        final sectionIndex = layout.rows[layout.rowAtOffset(offset)].section;
        final next = sectionIndex + 1 < layout.sections.length
            ? layout.rows[layout.sections[sectionIndex + 1].headerRow].offset
            : double.infinity;
        final top = math.min(0.0, next - offset - layout.headerExtent);
        return Positioned(
          key: const ValueKey<String>('photos-sticky-header'),
          top: top,
          left: 0,
          right: 0,
          height: layout.headerExtent,
          child: ColoredBox(color: _bgPrimary, child: builder(layout.sections[sectionIndex])),
        );
      },
    );
  }
}

class _PhotoSectionHeader extends StatelessWidget {
  const _PhotoSectionHeader({
    required this.label,
    required this.large,
    required this.selecting,
    required this.allSelected,
    required this.onSelectAll,
  });

  final String label;
  final bool large;
  final bool selecting;
  final bool allSelected;
  final VoidCallback onSelectAll;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(14, 0, 8, 0),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: GoogleFonts.geist(
                fontSize: large ? 17 : 14,
                fontWeight: large ? FontWeight.w700 : FontWeight.w600,
                color: _textPrimary,
                letterSpacing: large ? -0.3 : -0.1,
              ),
            ),
          ),
          if (selecting)
            IconButton(
              tooltip: appStrings.photosSelectGroup,
              onPressed: onSelectAll,
              icon: Icon(
                allSelected ? Icons.check_circle_rounded : Icons.radio_button_unchecked_rounded,
                size: 22,
                color: allSelected ? _accent : _textMuted,
              ),
            ),
        ],
      ),
    );
  }
}

/// One square of the grid: the thumbnail, a video's length, the selection.
class _PhotoTile extends StatefulWidget {
  const _PhotoTile({
    super.key,
    required this.item,
    required this.extent,
    required this.zoom,
    required this.thumbnails,
    required this.selecting,
    required this.selected,
    required this.onTap,
  });

  final PhotoItem? item;
  final double extent;
  final PhotoZoom zoom;
  final PhotoThumbnailCache thumbnails;
  final bool selecting;
  final bool selected;
  final VoidCallback onTap;

  @override
  State<_PhotoTile> createState() => _PhotoTileState();
}

class _PhotoTileState extends State<_PhotoTile> {
  Uint8List? _bytes;
  PhotoItem? _bound;

  @override
  void initState() {
    super.initState();
    _bind();
  }

  @override
  void didUpdateWidget(covariant _PhotoTile oldWidget) {
    super.didUpdateWidget(oldWidget);
    final item = widget.item;
    if (item?.id != _bound?.id || item?.etag != _bound?.etag) {
      _unbind();
      _bind();
    }
  }

  @override
  void dispose() {
    _unbind();
    super.dispose();
  }

  void _bind() {
    final item = widget.item;
    _bound = item;
    _bytes = null;
    // The server makes no thumbnails of videos.
    if (item == null || item.isVideo) return;
    final cached = widget.thumbnails.peek(item.id, item.etag);
    if (cached != null) {
      _bytes = cached;
      return;
    }
    unawaited(widget.thumbnails.load(item.id, item.etag).then((bytes) {
      if (!mounted || bytes == null || _bound?.id != item.id || _bound?.etag != item.etag) return;
      setState(() => _bytes = bytes);
    }));
  }

  void _unbind() {
    final item = _bound;
    if (item != null && _bytes == null && !item.isVideo) widget.thumbnails.release(item.id, item.etag);
    _bound = null;
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.item;
    final bytes = _bytes;
    final pixels = (widget.extent * MediaQuery.devicePixelRatioOf(context)).round();
    Widget picture = ColoredBox(color: _bgTertiary);
    if (bytes != null) {
      picture = Image.memory(
        bytes,
        fit: BoxFit.cover,
        width: widget.extent,
        height: widget.extent,
        cacheWidth: pixels,
        gaplessPlayback: true,
        filterQuality: FilterQuality.medium,
        // A quick fade the first time, none when it was already decoded.
        frameBuilder: (context, child, frame, synchronous) => synchronous
            ? child
            : AnimatedOpacity(opacity: frame == null ? 0 : 1, duration: const Duration(milliseconds: 140), child: child),
      );
    } else if (item != null && item.isVideo) {
      picture = DecoratedBox(
        decoration: BoxDecoration(gradient: LinearGradient(colors: <Color>[_bgTertiary, _bgSecondary], begin: Alignment.topLeft, end: Alignment.bottomRight)),
        child: Center(child: Icon(Icons.movie_outlined, color: _textMuted, size: math.min(32, widget.extent * 0.3))),
      );
    }
    if (item != null) picture = Hero(tag: 'photo-${item.id}', child: picture);
    final small = widget.zoom != PhotoZoom.day;
    final inset = widget.selected ? widget.extent * 0.09 : 0.0;
    return GestureDetector(
      onTap: widget.onTap,
      child: ColoredBox(
        color: widget.selected ? _accentMuted : Colors.transparent,
        child: Stack(
          fit: StackFit.expand,
          children: <Widget>[
            AnimatedPadding(
              duration: const Duration(milliseconds: 120),
              padding: EdgeInsets.all(inset),
              child: ClipRRect(borderRadius: BorderRadius.circular(widget.selected ? 6 : 0), child: picture),
            ),
            if (item != null && item.isVideo)
              Positioned(
                top: 4,
                right: 5,
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    if (!small && item.durationSeconds != null)
                      Text(
                        _photoDuration(item.durationSeconds!),
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 11.5,
                          fontWeight: FontWeight.w600,
                          shadows: <Shadow>[Shadow(blurRadius: 3, color: Colors.black54)],
                        ),
                      ),
                    Icon(Icons.play_circle_fill_rounded, size: small ? 13 : 16, color: Colors.white, shadows: const <Shadow>[Shadow(blurRadius: 3, color: Colors.black54)]),
                  ],
                ),
              ),
            if (item != null && item.favorite && !small && !widget.selecting)
              const Positioned(
                left: 5,
                bottom: 4,
                child: Icon(Icons.star_rounded, size: 15, color: Colors.white, shadows: <Shadow>[Shadow(blurRadius: 3, color: Colors.black54)]),
              ),
            if (widget.selecting && !small || widget.selected)
              Positioned(
                left: 5,
                top: 5,
                child: Icon(
                  widget.selected ? Icons.check_circle_rounded : Icons.radio_button_unchecked_rounded,
                  size: small ? 16 : 20,
                  color: widget.selected ? _accent : Colors.white,
                  shadows: const <Shadow>[Shadow(blurRadius: 3, color: Colors.black45)],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// The handle on the right edge: drag it to go anywhere in the timeline.
/// While held it shows the month it points at and the years along the way.
class _PhotoScrubber extends StatelessWidget {
  const _PhotoScrubber({
    required this.scroll,
    required this.layout,
    required this.visible,
    required this.onStart,
    required this.onEnd,
    required this.onScrub,
  });

  final ScrollController scroll;
  final PhotoGridLayout layout;
  final bool visible;
  final VoidCallback onStart;
  final VoidCallback onEnd;
  final ValueChanged<double> onScrub;

  static const double _thumbHeight = 44;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final track = math.max(1.0, constraints.maxHeight - _thumbHeight);
        void scrubAt(double dy) => onScrub((dy - _thumbHeight / 2) / track);
        return AnimatedBuilder(
          animation: scroll,
          builder: (context, _) {
            if (!scroll.hasClients || !scroll.position.hasContentDimensions) return const SizedBox.shrink();
            final max = scroll.position.maxScrollExtent;
            final fraction = max <= 0 ? 0.0 : (scroll.offset / max).clamp(0.0, 1.0);
            final top = fraction * track;
            final section = layout.sections.isEmpty ? null : layout.sections[layout.rows[layout.rowAtOffset(scroll.offset + 1)].section];
            final years = <Widget>[];
            if (visible) {
              var lastTop = -100.0;
              for (final (year, offset) in layout.yearMarks()) {
                final markTop = max <= 0 ? 0.0 : (offset / max).clamp(0.0, 1.0) * track + _thumbHeight / 2 - 8;
                if (markTop - lastTop < 22) continue;
                lastTop = markTop;
                years.add(Positioned(
                  right: 30,
                  top: markTop,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
                    decoration: BoxDecoration(color: _bgCard.withValues(alpha: 0.85), borderRadius: BorderRadius.circular(AppRadius.pill)),
                    child: Text(year, style: TextStyle(color: _textSecondary, fontSize: 11, fontWeight: FontWeight.w600)),
                  ),
                ));
              }
            }
            return IgnorePointer(
              ignoring: !visible,
              child: AnimatedOpacity(
                opacity: visible ? 1 : 0,
                duration: const Duration(milliseconds: 180),
                child: Stack(
                  children: <Widget>[
                    ...years,
                    if (section != null && visible)
                      Positioned(
                        right: 44,
                        top: top + 6,
                        child: Container(
                          key: const ValueKey<String>('photos-scrubber-label'),
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
                          decoration: BoxDecoration(
                            color: _bgCard,
                            borderRadius: BorderRadius.circular(AppRadius.pill),
                            border: Border.all(color: _border),
                            boxShadow: const <BoxShadow>[BoxShadow(color: Colors.black26, blurRadius: 8)],
                          ),
                          child: Text(
                            _photoMonthLabel(section.key),
                            style: GoogleFonts.geist(fontSize: 13, fontWeight: FontWeight.w600, color: _textPrimary),
                          ),
                        ),
                      ),
                    Positioned(
                      right: 0,
                      top: top,
                      child: GestureDetector(
                        key: const ValueKey<String>('photos-scrubber'),
                        behavior: HitTestBehavior.opaque,
                        onVerticalDragStart: (details) {
                          onStart();
                          scrubAt(top + details.localPosition.dy);
                        },
                        onVerticalDragUpdate: (details) => scrubAt(details.globalPosition.dy - _globalTop(context)),
                        onVerticalDragEnd: (_) => onEnd(),
                        onVerticalDragCancel: onEnd,
                        child: SizedBox(
                          width: 40,
                          height: _thumbHeight,
                          child: Center(
                            child: Container(
                              width: 26,
                              height: 38,
                              decoration: BoxDecoration(
                                color: _bgCard,
                                borderRadius: const BorderRadius.horizontal(left: Radius.circular(19)),
                                border: Border.all(color: _border),
                                boxShadow: const <BoxShadow>[BoxShadow(color: Colors.black26, blurRadius: 6)],
                              ),
                              child: Icon(Icons.unfold_more_rounded, size: 18, color: _textSecondary),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
        );
      },
    );
  }

  /// The scrubber track's top on screen.
  static double _globalTop(BuildContext context) {
    final box = context.findRenderObject();
    return box is RenderBox && box.hasSize ? box.localToGlobal(Offset.zero).dy : 0;
  }
}
