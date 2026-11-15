import 'dart:convert';
import 'dart:typed_data';
import 'dart:ui' show Size;

import 'saved_signatures.dart' show SavedKind;

part 'models_account.dart';

/// Models of the signing feature, one-to-one with the API contract.
///
/// Everything here is immutable and tolerant: extra keys are ignored, missing
/// keys fall back to a sensible default, and an unknown enum value maps to a
/// documented fallback instead of throwing. `toJson` writes what the server
/// reads when saving (`PUT /documents/:id/fields`, `/parties`).

// Parsing helpers

String _string(Object? value, [String fallback = '']) => value is String ? value : (value == null ? fallback : '$value');

String? _stringOrNull(Object? value) {
  if (value == null) return null;
  final text = value is String ? value : '$value';
  return text.isEmpty ? null : text;
}

int _int(Object? value, [int fallback = 0]) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse('$value') ?? fallback;
}

double _double(Object? value, [double fallback = 0]) {
  if (value is num) return value.toDouble();
  return double.tryParse('$value') ?? fallback;
}

bool _bool(Object? value, [bool fallback = false]) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  if (value is String) return value == 'true' || value == '1';
  return fallback;
}

/// ISO-8601 text or epoch milliseconds; null for anything else.
DateTime? parseSigningTime(Object? value) {
  if (value is DateTime) return value;
  if (value is num) return DateTime.fromMillisecondsSinceEpoch(value.toInt(), isUtc: true);
  if (value is String && value.isNotEmpty) return DateTime.tryParse(value);
  return null;
}

Map<String, dynamic> _map(Object? value) => value is Map ? value.cast<String, dynamic>() : const <String, dynamic>{};

Iterable<Map<String, dynamic>> _maps(Object? value) =>
    value is List ? value.whereType<Map>().map((row) => row.cast<String, dynamic>()) : const <Map<String, dynamic>>[];

T _enum<T extends Enum>(List<T> values, Object? raw, T fallback, [String Function(T)? wire]) {
  if (raw is! String) return fallback;
  for (final value in values) {
    if ((wire?.call(value) ?? value.name) == raw) return value;
  }
  return fallback;
}

String _snake(String camel) => camel.replaceAllMapped(RegExp('[A-Z]'), (match) => '_${match.group(0)!.toLowerCase()}');

// Enums

/// Lifecycle of a signing document. Unknown values read as [draft].
enum SigningStatus {
  draft,
  sent,
  inProgress,
  finalizing,
  completed,
  declined,
  expired,
  canceled;

  static SigningStatus parse(Object? raw) => _enum(values, raw, draft, (value) => _snake(value.name));

  String get wire => _snake(name);

  /// Nothing more will happen to the document.
  bool get isFinished => this == completed || this == declined || this == expired || this == canceled;

  /// Fields and parties may still be edited (before anyone opened the link).
  bool get isEditable => this == draft || this == sent;
}

/// A signer's progress. `waiting` = sequential order and not their turn yet.
/// Unknown values read as [pending].
enum PartyStatus {
  pending,
  waiting,
  opened,
  completed,
  declined;

  static PartyStatus parse(Object? raw) => _enum(values, raw, pending);
}

enum SigningOrder {
  preserved,
  random;

  static SigningOrder parse(Object? raw) => _enum(values, raw, preserved);
}

/// What the public signing page can be in. Unknown values read as [locked],
/// the safest state (nothing can be written).
enum PublicSigningState {
  ready,
  waiting,
  completed,
  declined,
  expired,
  canceled,

  /// The server never sends this word; it is what an unknown state reads as,
  /// so a newer server cannot make this app offer to sign.
  locked;

  static PublicSigningState parse(Object? raw) => _enum(values, raw, locked);
}

/// The kinds of box a document can carry. Unknown values read as [text].
enum FieldType {
  text,
  signature,
  initials,
  date,
  number,
  image,
  checkbox,
  multiple,
  radio,
  select,
  cells,
  stamp,
  phone,
  file,
  heading;

  static FieldType parse(Object? raw) => _enum(values, raw, text);
}

/// How a signature image came about; sent with the upload.
enum SignatureMethod {
  drawn,
  typed,
  uploaded;

