part of 'main.dart';

// The full-screen photo pager of the Photos tab: swipe sideways through the
// whole timeline, pinch to zoom, swipe down to close. A photo shows its
// grid thumbnail at once, then the 1024-pixel preview; zooming in loads the
// original. Videos stream with the same player as the file viewer.

/// Files up to this size load in full when a photo is zoomed in.
const int _photoOriginalLimitBytes = 60 * 1024 * 1024;

Future<void> showPhotoViewer(BuildContext context, NeoCloudController controller, int index) {
  return Navigator.of(context).push(PageRouteBuilder<void>(
    opaque: false,
    transitionDuration: const Duration(milliseconds: 220),
    reverseTransitionDuration: const Duration(milliseconds: 180),
    pageBuilder: (context, animation, _) => FadeTransition(
      opacity: animation,
      child: _PhotoViewer(controller: controller, initialIndex: index),
    ),
  ));
}

class _PhotoViewer extends StatefulWidget {
  const _PhotoViewer({required this.controller, required this.initialIndex});

  final NeoCloudController controller;
  final int initialIndex;

  @override
  State<_PhotoViewer> createState() => _PhotoViewerState();
}

class _PhotoViewerState extends State<_PhotoViewer> {
  late final PageController _pages = PageController(initialPage: widget.initialIndex);
  late int _index = widget.initialIndex;
  bool _chrome = true;
  bool _zoomed = false;
  double _dragDy = 0;

  /// Previews (1024 px) of the photos around the open one, by id-etag.
  final LinkedHashMap<String, Future<Uint8List?>> _previews = LinkedHashMap<String, Future<Uint8List?>>();

  NeoCloudController get controller => widget.controller;
  PhotoTimeline get timeline => controller.photoLibrary.timeline;

  @override
  void initState() {
    super.initState();
    timeline.addListener(_changed);
    _warmAround(_index);
  }

  @override
  void dispose() {
    timeline.removeListener(_changed);
    _pages.dispose();
    super.dispose();
  }

  void _changed() {
    if (!mounted) return;
    if (timeline.total == 0) {
      Navigator.of(context).maybePop();
      return;
    }
    setState(() {});
  }

  Future<Uint8List?> _preview(PhotoItem item) {
    final key = PhotoThumbnailCache.keyOf(item.id, item.etag);
    final existing = _previews.remove(key);
    final future = existing ?? controller.client.photoThumbnail(item.id, item.etag, large: true).catchError((Object _) => null);
    _previews[key] = future;
    while (_previews.length > 12) {
      _previews.remove(_previews.keys.first);
    }
    return future;
  }

  void _warmAround(int index) {
    for (final near in <int>[index, index + 1, index - 1]) {
      timeline.ensureLoaded(near);
      final item = timeline.itemAt(near);
      if (item != null && !item.isVideo) unawaited(_preview(item));
    }
  }

  PhotoItem? get _current => timeline.itemAt(_index);

  void _close() => Navigator.of(context).maybePop();

  Future<void> _trash() async {
    final item = _current;
    if (item == null) return;
    await _trashPhotos(controller, <FileNode>[item.toFileNode()]);
  }

  Future<void> _favorite() async {
    final item = _current;
    if (item == null) return;
    await _favoritePhotos(controller, <FileNode>[item.toFileNode()]);
  }

  void _showInFolder() {
    final item = _current;
    if (item == null) return;
    Navigator.of(context).pop();
    controller.openFolder(item.parentId);
  }

