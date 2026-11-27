import 'dart:typed_data';

import 'models.dart';
import 'quick_sign.dart';

/// The two faces of the signing API, as interfaces so the editor, the status
/// page and the public flow can be built and tested against
/// [FakeSigningApi] / [FakePublicSigningApi] before (or without) a server.
///
/// * [SigningApi]: the creator's routes under `/api/signing`; session or
///   phone token, reached as `controller.signing`.
/// * [PublicSigningApi]: the signer's routes under `/api/public/sign/:token`;
///   no account, no auth header, no session-expiry handling. Made from a
///   token with `controller.publicSigning(token)`.
///
/// Every failure is a [SigningException].

enum SigningErrorKind {
  /// The server couldn't be reached.
  network,

  /// 429: too many requests; see [SigningException.retryAfter].
  rateLimited,

  /// 404: an unknown id, or (public) an unknown, revoked or replaced link.
  notFound,

  /// 410 or an `*_EXPIRED` code: the request ran out.
  expired,

  /// 409: the document changed under the caller (already opened, completed,
  /// canceled, not this party's turn).
  conflict,

  /// 400/422: the server refused the content; [SigningException.message]
  /// says why in words for the user.
  invalid,

  /// 401/403: not signed in, or not allowed.
  unauthorized,

  /// Anything else, 5xx included.
  server,
}

class SigningException implements Exception {
  const SigningException(this.kind, {this.statusCode = 0, this.message = '', this.code, this.retryAfter, this.fieldIds = const <String>[]});

  /// Maps an HTTP failure to a kind.
  factory SigningException.fromStatus(int statusCode, {String message = '', String? code, Duration? retryAfter, List<String> fieldIds = const <String>[]}) {
    final SigningErrorKind kind;
    if (statusCode == 0) {
      kind = SigningErrorKind.network;
    } else if (statusCode == 429) {
      kind = SigningErrorKind.rateLimited;
    } else if (statusCode == 410 || (code != null && code.toUpperCase().contains('EXPIRED'))) {
      kind = SigningErrorKind.expired;
    } else if (statusCode == 404) {
      kind = SigningErrorKind.notFound;
    } else if (statusCode == 409) {
      kind = SigningErrorKind.conflict;
    } else if (statusCode == 400 || statusCode == 413 || statusCode == 415 || statusCode == 422) {
      kind = SigningErrorKind.invalid;
    } else if (statusCode == 401 || statusCode == 403) {
      kind = SigningErrorKind.unauthorized;
    } else {
      kind = SigningErrorKind.server;
    }
    return SigningException(kind, statusCode: statusCode, message: message, code: code, retryAfter: retryAfter, fieldIds: fieldIds);
  }

  final SigningErrorKind kind;
  final int statusCode;

  /// The server's text; may be empty and is not meant for non-admins when
  /// [kind] is [SigningErrorKind.server]: show your own sentence there.
  final String message;

  /// The API's stable machine code (`SIGNING_LOCKED`, …), when it sent one.
  final String? code;
  final Duration? retryAfter;

  /// `FIELDS_INCOMPLETE`: the required fields that are still empty.
  final List<String> fieldIds;

  bool get isNetwork => kind == SigningErrorKind.network;

  @override
  String toString() => 'SigningException($kind, $statusCode${code == null ? '' : ', $code'})';
}

/// What `PUT /documents/:id` may change (draft, or sent and still unopened).
class SigningSettingsPatch {
  const SigningSettingsPatch({this.title, this.signingOrder, this.expiresAt, this.clearExpiry = false, this.locale});

  final String? title;
  final SigningOrder? signingOrder;
  final DateTime? expiresAt;

  /// Send `expiresAt: null`, i.e. the link never expires.
  final bool clearExpiry;

  /// `en` or `de`.
  final String? locale;