  /// The word the server uses (`draw`, `type`, `upload`).
  String get wire {
    switch (this) {
      case SignatureMethod.drawn:
        return 'draw';
      case SignatureMethod.typed:
        return 'type';
      case SignatureMethod.uploaded:
        return 'upload';
    }
  }

  /// Reads the server's word or the enum's own name.
  static SignatureMethod? parse(Object? raw) {
    if (raw is! String) return null;
    for (final value in values) {
      if (value.name == raw || value.wire == raw) return value;
    }
    return null;
  }
}

/// Audit entries. Unknown values read as [other].
enum SigningEventType {
  created,
  updated,
  sent,
  linkRegenerated,
  opened,
  consented,
  partyCompleted,
  partyDeclined,
  completed,
  finalized,
  canceled,
  expired,
  revoked,
  finalDownloaded,
  other;

  static SigningEventType parse(Object? raw) => _enum(values, raw, other, (value) => _snake(value.name));
}

// Geometry of a field

/// A box on a page: fractions 0..1 of the page as displayed (after /Rotate,
/// within the CropBox), origin top-left. [page] is **0-based**.
class FieldArea {
  const FieldArea({required this.page, required this.x, required this.y, required this.w, required this.h, this.option});

  factory FieldArea.fromJson(Map<String, dynamic> json) => FieldArea(
    page: _int(json['page']),
    x: _double(json['x']),
    y: _double(json['y']),
    w: _double(json['w']),
    h: _double(json['h']),
    option: _stringOrNull(json['option']),
  );

  final int page;
  final double x;
  final double y;
  final double w;
  final double h;

  /// The choice a radio field's box stands for.
  final String? option;

  Map<String, Object?> toJson() => <String, Object?>{'page': page, 'x': x, 'y': y, 'w': w, 'h': h, 'option': ?option};

  FieldArea copyWith({int? page, double? x, double? y, double? w, double? h, String? option}) =>
      FieldArea(page: page ?? this.page, x: x ?? this.x, y: y ?? this.y, w: w ?? this.w, h: h ?? this.h, option: option ?? this.option);

  @override
  bool operator ==(Object other) =>
      other is FieldArea && other.page == page && other.x == x && other.y == y && other.w == w && other.h == h && other.option == option;

  @override
  int get hashCode => Object.hash(page, x, y, w, h, option);

  @override
  String toString() => 'FieldArea(page: $page, x: $x, y: $y, w: $w, h: $h)';
}

/// Per-field settings (`prefs_json`): font size, date format, options, …
///
/// Map-backed so the inspector can edit any key the registry's schema lists
/// ([PrefKeys]) without a typed model per field type. Unknown keys survive a
/// round trip untouched.
class FieldPrefs {
  FieldPrefs([Map<String, Object?> values = const <String, Object?>{}])
    : values = Map<String, Object?>.unmodifiable(values);

  factory FieldPrefs.fromJson(Object? json) => FieldPrefs(Map<String, Object?>.from(_map(json)));

  static final FieldPrefs empty = FieldPrefs();

  final Map<String, Object?> values;

  Object? operator [](String key) => values[key];

  bool get isEmpty => values.isEmpty;

  /// A copy with [key] set; null removes it.
  FieldPrefs set(String key, Object? value) {
    final next = Map<String, Object?>.from(values);
    if (value == null) {
      next.remove(key);
    } else {
      next[key] = value;
    }
    return FieldPrefs(next);
  }

  double? get fontSize => _numOrNull(values[PrefKeys.fontSize]);
  String? get align => values[PrefKeys.align] as String?;
  /// `normal` or `bold`.
  String get fontType => (values[PrefKeys.fontType] as String?) ?? 'normal';
  String? get color => values[PrefKeys.color] as String?;
  String? get valign => values[PrefKeys.valign] as String?;
  String? get format => values[PrefKeys.format] as String?;
  double? get min => _numOrNull(values[PrefKeys.min]);
  double? get max => _numOrNull(values[PrefKeys.max]);
  int? get decimals => _numOrNull(values[PrefKeys.decimals])?.toInt();
  /// Number of character boxes of a cells field.
  int? get length => _numOrNull(values[PrefKeys.length])?.toInt();
  bool get multiline => _bool(values[PrefKeys.multiline]);
  bool get defaultToday => _bool(values[PrefKeys.defaultToday]);
  String? get placeholder => _stringOrNull(values[PrefKeys.placeholder]);

