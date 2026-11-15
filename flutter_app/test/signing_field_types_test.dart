import 'dart:ui' show Size;

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/l10n/app_language.dart';
import 'package:neocloud/src/signing/field_types.dart';
import 'package:neocloud/src/signing/models.dart';
import 'package:neocloud/src/signing/value_format.dart';

SigningField field(FieldType type, {bool required = true, Map<String, Object?> prefs = const <String, Object?>{}, Object? defaultValue}) =>
    SigningField(id: 'f', partyId: 'p', type: type, areas: const <FieldArea>[], required: required, prefs: FieldPrefs(prefs), defaultValue: defaultValue);

ValueIssue? check(SigningField f, Object? value) => FieldTypes.of(f.type).validate(f, value);

void main() {
  group('registry', () {
    test('has every type with sensible sizes', () {
      expect(FieldTypes.all, hasLength(FieldType.values.length));
      for (final type in FieldType.values) {
        final spec = FieldTypes.of(type);
        expect(spec.type, type);
        expect(spec.defaultSize.width, inInclusiveRange(0.02, 0.5), reason: type.name);
        expect(spec.defaultSize.height, inInclusiveRange(0.01, 0.15), reason: type.name);
        expect(spec.defaultSize.width, greaterThanOrEqualTo(spec.minSize.width), reason: type.name);
        expect(spec.defaultSize.height, greaterThanOrEqualTo(spec.minSize.height), reason: type.name);
        expect(spec.label(appStrings), isNotEmpty, reason: type.name);
      }
    });

    test('every pref has a label and choices where needed', () {
      for (final spec in FieldTypes.all) {
        for (final pref in spec.prefs) {
          expect(pref.label(appStrings), isNotEmpty, reason: '${spec.type.name}.${pref.key}');
          if (pref.kind == PrefKind.choice) {
            expect(pref.choices, isNotEmpty);
            for (final choice in pref.choices) {
              expect(choice.label(appStrings), isNotEmpty);
            }
          }
        }
        expect(spec.prefs.map((p) => p.key).toSet(), hasLength(spec.prefs.length), reason: 'duplicate keys in ${spec.type.name}');
      }
    });

    test('who fills what, and how', () {
      expect(FieldTypes.of(FieldType.heading).signerFillable, isFalse);
      expect(FieldTypes.fillable, hasLength(FieldType.values.length - 1));
      expect(FieldTypes.of(FieldType.signature).needsPad, isTrue);
      expect(FieldTypes.of(FieldType.initials).needsPad, isTrue);
      expect(FieldTypes.of(FieldType.image).needsUpload, isTrue);
      expect(FieldTypes.of(FieldType.image).imageAccept, isTrue);
      expect(FieldTypes.of(FieldType.file).imageAccept, isFalse);
      expect(FieldTypes.of(FieldType.select).hasOptions, isTrue);
      expect(FieldTypes.of(FieldType.text).hasOptions, isFalse);
    });

    test('a checkbox keeps a square on any page', () {
      final spec = FieldTypes.of(FieldType.checkbox);
      const a4 = Size(595, 842);
      final aspect = spec.lockedAspect(a4)!;
      final widthPx = spec.defaultSize.width * a4.width;
      final heightPx = spec.defaultSize.width / aspect * a4.height;
      expect(widthPx, closeTo(heightPx, 1e-6));
      expect(FieldTypes.of(FieldType.text).lockedAspect(a4), isNull);
    });

    test('create places a new field inside the page', () {
      final created = FieldTypes.create(type: FieldType.signature, partyId: 'p1', page: 2, centerX: 0.99, centerY: 0.01);
      final area = created.firstArea;
      expect(created.isLocal, isTrue);
      expect(area.page, 2);
      expect(area.x + area.w, lessThanOrEqualTo(1));
      expect(area.y, greaterThanOrEqualTo(0));
      expect(created.required, isTrue);
      final heading = FieldTypes.create(type: FieldType.heading, partyId: 'p1', page: 0, centerX: .5, centerY: .5);
      expect(heading.required, isFalse);
      final select = FieldTypes.create(type: FieldType.select, partyId: 'p1', page: 0, centerX: .5, centerY: .5);
      expect(select.prefs.options, hasLength(2));
      final box = FieldTypes.create(type: FieldType.checkbox, partyId: 'p', page: 0, centerX: .5, centerY: .5, pagePixels: const Size(595, 842));
      expect(box.firstArea.w * 595, closeTo(box.firstArea.h * 842, 1e-6));
    });
  });

  group('required and empty', () {
    test('an empty required value is an issue, an optional one is fine', () {
      for (final type in FieldTypes.fillable.map((spec) => spec.type)) {
        expect(check(field(type), null)?.code, ValueIssueCode.required, reason: type.name);
        expect(check(field(type, required: false), null), isNull, reason: type.name);
      }
      expect(check(field(FieldType.text), '   ')?.code, ValueIssueCode.required);
      expect(check(field(FieldType.multiple), <String>[])?.code, ValueIssueCode.required);
    });

    test('a required checkbox must be ticked', () {
      expect(check(field(FieldType.checkbox), false)?.code, ValueIssueCode.required);
      expect(check(field(FieldType.checkbox), true), isNull);
      expect(check(field(FieldType.checkbox, required: false), false), isNull);
    });

    test('image values count as present with a marker', () {
      expect(check(field(FieldType.signature), true), isNull);
      expect(check(field(FieldType.signature), const SigningValue(fieldId: 'f', hasFile: true)), isNull);
      expect(check(field(FieldType.signature), const SigningValue(fieldId: 'f')), isNotNull);
      expect(check(field(FieldType.file), 'contract.pdf'), isNull);
    });

    test('a heading never complains', () {
      expect(check(field(FieldType.heading), null), isNull);
    });
  });

  group('text', () {
    test('accepts text and rejects absurd length', () {
      expect(check(field(FieldType.text), 'Anna'), isNull);
      expect(check(field(FieldType.text), 'x' * (maxTextLength + 1))?.code, ValueIssueCode.tooLong);
    });

    test('normalize trims', () {
      expect(FieldTypes.of(FieldType.text).normalize(FieldPrefs.empty, '  Anna '), 'Anna');
    });

    test('pref keys are the ones the PDF engine reads', () {
      final keys = FieldTypes.of(FieldType.text).prefs.map((p) => p.key).toSet();
      expect(keys, containsAll(<String>['fontSize', 'fontType', 'color', 'align', 'valign', 'multiline']));
      expect(FieldTypes.of(FieldType.cells).prefs.map((p) => p.key), contains('cells'));
      expect(FieldTypes.of(FieldType.date).prefs.map((p) => p.key), contains('format'));
    });
  });

  group('number', () {
    final f = field(FieldType.number, prefs: <String, Object?>{PrefKeys.min: 0, PrefKeys.max: 100, PrefKeys.decimals: 2});

    test('checks range and decimals', () {
      expect(check(f, '42'), isNull);
      expect(check(f, '42,5'), isNull);
      expect(check(f, '42.25'), isNull);
      expect(check(f, '-1')?.code, ValueIssueCode.belowMin);
      expect(check(f, '101')?.code, ValueIssueCode.aboveMax);
      expect(check(f, '1.234')?.code, ValueIssueCode.tooManyDecimals);
      expect(check(f, 'abc')?.code, ValueIssueCode.invalidNumber);
      expect(check(f, '1e5')?.code, ValueIssueCode.invalidNumber);
    });

    test('issue carries its limit', () {
      expect(check(f, '101'), const ValueIssue(ValueIssueCode.aboveMax, 100));
      expect(check(f, '-1')!.message(appStrings), contains('0'));
    });

    test('parses German and English notation', () {
      expect(parseDecimal('1.234,5'), 1234.5);
      expect(parseDecimal('1,234.5'), 1234.5);
      expect(parseDecimal('1,5'), 1.5);
      expect(parseDecimal('1,234,567'), 1234567);
      expect(parseDecimal('1.234.567'), 1234567);
      expect(parseDecimal(' -3.5 '), -3.5);
      expect(parseDecimal('+7'), 7);
      expect(parseDecimal('1 000'), 1000);
      expect(parseDecimal(''), isNull);
      expect(parseDecimal('1,2,3x'), isNull);
    });

    test('normalize writes the canonical form', () {
      final spec = FieldTypes.of(FieldType.number);
      expect(spec.normalize(f.prefs, '1.234,50'), '1234.50');
      expect(spec.normalize(f.prefs, 12), '12');
      expect(spec.normalize(f.prefs, 'oops'), 'oops');
    });

    test('display pads to the decimals', () {
      expect(FieldTypes.of(FieldType.number).display(f, '12.5'), '12.50');
      expect(FieldTypes.of(FieldType.number).display(field(FieldType.number), '12.50'), '12.5');
    });
  });

  group('date', () {
    test('formats in all three notations', () {
      final date = DateTime(2026, 3, 7);
      expect(formatSigningDate(date, DateFormats.dotted), '07.03.2026');
      expect(formatSigningDate(date, DateFormats.iso), '2026-03-07');
      expect(formatSigningDate(date, DateFormats.us), '03/07/2026');
      expect(toSigningIsoDate(date), '2026-03-07');
    });

    test('parses what it formats and rejects non-dates', () {
      expect(parseSigningDate('07.03.2026', DateFormats.dotted), DateTime.utc(2026, 3, 7));
      expect(parseSigningDate('7.3.2026', DateFormats.dotted), DateTime.utc(2026, 3, 7));
      expect(parseSigningDate('03/07/2026', DateFormats.us), DateTime.utc(2026, 3, 7));
      expect(parseSigningDate('2026-03-07', DateFormats.iso), DateTime.utc(2026, 3, 7));
      expect(parseSigningDate('31.02.2026', DateFormats.dotted), isNull);
      expect(parseSigningDate('29.02.2025', DateFormats.dotted), isNull);
      expect(parseSigningDate('29.02.2028', DateFormats.dotted), isNotNull);
      expect(parseSigningDate('2026-13-01', DateFormats.iso), isNull);
      expect(parseSigningDate('07.03.2026', DateFormats.iso), isNull);
      expect(parseSigningDate('garbage', DateFormats.dotted), isNull);
    });

    test('defaults by locale', () {
      expect(DateFormats.defaultFor('de'), DateFormats.dotted);
      expect(DateFormats.defaultFor('de-AT'), DateFormats.dotted);
      expect(DateFormats.defaultFor('en'), DateFormats.us);
      expect(DateFormats.resolve('nonsense', 'de'), DateFormats.dotted);
      expect(DateFormats.resolve(DateFormats.iso, 'de'), DateFormats.iso);
    });

    test('validates the stored form, normalizes typed input', () {
      final f = field(FieldType.date, prefs: <String, Object?>{PrefKeys.format: DateFormats.dotted});
      final spec = FieldTypes.of(FieldType.date);
      expect(check(f, '2026-03-07'), isNull);
      expect(check(f, '2026-02-30')?.code, ValueIssueCode.invalidDate);
      expect(check(f, '07.03.2026')?.code, ValueIssueCode.invalidDate);
      expect(spec.normalize(f.prefs, '07.03.2026'), '2026-03-07');
      expect(spec.normalize(f.prefs, '03/07/2026'), '2026-03-07');
      expect(spec.normalize(f.prefs, DateTime(2026, 3, 7)), '2026-03-07');
      expect(spec.normalize(f.prefs, 'nope'), 'nope');
    });

    test('display follows the field format, then the locale', () {
      final dotted = field(FieldType.date, prefs: <String, Object?>{PrefKeys.format: DateFormats.dotted});
      final spec = FieldTypes.of(FieldType.date);
      expect(spec.display(dotted, '2026-03-07'), '07.03.2026');
      expect(spec.display(field(FieldType.date), '2026-03-07', locale: 'de'), '07.03.2026');
      expect(spec.display(field(FieldType.date), '2026-03-07', locale: 'en'), '03/07/2026');
    });

    test('default today fills the initial value', () {
      final f = field(FieldType.date, prefs: <String, Object?>{PrefKeys.defaultToday: true});
      expect(FieldTypes.of(FieldType.date).initialValue(f, now: DateTime(2026, 11, 15)), '2026-11-15');
      expect(FieldTypes.of(FieldType.text).initialValue(field(FieldType.text, defaultValue: 'Hi'), now: DateTime(2026)), 'Hi');
    });
  });

  group('phone', () {
    test('normalizes and validates', () {
      expect(normalizePhone('+49 (151) 234-567'), '+49151234567');
      expect(normalizePhone('0049 151 234567'), '+49151234567');
      expect(isValidPhone('+49 151 234567'), isTrue);
      expect(isValidPhone('0151/234567'), isTrue);
      expect(isValidPhone('12345'), isFalse);
      expect(isValidPhone('12345678901234567'), isFalse);
      expect(isValidPhone('call me'), isFalse);
      expect(isValidPhone('+49+151234567'), isFalse);
      expect(check(field(FieldType.phone), '+49 151 234567'), isNull);
      expect(check(field(FieldType.phone), '12')?.code, ValueIssueCode.invalidPhone);
      expect(FieldTypes.of(FieldType.phone).normalize(FieldPrefs.empty, ' +49 151 234567 '), '+49151234567');
    });
  });

  group('cells', () {
    final f = field(FieldType.cells, prefs: <String, Object?>{PrefKeys.length: 4});

    test('needs exactly the configured number of characters', () {
      expect(check(f, 'AB12'), isNull);
      expect(check(f, 'AB1')?.code, ValueIssueCode.wrongCellCount);
      expect(check(f, 'AB123'), const ValueIssue(ValueIssueCode.wrongCellCount, 4));
      expect(check(field(FieldType.cells), '123456'), isNull);
      expect(check(field(FieldType.cells), '12345')?.code, ValueIssueCode.wrongCellCount);
    });

    test('counts characters, not UTF-16 units', () {
      expect(check(f, 'A\u{1F600}BC'), isNull);
    });
  });

  group('choices', () {
    final options = <String, Object?>{PrefKeys.options: <String>['Yes', 'No', 'Maybe']};

    test('select and radio take one of the options', () {
      for (final type in <FieldType>[FieldType.select, FieldType.radio]) {
        final f = field(type, prefs: options);
        expect(check(f, 'Maybe'), isNull);
        expect(check(f, 'Never')?.code, ValueIssueCode.notAnOption);
      }
    });

    test('multiple takes a subset', () {
      final f = field(FieldType.multiple, prefs: options);
      expect(check(f, <String>['Yes', 'No']), isNull);
      expect(check(f, <String>['Yes', 'Perhaps'])?.code, ValueIssueCode.notAnOption);
      expect(FieldTypes.of(FieldType.multiple).display(f, <String>['Yes', 'No']), 'Yes, No');
    });
  });

  group('display of other types', () {
    test('checkbox shows a tick, images show nothing', () {
      expect(FieldTypes.of(FieldType.checkbox).display(field(FieldType.checkbox), true), '✓');
      expect(FieldTypes.of(FieldType.checkbox).display(field(FieldType.checkbox), false), '');
      expect(FieldTypes.of(FieldType.signature).display(field(FieldType.signature), true), '');
      expect(FieldTypes.of(FieldType.text).display(field(FieldType.text), null), '');
    });
  });

  test('issue messages exist in the current language for every code', () {
    for (final code in ValueIssueCode.values) {
      expect(ValueIssue(code, 5).message(appStrings), isNotEmpty, reason: code.name);
    }
  });
}
