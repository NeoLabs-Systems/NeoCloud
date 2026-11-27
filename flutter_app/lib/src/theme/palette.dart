import 'package:flutter/widgets.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' as neolabs;

/// Resolves the active [NeoCloudPalette] for the given [brightness].
NeoCloudPalette paletteFor(Brightness brightness) =>
    brightness == Brightness.light ? lightPalette : darkPalette;

/// Color tokens, NeoRouter's set: one accent (NeoLabs gold) marks primary
/// actions and the current selection; everything else is neutral olive-grey.
/// Status colors appear as small dots or short words, never as fills.
class NeoCloudPalette extends neolabs.NeoLabsPalette {
  NeoCloudPalette.fromShared(
    neolabs.NeoLabsPalette palette, {
    // This app-level token passes WCAG AA at the current shared-package pin.
    required super.textMuted,
    required this.onAccent,
    required this.shadow,
  }) : super(
         bgPrimary: palette.bgPrimary,
         bgSecondary: palette.bgSecondary,
         bgTertiary: palette.bgTertiary,
         bgCard: palette.bgCard,
         textPrimary: palette.textPrimary,
         textSecondary: palette.textSecondary,
         accent: palette.accent,
         accentHover: palette.accentHover,
         accentAlt: palette.accentAlt,
         accentMuted: palette.accentMuted,
         border: palette.border,
         borderLight: palette.borderLight,
         success: palette.success,
         warning: palette.warning,
         danger: palette.danger,
         info: palette.info,
       );

  final Color onAccent;
  final Color shadow;
}

final NeoCloudPalette darkPalette = NeoCloudPalette.fromShared(
  neolabs.darkPalette,
  textMuted: Color(0xFF838D7D),
  onAccent: Color(0xFF14110A),
  shadow: Color(0x33000000),
);

final NeoCloudPalette lightPalette = NeoCloudPalette.fromShared(
  neolabs.lightPalette,
  textMuted: Color(0xFF656B5D),
  onAccent: Color(0xFFFFFFFF),
  shadow: Color(0x141C2117),
);