  /// Choices of select, radio and multiple fields.
  List<String> get options {
    final raw = values[PrefKeys.options];
    if (raw is! List) return const <String>[];
    return raw.map((option) => '$option').where((option) => option.isNotEmpty).toList(growable: false);
  }

  static double? _numOrNull(Object? value) {
    if (value is num) return value.toDouble();
    if (value is String) return double.tryParse(value);
    return null;
  }

  Map<String, Object?> toJson() => values;

  @override
  bool operator ==(Object other) => other is FieldPrefs && jsonEncode(other.values) == jsonEncode(values);

  @override
  int get hashCode => jsonEncode(values).hashCode;
}

/// The keys of [FieldPrefs]. The registry (`field_types.dart`) says which
/// field type offers which.
abstract class PrefKeys {
  static const String fontSize = 'fontSize';
  static const String align = 'align';
  static const String fontType = 'fontType';
  static const String color = 'color';
  static const String valign = 'valign';
  static const String format = 'format';
  static const String min = 'min';
  static const String max = 'max';
  static const String decimals = 'decimals';
  static const String options = 'options';
  static const String placeholder = 'placeholder';
  static const String multiline = 'multiline';
  static const String length = 'cells';
  static const String charset = 'charset';
  static const String minLength = 'minLength';
  static const String maxLength = 'maxLength';
  static const String minSelect = 'minSelect';
  static const String maxSelect = 'maxSelect';
  static const String defaultToday = 'autoToday';
}

// Fields, values

/// What a party entered for a field. [value] is a String (text-like, ISO date,
/// option), a bool (checkbox) or a `List<String>` (multiple). Image-like
/// values (signature, initials, image, stamp, file) carry [hasFile] and, in
/// the creator's preview, [imageRef] (a `data:` URL or a route).
class SigningValue {
  const SigningValue({
    required this.fieldId,
    this.partyId,
    this.value,
    this.hasFile = false,
    this.method,
    this.fileName,
    this.imageRef,
    this.updatedAt,
  });

  factory SigningValue.fromJson(Map<String, dynamic> json, {String? fieldId}) {
    final raw = json.containsKey('value') ? json['value'] : json['valueJson'];
    return SigningValue(
      fieldId: fieldId ?? _string(json['fieldId']),
      partyId: _stringOrNull(json['partyId']),
      value: raw is List ? raw.map((item) => '$item').toList(growable: false) : raw,
      hasFile: _bool(json['hasFile']) || _stringOrNull(json['blobRef']) != null,
      method: SignatureMethod.parse(json['method']),
      fileName: _stringOrNull(json['fileName']),
      imageRef: _stringOrNull(json['image'] ?? json['imageUrl'] ?? json['imageRef']),
      updatedAt: parseSigningTime(json['updatedAt']),
    );
  }

  /// A value as the server stores it (`preview-values`): the JSON itself for
  /// typed fields, `{kind: 'image' | 'file', ...}` for uploads.
  factory SigningValue.fromStored(String fieldId, Object? raw) {
    if (raw is Map && (raw['kind'] == 'image' || raw['kind'] == 'file')) {
      return SigningValue(fieldId: fieldId, hasFile: true, fileName: _stringOrNull(raw['name']));
    }
    return SigningValue(fieldId: fieldId, value: raw is List ? raw.map((item) => '$item').toList(growable: false) : raw);
  }

  /// A value as the public API sends it: a typed value as it is, or for an
  /// upload the description `{kind: image|file, method?, name?, …}`.
  factory SigningValue.fromServer(String fieldId, Object? raw) {
    if (raw is Map && (raw['kind'] == 'image' || raw['kind'] == 'file')) {
      return SigningValue(
        fieldId: fieldId,
        hasFile: true,
        method: SignatureMethod.parse(raw['method']),
        fileName: _stringOrNull(raw['name']),
      );
    }
    return SigningValue(fieldId: fieldId, value: raw is List ? raw.map((item) => '$item').toList(growable: false) : raw);
  }

  final String fieldId;
  final String? partyId;
  final Object? value;
  final bool hasFile;
  final SignatureMethod? method;
  final String? fileName;
  final String? imageRef;
  final DateTime? updatedAt;

  /// Whether anything was entered.
  bool get isFilled {
    final current = value;
    if (hasFile || imageRef != null) return true;
    if (current == null) return false;
    if (current is String) return current.trim().isNotEmpty;
    if (current is List) return current.isNotEmpty;
    return true;
  }

