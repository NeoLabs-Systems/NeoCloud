part of 'main.dart';

// The middle of the editor: the pages with their field overlays, the zoom
// and rotate bars and the thumbnail rail. Each page's overlay listens to the
// editor state on its own, so dragging a box redraws the boxes of the pages
// in view and never the PDF underneath. The editor and quick sign share all
// of it, and the loading of the draft's PDF ([_DraftPages]).

/// Turns pages: the page in view, or all of them with [allPages].
typedef _RotatePages = void Function(int quarterTurns, {bool allPages});

/// What the editor and quick sign share around the draft's PDF: loading it,
/// loading it again when the server replaced it (pages were turned), and
/// turning pages.
mixin _DraftPages<T extends StatefulWidget> on State<T> {
  NeoCloudController get draftController;
  SigningEditorState get draftState;
  SigningPageController get draftPages;

  Uint8List? draftPdf;
  String? draftPdfError;
  String? _draftSha;
  int _draftLoads = 0;

  void watchDraftPdf() {
    _draftSha = draftState.document.snapshotSha256;
    draftState.addListener(_draftChanged);
    unawaited(_loadDraftPdf());
  }

  void unwatchDraftPdf() => draftState.removeListener(_draftChanged);

  void _draftChanged() {
    final sha = draftState.document.snapshotSha256;
    if (sha == _draftSha) return;
    _draftSha = sha;
    setState(() => draftPdf = null);
    unawaited(_loadDraftPdf());
  }

  Future<void> _loadDraftPdf() async {
    final load = ++_draftLoads;
    try {
      final bytes = await draftController.signing.pdf(draftState.document.id);
      if (mounted && load == _draftLoads) {
        setState(() {
          draftPdf = bytes;
          draftPdfError = null;
        });
      }
    } catch (error) {
      if (mounted && load == _draftLoads) setState(() => draftPdfError = signingErrorText(draftController, error, appStrings.signingPdfLoadFailed));
    }
  }

  void rotateDraftPages(int quarterTurns, {bool allPages = false}) {
    final pages = allPages ? null : <int>[draftPages.currentPage];
    unawaited(
      draftState.rotatePages(quarterTurns: quarterTurns, pages: pages).catchError((Object error) {
        if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(signingErrorText(draftController, error, appStrings.signingRotateFailed))));
      }),
    );
  }
}

class _PagesArea extends StatelessWidget {
  const _PagesArea({
    required this.state,
    required this.pages,
    required this.pdf,
    required this.error,
    required this.pagesBuilder,
    this.boxContent,
    this.typeLabel,
    this.onRotate,
  });

  final SigningEditorState state;
  final SigningPageController pages;
  final Uint8List? pdf;
  final String? error;
  final SigningPagesBuilder pagesBuilder;

  /// What a box shows instead of its label (quick sign: the value itself).
  final _BoxContent? boxContent;
  final String Function(FieldType type)? typeLabel;

  /// Shows the rotate bar while the draft can change.
  final _RotatePages? onRotate;

  List<Widget> _overlay(BuildContext context, int pageIndex, Size pageSize) => <Widget>[
    Positioned.fill(child: _PageOverlay(state: state, pageIndex: pageIndex, pageSize: pageSize, boxContent: boxContent)),
  ];

  @override
  Widget build(BuildContext context) {
    final bytes = pdf;
    final Widget body;
    if (error != null) {
      body = Center(child: Text(error!, style: TextStyle(color: _textSecondary)));
    } else if (bytes == null) {
      body = const Center(child: CircularProgressIndicator());
    } else {
      body = pagesBuilder(context, bytes, pages, _overlay);
    }
    return Stack(
      children: <Widget>[
        Positioned.fill(child: ColoredBox(color: _bgTertiary, child: body)),
        Positioned(left: 0, right: 0, top: 8, child: Center(child: _PlacingHint(state: state, typeLabel: typeLabel))),
        Positioned(right: 16, bottom: 16, child: _ZoomBar(pages: pages)),
        if (onRotate != null) Positioned(left: 16, bottom: 16, child: _RotateBar(state: state, onRotate: onRotate!)),
      ],
    );
  }
}

/// Turns the page in view left or right, or every page from its menu.
class _RotateBar extends StatelessWidget {
  const _RotateBar({required this.state, required this.onRotate});

