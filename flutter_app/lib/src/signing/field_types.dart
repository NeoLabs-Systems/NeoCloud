import 'package:flutter/material.dart' show IconData, Icons;
import 'package:flutter/painting.dart' show Size;

import '../l10n/app_language.dart';
import 'models.dart';
import 'value_format.dart';

/// The field-type registry: everything the editor and the signing flow need
/// to know about a [FieldType], in one table.
///
/// * the palette tile (icon, label), the size a new box starts with;
/// * which settings the inspector offers ([FieldTypeSpec.prefs]);
/// * whether a signer fills it and how ([FieldTypeSpec.signerFillable],
///   [FieldTypeSpec.needsPad], [FieldTypeSpec.needsUpload]);
/// * checking and formatting a value on the client ([FieldTypeSpec.validate],
///   [FieldTypeSpec.normalize], [FieldTypeSpec.display]). The server checks
///   again; this is for early, friendly feedback.
///
/// Value representation, per type:
/// text, phone, cells, date (ISO `YYYY-MM-DD`), number (canonical decimal
/// text), select, radio: `String`. checkbox: `bool`. multiple: `List<String>`.
/// signature, initials, image, stamp, file: an image or file, uploaded
/// separately; for validation any non-empty marker (`true`, a file name, a
/// filled [SigningValue]) means "present".

/// What a pref control looks like in the inspector.
/// [color] is `#rrggbb` text.
enum PrefKind { toggle, integer, decimal, choice, options, text, color }

class PrefChoice {
  const PrefChoice(this.value, this.label);

  final String value;
  final String Function(AppL10n) label;
}

/// A setting a field type offers; the inspector builds one control per entry.
class PrefSpec {
  const PrefSpec({
    required this.key,
    required this.kind,
    required this.label,
    this.choices = const <PrefChoice>[],
    this.min,
    this.max,
  });

  final String key;
  final PrefKind kind;
  final String Function(AppL10n) label;

  /// For [PrefKind.choice].
  final List<PrefChoice> choices;

  /// Bounds for [PrefKind.integer] and [PrefKind.decimal].
  final num? min;
  final num? max;
}

enum ValueIssueCode {
  required,
  tooLong,
  invalidNumber,
  belowMin,
  aboveMax,
  tooManyDecimals,
  invalidDate,
  invalidPhone,
  wrongCellCount,
  notAnOption,
}

/// Why a value isn't acceptable; [limit] is the number a message mentions.
class ValueIssue {
  const ValueIssue(this.code, [this.limit]);

  final ValueIssueCode code;
  final num? limit;

  /// A sentence for the signer.
  String message(AppL10n strings) {
    switch (code) {
      case ValueIssueCode.required:
        return strings.signingIssueRequired;
      case ValueIssueCode.tooLong:
        return strings.signingIssueTooLong(_fmt(limit));
      case ValueIssueCode.invalidNumber:
        return strings.signingIssueInvalidNumber;
      case ValueIssueCode.belowMin:
        return strings.signingIssueBelowMin(_fmt(limit));
      case ValueIssueCode.aboveMax:
        return strings.signingIssueAboveMax(_fmt(limit));
      case ValueIssueCode.tooManyDecimals:
        return strings.signingIssueDecimals(_fmt(limit));
      case ValueIssueCode.invalidDate:
        return strings.signingIssueInvalidDate;
      case ValueIssueCode.invalidPhone:
        return strings.signingIssueInvalidPhone;
      case ValueIssueCode.wrongCellCount:
        return strings.signingIssueCells(_fmt(limit));
      case ValueIssueCode.notAnOption:
        return strings.signingIssueNotAnOption;
    }
  }

  static String _fmt(num? value) => value == null ? '' : formatDecimal(value.toDouble());

  @override
  bool operator ==(Object other) => other is ValueIssue && other.code == code && other.limit == limit;

  @override
  int get hashCode => Object.hash(code, limit);

  @override
  String toString() => 'ValueIssue($code, $limit)';
}

/// Longest text a field takes (matches the server's cap).
const int maxTextLength = 5000;

/// How many characters a cells field has when it names none.
const int defaultCellCount = 6;

