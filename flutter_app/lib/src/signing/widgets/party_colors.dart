import 'dart:ui' show Color;

import '../../theme/contrast.dart';

/// The colours that tell parties apart on a page. They are picked to be
/// distinguishable from each other and readable over a white PDF page; the
/// gold of the app's accent is the first.
const List<Color> partyPalette = <Color>[
  Color(0xFFC9A227),
  Color(0xFF3B82F6),
  Color(0xFF10B981),
  Color(0xFFEF4444),
  Color(0xFF8B5CF6),
  Color(0xFFF97316),
  Color(0xFF06B6D4),
  Color(0xFFEC4899),
  Color(0xFF84CC16),
  Color(0xFF64748B),
  Color(0xFF14B8A6),
  Color(0xFFA16207),
];

/// The colour for the party at [index] (0-based). Past the palette (a
/// document may have 20 parties) the colours repeat, darker.
Color partyColor(int index) {
  final safe = index < 0 ? 0 : index;
  final base = partyPalette[safe % partyPalette.length];
  if (safe < partyPalette.length) return base;
  return Color.lerp(base, const Color(0xFF000000), 0.35)!;
}

/// `#rrggbb` for [color].
String colorToHex(Color color) {
  String two(double channel) => (channel * 255).round().clamp(0, 255).toRadixString(16).padLeft(2, '0');
  return '#${two(color.r)}${two(color.g)}${two(color.b)}';
}

/// Reads `#rrggbb` (or `rrggbb`); null for anything else.
Color? parseHexColor(String? text) {
  if (text == null) return null;
  final match = RegExp(r'^#?([0-9a-fA-F]{6})$').firstMatch(text.trim());
  if (match == null) return null;
  return Color(0xFF000000 | int.parse(match.group(1)!, radix: 16));
}

/// The colour a party is drawn in: the one the server stored, else the
/// palette's colour for its position.
Color resolvePartyColor(String? stored, int index) => parseHexColor(stored) ?? partyColor(index);

/// Black or white, whichever reads better on [background].
Color onPartyColor(Color background) {
  const white = Color(0xFFFFFFFF);
  const black = Color(0xFF111111);
  return contrastRatio(white, background) >= contrastRatio(black, background) ? white : black;
}
