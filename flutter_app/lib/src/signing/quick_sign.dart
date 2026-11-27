import 'dart:convert';
import 'dart:typed_data';

import 'models.dart';

/// The field types quick sign places.
const List<FieldType> quickSignTypes = <FieldType>[FieldType.signature, FieldType.initials, FieldType.date, FieldType.text];

/// The most fields one quick sign takes (the server's limit).
const int quickSignMaxFields = 50;

/// A signature or initials for quick sign: a picture of the signing profile
/// ([imageId]) or one made just now and sent along ([png] only).
class QuickSignPicture {
  const QuickSignPicture({required this.png, required this.method, this.imageId, this.typedText});

  final Uint8List png;
  final SignatureMethod method;
  final String? imageId;
  final String? typedText;

  bool get isSaved => imageId != null;
}

/// One box of a quick sign and what goes into it: a [picture] for a
/// signature or initials, a [value] (ISO date, text) otherwise.
class QuickSignField {
  const QuickSignField({required this.type, required this.areas, this.picture, this.value, this.prefs});

  final FieldType type;
  final List<FieldArea> areas;
  final QuickSignPicture? picture;
  final String? value;
  final Map<String, Object?>? prefs;

  Map<String, Object?> toJson() {
    final picture = this.picture;
    return <String, Object?>{
      'type': type.name,
      'areas': <Object?>[for (final area in areas) area.toJson()],
      'prefs': ?prefs,
      'value': ?value,
      if (picture != null && picture.isSaved) 'imageId': picture.imageId,
      if (picture != null && !picture.isSaved) ...<String, Object?>{
        'image': base64Encode(picture.png),
        'method': picture.method.wire,
        'text': ?picture.typedText,
      },
    };
  }
}

/// `POST /api/signing/documents/:id/quick-sign`: the owner signs a draft as
/// its only party. [signerName] is the name the consent is recorded with.
class QuickSignRequest {
  const QuickSignRequest({required this.signerName, required this.fields, this.timezone, this.tzOffsetMinutes});

  final String signerName;
  final List<QuickSignField> fields;
  final String? timezone;
  final int? tzOffsetMinutes;

  Map<String, Object?> toJson() => <String, Object?>{
    'signerName': signerName,
    'fields': <Object?>[for (final field in fields) field.toJson()],
    'timezone': ?timezone,
    'tzOffsetMinutes': ?tzOffsetMinutes,
  };
}