class FieldTypeSpec {
  const FieldTypeSpec({
    required this.type,
    required this.icon,
    required this.label,
    required this.defaultSize,
    required this.minSize,
    this.prefs = const <PrefSpec>[],
    this.signerFillable = true,
    this.needsPad = false,
    this.needsUpload = false,
    this.squareBox = false,
    this.hasOptions = false,
    this.imageAccept = false,
  });

  final FieldType type;
  final IconData icon;
  final String Function(AppL10n) label;

  /// Width and height as fractions of the page, chosen to look right on A4
  /// (595 x 842 pt).
  final Size defaultSize;
  final Size minSize;

  /// The settings the inspector offers.
  final List<PrefSpec> prefs;

  /// False for static boxes (a heading): no value to collect.
  final bool signerFillable;

  /// Filled by drawing, typing or uploading a signature image.
  final bool needsPad;

  /// Filled by choosing a file or image.
  final bool needsUpload;

  /// The box keeps a 1:1 ratio on the page (checkbox).
  final bool squareBox;

  /// Has a list of choices (`prefs.options`).
  final bool hasOptions;

  /// An upload that must be an image (image, stamp); false = any file.
  final bool imageAccept;

  /// The ratio (width / height) a box keeps in fraction space on a page of
  /// [pagePixels]; null when it may take any shape.
  double? lockedAspect(Size pagePixels) => squareBox ? pagePixels.height / pagePixels.width : null;

  bool get isImageValue => needsPad || needsUpload;

  // Values

  /// Whether [value] is empty for this type.
  bool isEmpty(Object? value) {
    if (value == null) return true;
    if (value is SigningValue) return !value.isFilled;
    if (value is String) return value.trim().isEmpty;
    if (value is List) return value.isEmpty;
    if (value is bool) return !value;
    return false;
  }

  /// The value to store for what the signer typed or picked: trimmed text,
  /// canonical number and date, compact phone number. Returns [raw] unchanged
  /// where there is nothing to canonicalise.
  Object? normalize(FieldPrefs prefs, Object? raw) {
    switch (type) {
      case FieldType.number:
        final text = raw is num ? '$raw' : '${raw ?? ''}';
        return normalizeDecimal(text) ?? text.trim();
      case FieldType.phone:
        return normalizePhone('${raw ?? ''}');
      case FieldType.date:
        if (raw is DateTime) return toSigningIsoDate(raw);
        final text = '${raw ?? ''}'.trim();
        final preferred = DateFormats.resolve(prefs.format, '');
        for (final format in <String>[preferred, ...DateFormats.all]) {
          final typed = parseSigningDate(text, format);
          if (typed != null) return toSigningIsoDate(typed);
        }
        return text;
      case FieldType.text:
      case FieldType.cells:
        return '${raw ?? ''}'.trim();
      default:
        return raw;
    }
  }

  /// The first problem with [value] for [field], or null when it is fine.
  /// An empty value is a problem only when the field is required.
  ValueIssue? validate(SigningField field, Object? value) {
    if (!signerFillable) return null;
    if (isEmpty(value)) return field.required ? const ValueIssue(ValueIssueCode.required) : null;
    final prefs = field.prefs;
    switch (type) {
      case FieldType.text:
        return '$value'.length > maxTextLength ? const ValueIssue(ValueIssueCode.tooLong, maxTextLength) : null;
      case FieldType.number:
        return _validateNumber(prefs, value);
      case FieldType.date:
        return parseSigningIsoDate('$value') == null ? const ValueIssue(ValueIssueCode.invalidDate) : null;
      case FieldType.phone:
        return isValidPhone('$value') ? null : const ValueIssue(ValueIssueCode.invalidPhone);
      case FieldType.cells:
        final count = prefs.length ?? defaultCellCount;
        return '$value'.runes.length == count ? null : ValueIssue(ValueIssueCode.wrongCellCount, count);
      case FieldType.select:
      case FieldType.radio:
        return prefs.options.contains('$value') ? null : const ValueIssue(ValueIssueCode.notAnOption);
      case FieldType.multiple:
        final chosen = value is List ? value : <Object?>['$value'];
        final options = prefs.options;
        return chosen.every((choice) => options.contains('$choice')) ? null : const ValueIssue(ValueIssueCode.notAnOption);
      case FieldType.checkbox:
      case FieldType.signature:
      case FieldType.initials:
      case FieldType.image:
      case FieldType.stamp:
      case FieldType.file:
      case FieldType.heading:
        return null;
    }
  }

