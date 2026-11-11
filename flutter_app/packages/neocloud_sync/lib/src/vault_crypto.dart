// Vault folder crypto, format NCV1 (docs/clients.md, section 4).
//
// No dart:io: this file is shared with Flutter web, where package:cryptography
// delegates to WebCrypto.

import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

const int _keyLength = 32;
const int _saltLength = 16;
const int _nonceLength = 12;
const int _tagLength = 16;
const int _prefixLength = 8;
const int _segmentSize = 65536;
const List<int> _magic = [0x4e, 0x43, 0x56, 0x31]; // "NCV1"

/// The minimum the contract allows.
const int minVaultIterations = 600000;

final AesGcm _aes = AesGcm.with256bits(nonceLength: _nonceLength);
final Hkdf _hkdf = Hkdf(hmac: Hmac.sha256(), outputLength: _keyLength);
final Random _random = Random.secure();

Uint8List _randomBytes(int length) => Uint8List.fromList(List<int>.generate(length, (_) => _random.nextInt(256)));

/// The passphrase did not open the vault (or the data was tampered with).
class VaultAuthenticationException implements Exception {
  VaultAuthenticationException(this.message);

  final String message;

  @override
  String toString() => 'VaultAuthenticationException: $message';
}

/// A freshly created vault: send [salt], [iterations] and [wrappedKey] to
/// `POST /api/vaults`, keep [crypto] to encrypt its contents.
class VaultSetup {
  VaultSetup({required this.salt, required this.iterations, required this.wrappedKey, required this.crypto});

  final String salt;
  final int iterations;
  final String wrappedKey;
  final VaultCrypto crypto;
}

/// An unlocked vault key with the NCV1 name and content ciphers.
class VaultCrypto {
  VaultCrypto._(this._vaultKey, this.salt, this.iterations);

  /// Rebuilds the ciphers from the raw 32-byte vault key (vault recovery:
  /// the key decoded from the recovery words). No key wrapping is involved,
  /// so [salt] and [iterations] only matter for a later [rewrap] — by
  /// default a fresh salt and [minVaultIterations], to be sent along with
  /// the new wrapped key in `PUT /api/vaults/:id/key`.
  factory VaultCrypto.fromKey(Uint8List vaultKey, {String? salt, int iterations = minVaultIterations}) {
    if (vaultKey.length != _keyLength) {
      throw ArgumentError.value(vaultKey.length, 'vaultKey', 'A vault key has $_keyLength bytes');
    }
    return VaultCrypto._(Uint8List.fromList(vaultKey), salt ?? base64.encode(_randomBytes(_saltLength)), iterations);
  }

  final List<int> _vaultKey;

  /// Base64 salt and iteration count the key is currently wrapped with.
  final String salt;
  final int iterations;

  SecretKey? _nameKey;

  static Future<VaultSetup> create(String passphrase, {int iterations = minVaultIterations}) async {
    final salt = _randomBytes(_saltLength);
    final vaultKey = _randomBytes(_keyLength);
    final saltText = base64.encode(salt);
    final wrapped = await _wrap(vaultKey, passphrase, salt, iterations);
    return VaultSetup(
      salt: saltText,
      iterations: iterations,
      wrappedKey: wrapped,
      crypto: VaultCrypto._(vaultKey, saltText, iterations),
    );
  }

  /// Opens a vault with the values from `GET /api/vaults/:id`. Throws
  /// [VaultAuthenticationException] for a wrong passphrase.
  static Future<VaultCrypto> unlock({
    required String passphrase,
    required String salt,
    required int iterations,
    required String wrappedKey,
  }) async {
    final kek = await _deriveKek(passphrase, base64.decode(salt), iterations);
    final blob = base64.decode(wrappedKey);
    if (blob.length != _nonceLength + _keyLength + _tagLength) {
      throw VaultAuthenticationException('Wrapped key has the wrong length.');
    }
    final vaultKey = await _open(kek, blob, const []);
    return VaultCrypto._(vaultKey, salt, iterations);
  }

  /// The vault key wrapped under [newPassphrase] with the same salt and
  /// iteration count, for `PUT /api/vaults/:id/key`.
  Future<String> rewrap(String newPassphrase) => _wrap(_vaultKey, newPassphrase, base64.decode(salt), iterations);

  /// base64url (no padding) of nonce ‖ ciphertext ‖ tag.
  Future<String> encryptName(String name) async {
    final key = await _namesKey();
    final nonce = _randomBytes(_nonceLength);
    final box = await _aes.encrypt(utf8.encode(name), secretKey: key, nonce: nonce);
    return base64Url.encode(box.concatenation()).replaceAll('=', '');
  }