  /// The bytes of a `data:` URL in [imageRef]; null for a route or none.
  Uint8List? get inlineImageBytes => decodeDataUrl(imageRef);

  Map<String, Object?> toJson() => <String, Object?>{'fieldId': fieldId, 'value': value};
}

/// Bytes of a `data:<type>;base64,<payload>` URL; null when it is not one.
Uint8List? decodeDataUrl(String? url) {
  if (url == null || !url.startsWith('data:')) return null;
  final comma = url.indexOf(',');
  if (comma < 0 || !url.substring(0, comma).endsWith(';base64')) return null;
  try {
    return base64Decode(url.substring(comma + 1));
  } on FormatException {
    return null;
  }
}

int _localIds = 0;

/// One box of the document. A field may have several [areas] (the same value
/// shown in each).
class SigningField {
  SigningField({
    required this.id,
    required this.partyId,
    required this.type,
    required this.areas,
    this.name = '',
    this.required = true,
    this.readonly = false,
    this.defaultValue,
    FieldPrefs? prefs,
    this.position = 0,
    this.value,
  }) : prefs = prefs ?? FieldPrefs.empty;

  factory SigningField.fromJson(Map<String, dynamic> json) {
    final rawPrefs = json.containsKey('prefs') ? json['prefs'] : json['prefsJson'];
    final value = json['value'];
    return SigningField(
      id: _string(json['id']),
      partyId: _string(json['partyId']),
      type: FieldType.parse(json['type']),
      name: _string(json['name']),
      required: _bool(json['required'], true),
      readonly: _bool(json['readonly']),
      defaultValue: json['defaultValue'],
      prefs: FieldPrefs.fromJson(rawPrefs),
      areas: _maps(json['areas']).map(FieldArea.fromJson).toList(growable: false),
      position: _int(json['position']),
      value: value == null ? null : SigningValue.fromServer(_string(json['id']), value),
    );
  }

  /// A field not yet known to the server: gets a client id the server
  /// replaces when it saves ([isLocal]).
  factory SigningField.local({
    required String partyId,
    required FieldType type,
    required List<FieldArea> areas,
    String name = '',
    bool required = true,
    FieldPrefs? prefs,
    Object? defaultValue,
  }) => SigningField(
    id: 'local_${DateTime.now().microsecondsSinceEpoch}_${_localIds++}',
    partyId: partyId,
    type: type,
    areas: areas,
    name: name,
    required: required,
    prefs: prefs ?? FieldPrefs.empty,
    defaultValue: defaultValue,
  );

  final String id;
  final String partyId;
  final FieldType type;
  final String name;
  final bool required;

  /// The party can't change it (a pre-filled value or a static heading).
  final bool readonly;
  final Object? defaultValue;
  final FieldPrefs prefs;
  final List<FieldArea> areas;
  final int position;

  /// Public view only: what another party already filled, for read-only
  /// rendering.
  final SigningValue? value;

  bool get isLocal => id.startsWith('local_');

  FieldArea get firstArea => areas.first;

  SigningField copyWith({
    String? id,
    String? partyId,
    FieldType? type,
    String? name,
    bool? required,
    bool? readonly,
    Object? defaultValue = _keep,
    FieldPrefs? prefs,
    List<FieldArea>? areas,
    int? position,
  }) => SigningField(
    id: id ?? this.id,
    partyId: partyId ?? this.partyId,
    type: type ?? this.type,
    name: name ?? this.name,
    required: required ?? this.required,
    readonly: readonly ?? this.readonly,
    defaultValue: identical(defaultValue, _keep) ? this.defaultValue : defaultValue,
    prefs: prefs ?? this.prefs,
    areas: areas ?? this.areas,
    position: position ?? this.position,
    value: value,
  );

  /// What `PUT /documents/:id/fields` reads; a local id is left out.
  Map<String, Object?> toJson() => <String, Object?>{
    if (!isLocal) 'id': id,
    'partyId': partyId,
    'type': type.name,
    'name': name,
    'required': required,
    'readonly': readonly,
    'defaultValue': defaultValue,
    'prefs': prefs.toJson(),
    'areas': areas.map((area) => area.toJson()).toList(growable: false),
    'position': position,
  };
}