  ValueIssue? _validateNumber(FieldPrefs prefs, Object? value) {
    final text = '$value';
    final number = parseDecimal(text);
    if (number == null) return const ValueIssue(ValueIssueCode.invalidNumber);
    final min = prefs.min;
    final max = prefs.max;
    if (min != null && number < min) return ValueIssue(ValueIssueCode.belowMin, min);
    if (max != null && number > max) return ValueIssue(ValueIssueCode.aboveMax, max);
    final decimals = prefs.decimals;
    if (decimals != null && decimalPlaces(text) > decimals) return ValueIssue(ValueIssueCode.tooManyDecimals, decimals);
    return null;
  }

  /// [value] as the signer reads it in the box: a date in the field's
  /// notation, a number with its decimals, choices joined by commas.
  /// Image-like values return an empty string (they are drawn, not written).
  String display(SigningField field, Object? value, {String locale = ''}) {
    if (value == null || isImageValue) return '';
    final prefs = field.prefs;
    switch (type) {
      case FieldType.date:
        final date = parseSigningIsoDate('$value');
        return date == null ? '$value' : formatSigningDate(date, DateFormats.resolve(prefs.format, locale));
      case FieldType.number:
        final number = parseDecimal('$value');
        if (number == null) return '$value';
        return formatDecimal(number, decimals: prefs.decimals);
      case FieldType.checkbox:
        return value == true ? (locale.startsWith('de') ? 'Ja' : 'Yes') : '';
      case FieldType.multiple:
        return value is List ? value.join(', ') : '$value';
      case FieldType.text:
      case FieldType.phone:
      case FieldType.cells:
      case FieldType.select:
      case FieldType.radio:
        return '$value';
      case FieldType.heading:
      case FieldType.signature:
      case FieldType.initials:
      case FieldType.image:
      case FieldType.stamp:
      case FieldType.file:
        return '';
    }
  }

  /// What a field starts with before the signer touches it: today for a date
  /// that asks for it, else the creator's default.
  Object? initialValue(SigningField field, {DateTime? now}) {
    if (type == FieldType.date && field.prefs.defaultToday) return toSigningIsoDate(now ?? DateTime.now());
    return field.defaultValue;
  }
}

// Settings several types share

const PrefSpec _fontSize = PrefSpec(key: PrefKeys.fontSize, kind: PrefKind.integer, label: _labelFontSize, min: 6, max: 72);
const PrefSpec _align = PrefSpec(
  key: PrefKeys.align,
  kind: PrefKind.choice,
  label: _labelAlign,
  choices: <PrefChoice>[
    PrefChoice('left', _labelAlignLeft),
    PrefChoice('center', _labelAlignCenter),
    PrefChoice('right', _labelAlignRight),
  ],
);
const PrefSpec _fontType = PrefSpec(
  key: PrefKeys.fontType,
  kind: PrefKind.choice,
  label: _labelFontType,
  choices: <PrefChoice>[
    PrefChoice('normal', _labelRegular),
    PrefChoice('bold', _labelBold),
  ],
);
const PrefSpec _valign = PrefSpec(
  key: PrefKeys.valign,
  kind: PrefKind.choice,
  label: _labelValign,
  choices: <PrefChoice>[
    PrefChoice('top', _labelTop),
    PrefChoice('center', _labelMiddle),
    PrefChoice('bottom', _labelBottom),
  ],
);
const PrefSpec _color = PrefSpec(key: PrefKeys.color, kind: PrefKind.color, label: _labelColor);
const PrefSpec _placeholder = PrefSpec(key: PrefKeys.placeholder, kind: PrefKind.text, label: _labelPlaceholder);
const PrefSpec _options = PrefSpec(key: PrefKeys.options, kind: PrefKind.options, label: _labelOptions);

