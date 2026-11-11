part of 'main.dart';

// NeoRouter's theme (main_theme.dart), on NeoCloud's palette names. The
// tokens are read without a BuildContext through the getters below; the
// controller mirrors the chosen theme mode into [_appThemeMode].

ThemeMode _appThemeMode = ThemeMode.system;

Brightness get _appBrightness {
  switch (_appThemeMode) {
    case ThemeMode.light:
      return Brightness.light;
    case ThemeMode.dark:
      return Brightness.dark;
    case ThemeMode.system:
      return WidgetsBinding.instance.platformDispatcher.platformBrightness;
  }
}

NeoCloudPalette get _palette => paletteFor(_appBrightness);

Color get _bgPrimary => _palette.bgPrimary;
Color get _bgSecondary => _palette.bgSecondary;
Color get _bgTertiary => _palette.bgTertiary;
Color get _bgCard => _palette.bgCard;
Color get _textPrimary => _palette.textPrimary;
Color get _textSecondary => _palette.textSecondary;
Color get _textMuted => _palette.textMuted;
Color get _accent => _palette.accent;
Color get _accentHover => _palette.accentHover;
Color get _accentAlt => _palette.accentAlt;
Color get _accentMuted => _palette.accentMuted;
Color get _onAccent => _palette.onAccent;
Color get _border => _palette.border;
Color get _borderLight => _palette.borderLight;
Color get _success => _palette.success;
Color get _warning => _palette.warning;
Color get _danger => _palette.danger;
Color get _info => _palette.info;

/// Raised panels (sign-in card, upload panel, popovers).
List<BoxShadow> get _softPanelShadow => <BoxShadow>[
  BoxShadow(color: _palette.shadow, blurRadius: 32, offset: const Offset(0, 12)),
];

TextStyle _displayTitleStyle([double size = 28]) => TextStyle(
  color: _textPrimary,
  fontWeight: FontWeight.w700,
  fontSize: size,
  letterSpacing: size >= 24 ? -0.7 : -0.3,
  height: 1.15,
);

/// Small label over a group of fields: muted, not colored.
TextStyle _sectionEyebrowStyle() => TextStyle(
  color: _textMuted,
  fontWeight: FontWeight.w600,
  fontSize: 11.5,
  letterSpacing: 0.8,
);

/// Title of a card or section.
TextStyle _sectionTitleStyle() =>
    TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 15, letterSpacing: -0.15);

