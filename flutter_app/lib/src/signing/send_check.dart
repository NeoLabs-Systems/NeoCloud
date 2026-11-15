import 'field_types.dart';
import 'models.dart';

/// What sending a draft would run into, from the parties and fields alone.
class SendCheck {
  const SendCheck({this.withoutFields = const <String>[], this.withoutRequired = const <String>[], this.invalidFields = const <String>[]});

  /// Parties with nothing to fill in: the server refuses these.
  final List<String> withoutFields;

  /// Parties that could finish without doing anything: worth a warning.
  final List<String> withoutRequired;

  /// Fields whose settings the server would refuse.
  final List<String> invalidFields;

  bool get blocked => withoutFields.isNotEmpty || invalidFields.isNotEmpty;
  bool get hasWarnings => withoutRequired.isNotEmpty;
}

bool _fillable(SigningField field) => !field.readonly && FieldTypes.of(field.type).signerFillable;

SendCheck checkSend(List<SigningParty> parties, List<SigningField> fields, {Iterable<String> invalidFieldIds = const <String>[]}) {
  final withoutFields = <String>[];
  final withoutRequired = <String>[];
  for (final party in parties) {
    final own = fields.where((field) => field.partyId == party.id && _fillable(field)).toList(growable: false);
    if (own.isEmpty) {
      withoutFields.add(party.id);
    } else if (!own.any((field) => field.required)) {
      withoutRequired.add(party.id);
    }
  }
  return SendCheck(withoutFields: withoutFields, withoutRequired: withoutRequired, invalidFields: invalidFieldIds.toList(growable: false));
}
