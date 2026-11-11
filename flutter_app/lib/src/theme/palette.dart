import 'package:flutter/widgets.dart';

/// Resolves the active [NeoCloudPalette] for the given [brightness].
NeoCloudPalette paletteFor(Brightness brightness) =>
    brightness == Brightness.light ? lightPalette : darkPalette;

/// Color tokens, NeoRouter's set: one accent (NeoLabs gold) marks primary
/// actions and the current selection; everything else is neutral olive-grey.
/// Status colors appear as small dots or short words, never as fills.
class NeoCloudPalette {
  const NeoCloudPalette({
    required this.bgPrimary,
    required this.bgSecondary,
    required this.bgTertiary,
    required this.bgCard,
    required this.textPrimary,
    required this.textSecondary,
    required this.textMuted,
    required this.accent,
    required this.accentHover,
    required this.accentAlt,
    required this.accentMuted,
    required this.onAccent,
    required this.border,
    required this.borderLight,
    required this.success,
    required this.warning,
    required this.danger,
    required this.info,
    required this.shadow,
  });

  /// Page background (NeoRouter "scaffold").
  final Color bgPrimary;

  /// Sidebar, rails and cards (NeoRouter "surface").
  final Color bgSecondary;

  /// Raised wells, hover and pressed states (NeoRouter "surfaceRaised").
  final Color bgTertiary;

  /// Card surface.
  final Color bgCard;

  final Color textPrimary;
  final Color textSecondary;
  final Color textMuted;

  /// NeoLabs gold: primary actions and the current selection only.
  final Color accent;
  final Color accentHover;

  /// Neutral second series color (no second hue).
  final Color accentAlt;

  /// Soft gold wash for the selected row.
  final Color accentMuted;
  final Color onAccent;
  final Color border;
  final Color borderLight;
  final Color success;
  final Color warning;
  final Color danger;
  final Color info;
  final Color shadow;
}

const NeoCloudPalette darkPalette = NeoCloudPalette(
  bgPrimary: Color(0xFF0E1511),
  bgSecondary: Color(0xFF141C17),
  bgTertiary: Color(0xFF1B241E),
  bgCard: Color(0xFF141C17),
  textPrimary: Color(0xFFECEFE5),
  textSecondary: Color(0xFFA9B2A2),
  textMuted: Color(0xFF828C7C),
  accent: Color(0xFFE1B052),
  accentHover: Color(0xFFEAC27A),
  accentAlt: Color(0xFF6E7A6A),
  accentMuted: Color(0x1FE1B052),
  onAccent: Color(0xFF14110A),
  border: Color(0x14E0F0E0),
  borderLight: Color(0x22E0F0E0),
  success: Color(0xFF7DBF86),
  warning: Color(0xFFD7A84E),
  danger: Color(0xFFE08A79),
  info: Color(0xFF6E7A6A),
  shadow: Color(0x33000000),
);

const NeoCloudPalette lightPalette = NeoCloudPalette(
  bgPrimary: Color(0xFFF6F4EE),
  bgSecondary: Color(0xFFFFFFFF),
  bgTertiary: Color(0xFFF1EEE6),
  bgCard: Color(0xFFFFFFFF),
  textPrimary: Color(0xFF1C2117),
  textSecondary: Color(0xFF545B4B),
  textMuted: Color(0xFF686E60),
  accent: Color(0xFFB07D2B),
  accentHover: Color(0xFF8A5F1C),
  accentAlt: Color(0xFFA6AB9C),
  accentMuted: Color(0x1AB07D2B),
  onAccent: Color(0xFFFFFFFF),
  border: Color(0x141C2117),
  borderLight: Color(0x241C2117),
  success: Color(0xFF4F8A55),
  warning: Color(0xFFA9741C),
  danger: Color(0xFFB4483C),
  info: Color(0xFFA6AB9C),
  shadow: Color(0x141C2117),
);
