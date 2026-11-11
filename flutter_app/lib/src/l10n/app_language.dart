import 'dart:ui';

import 'package:shared_preferences/shared_preferences.dart';

import '../../l10n/gen/app_l10n.dart';

export '../../l10n/gen/app_l10n.dart';

/// A language NeoCloud can present itself in. The label is the language's own
/// name, which is what a picker must show.
enum AppLanguage {
  english('en', 'English'),
  german('de', 'Deutsch');

  const AppLanguage(this.code, this.label);

  final String code;
  final String label;

  Locale get locale => Locale(code);

  static const AppLanguage fallback = AppLanguage.english;

  static AppLanguage? fromCode(String? code) {
    final normalized = code?.trim().toLowerCase() ?? '';
    for (final language in AppLanguage.values) {
      if (language.code == normalized) return language;
    }
    return null;
  }

  static AppLanguage detect(List<Locale> systemLocales) {
    for (final locale in systemLocales) {
      final match = fromCode(locale.languageCode);
      if (match != null) return match;
    }
    return fallback;
  }
}

const String appLanguagePreferenceKey = 'appLanguage';

/// The language the app is in, for code that runs away from any widget.
AppLanguage currentAppLanguage = AppLanguage.fallback;

AppL10n get appStrings => lookupAppL10n(currentAppLanguage.locale);

/// Reads the stored language, or picks one from the device the first time.
Future<void> ensureAppLanguageLoaded() async {
  final preferences = await SharedPreferences.getInstance();
  final stored = AppLanguage.fromCode(preferences.getString(appLanguagePreferenceKey));
  if (stored != null) {
    currentAppLanguage = stored;
    return;
  }
  final locales = PlatformDispatcher.instance.locales;
  currentAppLanguage = AppLanguage.detect(
    locales.isEmpty ? <Locale>[PlatformDispatcher.instance.locale] : locales,
  );
}

Future<void> storeAppLanguage(AppLanguage language) async {
  currentAppLanguage = language;
  final preferences = await SharedPreferences.getInstance();
  await preferences.setString(appLanguagePreferenceKey, language.code);
}