  Map<String, Object?> toJson() => <String, Object?>{
    'title': ?title,
    if (signingOrder != null) 'signingOrder': signingOrder!.name,
    if (expiresAt != null) 'expiresAt': expiresAt!.toUtc().toIso8601String(),
    if (clearExpiry) 'expiresAt': null,
    'locale': ?locale,
  };
}

/// `GET /api/signing/capabilities`.
class SigningCapabilities {
  const SigningCapabilities({this.office = false, this.maxParties = 20, this.maxFields = 500, this.emailConfigured = false});

  factory SigningCapabilities.fromJson(Map<String, dynamic> json) {
    final formats = json['formats'] is Map ? (json['formats'] as Map) : const <String, dynamic>{};
    return SigningCapabilities(
      office: formats['office'] == true,
      maxParties: json['maxParties'] is int ? json['maxParties'] as int : 20,
      maxFields: json['maxFields'] is int ? json['maxFields'] as int : 500,
      emailConfigured: json['email'] is Map && (json['email'] as Map)['configured'] == true,
    );
  }

  /// Office documents are converted (LibreOffice on the server).
  final bool office;
  final int maxParties;
  final int maxFields;

  /// The server can send mail; every email option of signing hangs on it.
  final bool emailConfigured;
}

/// One field a detection added: what it was found as and how sure it is.
class DetectedField {
  const DetectedField({required this.id, required this.role, this.confidence = 1, this.source = 'layout'});

  factory DetectedField.fromJson(Map<String, dynamic> json) => DetectedField(
    id: '${json['id'] ?? ''}',
    role: '${json['role'] ?? ''}',
    confidence: json['confidence'] is num ? (json['confidence'] as num).toDouble() : 1,
    source: '${json['source'] ?? 'layout'}',
  );

  final String id;

  /// signature, initials, date, name, place, text, checkbox, radio, select,
  /// multiple or cells.
  final String role;

  /// 0..1; the PDF's own form fields have 1.
  final double confidence;

  /// `form` (the PDF's form field) or `layout` (read from the page).
  final String source;
}

/// `POST /documents/:id/import-fields`: what was added, and all fields now.
class SigningDetection {
  const SigningDetection({required this.fields, this.detected = const <DetectedField>[]});

  factory SigningDetection.fromJson(Map<String, dynamic> json) => SigningDetection(
    fields: <SigningField>[
      for (final row in json['fields'] is List ? json['fields'] as List : const <Object?>[])
        if (row is Map) SigningField.fromJson(row.cast<String, dynamic>()),
    ],
    detected: <DetectedField>[
      for (final row in json['detected'] is List ? json['detected'] as List : const <Object?>[])
        if (row is Map) DetectedField.fromJson(row.cast<String, dynamic>()),
    ],
  );

  final List<SigningField> fields;
  final List<DetectedField> detected;

  int get imported => detected.length;
}

abstract class SigningApi {
  /// What the server can turn into a signable document.
  Future<SigningCapabilities> capabilities();

  /// Makes [nodeId] (a file of the account) a signing draft. The PDF is
  /// frozen now, so field positions always match its pages.
  Future<SigningDetail> createDocument({required String nodeId, String? title, String? locale});

  Future<SigningListPage> list({SigningStatus? status, int? limit, String? cursor});

  Future<SigningDetail> get(String id);

  /// The frozen PDF, whole (no Range), for pdfrx.
  Future<Uint8List> pdf(String id);

  /// Every value entered so far, for the progress preview. Images carry
  /// [SigningValue.imageRef].
  Future<List<SigningValue>> previewValues(String id);

  Future<SigningDocument> update(String id, SigningSettingsPatch patch);

  /// Replace-all save; returns the fields as stored, with their ids.
  Future<List<SigningField>> saveFields(String id, List<SigningField> fields);

  /// Replace-all save of 1..20 parties; returns them as stored.
  Future<List<SigningParty>> saveParties(String id, List<PartyDraft> parties);