ThemeData _buildNeoCloudTheme(NeoCloudPalette palette, Brightness brightness) {
  final base = ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: ColorScheme.fromSeed(seedColor: palette.accent, brightness: brightness),
  );
  final buttonShape = RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control));
  final inputBorder = OutlineInputBorder(
    borderRadius: BorderRadius.circular(AppRadius.control),
    borderSide: BorderSide(color: palette.borderLight),
  );

  return base.copyWith(
    scaffoldBackgroundColor: palette.bgPrimary,
    colorScheme: base.colorScheme.copyWith(
      primary: palette.accent,
      onPrimary: palette.onAccent,
      secondary: palette.textSecondary,
      tertiary: palette.textSecondary,
      surface: palette.bgSecondary,
      onSurface: palette.textPrimary,
      surfaceContainerHighest: palette.bgTertiary,
      outline: palette.borderLight,
      outlineVariant: palette.border,
      error: palette.danger,
    ),
    textTheme: GoogleFonts.geistTextTheme(base.textTheme).apply(bodyColor: palette.textPrimary, displayColor: palette.textPrimary),
    splashFactory: InkRipple.splashFactory,
    hoverColor: palette.textPrimary.withValues(alpha: 0.04),
    highlightColor: Colors.transparent,
    cardTheme: CardThemeData(
      color: palette.bgCard,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.card),
        side: BorderSide(color: palette.border),
      ),
    ),
    dividerColor: palette.border,
    dividerTheme: DividerThemeData(color: palette.border, thickness: 1, space: 1),
    dialogTheme: DialogThemeData(
      backgroundColor: palette.bgSecondary,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.panel),
        side: BorderSide(color: palette.border),
      ),
      titleTextStyle: GoogleFonts.geist(color: palette.textPrimary, fontSize: 18, fontWeight: FontWeight.w600),
    ),
    bottomSheetTheme: BottomSheetThemeData(backgroundColor: palette.bgSecondary, surfaceTintColor: Colors.transparent),
    popupMenuTheme: PopupMenuThemeData(
      color: palette.bgSecondary,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control), side: BorderSide(color: palette.border)),
    ),
    menuTheme: MenuThemeData(
      style: MenuStyle(
        backgroundColor: WidgetStatePropertyAll<Color>(palette.bgSecondary),
        surfaceTintColor: const WidgetStatePropertyAll<Color>(Colors.transparent),
        shape: WidgetStatePropertyAll<OutlinedBorder>(
          RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control), side: BorderSide(color: palette.border)),
        ),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: palette.textPrimary,
      contentTextStyle: TextStyle(color: palette.bgPrimary, fontSize: 13.5),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: palette.bgSecondary,
      isDense: true,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      labelStyle: TextStyle(color: palette.textMuted, fontSize: 13.5),
      floatingLabelStyle: TextStyle(color: palette.textSecondary, fontSize: 13),
      hintStyle: TextStyle(color: palette.textMuted),
      helperStyle: TextStyle(color: palette.textMuted, fontSize: 12),
      border: inputBorder,
      enabledBorder: inputBorder,
      focusedBorder: inputBorder.copyWith(borderSide: BorderSide(color: palette.accent, width: 1.2)),
      errorBorder: inputBorder.copyWith(borderSide: BorderSide(color: palette.danger)),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: palette.accent,
        foregroundColor: palette.onAccent,
        disabledBackgroundColor: palette.bgTertiary,
        disabledForegroundColor: palette.textMuted,
        elevation: 0,
        minimumSize: const Size(0, 40),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: palette.textPrimary,
        side: BorderSide(color: palette.borderLight),
        minimumSize: const Size(0, 40),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: palette.textSecondary,
        minimumSize: const Size(0, 36),
        shape: buttonShape,
        textStyle: const TextStyle(fontWeight: FontWeight.w500, fontSize: 13.5),
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(foregroundColor: palette.textSecondary, shape: buttonShape),
    ),
    segmentedButtonTheme: SegmentedButtonThemeData(
      style: SegmentedButton.styleFrom(
        selectedBackgroundColor: palette.bgTertiary,
        selectedForegroundColor: palette.textPrimary,
        foregroundColor: palette.textSecondary,
        side: BorderSide(color: palette.borderLight),
        visualDensity: VisualDensity.compact,
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
      ),
    ),
    iconTheme: IconThemeData(color: palette.textSecondary),
    checkboxTheme: CheckboxThemeData(
      fillColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? palette.accent : Colors.transparent),
      checkColor: WidgetStatePropertyAll<Color>(palette.onAccent),
      side: BorderSide(color: palette.borderLight, width: 1.4),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
    ),
    chipTheme: base.chipTheme.copyWith(
      backgroundColor: palette.bgSecondary,
      selectedColor: palette.bgTertiary,
      checkmarkColor: palette.textPrimary,
      side: BorderSide(color: palette.border),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.pill)),
      labelStyle: TextStyle(color: palette.textSecondary, fontWeight: FontWeight.w500, fontSize: 13),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      showCheckmark: false,
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(color: palette.textPrimary, borderRadius: BorderRadius.circular(8)),
      textStyle: TextStyle(color: palette.bgPrimary, fontSize: 12),
      waitDuration: const Duration(milliseconds: 400),
    ),
    // Neutral switches: "on" is the text color, so gold stays reserved for
    // primary actions.
    switchTheme: SwitchThemeData(
      materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
      thumbColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? palette.bgPrimary : palette.textMuted),
      trackColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? palette.textPrimary.withValues(alpha: 0.85) : palette.bgTertiary),
      trackOutlineColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? Colors.transparent : palette.borderLight),
      thumbIcon: const WidgetStatePropertyAll<Icon?>(null),
    ),
    sliderTheme: base.sliderTheme.copyWith(
      activeTrackColor: palette.accent,
      inactiveTrackColor: palette.bgTertiary,
      thumbColor: palette.accent,
      overlayColor: palette.accentMuted,
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: palette.accent, linearTrackColor: palette.bgTertiary),
    tabBarTheme: TabBarThemeData(
      labelColor: palette.textPrimary,
      unselectedLabelColor: palette.textMuted,
      indicatorColor: palette.textPrimary,
      dividerColor: palette.border,
      labelStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
      unselectedLabelStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500),
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: palette.bgPrimary,
      surfaceTintColor: Colors.transparent,
      foregroundColor: palette.textPrimary,
      elevation: 0,
      scrolledUnderElevation: 0,
    ),
  );
}
