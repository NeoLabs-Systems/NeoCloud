/// Parsing and formatting of what a signer types: dates, numbers, phone
/// numbers. Pure functions, used by the field registry and the signing UI.
library;

/// The date notations a date field offers, as `prefs.format`.
abstract class DateFormats {
  static const String dotted = 'DD.MM.YYYY';
  static const String iso = 'YYYY-MM-DD';
  static const String us = 'MM/DD/YYYY';

  static const List<String> all = <String>[dotted, iso, us];

  /// The notation for a document in [locale] when the field names none.
  static String defaultFor(String locale) => locale.toLowerCase().startsWith('de') ? dotted : us;

  /// [format] if it is one of [all], else [defaultFor].
  static String resolve(String? format, String locale) => all.contains(format) ? format! : defaultFor(locale);
}

String _two(int value) => value.toString().padLeft(2, '0');

/// `2026-03-07`, the canonical form values are stored in.
String toSigningIsoDate(DateTime date) => '${date.year.toString().padLeft(4, '0')}-${_two(date.month)}-${_two(date.day)}';

/// [date] written in [format] (one of [DateFormats.all]).
String formatSigningDate(DateTime date, String format) {
  final year = date.year.toString().padLeft(4, '0');
  switch (format) {
    case DateFormats.iso:
      return '$year-${_two(date.month)}-${_two(date.day)}';
    case DateFormats.us:
      return '${_two(date.month)}/${_two(date.day)}/$year';
    default:
      return '${_two(date.day)}.${_two(date.month)}.$year';
  }
}

/// A real calendar date from year, month and day; null for 31 February etc.
DateTime? _calendarDate(int year, int month, int day) {
  if (year < 1000 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31) return null;
  final date = DateTime.utc(year, month, day);
  return date.month == month && date.day == day ? date : null;
}

/// Reads [text] written in [format]; null if it isn't a real date.
DateTime? parseSigningDate(String text, String format) {
  final value = text.trim();
  final RegExp pattern;
  switch (format) {
    case DateFormats.iso:
      pattern = RegExp(r'^(\d{4})-(\d{1,2})-(\d{1,2})$');
    case DateFormats.us:
      pattern = RegExp(r'^(\d{1,2})/(\d{1,2})/(\d{4})$');
    default:
      pattern = RegExp(r'^(\d{1,2})\.(\d{1,2})\.(\d{4})$');
  }
  final match = pattern.firstMatch(value);
  if (match == null) return null;
  final a = int.parse(match.group(1)!);
  final b = int.parse(match.group(2)!);
  final c = int.parse(match.group(3)!);
  switch (format) {
    case DateFormats.iso:
      return _calendarDate(a, b, c);
    case DateFormats.us:
      return _calendarDate(c, a, b);
    default:
      return _calendarDate(c, b, a);
  }
}

/// A stored (ISO) date.
DateTime? parseSigningIsoDate(String text) => parseSigningDate(text, DateFormats.iso);

/// A typed number as canonical text (`-1234.5`), or null when it isn't one.
/// `1234.5`, `1234,5`, `1.234,5` and `1,234.5` all work: with both kinds of
/// separator the last one is the decimal point; a single separator is the
/// decimal point; the same separator repeated groups thousands.
String? normalizeDecimal(String text) {
  var value = text.trim().replaceAll(RegExp(r'[\s\u00a0\u202f]'), '');
  if (value.isEmpty) return null;
  final lastComma = value.lastIndexOf(',');
  final lastDot = value.lastIndexOf('.');
  if (lastComma >= 0 && lastDot >= 0) {
    value = lastComma > lastDot ? value.replaceAll('.', '').replaceAll(',', '.') : value.replaceAll(',', '');
  } else if (lastComma >= 0) {
    value = ','.allMatches(value).length > 1 ? value.replaceAll(',', '') : value.replaceAll(',', '.');
  } else if ('.'.allMatches(value).length > 1) {
    value = value.replaceAll('.', '');
  }
  if (!RegExp(r'^[+-]?(\d+\.?\d*|\.\d+)$').hasMatch(value)) return null;
  return value.startsWith('+') ? value.substring(1) : value;
}

double? parseDecimal(String text) {
  final normal = normalizeDecimal(text);
  return normal == null ? null : double.tryParse(normal);
}

/// How many places follow the decimal point of a typed number.
int decimalPlaces(String text) {
  final normal = normalizeDecimal(text);
  if (normal == null) return 0;
  final dot = normal.indexOf('.');
  return dot < 0 ? 0 : normal.length - dot - 1;
}

/// A number written with [decimals] places (none: as short as it can be).
String formatDecimal(double value, {int? decimals}) {
  if (decimals != null) return value.toStringAsFixed(decimals);
  return value == value.truncateToDouble() ? value.toStringAsFixed(0) : '$value';
}

final RegExp _phoneNoise = RegExp(r'[\s().\-/ ]');

/// A phone number without spaces and punctuation, `+` kept at the front:
/// `+49 (151) 234-567` → `+49151234567`. `00` becomes `+`.
String normalizePhone(String text) {
  var value = text.trim().replaceAll(_phoneNoise, '');
  if (value.startsWith('00')) value = '+${value.substring(2)}';
  return value;
}

/// 6 to 15 digits, an optional leading `+`.
bool isValidPhone(String text) => RegExp(r'^\+?\d{6,15}$').hasMatch(normalizePhone(text));