const Object _keep = Object();

// Parties

class SigningParty {
  const SigningParty({
    required this.id,
    required this.name,
    this.position = 0,
    this.color = '',
    this.email,
    this.status = PartyStatus.pending,
    this.openedAt,
    this.consentedAt,
    this.completedAt,
    this.declinedAt,
    this.declineReason,
    this.signerName,
    this.signUrl,
    this.hasLink = false,
    this.linkHint,
    this.isSelf = false,
    this.delivery = const PartyDelivery(),
  });

  factory SigningParty.fromJson(Map<String, dynamic> json) => SigningParty(
    id: _string(json['id']),
    name: _string(json['name']),
    position: _int(json['position']),
    color: _string(json['color']),
    email: _stringOrNull(json['email']),
    status: PartyStatus.parse(json['status']),
    openedAt: parseSigningTime(json['openedAt']),
    consentedAt: parseSigningTime(json['consentedAt']),
    completedAt: parseSigningTime(json['completedAt']),
    declinedAt: parseSigningTime(json['declinedAt']),
    declineReason: _stringOrNull(json['declineReason']),
    signerName: _stringOrNull(json['signerName']),
    signUrl: _stringOrNull(json['signUrl']),
    hasLink: _bool(json['hasLink']),
    linkHint: _stringOrNull(json['linkHint']),
    isSelf: _bool(json['isSelf']),
    delivery: PartyDelivery.fromJson(json['delivery']),
  );

  final String id;
  final String name;
  final int position;

  /// `#rrggbb`, or empty: use [partyColor] with the party's index.
  final String color;
  final String? email;
  final PartyStatus status;
  final DateTime? openedAt;
  final DateTime? consentedAt;
  final DateTime? completedAt;
  final DateTime? declinedAt;
  final String? declineReason;
  final String? signerName;

  /// The full link; only present right after send/regenerate.
  final String? signUrl;

  /// Whether a working link exists (it can't be shown again).
  final bool hasLink;

  /// The last characters of the token, to tell links apart.
  final String? linkHint;

  /// The creator signs this part themselves.
  final bool isSelf;

  /// How the invitation mail went (none when no mail was sent).
  final PartyDelivery delivery;

  SigningParty copyWith({String? name, String? email, PartyStatus? status, DateTime? completedAt, bool? hasLink, String? linkHint, bool? isSelf, PartyDelivery? delivery}) => SigningParty(
    id: id,
    name: name ?? this.name,
    position: position,
    color: color,
    email: email ?? this.email,
    status: status ?? this.status,
    openedAt: openedAt,
    consentedAt: consentedAt,
    completedAt: completedAt ?? this.completedAt,
    declinedAt: declinedAt,
    declineReason: declineReason,
    signerName: signerName,
    signUrl: signUrl,
    hasLink: hasLink ?? this.hasLink,
    linkHint: linkHint ?? this.linkHint,
    isSelf: isSelf ?? this.isSelf,
    delivery: delivery ?? this.delivery,
  );
}

/// A party as the creator edits it (`PUT /documents/:id/parties`).
class PartyDraft {
  const PartyDraft({this.id, required this.name, this.email, this.color, this.isSelf = false});

  factory PartyDraft.of(SigningParty party) => PartyDraft(id: party.id, name: party.name, email: party.email, color: party.color, isSelf: party.isSelf);

  /// Null for a new party.
  final String? id;
  final String name;
  final String? email;
  final String? color;
  final bool isSelf;

  Map<String, Object?> toJson() => <String, Object?>{
    'id': ?id,
    'name': name,
    if (email != null && email!.isNotEmpty) 'email': email,
    if (color != null && color!.isNotEmpty) 'color': color,
    if (isSelf) 'isSelf': true,
  };
}

// Documents

class SigningDocument {
  const SigningDocument({
    required this.id,
    required this.title,
    this.status = SigningStatus.draft,
    this.signingOrder = SigningOrder.preserved,
    this.locale = '',
    this.expiresAt,
    this.pageCount = 0,
    this.sourceNodeId,
    this.finalNodeId,
    this.snapshotSha256,
    this.finalSha256,
    this.finalizeError,
    this.pageSizes = const <Size>[],
    this.createdAt,
    this.updatedAt,
    this.sentAt,
    this.completedAt,
    this.canceledAt,
    this.message,
    this.reminderDays = 0,
  });