String _labelFontSize(AppL10n s) => s.signingPrefFontSize;
String _labelAlign(AppL10n s) => s.signingPrefAlign;
String _labelAlignLeft(AppL10n s) => s.signingPrefAlignLeft;
String _labelAlignCenter(AppL10n s) => s.signingPrefAlignCenter;
String _labelAlignRight(AppL10n s) => s.signingPrefAlignRight;
String _labelFontType(AppL10n s) => s.signingPrefFontType;
String _labelRegular(AppL10n s) => s.signingPrefFontRegular;
String _labelBold(AppL10n s) => s.signingPrefFontBold;
String _labelValign(AppL10n s) => s.signingPrefValign;
String _labelTop(AppL10n s) => s.signingPrefValignTop;
String _labelMiddle(AppL10n s) => s.signingPrefValignMiddle;
String _labelBottom(AppL10n s) => s.signingPrefValignBottom;
String _labelColor(AppL10n s) => s.signingPrefColor;
String _labelPlaceholder(AppL10n s) => s.signingPrefPlaceholder;
String _labelOptions(AppL10n s) => s.signingPrefOptions;
String _labelMultiline(AppL10n s) => s.signingPrefMultiline;
String _labelDateFormat(AppL10n s) => s.signingPrefDateFormat;
String _labelDefaultToday(AppL10n s) => s.signingPrefDefaultToday;
String _labelMin(AppL10n s) => s.signingPrefMin;
String _labelMax(AppL10n s) => s.signingPrefMax;
String _labelDecimals(AppL10n s) => s.signingPrefDecimals;
String _labelCells(AppL10n s) => s.signingPrefCells;
String _labelMinLength(AppL10n s) => s.signingPrefMinLength;
String _labelMaxLength(AppL10n s) => s.signingPrefMaxLength;
String _labelTextFormat(AppL10n s) => s.signingPrefTextFormat;
String _labelFormatAny(AppL10n s) => s.signingPrefFormatAny;
String _labelFormatEmail(AppL10n s) => s.signingPrefFormatEmail;
String _labelNumberFormat(AppL10n s) => s.signingPrefNumberFormat;
String _labelFormatPlain(AppL10n s) => s.signingPrefFormatPlain;
String _labelFormatComma(AppL10n s) => s.signingPrefFormatComma;
String _labelFormatDot(AppL10n s) => s.signingPrefFormatDot;
String _labelFormatSpace(AppL10n s) => s.signingPrefFormatSpace;
String _labelFormatUsd(AppL10n s) => s.signingPrefFormatUsd;
String _labelFormatEur(AppL10n s) => s.signingPrefFormatEur;
String _labelFormatGbp(AppL10n s) => s.signingPrefFormatGbp;
String _labelCharset(AppL10n s) => s.signingPrefCharset;
String _labelCharsAny(AppL10n s) => s.signingPrefCharsAny;
String _labelCharsDigits(AppL10n s) => s.signingPrefCharsDigits;
String _labelCharsLetters(AppL10n s) => s.signingPrefCharsLetters;
String _labelMinSelect(AppL10n s) => s.signingPrefMinSelect;
String _labelMaxSelect(AppL10n s) => s.signingPrefMaxSelect;

String _formatDotted(AppL10n s) => DateFormats.dotted;
String _formatIso(AppL10n s) => DateFormats.iso;
String _formatUs(AppL10n s) => DateFormats.us;

