import 'dart:math' as math;
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter/painting.dart';

/// Turning a signature into a PNG: drawn strokes, typed text, an uploaded
/// picture. All of them end up as a transparent PNG trimmed to its ink and
/// small enough to upload (at most [maxSignatureWidth] x [maxSignatureHeight]).

const double maxSignatureWidth = 900;
const double maxSignatureHeight = 300;

/// Ink is padded by this many logical pixels before the PNG is cut.
const double _inkPadding = 6;

/// How crisp a small signature is made: its pixels per logical pixel.
const double _targetDensity = 3;

/// The drawn strokes as a path: quadratic Béziers through the midpoints of
/// the sampled points, which turns a jagged polyline into a smooth line. A
/// stroke of one point is a dot.
Path strokePath(List<Offset> points) {
  final path = Path();
  if (points.isEmpty) return path;
  if (points.length == 1) {
    return path..addOval(Rect.fromCircle(center: points.first, radius: 0.5));
  }
  path.moveTo(points.first.dx, points.first.dy);
  if (points.length == 2) return path..lineTo(points.last.dx, points.last.dy);
  for (var index = 1; index < points.length - 1; index++) {
    final current = points[index];
    final next = points[index + 1];
    path.quadraticBezierTo(current.dx, current.dy, (current.dx + next.dx) / 2, (current.dy + next.dy) / 2);
  }
  path.lineTo(points.last.dx, points.last.dy);
  return path;
}

/// Paints [strokes] with a round pen of [width] in [color].
void paintStrokes(Canvas canvas, List<List<Offset>> strokes, {required Color color, double width = 2.6}) {
  final paint = Paint()
    ..color = color
    ..style = PaintingStyle.stroke
    ..strokeWidth = width
    ..strokeCap = StrokeCap.round
    ..strokeJoin = StrokeJoin.round
    ..isAntiAlias = true;
  final dots = Paint()
    ..color = color
    ..style = PaintingStyle.fill
    ..isAntiAlias = true;
  for (final stroke in strokes) {
    if (stroke.length == 1) {
      canvas.drawCircle(stroke.first, width / 2, dots);
    } else if (stroke.length > 1) {
      canvas.drawPath(strokePath(stroke), paint);
    }
  }
}

/// The box around all ink, grown by the pen width; null without ink.
Rect? inkBounds(List<List<Offset>> strokes, {double width = 2.6}) {
  Rect? bounds;
  for (final stroke in strokes) {
    for (final point in stroke) {
      final box = Rect.fromCircle(center: point, radius: width / 2);
      bounds = bounds == null ? box : bounds.expandToInclude(box);
    }
  }
  return bounds;
}

/// The scale that fits [size] into the size limits, but never above
/// [_targetDensity]: a tiny signature is drawn at 3x, a huge one shrunk.
double _fitScale(Size size) {
  if (size.isEmpty) return 1;
  return math.min(_targetDensity, math.min(maxSignatureWidth / size.width, maxSignatureHeight / size.height));
}

Future<Uint8List?> _toPng(ui.Picture picture, int width, int height) async {
  final image = await picture.toImage(width, height);
  try {
    final data = await image.toByteData(format: ui.ImageByteFormat.png);
    return data?.buffer.asUint8List();
  } finally {
    image.dispose();
    picture.dispose();
  }
}

/// [strokes] as a transparent PNG cut to the ink; null when there is none.
Future<Uint8List?> exportStrokesPng(List<List<Offset>> strokes, {Color color = const Color(0xFF111827), double width = 2.6}) async {
  final bounds = inkBounds(strokes, width: width);
  if (bounds == null) return null;
  final box = bounds.inflate(_inkPadding);
  final scale = _fitScale(box.size);
  final pixelWidth = math.max(1, (box.width * scale).ceil());
  final pixelHeight = math.max(1, (box.height * scale).ceil());
  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder)
    ..scale(scale)
    ..translate(-box.left, -box.top);
  paintStrokes(canvas, strokes, color: color, width: width);
  return _toPng(recorder.endRecording(), pixelWidth, pixelHeight);
}

/// [text] set in [fontFamily] as a transparent PNG; null for blank text.
Future<Uint8List?> renderTypedPng(
  String text, {
  required String fontFamily,
  Color color = const Color(0xFF111827),
  double fontSize = 64,
}) async {
  final value = text.trim();
  if (value.isEmpty) return null;
  final painter = TextPainter(
    text: TextSpan(text: value, style: TextStyle(fontFamily: fontFamily, fontSize: fontSize, color: color, height: 1.25)),
    textDirection: TextDirection.ltr,
    maxLines: 1,
  )..layout();
  final box = Size(painter.width + _inkPadding * 2, painter.height + _inkPadding * 2);
  final scale = _fitScale(box);
  final pixelWidth = math.max(1, (box.width * scale).ceil());
  final pixelHeight = math.max(1, (box.height * scale).ceil());
  final recorder = ui.PictureRecorder();
  final canvas = Canvas(recorder)..scale(scale);
  painter.paint(canvas, const Offset(_inkPadding, _inkPadding));
  painter.dispose();
  return _toPng(recorder.endRecording(), pixelWidth, pixelHeight);
}

/// An uploaded picture re-drawn as a PNG that fits the size limits (never
/// enlarged); null when it can't be decoded. Metadata is dropped on the way.
Future<Uint8List?> normalizeImagePng(Uint8List bytes) async {
  ui.Codec? codec;
  try {
    codec = await ui.instantiateImageCodec(bytes);
    final frame = await codec.getNextFrame();
    final image = frame.image;
    final size = Size(image.width.toDouble(), image.height.toDouble());
    final scale = math.min(1.0, math.min(maxSignatureWidth / size.width, maxSignatureHeight / size.height));
    final width = math.max(1, (size.width * scale).round());
    final height = math.max(1, (size.height * scale).round());
    final recorder = ui.PictureRecorder();
    Canvas(recorder).drawImageRect(
      image,
      Offset.zero & size,
      Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble()),
      Paint()..filterQuality = FilterQuality.high,
    );
    image.dispose();
    return await _toPng(recorder.endRecording(), width, height);
  } catch (_) {
    return null;
  } finally {
    codec?.dispose();
  }
}