  factory SigningDocument.fromJson(Map<String, dynamic> json) => SigningDocument(
    id: _string(json['id']),
    title: _string(json['title']),
    status: SigningStatus.parse(json['status']),
    signingOrder: SigningOrder.parse(json['signingOrder']),
    locale: _string(json['locale']),
    expiresAt: parseSigningTime(json['expiresAt']),
    pageCount: _int(json['pageCount']),
    sourceNodeId: _stringOrNull(json['sourceNodeId']),
    finalNodeId: _stringOrNull(json['finalNodeId']),
    snapshotSha256: _stringOrNull(json['snapshotSha256']),
    finalSha256: _stringOrNull(json['finalSha256']),
    finalizeError: _stringOrNull(json['finalizeError']),
    pageSizes: _maps(json['pages']).map((page) => Size(_double(page['width'], 595), _double(page['height'], 842))).toList(growable: false),
    createdAt: parseSigningTime(json['createdAt']),
    updatedAt: parseSigningTime(json['updatedAt']),
    sentAt: parseSigningTime(json['sentAt']),
    completedAt: parseSigningTime(json['completedAt']),
    canceledAt: parseSigningTime(json['canceledAt']),
    message: _stringOrNull(json['message']),
    reminderDays: _int(json['reminderDays']),
  );

  final String id;
  final String title;
  final SigningStatus status;
  final SigningOrder signingOrder;

  /// `en` or `de`; empty = the signer's language.
  final String locale;
  final DateTime? expiresAt;
  final int pageCount;
  final String? sourceNodeId;
  final String? finalNodeId;
  final String? snapshotSha256;
  final String? finalSha256;

  /// Why the signed file isn't there yet (the server retries on its own).
  final String? finalizeError;

  /// Each page's size as displayed; only the ratios matter.
  final List<Size> pageSizes;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? sentAt;
  final DateTime? completedAt;
  final DateTime? canceledAt;

  /// The creator's note in the invitation mail.
  final String? message;

  /// Automatic reminders every this many days; 0 = off.
  final int reminderDays;
}

/// `GET /documents/:id`.
class SigningDetail {
  const SigningDetail({required this.document, required this.parties, required this.fields, this.warnings = const <String>[]});

  factory SigningDetail.fromJson(Map<String, dynamic> json) => SigningDetail(
    warnings: json['warnings'] is List ? (json['warnings'] as List).map((item) => '$item').toList(growable: false) : const <String>[],
    document: SigningDocument.fromJson(_map(json['document'])),
    parties: _maps(json['parties']).map(SigningParty.fromJson).toList(growable: false),
    fields: _maps(json['fields']).map(SigningField.fromJson).toList(growable: false),
  );

  final SigningDocument document;
  final List<SigningParty> parties;
  final List<SigningField> fields;

  /// Notes from creating the document, e.g. `FORM_FIELDS_FLATTENED`.
  final List<String> warnings;
}

/// A signing link, shown only when it is created or regenerated.
class SigningLink {
  const SigningLink({required this.partyId, required this.url});

  factory SigningLink.fromJson(Map<String, dynamic> json) =>
      SigningLink(partyId: _string(json['partyId']), url: _string(json['url']));

  final String partyId;
  final String url;
}

/// `POST /documents/:id/send`.
class SigningSendResult {
  const SigningSendResult({required this.document, required this.links, this.delivery = const <SendDelivery>[]});

  factory SigningSendResult.fromJson(Map<String, dynamic> json) => SigningSendResult(
    document: SigningDocument.fromJson(_map(json['document'])),
    links: _maps(json['links']).map(SigningLink.fromJson).toList(growable: false),
    delivery: _maps(json['delivery']).map(SendDelivery.fromJson).toList(growable: false),
  );

  final SigningDocument document;
  final List<SigningLink> links;

  /// Per party, how the invitation mails went (empty without mail).
  final List<SendDelivery> delivery;
}

class SigningListParty {
  const SigningListParty({required this.id, required this.name, this.color = '', this.status = PartyStatus.pending});

  factory SigningListParty.fromJson(Map<String, dynamic> json) => SigningListParty(
    id: _string(json['id']),
    name: _string(json['name']),
    color: _string(json['color']),
    status: PartyStatus.parse(json['status']),
  );