/// The registry. Read it through [FieldTypes].
final Map<FieldType, FieldTypeSpec> _specs = <FieldType, FieldTypeSpec>{
  FieldType.text: FieldTypeSpec(
    type: FieldType.text,
    icon: Icons.text_fields_rounded,
    label: (s) => s.signingFieldText,
    defaultSize: const Size(0.30, 0.030),
    minSize: const Size(0.04, 0.014),
    prefs: const <PrefSpec>[
      _placeholder,
      _fontSize,
      _fontType,
      _color,
      _align,
      _valign,
      PrefSpec(key: PrefKeys.multiline, kind: PrefKind.toggle, label: _labelMultiline),
      PrefSpec(key: PrefKeys.minLength, kind: PrefKind.integer, label: _labelMinLength, min: 1, max: maxTextLength),
      PrefSpec(key: PrefKeys.maxLength, kind: PrefKind.integer, label: _labelMaxLength, min: 1, max: maxTextLength),
      PrefSpec(
        key: PrefKeys.format,
        kind: PrefKind.choice,
        label: _labelTextFormat,
        choices: <PrefChoice>[PrefChoice('none', _labelFormatAny), PrefChoice('email', _labelFormatEmail)],
      ),
    ],
  ),
  FieldType.signature: FieldTypeSpec(
    type: FieldType.signature,
    icon: Icons.draw_rounded,
    label: (s) => s.signingFieldSignature,
    defaultSize: const Size(0.25, 0.060),
    minSize: const Size(0.08, 0.025),
    needsPad: true,
  ),
  FieldType.initials: FieldTypeSpec(
    type: FieldType.initials,
    icon: Icons.abc_rounded,
    label: (s) => s.signingFieldInitials,
    defaultSize: const Size(0.10, 0.050),
    minSize: const Size(0.04, 0.022),
    needsPad: true,
  ),
  FieldType.date: FieldTypeSpec(
    type: FieldType.date,
    icon: Icons.calendar_today_rounded,
    label: (s) => s.signingFieldDate,
    defaultSize: const Size(0.18, 0.030),
    minSize: const Size(0.06, 0.014),
    prefs: const <PrefSpec>[
      PrefSpec(
        key: PrefKeys.format,
        kind: PrefKind.choice,
        label: _labelDateFormat,
        choices: <PrefChoice>[
          PrefChoice(DateFormats.dotted, _formatDotted),
          PrefChoice(DateFormats.iso, _formatIso),
          PrefChoice(DateFormats.us, _formatUs),
        ],
      ),
      PrefSpec(key: PrefKeys.defaultToday, kind: PrefKind.toggle, label: _labelDefaultToday),
      _fontSize,
      _fontType,
      _color,
      _align,
    ],
  ),
  FieldType.number: FieldTypeSpec(
    type: FieldType.number,
    icon: Icons.numbers_rounded,
    label: (s) => s.signingFieldNumber,
    defaultSize: const Size(0.15, 0.030),
    minSize: const Size(0.04, 0.014),
    prefs: const <PrefSpec>[
      PrefSpec(key: PrefKeys.min, kind: PrefKind.decimal, label: _labelMin),
      PrefSpec(key: PrefKeys.max, kind: PrefKind.decimal, label: _labelMax),
      PrefSpec(key: PrefKeys.decimals, kind: PrefKind.integer, label: _labelDecimals, min: 0, max: 10),
      PrefSpec(
        key: PrefKeys.format,
        kind: PrefKind.choice,
        label: _labelNumberFormat,
        choices: <PrefChoice>[
          PrefChoice('none', _labelFormatPlain),
          PrefChoice('comma', _labelFormatComma),
          PrefChoice('dot', _labelFormatDot),
          PrefChoice('space', _labelFormatSpace),
          PrefChoice('usd', _labelFormatUsd),
          PrefChoice('eur', _labelFormatEur),
          PrefChoice('gbp', _labelFormatGbp),
        ],
      ),
      _fontSize,
      _fontType,
      _color,
      _align,
    ],
  ),
  FieldType.image: FieldTypeSpec(
    type: FieldType.image,
    icon: Icons.image_outlined,
    label: (s) => s.signingFieldImage,
    defaultSize: const Size(0.20, 0.100),
    minSize: const Size(0.05, 0.030),
    needsUpload: true,
    imageAccept: true,
  ),
  FieldType.checkbox: FieldTypeSpec(
    type: FieldType.checkbox,
    icon: Icons.check_box_outlined,
    label: (s) => s.signingFieldCheckbox,
    defaultSize: const Size(0.035, 0.0247),
    minSize: const Size(0.02, 0.0143),
    squareBox: true,
  ),
  FieldType.multiple: FieldTypeSpec(
    type: FieldType.multiple,
    icon: Icons.checklist_rounded,
    label: (s) => s.signingFieldMultiple,
    defaultSize: const Size(0.30, 0.080),
    minSize: const Size(0.08, 0.030),
    prefs: const <PrefSpec>[
      _options,
      _fontSize,
      PrefSpec(key: PrefKeys.minSelect, kind: PrefKind.integer, label: _labelMinSelect, min: 1, max: 50),
      PrefSpec(key: PrefKeys.maxSelect, kind: PrefKind.integer, label: _labelMaxSelect, min: 1, max: 50),
    ],
    hasOptions: true,
  ),
  FieldType.radio: FieldTypeSpec(
    type: FieldType.radio,
    icon: Icons.radio_button_checked_rounded,
    label: (s) => s.signingFieldRadio,
    defaultSize: const Size(0.30, 0.080),
    minSize: const Size(0.08, 0.030),
    prefs: const <PrefSpec>[_options, _fontSize],
    hasOptions: true,
  ),
  FieldType.select: FieldTypeSpec(
    type: FieldType.select,
    icon: Icons.arrow_drop_down_circle_outlined,
    label: (s) => s.signingFieldSelect,
    defaultSize: const Size(0.25, 0.030),
    minSize: const Size(0.06, 0.014),
    prefs: const <PrefSpec>[_options, _fontSize, _align],
    hasOptions: true,
  ),
  FieldType.cells: FieldTypeSpec(
    type: FieldType.cells,
    icon: Icons.view_column_outlined,
    label: (s) => s.signingFieldCells,
    defaultSize: const Size(0.30, 0.035),
    minSize: const Size(0.08, 0.016),
    prefs: const <PrefSpec>[
      PrefSpec(key: PrefKeys.length, kind: PrefKind.integer, label: _labelCells, min: 1, max: 50),
      PrefSpec(
        key: PrefKeys.charset,
        kind: PrefKind.choice,
        label: _labelCharset,
        choices: <PrefChoice>[PrefChoice('any', _labelCharsAny), PrefChoice('digits', _labelCharsDigits), PrefChoice('letters', _labelCharsLetters)],
      ),
      _fontSize,
      _color,
    ],
  ),
  FieldType.stamp: FieldTypeSpec(
    type: FieldType.stamp,
    icon: Icons.approval_rounded,
    label: (s) => s.signingFieldStamp,
    defaultSize: const Size(0.18, 0.090),
    minSize: const Size(0.05, 0.030),
    needsUpload: true,
    imageAccept: true,
  ),
  FieldType.phone: FieldTypeSpec(
    type: FieldType.phone,
    icon: Icons.phone_outlined,
    label: (s) => s.signingFieldPhone,
    defaultSize: const Size(0.22, 0.030),
    minSize: const Size(0.06, 0.014),
    prefs: const <PrefSpec>[_placeholder, _fontSize, _align],
  ),
  FieldType.file: FieldTypeSpec(
    type: FieldType.file,
    icon: Icons.attach_file_rounded,
    label: (s) => s.signingFieldFile,
    defaultSize: const Size(0.25, 0.040),
    minSize: const Size(0.08, 0.020),
    needsUpload: true,
  ),
  FieldType.heading: FieldTypeSpec(
    type: FieldType.heading,
    icon: Icons.title_rounded,
    label: (s) => s.signingFieldHeading,
    defaultSize: const Size(0.40, 0.030),
    minSize: const Size(0.06, 0.014),
    prefs: const <PrefSpec>[_fontSize, _fontType, _color, _align, _valign],
    signerFillable: false,
  ),
};

