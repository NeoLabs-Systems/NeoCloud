import 'dart:typed_data';

import 'models.dart';
import 'saved_signatures.dart';
import 'signing_api.dart';

/// What the creator and the signer do with their NeoCloud account, apart
/// from the documents themselves: reminders, the signing profile, claiming a
/// link and the inbox. A separate interface from [SigningApi] so a fake can
/// stand in for it (`fake_signing_account_api.dart`); every failure is a
/// [SigningException]. Reached as `controller.signingAccount`.
abstract class SigningAccountApi {
  // Reminders

  /// Sends the invitation mail to [partyId] again. Throws
  /// `PARTY_NOT_ACTIVE` (409), `NO_EMAIL` (400), `EMAIL_NOT_CONFIGURED`
  /// (503) and `REMINDER_TOO_SOON` (429, [SigningException.retryAfter]).
  Future<RemindResult> remind(String documentId, String partyId, {String? message});

  /// A reminder for everyone who hasn't signed; one result per party.
  Future<List<RemindResult>> remindAll(String documentId);

  /// The creator's own party: a link for this browser (`/app/sign/<grant>`).
  Future<String> openAsOwner(String documentId, String partyId);

  // Profile

  Future<SigningProfile> profile();

  Future<SigningProfile> saveProfile(SigningProfile profile);

  /// Keeps a picture in the profile; [makeDefault] makes it the one used.
  Future<ProfileImage> addImage(
    Uint8List png, {
    required SavedKind kind,
    required SignatureMethod method,
    String? label,
    bool makeDefault = false,
  });

  Future<Uint8List> imageBytes(String imageId);

  Future<void> updateImage(String imageId, {String? label, bool? isDefault});

  Future<void> deleteImage(String imageId);

  // Claim and inbox

  /// Binds the signed-in account to the party of [token]; with [apply] the
  /// matching fields are filled from the profile. `409 ALREADY_CLAIMED` when
  /// another account has it.
  Future<ClaimResult> claim(String token, {bool apply = false, bool preview = false});

  /// The signed file of a finished inbox request, for a signer whose link
  /// (a grant) ended with their signature. [certificate] false leaves the
  /// signature pages out.
  Future<Uint8List> inboxFinal(String partyId, {bool certificate = true});

  Future<InboxPage> inbox({required bool done});

  /// A grant link (`/app/sign/<grant>`) for an inbox entry.
  Future<String> openInbox(String partyId);
}