  @override
  Widget build(BuildContext context) {
    final item = _current;
    final dragFraction = (_dragDy.abs() / 300).clamp(0.0, 1.0);
    final takenAt = item?.takenDate;
    final locale = currentAppLanguage.code;
    return Scaffold(
      backgroundColor: Colors.black.withValues(alpha: 1 - dragFraction * 0.8),
      body: Stack(
        children: <Widget>[
          Positioned.fill(
            child: Transform.translate(
              offset: Offset(0, _dragDy),
              child: Transform.scale(
                scale: 1 - dragFraction * 0.15,
                child: PageView.builder(
                  controller: _pages,
                  physics: _zoomed ? const NeverScrollableScrollPhysics() : const PageScrollPhysics(),
                  itemCount: timeline.total,
                  onPageChanged: (index) {
                    setState(() {
                      _index = index;
                      _zoomed = false;
                    });
                    _warmAround(index);
                  },
                  itemBuilder: (context, index) {
                    timeline.ensureLoaded(index);
                    final page = timeline.itemAt(index);
                    if (page == null) return const Center(child: CircularProgressIndicator(strokeWidth: 2));
                    return GestureDetector(
                      onTap: () => setState(() => _chrome = !_chrome),
                      onVerticalDragUpdate: _zoomed ? null : (details) => setState(() => _dragDy += details.delta.dy),
                      onVerticalDragEnd: _zoomed
                          ? null
                          : (details) {
                              if (_dragDy.abs() > 120 || (details.primaryVelocity ?? 0).abs() > 900) {
                                _close();
                              } else {
                                setState(() => _dragDy = 0);
                              }
                            },
                      child: page.isVideo
                          ? (index == _index
                                ? _MediaPlayer(
                                    key: ValueKey<String>('video-${page.id}'),
                                    uri: controller.client.contentUri(page.id),
                                    headers: controller.client.mediaHeaders,
                                    audioOnly: false,
                                    file: page.toFileNode(),
                                    onDownload: () => unawaited(controller.downloadFile(page.toFileNode())),
                                  )
                                : Center(child: Icon(Icons.play_circle_outline_rounded, size: 64, color: Colors.white54)))
                          : _ZoomablePhoto(
                              key: ValueKey<String>('photo-${page.id}'),
                              item: page,
                              thumbnail: controller.photoLibrary.thumbnails.peek(page.id, page.etag),
                              preview: _preview(page),
                              loadOriginal: page.size <= _photoOriginalLimitBytes
                                  ? () => controller.client.fileBytes(page.id)
                                  : null,
                              onZoomed: (zoomed) {
                                if (zoomed != _zoomed) setState(() => _zoomed = zoomed);
                              },
                            ),
                    );
                  },
                ),
              ),
            ),
          ),
          AnimatedOpacity(
            opacity: _chrome && _dragDy == 0 ? 1 : 0,
            duration: const Duration(milliseconds: 160),
            child: IgnorePointer(
              ignoring: !_chrome,
              child: Column(
                children: <Widget>[
                  Container(
                    decoration: const BoxDecoration(gradient: LinearGradient(begin: Alignment.topCenter, end: Alignment.bottomCenter, colors: <Color>[Colors.black54, Colors.transparent])),
                    child: SafeArea(
                      bottom: false,
                      child: Row(
                        children: <Widget>[
                          IconButton(tooltip: appStrings.close, onPressed: _close, icon: const Icon(Icons.arrow_back_rounded, color: Colors.white)),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              mainAxisSize: MainAxisSize.min,
                              children: <Widget>[
                                if (takenAt != null)
                                  Text(DateFormat.yMMMEd(locale).format(takenAt), style: const TextStyle(color: Colors.white, fontSize: 14, fontWeight: FontWeight.w600)),
                                if (takenAt != null)
                                  Text(DateFormat.jm(locale).format(takenAt), style: const TextStyle(color: Colors.white70, fontSize: 12)),
                              ],
                            ),
                          ),
                          IconButton(tooltip: appStrings.showInFolder, onPressed: _showInFolder, icon: const Icon(Icons.folder_open_outlined, color: Colors.white)),
                        ],
                      ),
                    ),
                  ),
                  const Spacer(),
                  if (item != null)
                    Container(
                      decoration: const BoxDecoration(gradient: LinearGradient(begin: Alignment.bottomCenter, end: Alignment.topCenter, colors: <Color>[Colors.black54, Colors.transparent])),
                      child: SafeArea(
                        top: false,
                        child: Padding(
                          padding: const EdgeInsets.symmetric(vertical: 4),
                          child: Row(
                            mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                            children: <Widget>[
                              IconButton(
                                tooltip: appStrings.shareLink,
                                onPressed: () => unawaited(_sharePhotos(context, controller, <FileNode>[item.toFileNode()])),
                                icon: const Icon(Icons.link_rounded, color: Colors.white),
                              ),
                              IconButton(
                                tooltip: item.favorite ? appStrings.filesUnstar : appStrings.filesStar,
                                onPressed: () => unawaited(_favorite()),
                                icon: Icon(item.favorite ? Icons.star_rounded : Icons.star_outline_rounded, color: item.favorite ? _accent : Colors.white),
                              ),
                              IconButton(
                                tooltip: appStrings.download,
                                onPressed: () => unawaited(controller.downloadFile(item.toFileNode())),
                                icon: const Icon(Icons.download_rounded, color: Colors.white),
                              ),
                              IconButton(
                                tooltip: appStrings.moveToTrash,
                                onPressed: () => unawaited(_trash()),
                                icon: const Icon(Icons.delete_outline_rounded, color: Colors.white),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// One photo: pinch or double-tap to zoom. Shows the best picture it has so
/// far (grid thumbnail, preview, original).
class _ZoomablePhoto extends StatefulWidget {
  const _ZoomablePhoto({
    super.key,
    required this.item,
    required this.thumbnail,
    required this.preview,
    required this.loadOriginal,
    required this.onZoomed,
  });

  final PhotoItem item;
  final Uint8List? thumbnail;
  final Future<Uint8List?> preview;
  final Future<Uint8List> Function()? loadOriginal;
  final ValueChanged<bool> onZoomed;

  @override
  State<_ZoomablePhoto> createState() => _ZoomablePhotoState();
}

class _ZoomablePhotoState extends State<_ZoomablePhoto> {
  final TransformationController _transform = TransformationController();
  Uint8List? _preview;
  Uint8List? _original;
  bool _loadingOriginal = false;
  bool _failed = false;
  TapDownDetails? _doubleTap;

  @override
  void initState() {
    super.initState();
    unawaited(widget.preview.then((bytes) {
      if (!mounted) return;
      setState(() {
        _preview = bytes;
        _failed = bytes == null && widget.thumbnail == null;
      });
    }));
  }

  @override
  void dispose() {
    _transform.dispose();
    super.dispose();
  }

  bool get _isZoomed => _transform.value.getMaxScaleOnAxis() > 1.01;

  void _interactionEnded() {
    widget.onZoomed(_isZoomed);
    if (_isZoomed) _fetchOriginal();
  }

  void _fetchOriginal() {
    final load = widget.loadOriginal;
    if (load == null || _original != null || _loadingOriginal) return;
    _loadingOriginal = true;
    unawaited(load().then((bytes) {
      if (mounted) setState(() => _original = bytes);
    }).catchError((Object _) {}));
  }

  void _toggleZoom() {
    if (_isZoomed) {
      _transform.value = Matrix4.identity();
    } else {
      final at = _doubleTap?.localPosition ?? Offset.zero;
      _transform.value = Matrix4.identity()
        ..translateByDouble(-at.dx * 1.5, -at.dy * 1.5, 0, 1)
        ..scaleByDouble(2.5, 2.5, 1, 1);
      _fetchOriginal();
    }
    widget.onZoomed(_isZoomed);
  }

  @override
  Widget build(BuildContext context) {
    final bytes = _original ?? _preview ?? widget.thumbnail;
    Widget picture;
    if (bytes == null) {
      picture = _failed
          ? Center(child: Text(appStrings.photosPhotoUnavailable, style: const TextStyle(color: Colors.white70)))
          : const Center(child: CircularProgressIndicator(strokeWidth: 2));
    } else {
      picture = Image.memory(bytes, fit: BoxFit.contain, gaplessPlayback: true, filterQuality: FilterQuality.medium);
    }
    return GestureDetector(
      onDoubleTapDown: (details) => _doubleTap = details,
      onDoubleTap: _toggleZoom,
      child: InteractiveViewer(
        transformationController: _transform,
        minScale: 1,
        maxScale: 6,
        panEnabled: _isZoomed,
        onInteractionEnd: (_) => setState(_interactionEnded),
        child: SizedBox.expand(child: Hero(tag: 'photo-${widget.item.id}', child: picture)),
      ),
    );
  }
}