  final SigningEditorState state;
  final _RotatePages onRotate;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) {
        if (state.locked) return const SizedBox.shrink();
        final busy = state.rotating;
        return _FloatingPill(
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              IconButton(
                key: const ValueKey<String>('signing-rotate-left'),
                tooltip: appStrings.signingRotateLeft,
                onPressed: busy ? null : () => onRotate(-1),
                icon: const Icon(Icons.rotate_left_rounded, size: 20),
              ),
              IconButton(
                key: const ValueKey<String>('signing-rotate-right'),
                tooltip: appStrings.signingRotateRight,
                onPressed: busy ? null : () => onRotate(1),
                icon: busy ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.rotate_right_rounded, size: 20),
              ),
              PopupMenuButton<int>(
                key: const ValueKey<String>('signing-rotate-all'),
                tooltip: appStrings.signingRotateAll,
                enabled: !busy,
                icon: Icon(Icons.more_vert_rounded, size: 20, color: _textSecondary),
                onSelected: (turns) => onRotate(turns, allPages: true),
                itemBuilder: (context) => <PopupMenuEntry<int>>[
                  PopupMenuItem<int>(value: -1, child: Text(appStrings.signingRotateAllLeft)),
                  PopupMenuItem<int>(value: 1, child: Text(appStrings.signingRotateAllRight)),
                  PopupMenuItem<int>(value: 2, child: Text(appStrings.signingRotateAllHalf)),
                ],
              ),
            ],
          ),
        );
      },
    );
  }
}

/// A small card that floats over the pages: the zoom bar, the placing hint.
class _FloatingPill extends StatelessWidget {
  const _FloatingPill({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: _bgCard,
        borderRadius: BorderRadius.circular(AppRadius.pill),
        border: Border.all(color: _border),
        boxShadow: <BoxShadow>[BoxShadow(color: _palette.shadow, blurRadius: 10, offset: const Offset(0, 2))],
      ),
      child: child,
    );
  }
}

/// "Tap the page to place …" while a palette tile is armed.
class _PlacingHint extends StatelessWidget {
  const _PlacingHint({required this.state, this.typeLabel});

  final SigningEditorState state;
  final String Function(FieldType type)? typeLabel;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) {
        final type = state.armedType;
        if (type == null) return const SizedBox.shrink();
        return _FloatingPill(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(14, 6, 6, 6),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Icon(FieldTypes.of(type).icon, size: 16, color: _textSecondary),
                const SizedBox(width: 8),
                Flexible(child: Text(appStrings.signingPlaceArmed(typeLabel?.call(type) ?? FieldTypes.of(type).label(appStrings)), style: TextStyle(color: _textPrimary, fontSize: 13))),
                IconButton(visualDensity: VisualDensity.compact, onPressed: () => state.arm(null), icon: const Icon(Icons.close_rounded, size: 18)),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _ZoomBar extends StatelessWidget {
  const _ZoomBar({required this.pages});

  final SigningPageController pages;

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: pages,
      builder: (context, _) => _FloatingPill(
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            IconButton(key: const ValueKey<String>('signing-zoom-out'), tooltip: appStrings.signingZoomOut, onPressed: () => pages.zoomBy(1 / 1.2), icon: const Icon(Icons.remove_rounded, size: 20)),
            TextButton(
              key: const ValueKey<String>('signing-zoom-fit'),
              onPressed: pages.resetZoom,
              style: TextButton.styleFrom(minimumSize: const Size(48, 36), padding: EdgeInsets.zero),
              child: Tooltip(message: appStrings.signingZoomFit, child: Text('${(pages.zoom * 100).round()}%', style: TextStyle(color: _textSecondary, fontSize: 13))),
            ),
            IconButton(key: const ValueKey<String>('signing-zoom-in'), tooltip: appStrings.signingZoomIn, onPressed: () => pages.zoomBy(1.2), icon: const Icon(Icons.add_rounded, size: 20)),
          ],
        ),
      ),
    );
  }
}

/// A short line shown inside a box so it reads as what it is.
String? _boxPreview(SigningField field) {
  final spec = FieldTypes.of(field.type);
  final fallback = field.defaultValue;
  if (fallback != null && '$fallback'.isNotEmpty && !spec.isImageValue) return '$fallback';
  if (spec.hasOptions) return field.prefs.options.join(' · ');
  final placeholder = field.prefs.placeholder;
  if (placeholder != null) return placeholder;
  return null;
}

typedef _BoxContent = Widget Function(SigningField field);

/// One page's boxes, a drop target for palette tiles and a tap target for
/// placing the armed type or clearing the selection.
class _PageOverlay extends StatelessWidget {
  const _PageOverlay({required this.state, required this.pageIndex, required this.pageSize, this.boxContent});

