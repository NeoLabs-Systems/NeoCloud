import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:neocloud_sync/vault.dart';

// The vault key's wrapping (docs/clients.md §4) done in the app, because a
// recovery key needs the raw 32-byte vault key, which package:neocloud_sync's
// VaultCrypto keeps private. Same format as the package: KEK =
// PBKDF2-HMAC-SHA256(passphrase, salt 16 B, iterations, 32 B), wrappedKey =
// base64(nonce 12 B ‖ AES-256-GCM(KEK, vaultKey) ‖ tag 16 B).

/// What new vaults and re-wrapped keys use (OWASP 2023+; the server's floor).
const int vaultIterations = 600000;

const int _keyLength = 32;
const int _saltLength = 16;
const int _nonceLength = 12;
const int _tagLength = 16;

final AesGcm _aes = AesGcm.with256bits(nonceLength: _nonceLength);
final Random _random = Random.secure();

Uint8List _randomBytes(int length) => Uint8List.fromList(List<int>.generate(length, (_) => _random.nextInt(256)));

/// A wrapped vault key as `POST /api/vaults` and `PUT /api/vaults/:id/key` take it.
class WrappedVaultKey {
  const WrappedVaultKey({required this.salt, required this.iterations, required this.wrappedKey});

  final String salt;
  final int iterations;
  final String wrappedKey;
}

/// 32 random bytes for a new vault.
Uint8List newVaultKey() => _randomBytes(_keyLength);

Future<SecretKey> _kek(String passphrase, List<int> salt, int iterations) =>
    Pbkdf2(macAlgorithm: Hmac.sha256(), iterations: iterations, bits: _keyLength * 8)
        .deriveKey(secretKey: SecretKey(utf8.encode(passphrase)), nonce: salt);

/// [key] wrapped under [passphrase] with a fresh salt.
Future<WrappedVaultKey> wrapVaultKey(List<int> key, String passphrase, {int iterations = vaultIterations}) async {
  final salt = _randomBytes(_saltLength);
  final box = await _aes.encrypt(key, secretKey: await _kek(passphrase, salt, iterations), nonce: _randomBytes(_nonceLength));
  return WrappedVaultKey(salt: base64.encode(salt), iterations: iterations, wrappedKey: base64.encode(box.concatenation()));
}

/// The raw vault key. Throws [VaultAuthenticationException] for a wrong
/// passphrase.
Future<Uint8List> unwrapVaultKey({
  required String passphrase,
  required String salt,
  required int iterations,
  required String wrappedKey,
}) async {
  final blob = base64.decode(wrappedKey);
  if (blob.length != _nonceLength + _keyLength + _tagLength) {
    throw VaultAuthenticationException('Wrapped key has the wrong length.');
  }
  final box = SecretBox.fromConcatenation(blob, nonceLength: _nonceLength, macLength: _tagLength);
  try {
    return Uint8List.fromList(await _aes.decrypt(box, secretKey: await _kek(passphrase, base64.decode(salt), iterations)));
  } on SecretBoxAuthenticationError {
    throw VaultAuthenticationException('Wrong passphrase or damaged data.');
  }
}

/// The package's name and content ciphers for a raw [key].
Future<VaultCrypto> vaultCryptoFromKey(List<int> key) async => VaultCrypto.fromKey(Uint8List.fromList(key));
