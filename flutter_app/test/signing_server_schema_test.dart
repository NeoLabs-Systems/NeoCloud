import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/signing/field_types.dart';
import 'package:neocloud/src/signing/models.dart';

/// The server's accepted field types and preferences, dumped by
/// `node scripts/dump_signing_schema.js`. The creator may only offer what the
/// server takes, or saving the fields would be refused.
Map<String, dynamic> _server() =>
    (jsonDecode(
              File(
                'test/fixtures/signing_server_schema.json',
              ).readAsStringSync(),
            )
            as Map<String, dynamic>)['types']
        as Map<String, dynamic>;

void main() {
  final types = _server();

  test('both sides know the same field types', () {
    expect(
      FieldType.values.map((type) => type.name).toSet(),
      types.keys.toSet(),
    );
  });

  for (final type in FieldType.values) {
    group(type.name, () {
      final spec = FieldTypes.of(type);
      final server = types[type.name] as Map<String, dynamic>;
      final prefs = server['prefs'] as Map<String, dynamic>;

      test('offers only preferences the server accepts', () {
        for (final pref in spec.prefs) {
          expect(
            prefs.containsKey(pref.key),
            isTrue,
            reason: 'server has no "${pref.key}" for ${type.name}',
          );
        }
      });

      test('offers the preferences the server needs', () {
        final offered = spec.prefs.map((pref) => pref.key).toSet();
        for (final needed
            in (server['requiredPrefs'] as List<dynamic>).cast<String>()) {
          final built = type == FieldType.cells
              ? PrefKeys.length == needed
              : offered.contains(needed);
          expect(built, isTrue, reason: '${type.name} needs "$needed"');
        }
      });

      test('uses values and limits within the server schema', () {
        for (final pref in spec.prefs) {
          final rule = prefs[pref.key] as Map<String, dynamic>;
          switch (pref.kind) {
            case PrefKind.choice:
              if (rule['kind'] == 'enum') {
                final allowed = (rule['values'] as List<dynamic>)
                    .cast<String>();
                for (final choice in pref.choices) {
                  expect(
                    allowed,
                    contains(choice.value),
                    reason: '${type.name}.${pref.key}',
                  );
                }
              } else {
                expect(
                  rule['kind'],
                  'pattern',
                  reason: '${type.name}.${pref.key}',
                );
                final regex = RegExp(rule['regex'] as String);
                for (final choice in pref.choices) {
                  expect(
                    regex.hasMatch(choice.value),
                    isTrue,
                    reason: '${type.name}.${pref.key}=${choice.value}',
                  );
                }
              }
            case PrefKind.integer:
              expect(rule['kind'], 'int', reason: pref.key);
              if (pref.min != null) {
                expect(pref.min, greaterThanOrEqualTo(rule['min'] as num), reason: pref.key);
              }
              if (pref.max != null) {
                expect(pref.max, lessThanOrEqualTo(rule['max'] as num), reason: pref.key);
              }
            case PrefKind.decimal:
              expect(rule['kind'], 'number', reason: pref.key);
            case PrefKind.toggle:
              expect(rule['kind'], 'bool', reason: pref.key);
            case PrefKind.options:
              expect(rule['kind'], 'strings', reason: pref.key);
            case PrefKind.text:
              expect(rule['kind'], 'string', reason: pref.key);
            case PrefKind.color:
              expect(rule['kind'], 'color', reason: pref.key);
          }
        }
      });

      test('agrees on how the field is filled', () {
        final storage = server['storage'] as String;
        expect(spec.signerFillable, storage != 'static');
        expect(spec.needsPad || spec.needsUpload, storage == 'blob');
        expect(
          spec.hasOptions,
          (server['requiredPrefs'] as List<dynamic>).contains('options'),
        );
      });
    });
  }

  test('preferences the server refuses are not in the registry', () {
    const refused = <String>{'background', 'length', 'multiple', 'required'};
    for (final spec in FieldTypes.all) {
      for (final pref in spec.prefs) {
        expect(refused, isNot(contains(pref.key)), reason: spec.type.name);
      }
    }
  });
}