  final SigningEditorState state;
  final int pageIndex;
  final Size pageSize;
  final _BoxContent? boxContent;

  void _place(FieldType type, Offset local) {
    final center = pixelsToFractionPoint(local, pageSize);
    state.addField(type, pageIndex, center, pageSize);
  }

  @override
  Widget build(BuildContext context) {
    return DragTarget<FieldType>(
      onAcceptWithDetails: (details) {
        final box = context.findRenderObject();
        if (box is RenderBox) _place(details.data, box.globalToLocal(details.offset));
      },
      builder: (context, candidates, _) {
        return RepaintBoundary(
          child: ListenableBuilder(
            listenable: state,
            builder: (context, _) {
              final selected = state.selectedId;
              final problems = state.fieldsWithProblems.toSet();
              final boxes = <Widget>[];
              for (final field in state.fieldsOnPage(pageIndex)) {
                for (final (index, area) in field.areas.indexed) {
                  if (area.page != pageIndex) continue;
                  boxes.add(
                    Positioned.fromRect(
                      key: ValueKey<String>('signing-box-${field.id}-$index'),
                      rect: fractionToPixels(areaRect(area), pageSize),
                      child: _EditableFieldBox(
                        state: state,
                        field: field,
                        areaIndex: index,
                        pageSize: pageSize,
                        selected: field.id == selected,
                        hasProblem: problems.contains(field.id),
                        content: boxContent?.call(field),
                      ),
                    ),
                  );
                }
              }
              return Stack(
                clipBehavior: Clip.none,
                fit: StackFit.expand,
                children: <Widget>[
                  GestureDetector(
                    key: ValueKey<String>('signing-page-$pageIndex'),
                    behavior: HitTestBehavior.opaque,
                    onTapUp: (details) {
                      final armed = state.armedType;
                      if (armed != null) {
                        _place(armed, details.localPosition);
                      } else {
                        state.select(null);
                      }
                    },
                    child: candidates.isEmpty ? const SizedBox.expand() : ColoredBox(color: _accent.withValues(alpha: 0.06)),
                  ),
                  ...boxes,
                  IgnorePointer(child: _SnapGuides(guides: state.guides, pageSize: pageSize)),
                ],
              );
            },
          ),
        );
      },
    );
  }
}

class _SnapGuides extends StatelessWidget {
  const _SnapGuides({required this.guides, required this.pageSize});

  final ValueNotifier<List<SnapGuide>> guides;
  final Size pageSize;

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<List<SnapGuide>>(
      valueListenable: guides,
      builder: (context, lines, _) => CustomPaint(painter: _GuidePainter(lines, _accent), size: pageSize),
    );
  }
}

class _GuidePainter extends CustomPainter {
  _GuidePainter(this.lines, this.color);

  final List<SnapGuide> lines;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 1;
    for (final line in lines) {
      if (line.vertical) {
        canvas.drawLine(Offset(line.position * size.width, 0), Offset(line.position * size.width, size.height), paint);
      } else {
        canvas.drawLine(Offset(0, line.position * size.height), Offset(size.width, line.position * size.height), paint);
      }
    }
  }

  @override
  bool shouldRepaint(_GuidePainter old) => old.lines != lines || old.color != color;
}

class _EditableFieldBox extends StatefulWidget {
  const _EditableFieldBox({
    required this.state,
    required this.field,
    required this.areaIndex,
    required this.pageSize,
    required this.selected,
    required this.hasProblem,
    this.content,
  });

  final SigningEditorState state;
  final SigningField field;
  final int areaIndex;
  final Size pageSize;
  final bool selected;
  final bool hasProblem;

  /// Shown inside the box in place of the label chip and preview text.
  final Widget? content;

  @override
  State<_EditableFieldBox> createState() => _EditableFieldBoxState();
}

class _EditableFieldBoxState extends State<_EditableFieldBox> {
  Rect _start = Rect.zero;
  Offset _travel = Offset.zero;
  bool _hovered = false;

  SigningEditorState get _state => widget.state;

  void _beginGesture() {
    final area = _state.fieldById(widget.field.id)?.areas[widget.areaIndex] ?? widget.field.areas[widget.areaIndex];
    _start = areaRect(area);
    _travel = Offset.zero;
    _state.select(widget.field.id);
    _state.beginGesture();
  }

  Offset _fractions(Offset pixels) => Offset(pixels.dx / widget.pageSize.width, pixels.dy / widget.pageSize.height);