  Future<String> decryptName(String stored) async {
    final key = await _namesKey();
    final Uint8List blob;
    try {
      blob = base64Url.decode(base64Url.normalize(stored));
    } on FormatException {
      throw VaultAuthenticationException('Name is not base64url.');
    }
    if (blob.length < _nonceLength + _tagLength) throw VaultAuthenticationException('Name is too short.');
    return utf8.decode(await _open(key, blob, const []));
  }

  /// `NCV1` ‖ prefix ‖ segments of ciphertext ‖ tag.
  Future<Uint8List> encryptContent(Uint8List plaintext) async {
    final prefix = _randomBytes(_prefixLength);
    final fileKey = await _fileKey(prefix);
    final segments = plaintext.isEmpty ? 1 : (plaintext.length + _segmentSize - 1) ~/ _segmentSize;
    final out = BytesBuilder(copy: false)
      ..add(_magic)
      ..add(prefix);
    for (var i = 0; i < segments; i++) {
      final start = i * _segmentSize;
      final end = min(start + _segmentSize, plaintext.length);
      final last = i == segments - 1;
      final box = await _aes.encrypt(
        Uint8List.sublistView(plaintext, start, end),
        secretKey: fileKey,
        nonce: _segmentNonce(prefix, i),
        aad: [last ? 1 : 0],
      );
      out
        ..add(box.cipherText)
        ..add(box.mac.bytes);
    }
    return out.takeBytes();
  }

  Future<Uint8List> decryptContent(Uint8List data) async {
    if (data.length < _magic.length + _prefixLength + _tagLength) {
      throw VaultAuthenticationException('Not an NCV1 file (too short).');
    }
    for (var i = 0; i < _magic.length; i++) {
      if (data[i] != _magic[i]) throw VaultAuthenticationException('Not an NCV1 file.');
    }
    final prefix = Uint8List.sublistView(data, _magic.length, _magic.length + _prefixLength);
    final fileKey = await _fileKey(prefix);
    const sealed = _segmentSize + _tagLength;
    final body = Uint8List.sublistView(data, _magic.length + _prefixLength);
    final segments = (body.length + sealed - 1) ~/ sealed;
    final out = BytesBuilder(copy: false);
    for (var i = 0; i < segments; i++) {
      final start = i * sealed;
      final end = min(start + sealed, body.length);
      if (end - start < _tagLength) throw VaultAuthenticationException('Truncated segment.');
      final last = i == segments - 1;
      final segment = Uint8List.sublistView(body, start, end);
      final box = SecretBox(
        Uint8List.sublistView(segment, 0, segment.length - _tagLength),
        nonce: _segmentNonce(prefix, i),
        mac: Mac(Uint8List.sublistView(segment, segment.length - _tagLength)),
      );
      try {
        out.add(await _aes.decrypt(box, secretKey: fileKey, aad: [last ? 1 : 0]));
      } on SecretBoxAuthenticationError {
        throw VaultAuthenticationException('Segment $i failed authentication.');
      }
    }
    return out.takeBytes();
  }

  Future<SecretKey> _namesKey() async => _nameKey ??= await _hkdf.deriveKey(
    secretKey: SecretKey(_vaultKey),
    nonce: const [],
    info: utf8.encode('ncv1-name'),
  );

  Future<SecretKey> _fileKey(List<int> prefix) =>
      _hkdf.deriveKey(secretKey: SecretKey(_vaultKey), nonce: prefix, info: utf8.encode('ncv1-file'));

  static List<int> _segmentNonce(List<int> prefix, int index) {
    final nonce = Uint8List(_nonceLength)..setAll(0, prefix);
    ByteData.sublistView(nonce).setUint32(_prefixLength, index, Endian.big);
    return nonce;
  }

  static Future<SecretKey> _deriveKek(String passphrase, List<int> salt, int iterations) {
    final pbkdf2 = Pbkdf2(macAlgorithm: Hmac.sha256(), iterations: iterations, bits: _keyLength * 8);
    return pbkdf2.deriveKey(secretKey: SecretKey(utf8.encode(passphrase)), nonce: salt);
  }

  static Future<String> _wrap(List<int> vaultKey, String passphrase, List<int> salt, int iterations) async {
    final kek = await _deriveKek(passphrase, salt, iterations);
    final box = await _aes.encrypt(vaultKey, secretKey: kek, nonce: _randomBytes(_nonceLength));
    return base64.encode(box.concatenation());
  }

  static Future<List<int>> _open(SecretKey key, List<int> blob, List<int> aad) async {
    final box = SecretBox.fromConcatenation(blob, nonceLength: _nonceLength, macLength: _tagLength);
    try {
      return await _aes.decrypt(box, secretKey: key, aad: aad);
    } on SecretBoxAuthenticationError {
      throw VaultAuthenticationException('Wrong passphrase or damaged data.');
    }
  }
}