  final String id;
  final String name;
  final String color;
  final PartyStatus status;
}

/// A row of `GET /documents`.
class SigningListItem {
  const SigningListItem({
    required this.id,
    required this.title,
    this.status = SigningStatus.draft,
    this.parties = const <SigningListParty>[],
    this.pageCount = 0,
    this.expiresAt,
    this.createdAt,
    this.updatedAt,
    this.completedAt,
    this.finalNodeId,
    this.sourceNodeId,
  });

  factory SigningListItem.fromJson(Map<String, dynamic> json) => SigningListItem(
    id: _string(json['id']),
    title: _string(json['title']),
    status: SigningStatus.parse(json['status']),
    parties: _maps(json['parties']).map(SigningListParty.fromJson).toList(growable: false),
    pageCount: _int(json['pageCount']),
    expiresAt: parseSigningTime(json['expiresAt']),
    createdAt: parseSigningTime(json['createdAt']),
    updatedAt: parseSigningTime(json['updatedAt']),
    completedAt: parseSigningTime(json['completedAt']),
    finalNodeId: _stringOrNull(json['finalNodeId']),
    sourceNodeId: _stringOrNull(json['sourceNodeId']),
  );

  final String id;
  final String title;
  final SigningStatus status;
  final List<SigningListParty> parties;
  final int pageCount;
  final DateTime? expiresAt;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? completedAt;
  final String? finalNodeId;
  final String? sourceNodeId;

  int get completedParties => parties.where((party) => party.status == PartyStatus.completed).length;
}

class SigningListPage {
  const SigningListPage({required this.items, this.nextCursor});

  factory SigningListPage.fromJson(Map<String, dynamic> json) => SigningListPage(
    items: _maps(json['items']).map(SigningListItem.fromJson).toList(growable: false),
    nextCursor: _stringOrNull(json['nextCursor']),
  );

  final List<SigningListItem> items;
  final String? nextCursor;
}

// Events

class SigningEvent {
  const SigningEvent({
    required this.id,
    required this.type,
    this.rawType = '',
    this.partyId,
    this.at,
    this.ip,
    this.userAgent,
    this.data = const <String, dynamic>{},
    this.hash,
    this.prevHash,
  });

  factory SigningEvent.fromJson(Map<String, dynamic> json) => SigningEvent(
    id: _string(json['id']),
    type: SigningEventType.parse(json['type']),
    rawType: _string(json['type']),
    partyId: _stringOrNull(json['partyId']),
    at: parseSigningTime(json['at']),
    ip: _stringOrNull(json['ip']),
    userAgent: _stringOrNull(json['userAgent']),
    data: _map(json['data']),
    hash: _stringOrNull(json['hash']),
    prevHash: _stringOrNull(json['prevHash']),
  );

  final String id;
  final SigningEventType type;

  /// The type as the server wrote it, for types this app doesn't know.
  final String rawType;
  final String? partyId;
  final DateTime? at;
  final String? ip;
  final String? userAgent;
  final Map<String, dynamic> data;
  final String? hash;
  final String? prevHash;
}

class SigningEventLog {
  const SigningEventLog({required this.events, this.chainValid, this.brokenAt});

  factory SigningEventLog.fromJson(Map<String, dynamic> json) => SigningEventLog(
    events: _maps(json['events']).map(SigningEvent.fromJson).toList(growable: false),
    chainValid: json['verify'] is Map ? _bool(_map(json['verify'])['chainValid']) : null,
    brokenAt: json['verify'] is Map ? _stringOrNull(_map(json['verify'])['brokenAt']) : null,
  );

  /// The first entry that doesn't match, when the chain is broken.
  final String? brokenAt;

  final List<SigningEvent> events;

  /// Whether the hash chain checked out; null when the server didn't say.
  final bool? chainValid;
}

// The public (signer) side

class PublicDocumentInfo {
  const PublicDocumentInfo({
    required this.title,
    this.pageCount = 0,
    this.locale = '',
    this.expiresAt,
    this.consentText = '',
    this.consentVersion = '',
  });

  factory PublicDocumentInfo.fromJson(Map<String, dynamic> json) => PublicDocumentInfo(
    title: _string(json['title']),
    pageCount: _int(json['pageCount']),
    locale: _string(json['locale']),
    expiresAt: parseSigningTime(json['expiresAt']),
    consentText: _string(json['consentText']),
    consentVersion: _string(json['consentVersion']),
  );

