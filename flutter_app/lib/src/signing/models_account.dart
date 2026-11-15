part of 'models.dart';

// The account side of signing: mail delivery of invitations and reminders,
// the signing profile, claiming a link with an account and the inbox of
// requests addressed to the account. One-to-one with the API contract; like
// the rest of the models these are tolerant of missing and unknown values.

/// What happened to one party's invitation mail. Unknown reads as [none].
enum DeliveryStatus {
  none,
  queued,
  sent,
  skipped,
  noEmail,
  failed;

  static DeliveryStatus parse(Object? raw) => _enum(values, raw, none, (value) => _snake(value.name));

  String get wire => _snake(name);
}

/// `party.delivery` of a sent document.
class PartyDelivery {
  const PartyDelivery({this.status = DeliveryStatus.none, this.lastSentAt, this.attempts = 0, this.lastError, this.errorCode, this.remindersSent = 0, this.nextReminderAt});

  factory PartyDelivery.fromJson(Object? raw) {
    final json = _map(raw);
    return PartyDelivery(
      status: DeliveryStatus.parse(json['status']),
      lastSentAt: parseSigningTime(json['lastSentAt']),
      attempts: _int(json['attempts']),
      lastError: _stringOrNull(json['lastError']),
      errorCode: _stringOrNull(json['errorCode']),
      remindersSent: _int(json['remindersSent']),
      nextReminderAt: parseSigningTime(json['nextReminderAt']),
    );
  }

  final DeliveryStatus status;
  final DateTime? lastSentAt;
  final int attempts;

  /// A short sentence from the server; the screen shows its own text by [errorCode].
  final String? lastError;

  /// `SMTP_UNREACHABLE`, `SMTP_REJECTED`, `INVALID_ADDRESS`, `RATE_LIMITED` or `UNKNOWN`.
  final String? errorCode;
  final int remindersSent;
  final DateTime? nextReminderAt;
}

/// One entry of `delivery` in the send answer.
class SendDelivery {
  const SendDelivery({required this.partyId, required this.status, this.error});

  factory SendDelivery.fromJson(Map<String, dynamic> json) =>
      SendDelivery(partyId: _string(json['partyId']), status: DeliveryStatus.parse(json['status']), error: _stringOrNull(json['error']));

  final String partyId;
  final DeliveryStatus status;
  final String? error;
}

/// The mail options of `POST /documents/:id/send`.
class SendOptions {
  const SendOptions({this.message, this.deliverEmail, this.reminderDays});

  final String? message;
  final bool? deliverEmail;

  /// 0 (off), 2, 3, 5, 7 or 14.
  final int? reminderDays;

  static const List<int> reminderChoices = <int>[0, 2, 3, 5, 7, 14];

  Map<String, Object?> toJson() => <String, Object?>{
    if (message != null && message!.trim().isNotEmpty) 'message': message!.trim(),
    'deliverEmail': ?deliverEmail,
    'reminderDays': ?reminderDays,
  };
}

/// The answer of a reminder: the party's delivery state afterwards.
class RemindResult {
  const RemindResult({required this.partyId, this.delivery = const PartyDelivery(), this.errorCode});

  final String partyId;
  final PartyDelivery delivery;

  /// Set by "remind everyone" when this party's reminder was refused
  /// (`REMINDER_TOO_SOON`, `NO_EMAIL`, `PARTY_NOT_ACTIVE`).
  final String? errorCode;

  bool get ok => errorCode == null;
}

// Profile

class ProfilePreset {
  const ProfilePreset({required this.id, required this.label, required this.value});

  factory ProfilePreset.fromJson(Map<String, dynamic> json) => ProfilePreset(id: _string(json['id']), label: _string(json['label']), value: _string(json['value']));

  final String id;
  final String label;
  final String value;

  Map<String, Object?> toJson() => <String, Object?>{'id': id, 'label': label, 'value': value};
}

/// A signature or initials kept in the profile (the picture is fetched on
/// its own: `GET /api/signing/profile/images/:id`).
class ProfileImage {
  const ProfileImage({required this.id, required this.kind, this.method, this.label, this.isDefault = false, this.createdAt});

  factory ProfileImage.fromJson(Map<String, dynamic> json) => ProfileImage(
    id: _string(json['id']),
    kind: _string(json['kind']) == 'initials' ? SavedKind.initials : SavedKind.signature,
    method: SignatureMethod.parse(json['method']),
    label: _stringOrNull(json['label']),
    isDefault: _bool(json['isDefault']),
    createdAt: parseSigningTime(json['createdAt']),
  );

  final String id;
  final SavedKind kind;
  final SignatureMethod? method;
  final String? label;
  final bool isDefault;
  final DateTime? createdAt;
}

/// `GET/PUT /api/signing/profile`.
class SigningProfile {
  const SigningProfile({
    this.displayName = '',
    this.email = '',
    this.phone = '',
    this.presets = const <ProfilePreset>[],
    this.autoFill = true,
    this.autoSave = true,
    this.saveFolderId,
    this.notifyOnEachSignature = false,
    this.signatures = const <ProfileImage>[],
  });

