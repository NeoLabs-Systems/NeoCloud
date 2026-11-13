import 'dart:async';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:neocloud_sync/vault.dart';

// NCV1 content (docs/clients.md §4) encrypted as a stream, segment by
// segment, so a large file never sits in memory whole. VaultCrypto in
// package:neocloud_sync only takes whole files; this produces the same bytes
// from the raw vault key.

const int vaultSegmentSize = 65536;
const int _prefixLength = 8;
const int _nonceLength = 12;
const int _tagLength = 16;
const List<int> _magic = <int>[0x4e, 0x43, 0x56, 0x31]; // "NCV1"

/// Bytes before the first segment: "NCV1" and the 8-byte prefix.
const int vaultHeaderSize = 4 + _prefixLength;

final AesGcm _aes = AesGcm.with256bits(nonceLength: _nonceLength);
final Hkdf _hkdf = Hkdf(hmac: Hmac.sha256(), outputLength: 32);
final Random _random = Random.secure();

/// The size of [plainSize] bytes once encrypted.
int vaultCiphertextSize(int plainSize) {
  final segments = plainSize == 0 ? 1 : (plainSize + vaultSegmentSize - 1) ~/ vaultSegmentSize;
  return vaultHeaderSize + segments * _tagLength + plainSize;
}

Future<SecretKey> _fileKey(List<int> vaultKey, List<int> prefix) =>
    _hkdf.deriveKey(secretKey: SecretKey(vaultKey), nonce: prefix, info: utf8.encode('ncv1-file'));

List<int> _segmentNonce(List<int> prefix, int index) {
  final nonce = Uint8List(_nonceLength)..setAll(0, prefix);
  ByteData.sublistView(nonce).setUint32(_prefixLength, index, Endian.big);
  return nonce;
}

/// The file changed while it was read: it has more or fewer bytes than its
/// listed size.
class VaultSourceSizeMismatch implements Exception {
  const VaultSourceSizeMismatch();
}

/// [plaintext] (exactly [size] bytes) as NCV1 ciphertext: the header, then
/// one chunk per segment. Throws [VaultSourceSizeMismatch] when the stream
/// has another length.
Stream<Uint8List> encryptVaultStream(List<int> vaultKey, Stream<List<int>> plaintext, int size) async* {
  final prefix = Uint8List.fromList(List<int>.generate(_prefixLength, (_) => _random.nextInt(256)));
  final key = await _fileKey(vaultKey, prefix);
  yield Uint8List.fromList(<int>[..._magic, ...prefix]);
  final segments = size == 0 ? 1 : (size + vaultSegmentSize - 1) ~/ vaultSegmentSize;
  var index = 0;
  var read = 0;
  final buffer = BytesBuilder(copy: false);

  Future<Uint8List> seal(Uint8List segment) async {
    final last = index == segments - 1;
    final box = await _aes.encrypt(segment, secretKey: key, nonce: _segmentNonce(prefix, index), aad: <int>[last ? 1 : 0]);
    index += 1;
    return Uint8List.fromList(<int>[...box.cipherText, ...box.mac.bytes]);
  }

  await for (final chunk in plaintext) {
    read += chunk.length;
    if (read > size) throw const VaultSourceSizeMismatch();
    final bytes = chunk is Uint8List ? chunk : Uint8List.fromList(chunk);
    var offset = 0;
    while (offset < bytes.length) {
      final take = min(vaultSegmentSize - buffer.length, bytes.length - offset);
      buffer.add(Uint8List.sublistView(bytes, offset, offset + take));
      offset += take;
      // A full segment is sealed only once more bytes are known to follow,
      // or it is the last one by size.
      if (buffer.length == vaultSegmentSize && index < segments - 1) yield await seal(buffer.takeBytes());
    }
  }
  if (read != size) throw const VaultSourceSizeMismatch();
  yield await seal(buffer.takeBytes());
}

/// Decrypts segment [index] of a file whose [header] (first 12 bytes) is
/// given; [sealed] is the segment's ciphertext and tag. Throws
/// [VaultAuthenticationException] when it doesn't open.
Future<Uint8List> decryptVaultSegment(List<int> vaultKey, List<int> header, int index, List<int> sealed, {required bool last}) async {
  if (header.length < vaultHeaderSize || sealed.length < _tagLength) {
    throw VaultAuthenticationException('Too short.');
  }
  for (var i = 0; i < _magic.length; i++) {
    if (header[i] != _magic[i]) throw VaultAuthenticationException('Not an NCV1 file.');
  }
  final prefix = header.sublist(_magic.length, vaultHeaderSize);
  final box = SecretBox(
    sealed.sublist(0, sealed.length - _tagLength),
    nonce: _segmentNonce(prefix, index),
    mac: Mac(sealed.sublist(sealed.length - _tagLength)),
  );
  try {
    return Uint8List.fromList(await _aes.decrypt(box, secretKey: await _fileKey(vaultKey, prefix), aad: <int>[last ? 1 : 0]));
  } on SecretBoxAuthenticationError {
    throw VaultAuthenticationException('Segment $index failed authentication.');
  }
}
