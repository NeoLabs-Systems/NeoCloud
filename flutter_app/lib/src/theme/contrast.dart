import 'dart:math' as math;

import 'package:flutter/painting.dart';

/// WCAG 2.x relative luminance of an opaque color.
double relativeLuminance(Color color) {
  double channel(double value) => value <= 0.04045 ? value / 12.92 : math.pow((value + 0.055) / 1.055, 2.4).toDouble();
  return 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
}

/// WCAG contrast ratio between [foreground] and [background], 1..21. A
/// translucent foreground is blended onto the background first.
double contrastRatio(Color foreground, Color background) {
  final blended = Color.alphaBlend(foreground, background);
  final a = relativeLuminance(blended);
  final b = relativeLuminance(background);
  return (math.max(a, b) + 0.05) / (math.min(a, b) + 0.05);
}