  factory SigningProfile.fromJson(Map<String, dynamic> json) => SigningProfile(
    displayName: _string(json['displayName']),
    email: _string(json['email']),
    phone: _string(json['phone']),
    presets: _maps(json['presets']).map(ProfilePreset.fromJson).toList(growable: false),
    autoFill: _bool(json['autoFill'], true),
    autoSave: _bool(json['autoSave'], true),
    saveFolderId: _stringOrNull(json['saveFolderId']),
    notifyOnEachSignature: _bool(json['notifyOnEachSignature']),
    signatures: _maps(json['signatures']).map(ProfileImage.fromJson).toList(growable: false),
  );

  final String displayName;
  final String email;
  final String phone;
  final List<ProfilePreset> presets;
  final bool autoFill;
  final bool autoSave;

  /// Null: the server's own "Signed documents" folder.
  final String? saveFolderId;
  final bool notifyOnEachSignature;
  final List<ProfileImage> signatures;

  List<ProfileImage> imagesOf(SavedKind kind) => signatures.where((image) => image.kind == kind).toList(growable: false);

  SigningProfile copyWith({
    String? displayName,
    String? email,
    String? phone,
    List<ProfilePreset>? presets,
    bool? autoFill,
    bool? autoSave,
    Object? saveFolderId = _keep,
    bool? notifyOnEachSignature,
    List<ProfileImage>? signatures,
  }) => SigningProfile(
    displayName: displayName ?? this.displayName,
    email: email ?? this.email,
    phone: phone ?? this.phone,
    presets: presets ?? this.presets,
    autoFill: autoFill ?? this.autoFill,
    autoSave: autoSave ?? this.autoSave,
    saveFolderId: identical(saveFolderId, _keep) ? this.saveFolderId : saveFolderId as String?,
    notifyOnEachSignature: notifyOnEachSignature ?? this.notifyOnEachSignature,
    signatures: signatures ?? this.signatures,
  );

  /// What `PUT` takes: the settings, not the pictures (they have their own
  /// calls).
  Map<String, Object?> toJson() => <String, Object?>{
    'displayName': displayName,
    'email': email,
    'phone': phone,
    'presets': presets.map((preset) => preset.toJson()).toList(growable: false),
    'autoFill': autoFill,
    'autoSave': autoSave,
    'saveFolderId': saveFolderId,
    'notifyOnEachSignature': notifyOnEachSignature,
  };
}

// Claim

class PrefillItem {
  const PrefillItem({required this.fieldId, this.name = '', this.type = '', this.source = '', this.value, this.imageId});

  factory PrefillItem.fromJson(Map<String, dynamic> json) => PrefillItem(
    fieldId: _string(json['fieldId']),
    name: _string(json['name']),
    type: _string(json['type']),
    source: _string(json['source']),
    value: json['value'],
    imageId: _stringOrNull(json['imageId']),
  );

  final String fieldId;
  final String name;
  final String type;
  final String source;
  final Object? value;
  final String? imageId;
}

/// `POST /api/signing/claim`.
class ClaimResult {
  const ClaimResult({required this.partyId, this.documentTitle = '', this.prefill = const <PrefillItem>[], this.canPrefill = false, this.applied = const <String>[]});

  factory ClaimResult.fromJson(Map<String, dynamic> json) => ClaimResult(
    partyId: _string(json['partyId']),
    documentTitle: _string(json['documentTitle']),
    prefill: _maps(json['prefill']).map(PrefillItem.fromJson).toList(growable: false),
    canPrefill: _bool(json['canPrefill']),
    applied: json['applied'] is List ? (json['applied'] as List).map((id) => '$id').toList(growable: false) : const <String>[],
  );

  final String partyId;
  final String documentTitle;
  final List<PrefillItem> prefill;
  final bool canPrefill;

  /// The fields that were filled in from the profile (with `apply: true`).
  final List<String> applied;
}

// Inbox

/// One request addressed to the account (`GET /api/signing/inbox`).
class InboxItem {
  const InboxItem({
    this.finalAvailable = false,
    required this.documentId,
    required this.partyId,
    required this.title,
    this.creatorName = '',
    this.status = PartyStatus.pending,
    this.expiresAt,
    this.sentAt,
    this.completedAt,
    this.savedNodeId,
  });

  factory InboxItem.fromJson(Map<String, dynamic> json) => InboxItem(
    documentId: _string(json['documentId']),
    partyId: _string(json['partyId']),
    title: _string(json['title']),
    creatorName: _string(json['creatorName']),
    status: PartyStatus.parse(json['status']),
    expiresAt: parseSigningTime(json['expiresAt']),
    sentAt: parseSigningTime(json['sentAt']),
    completedAt: parseSigningTime(json['completedAt']),
    savedNodeId: _stringOrNull(json['savedNodeId']),
    finalAvailable: _bool(json['finalAvailable']),
  );

  /// The signed file can be downloaded through the inbox.
  final bool finalAvailable;
  final String documentId;
  final String partyId;
  final String title;
  final String creatorName;

  /// `waiting`: an earlier signer is first; anything else open is ready.
  final PartyStatus status;
  final DateTime? expiresAt;
  final DateTime? sentAt;
  final DateTime? completedAt;
  final String? savedNodeId;

  bool get isWaiting => status == PartyStatus.waiting;
}

class InboxPage {
  const InboxPage({this.items = const <InboxItem>[], this.todo = 0});

  factory InboxPage.fromJson(Map<String, dynamic> json) =>
      InboxPage(items: _maps(json['items']).map(InboxItem.fromJson).toList(growable: false), todo: _int(_map(json['counts'])['todo']));

  final List<InboxItem> items;
  final int todo;
}