  final String title;
  final int pageCount;
  final String locale;
  final DateTime? expiresAt;
  final String consentText;
  final String consentVersion;
}

class PublicParty {
  const PublicParty({
    required this.id,
    required this.name,
    this.color = '',
    this.status = PartyStatus.pending,
    this.consentedAt,
    this.signerName,
  });

  factory PublicParty.fromJson(Map<String, dynamic> json) => PublicParty(
    id: _string(json['id']),
    name: _string(json['name']),
    color: _string(json['color']),
    status: PartyStatus.parse(json['status']),
    consentedAt: parseSigningTime(json['consentedAt']),
    signerName: _stringOrNull(json['signerName']),
  );

  final String id;
  final String name;
  final String color;
  final PartyStatus status;

  /// Set once the signer agreed to sign electronically (own party only).
  final DateTime? consentedAt;

  /// The name given at consent (own party only).
  final String? signerName;
}

class PublicTurn {
  const PublicTurn({this.canSign = false, this.waitingCount = 0});

  factory PublicTurn.fromJson(Map<String, dynamic> json) => PublicTurn(canSign: _bool(json['canSign']), waitingCount: _int(json['waitingCount']));

  final bool canSign;

  /// How many other signers have to sign before this one. Never their names:
  /// a signer only learns how many.
  final int waitingCount;
}

/// How many signers have finished, out of all of them. Numbers only.
class PublicProgress {
  const PublicProgress({this.done = 0, this.total = 0});

  factory PublicProgress.fromJson(Map<String, dynamic> json) => PublicProgress(done: _int(json['done']), total: _int(json['total']));

  final int done;
  final int total;

  bool get everyoneDone => total > 0 && done >= total;
}

/// `GET /api/public/sign/:token`.
class PublicSigningView {
  const PublicSigningView({
    required this.state,
    required this.document,
    required this.party,
    this.progress = const PublicProgress(),
    this.fields = const <SigningField>[],
    this.myValues = const <SigningValue>[],
    this.turn = const PublicTurn(),
    this.savedNodeId,
  });

  factory PublicSigningView.fromJson(Map<String, dynamic> json) => PublicSigningView(
    state: PublicSigningState.parse(json['state']),
    document: PublicDocumentInfo.fromJson(_map(json['document'])),
    party: PublicParty.fromJson(_map(json['party'])),
    progress: PublicProgress.fromJson(_map(json['progress'])),
    fields: _maps(json['fields']).map(SigningField.fromJson).toList(growable: false),
    myValues: _values(json['myValues']),
    turn: PublicTurn.fromJson(_map(json['turn'])),
    savedNodeId: _stringOrNull(json['savedNodeId'] ?? (json['autosaved'] is String ? json['autosaved'] : null)),
  );

  /// `myValues` is a `fieldId → value` map (a list of value objects is read too).
  static List<SigningValue> _values(Object? raw) {
    if (raw is List) return _maps(raw).map((row) => SigningValue.fromJson(row)).toList(growable: false);
    if (raw is Map) {
      return raw.entries.map((entry) => SigningValue.fromServer('${entry.key}', entry.value)).toList(growable: false);
    }
    return const <SigningValue>[];
  }

  final PublicSigningState state;
  final PublicDocumentInfo document;
  final PublicParty party;

  /// How many signers have finished; the other parties' names are never sent.
  final PublicProgress progress;

  /// This party's fields and, read-only, the filled fields of the others.
  final List<SigningField> fields;
  final List<SigningValue> myValues;
  final PublicTurn turn;

  /// The signed copy the server filed into the signer's own cloud (only for
  /// a signed-in signer whose profile asks for it), when the payload says so.
  final String? savedNodeId;

  /// The fields this party fills in, in reading order (page, top, left).
  List<SigningField> get ownFields => fields.where((field) => field.partyId == party.id).toList(growable: false);

  /// Other parties' fields, to draw read-only.
  List<SigningField> get otherFields => fields.where((field) => field.partyId != party.id).toList(growable: false);

  bool get canSign => state == PublicSigningState.ready && turn.canSign;

  SigningValue? valueOf(String fieldId) {
    for (final value in myValues) {
      if (value.fieldId == fieldId) return value;
    }
    return null;
  }
}