/// Access to the registry.
abstract class FieldTypes {
  static FieldTypeSpec of(FieldType type) => _specs[type]!;

  /// Every type, in the order the palette shows them.
  static List<FieldTypeSpec> get all => FieldType.values.map(of).toList(growable: false);

  /// The types a signer fills in.
  static List<FieldTypeSpec> get fillable => all.where((spec) => spec.signerFillable).toList(growable: false);

  /// A new field of [type] for [partyId], its first area centred on
  /// [center] (fractions) of [page]; sizes come from the registry. Pass
  /// [pagePixels] to keep square boxes square.
  static SigningField create({
    required FieldType type,
    required String partyId,
    required int page,
    required double centerX,
    required double centerY,
    Size? pagePixels,
  }) {
    final spec = of(type);
    var width = spec.defaultSize.width;
    var height = spec.defaultSize.height;
    final aspect = pagePixels == null ? null : spec.lockedAspect(pagePixels);
    if (aspect != null) height = width / aspect;
    final left = (centerX - width / 2).clamp(0.0, 1 - width).toDouble();
    final top = (centerY - height / 2).clamp(0.0, 1 - height).toDouble();
    return SigningField.local(
      partyId: partyId,
      type: type,
      areas: <FieldArea>[FieldArea(page: page, x: left, y: top, w: width, h: height)],
      required: spec.signerFillable,
      prefs: spec.hasOptions
          ? FieldPrefs(<String, Object?>{
              PrefKeys.options: <String>[appStrings.signingOptionDefault('1'), appStrings.signingOptionDefault('2')],
            })
          : (type == FieldType.cells ? FieldPrefs(<String, Object?>{PrefKeys.length: defaultCellCount}) : null),
    );
  }
}
