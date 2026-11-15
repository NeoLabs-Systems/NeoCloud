import '../l10n/app_language.dart';
import 'field_types.dart';
import 'models.dart';

/// What is wrong with a field's settings, found before saving so the
/// creator can fix it instead of meeting the server's refusal.
enum FieldProblem {
  options,
  optionsUnique,
  range,
  length,
  select,
  fontSize,
  cells,
  prefRange,
  defaultValue;

  String message(AppL10n strings) {
    switch (this) {
      case FieldProblem.options:
        return strings.signingProblemOptions;
      case FieldProblem.optionsUnique:
        return strings.signingProblemOptionsUnique;
      case FieldProblem.range:
        return strings.signingProblemRange;
      case FieldProblem.length:
        return strings.signingProblemLength;
      case FieldProblem.select:
        return strings.signingProblemSelect;
      case FieldProblem.fontSize:
        return strings.signingProblemFontSize;
      case FieldProblem.cells:
        return strings.signingProblemCells;
      case FieldProblem.prefRange:
        return strings.signingProblemPrefRange;
      case FieldProblem.defaultValue:
        return strings.signingProblemDefault;
    }
  }
}

/// The problems of [field]'s settings; empty when the server will accept them.
List<FieldProblem> fieldProblems(SigningField field) {
  final spec = FieldTypes.of(field.type);
  final prefs = field.prefs;
  final problems = <FieldProblem>[];
  if (spec.hasOptions) {
    final raw = prefs[PrefKeys.options];
    final options = raw is List ? raw.map((option) => '$option'.trim()).toList() : const <String>[];
    if (options.isEmpty) {
      problems.add(FieldProblem.options);
    } else if (options.any((option) => option.isEmpty) || options.toSet().length != options.length) {
      problems.add(FieldProblem.optionsUnique);
    }
  }
  final min = prefs.min;
  final max = prefs.max;
  if (min != null && max != null && min > max) problems.add(FieldProblem.range);
  final minLength = _int(prefs[PrefKeys.minLength]);
  final maxLength = _int(prefs[PrefKeys.maxLength]);
  if (minLength != null && maxLength != null && minLength > maxLength) problems.add(FieldProblem.length);
  final minSelect = _int(prefs[PrefKeys.minSelect]);
  final maxSelect = _int(prefs[PrefKeys.maxSelect]);
  if (minSelect != null && maxSelect != null && minSelect > maxSelect) problems.add(FieldProblem.select);
  final size = prefs.fontSize;
  if (size != null && (size < 6 || size > 72)) problems.add(FieldProblem.fontSize);
  if (field.type == FieldType.cells) {
    final cells = prefs.length;
    if (cells == null || cells < 1 || cells > 50) problems.add(FieldProblem.cells);
  }
  for (final pref in spec.prefs) {
    final value = prefs[pref.key];
    if (pref.kind != PrefKind.integer || value is! num || pref.key == PrefKeys.fontSize || pref.key == PrefKeys.length) continue;
    if ((pref.min != null && value < pref.min!) || (pref.max != null && value > pref.max!)) {
      problems.add(FieldProblem.prefRange);
      break;
    }
  }
  final fallback = field.defaultValue;
  if (fallback != null && '$fallback'.isNotEmpty && spec.signerFillable && !spec.isImageValue) {
    final probe = field.copyWith(required: false);
    if (spec.validate(probe, fallback) != null) problems.add(FieldProblem.defaultValue);
  }
  return problems;
}

int? _int(Object? value) => value is num ? value.toInt() : null;
