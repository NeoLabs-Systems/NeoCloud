import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../l10n/app_language.dart';
import '../../theme/palette.dart';
import 'signature_render.dart';

/// What the pad has drawn so far: strokes in pad coordinates, with undo and
/// clear. The same controller that the pad draws feeds [exportPng].
class SignaturePadController extends ChangeNotifier {
  final List<List<Offset>> _strokes = <List<Offset>>[];

  /// The pen: dark ink, which reads on the white page whatever the app theme.
  static const Color ink = Color(0xFF111827);
  static const double penWidth = 2.6;

  List<List<Offset>> get strokes => List<List<Offset>>.unmodifiable(_strokes);

  bool get isEmpty => _strokes.isEmpty;

  void beginStroke(Offset point) {
    _strokes.add(<Offset>[point]);
    notifyListeners();
  }

  void extendStroke(Offset point) {
    if (_strokes.isEmpty) return;
    final stroke = _strokes.last;
    // Points closer than a pixel add noise, not shape.
    if ((stroke.last - point).distance < 0.8) return;
    stroke.add(point);
    notifyListeners();
  }

  void undo() {
    if (_strokes.isEmpty) return;
    _strokes.removeLast();
    notifyListeners();
  }

  void clear() {
    if (_strokes.isEmpty) return;
    _strokes.clear();
    notifyListeners();
  }

  /// A transparent PNG cut to the ink (at most 900 x 300 px); null when
  /// nothing was drawn.
  Future<Uint8List?> exportPng() => exportStrokesPng(_strokes, color: ink, width: penWidth);
}

/// A pad to sign on with a finger, pen or mouse. Sized by its parent; keeps
/// the page from scrolling while a stroke is down.
class SignaturePad extends StatelessWidget {
  const SignaturePad({super.key, required this.controller, this.height = 200, this.hint});

  final SignaturePadController controller;
  final double height;

  /// Faint text under the line while the pad is empty; defaults to "Sign here".
  final String? hint;

  @override
  Widget build(BuildContext context) {
    final palette = paletteFor(Theme.of(context).brightness);
    // The pad is a white sheet in both themes: the ink is dark.
    return Semantics(
      label: hint ?? appStrings.signingPadHint,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(14),
        child: SizedBox(
          height: height,
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: Colors.white,
              border: Border.all(color: palette.borderLight),
              borderRadius: BorderRadius.circular(14),
            ),
            child: _PadSurface(controller: controller, hint: hint ?? appStrings.signingPadHint),
          ),
        ),
      ),
    );
  }
}

class _PadSurface extends StatefulWidget {
  const _PadSurface({required this.controller, required this.hint});

  final SignaturePadController controller;
  final String hint;

  @override
  State<_PadSurface> createState() => _PadSurfaceState();
}

class _PadSurfaceState extends State<_PadSurface> {
  int? _pointer;

  @override
  Widget build(BuildContext context) {
    return Listener(
      behavior: HitTestBehavior.opaque,
      onPointerDown: (event) {
        if (_pointer != null) return;
        _pointer = event.pointer;
        widget.controller.beginStroke(event.localPosition);
      },
      onPointerMove: (event) {
        if (event.pointer == _pointer) widget.controller.extendStroke(event.localPosition);
      },
      onPointerUp: (event) {
        if (event.pointer == _pointer) _pointer = null;
      },
      onPointerCancel: (event) {
        if (event.pointer == _pointer) _pointer = null;
      },
      child: GestureDetector(
        // Claims the vertical drag so a scrolling sheet leaves the stroke alone.
        onVerticalDragStart: (_) {},
        onHorizontalDragStart: (_) {},
        child: ListenableBuilder(
          listenable: widget.controller,
          builder: (context, _) => CustomPaint(
            painter: _PadPainter(widget.controller.strokes, hint: widget.controller.isEmpty ? widget.hint : null),
            child: const SizedBox.expand(),
          ),
        ),
      ),
    );
  }
}

class _PadPainter extends CustomPainter {
  _PadPainter(this.strokes, {this.hint});

  final List<List<Offset>> strokes;
  final String? hint;

  @override
  void paint(Canvas canvas, Size size) {
    final baseline = size.height * 0.72;
    canvas.drawLine(
      Offset(16, baseline),
      Offset(size.width - 16, baseline),
      Paint()
        ..color = const Color(0xFFD1D5DB)
        ..strokeWidth = 1,
    );
    final text = hint;
    if (text != null) {
      final painter = TextPainter(
        text: TextSpan(text: text, style: const TextStyle(color: Color(0xFF9CA3AF), fontSize: 14)),
        textDirection: TextDirection.ltr,
        maxLines: 1,
      )..layout(maxWidth: size.width - 32);
      painter.paint(canvas, Offset(16, baseline + 6));
      painter.dispose();
    }
    paintStrokes(canvas, strokes, color: SignaturePadController.ink, width: SignaturePadController.penWidth);
  }

  @override
  bool shouldRepaint(covariant _PadPainter old) => true;
}