  /// Finds the PDF's form fields and the places its pages mark for a
  /// signature, initials, a date, a name or a tick, and adds them to the
  /// first party (draft only). [roles] keeps only those roles (see
  /// [DetectedField.role]); [minConfidence] only the more certain finds.
  /// Spots already covered by a field are skipped.
  Future<SigningDetection> detectFields(String id, {Set<String>? roles, double? minConfidence});

  /// Turns pages by [quarterTurns] clockwise (negative: the other way), all
  /// of them when [pages] (0-based) is null. Draft, or sent and unopened. The
  /// frozen PDF is replaced and the boxes on the turned pages move with them.
  Future<SigningDetail> rotatePages(String id, {required int quarterTurns, List<int>? pages});

  /// Freezes the PDF and makes the links. This is the only time the links
  /// are returned (besides [regenerateLink]). The server refuses a party
  /// without anything to fill in (`PARTY_WITHOUT_FIELDS`).
  ///
  /// [options] carry the mail settings (message, whether to mail the
  /// invitations, automatic reminders); null sends as before.
  Future<SigningSendResult> send(String id, {SendOptions? options});

  /// A new link for [partyId]; the old one stops working.
  Future<SigningLink> regenerateLink(String id, String partyId);

  Future<SigningDocument> cancel(String id);

  /// Kills one party's link without touching the others.
  Future<void> revokeParty(String id, String partyId);

  Future<void> delete(String id);

  Future<SigningDetail> duplicate(String id);

  /// Signs the draft [id] as its owner in one step: the owner becomes its
  /// only party, the fields are placed and filled, and the document goes on
  /// to `finalizing`; the signed file is saved beside the original. A refusal
  /// before anything was signed leaves the draft; a failure later removes it.
  Future<SigningDetail> quickSign(String id, QuickSignRequest request);

  /// The picture a signer uploaded for [fieldId], for the progress preview.
  Future<Uint8List> valueImage(String id, String fieldId);

  /// Tries the signed file again right away (document `finalizing`).
  Future<SigningDetail> retryFinalization(String id);

  Future<SigningEventLog> events(String id);

  /// The audit trail as the server writes it, for saving to a file.
  Future<Map<String, dynamic>> auditJson(String id);

  /// The signed PDF; only once the document is completed. With [certificate]
  /// false the appended signature pages (certificate, audit trail) are left
  /// out.
  Future<Uint8List> finalPdf(String id, {bool certificate = true});

  /// The signed file of a library node without the signature pages
  /// (`GET /api/signing/nodes/:nodeId/clean`).
  Future<Uint8List> cleanPdfForNode(String nodeId);
}

abstract class PublicSigningApi {
  /// The page's state, this party's fields and values. The first call marks
  /// the link as opened.
  Future<PublicSigningView> view();

  /// The frozen PDF, whole.
  Future<Uint8List> pdf();

  /// Records consent to sign electronically, with the name the signer will
  /// sign with and (optionally) their time zone for the audit trail.
  Future<void> consent({required String signerName, String? timezone, int? tzOffsetMinutes});

  /// Saves one value (autosave); see `field_types.dart` for the shapes.
  Future<void> saveValue(String fieldId, Object? value);

  /// Uploads an image or file for a field (a signature, initials, an image, a
  /// stamp, a file). [method] says how a signature came about; [typedText] is
  /// the typed name of a typed signature.
  Future<void> uploadValue(
    String fieldId, {
    required Uint8List bytes,
    required String fileName,
    String contentType = 'image/png',
    SignatureMethod? method,
    String? typedText,
  });

  /// The image a filled field holds (an earlier signature, another party's).
  Future<Uint8List> valueImage(String fieldId);

  /// Finishes this party's part; returns the state afterwards (`completed`,
  /// or `waiting`/… if the document moved on).
  Future<PublicSigningState> complete();

  Future<void> decline({String? reason});

  /// After completing: whether the signed copy can be downloaded already.
  Future<bool> finalAvailable();

  /// The signed file; [certificate] false leaves the appended signature
  /// pages out.
  Future<Uint8List> finalPdf({bool certificate = true});
}
