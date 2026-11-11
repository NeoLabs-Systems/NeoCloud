part of 'main.dart';

// NeoRouter's spacing, radius and breakpoint scale.

abstract class AppSpacing {
  static const double xs = 8.0;
  static const double sm = 12.0;
  static const double md = 16.0;
  static const double lg = 24.0;
  static const double xl = 32.0;
}

abstract class AppBreakpoints {
  static const double mobile = 640.0;
  static const double tablet = 980.0;
  static const double desktop = 1320.0;
}

abstract class AppRadius {
  static const double tag = 8.0;
  static const double control = 10.0;
  static const double card = 14.0;
  static const double input = 10.0;
  static const double panel = 18.0;
  static const double pill = 999.0;
}

/// Widest the content of a page grows; wider windows center it.
const double kContentMaxWidth = 1120;
