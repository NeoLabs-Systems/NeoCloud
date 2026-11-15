import 'dart:typed_data';

import 'account_api.dart';
import 'models.dart';
import 'saved_signatures.dart';
import 'signing_api.dart';

/// An in-memory [SigningAccountApi] for tests and the browser check: a
/// profile that stores what it is given, claims, an inbox and reminders that
/// can be made to fail on demand.
class FakeSigningAccountApi implements SigningAccountApi {
  FakeSigningAccountApi({SigningProfile? profile, this.inboxItems = const <InboxItem>[], this.doneItems = const <InboxItem>[]}) : _profile = profile ?? const SigningProfile();

  SigningProfile _profile;
  List<InboxItem> inboxItems;
  List<InboxItem> doneItems;
  final Map<String, Uint8List> _images = <String, Uint8List>{};
  int _counter = 0;

  /// Throws this on the next call, once.
  SigningException? failNext;

  /// What [remind] throws until set back to null (e.g. a 429).
  SigningException? remindError;

  /// What [claim] throws (e.g. `ALREADY_CLAIMED`).
  SigningException? claimError;

  /// What a claim reports as fillable and fills.
  bool canPrefill = true;
  List<String> prefillFieldIds = const <String>[];

  /// Runs when a claim applies the profile (fill the public fake's values).
  void Function()? onApply;

  final List<String> reminded = <String>[];
  final List<String> claimed = <String>[];
  final List<bool> claimApplied = <bool>[];
  final List<String> opened = <String>[];
  int profileSaves = 0;

  void _maybeFail() {
    final error = failNext;
    if (error != null) {
      failNext = null;
      throw error;
    }
  }

  SigningProfile get current => _profile;

  @override
  Future<RemindResult> remind(String documentId, String partyId, {String? message}) async {
    _maybeFail();
    final error = remindError;
    if (error != null) throw error;
    reminded.add(partyId);
    return RemindResult(partyId: partyId, delivery: PartyDelivery(status: DeliveryStatus.sent, lastSentAt: DateTime.now(), attempts: 1, remindersSent: reminded.where((id) => id == partyId).length));
  }

  @override
  Future<List<RemindResult>> remindAll(String documentId) async {
    _maybeFail();
    final error = remindError;
    if (error != null) throw error;
    return <RemindResult>[];
  }

  @override
  Future<String> openAsOwner(String documentId, String partyId) async {
    _maybeFail();
    opened.add(partyId);
    return 'https://cloud.example/app/sign/grant-$partyId';
  }

  @override
  Future<SigningProfile> profile() async {
    _maybeFail();
    return _profile;
  }

  @override
  Future<SigningProfile> saveProfile(SigningProfile profile) async {
    _maybeFail();
    profileSaves++;
    _profile = profile.copyWith(signatures: _profile.signatures);
    return _profile;
  }

  @override
  Future<ProfileImage> addImage(Uint8List png, {required SavedKind kind, required SignatureMethod method, String? label, bool makeDefault = false}) async {
    _maybeFail();
    final existing = _profile.imagesOf(kind);
    if (existing.length >= 5) throw const SigningException(SigningErrorKind.invalid, statusCode: 400, code: 'TOO_MANY');
    final image = ProfileImage(id: 'img${++_counter}', kind: kind, method: method, label: label, isDefault: makeDefault || existing.isEmpty, createdAt: DateTime.now());
    _images[image.id] = png;
    _profile = _profile.copyWith(
      signatures: <ProfileImage>[
        for (final item in _profile.signatures)
          if (item.kind == kind && image.isDefault) _withDefault(item, false) else item,
        image,
      ],
    );
    return image;
  }

  ProfileImage _withDefault(ProfileImage image, bool isDefault, {String? label}) =>
      ProfileImage(id: image.id, kind: image.kind, method: image.method, label: label ?? image.label, isDefault: isDefault, createdAt: image.createdAt);

  @override
  Future<Uint8List> imageBytes(String imageId) async {
    _maybeFail();
    return _images[imageId] ?? kFakePng;
  }

  @override
  Future<void> updateImage(String imageId, {String? label, bool? isDefault}) async {
    _maybeFail();
    final target = _profile.signatures.firstWhere((image) => image.id == imageId, orElse: () => throw const SigningException(SigningErrorKind.notFound, statusCode: 404));
    _profile = _profile.copyWith(
      signatures: <ProfileImage>[
        for (final image in _profile.signatures)
          if (image.id == imageId)
            _withDefault(image, isDefault ?? image.isDefault, label: label)
          else if (isDefault == true && image.kind == target.kind)
            _withDefault(image, false)
          else
            image,
      ],
    );
  }

  @override
  Future<void> deleteImage(String imageId) async {
    _maybeFail();
    final removed = _profile.signatures.where((image) => image.id == imageId).firstOrNull;
    final rest = _profile.signatures.where((image) => image.id != imageId).toList();
    if (removed != null && removed.isDefault) {
      final next = rest.indexWhere((image) => image.kind == removed.kind);
      if (next >= 0) rest[next] = _withDefault(rest[next], true);
    }
    _profile = _profile.copyWith(signatures: rest);
  }

  @override
  Future<ClaimResult> claim(String token, {bool apply = false, bool preview = false}) async {
    _maybeFail();
    final error = claimError;
    if (error != null) throw error;
    if (!preview) {
      claimed.add(token);
      claimApplied.add(apply);
    }
    if (apply) onApply?.call();
    return ClaimResult(
      partyId: 'p-claimed',
      documentTitle: 'Lease',
      canPrefill: canPrefill,
      prefill: <PrefillItem>[for (final id in prefillFieldIds) PrefillItem(fieldId: id, name: 'Field $id', type: 'text', value: 'Saved value')],
      applied: apply ? prefillFieldIds : const <String>[],
    );
  }

  /// What [inboxFinal] returns; null answers 404.
  Uint8List? inboxFinalBytes;
  final List<String> inboxFinalRequests = <String>[];

  @override
  Future<Uint8List> inboxFinal(String partyId, {bool certificate = true}) async {
    _maybeFail();
    inboxFinalRequests.add(partyId);
    final bytes = inboxFinalBytes;
    if (bytes == null) throw const SigningException(SigningErrorKind.notFound, statusCode: 404);
    return bytes;
  }

  @override
  Future<InboxPage> inbox({required bool done}) async {
    _maybeFail();
    return InboxPage(items: done ? doneItems : inboxItems, todo: inboxItems.length);
  }

  @override
  Future<String> openInbox(String partyId) async {
    _maybeFail();
    opened.add(partyId);
    return 'https://cloud.example/app/sign/grant-$partyId';
  }
}

/// A 1x1 transparent PNG, for pictures the fake keeps no bytes of.
final Uint8List kFakePng = Uint8List.fromList(const <int>[
  0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06,
  0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xff, 0xff, 0x3f, 0x00, 0x05, 0xfe, 0x02,
  0xfe, 0xa7, 0x35, 0x81, 0x84, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
]);