  @override
  Widget build(BuildContext context) {
    final field = widget.field;
    final spec = FieldTypes.of(field.type);
    final locked = _state.locked;
    final color = _state.colorOf(field.partyId);
    final name = field.name.isEmpty ? spec.label(appStrings) : field.name;
    final preview = _boxPreview(field);
    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTap: () => _state.select(field.id),
        onPanStart: locked ? null : (_) => _beginGesture(),
        onPanUpdate: locked
            ? null
            : (details) {
                _travel += details.delta;
                _state.dragMove(field.id, widget.areaIndex, _start, _fractions(_travel));
              },
        onPanEnd: locked ? null : (_) => _state.endGesture(),
        onPanCancel: locked ? null : _state.endGesture,
        child: FieldBox(
          color: color,
          icon: spec.icon,
          label: name,
          selected: widget.selected,
          hovered: _hovered,
          hasError: widget.hasProblem,
          readonly: locked,
          showHandles: !locked,
          showLabel: widget.content == null,
          handleHitSize: MediaQuery.sizeOf(context).width < 700 ? 36 : 24,
          onResizeStart: (_) {
            _beginGesture();
          },
          onResizeUpdate: (handle, delta) {
            _travel += delta;
            _state.dragResize(field.id, widget.areaIndex, _start, handle, _fractions(_travel), widget.pageSize);
          },
          onResizeEnd: _state.endGesture,
          child: widget.content != null || preview == null
              ? widget.content
              : Align(
                  alignment: Alignment.centerLeft,
                  child: Text(preview, maxLines: 2, overflow: TextOverflow.ellipsis, style: const TextStyle(color: Color(0x99000000), fontSize: 11)),
                ),
        ),
      ),
    );
  }
}

// The thumbnail rail

/// Small stand-ins of the pages with their boxes in the parties' colours;
/// tapping one scrolls to the page.
class _PageRail extends StatelessWidget {
  const _PageRail({required this.state, required this.pages});

  final SigningEditorState state;
  final SigningPageController pages;

  @override
  Widget build(BuildContext context) {
    final sizes = state.document.pageSizes;
    final count = state.document.pageCount > 0 ? state.document.pageCount : sizes.length;
    return Container(
      width: 112,
      color: _bgSecondary,
      child: ListenableBuilder(
        listenable: Listenable.merge(<Listenable>[state, pages]),
        builder: (context, _) => ListView.builder(
          key: const ValueKey<String>('signing-rail'),
          padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 12),
          itemCount: count,
          itemBuilder: (context, index) {
            final size = index < sizes.length ? sizes[index] : const Size(595, 842);
            final active = pages.currentPage == index;
            return Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: InkWell(
                key: ValueKey<String>('signing-thumb-$index'),
                borderRadius: BorderRadius.circular(AppRadius.tag),
                onTap: () => unawaited(pages.scrollToPage(index)),
                child: Column(
                  children: <Widget>[
                    AspectRatio(
                      aspectRatio: size.width / size.height,
                      child: DecoratedBox(
                        decoration: BoxDecoration(
                          color: Colors.white,
                          border: Border.all(color: active ? _accent : _border, width: active ? 2 : 1),
                          borderRadius: BorderRadius.circular(3),
                        ),
                        child: CustomPaint(
                          painter: _ThumbPainter(
                            boxes: <(Rect, Color)>[
                              for (final field in state.fieldsOnPage(index))
                                for (final area in field.areas)
                                  if (area.page == index) (areaRect(area), state.colorOf(field.partyId)),
                            ],
                          ),
                        ),
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text('${index + 1}', style: TextStyle(color: active ? _textPrimary : _textMuted, fontSize: 12, fontWeight: active ? FontWeight.w600 : FontWeight.w400)),
                  ],
                ),
              ),
            );
          },
        ),
      ),
    );
  }
}

class _ThumbPainter extends CustomPainter {
  _ThumbPainter({required this.boxes});

  final List<(Rect, Color)> boxes;

  @override
  void paint(Canvas canvas, Size size) {
    for (final (rect, color) in boxes) {
      final scaled = Rect.fromLTWH(rect.left * size.width, rect.top * size.height, math.max(2, rect.width * size.width), math.max(2, rect.height * size.height));
      canvas.drawRect(scaled, Paint()..color = color.withValues(alpha: 0.55));
    }
  }

  @override
  bool shouldRepaint(_ThumbPainter old) => old.boxes.length != boxes.length || !listEquals(old.boxes, boxes);
}
