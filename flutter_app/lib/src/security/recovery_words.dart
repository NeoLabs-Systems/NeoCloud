import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

import 'bip39_words.dart';

// Words for keys people have to write down: a vault's recovery key, and
// generated passphrases and share passwords.
//
// Recovery keys use BIP-39's encoding, so they can be checked by hand or with
// any BIP-39 tool: 32 bytes of key + the first 8 bits of their SHA-256 =
// 264 bits = 24 words of 11 bits each. The last word therefore carries a
// checksum and a typo is caught before anything is re-wrapped.

/// Words in a recovery key for a 32-byte vault key.
const int recoveryWordCount = 24;

final Random _random = Random.secure();

final Map<String, int> _index = <String, int>{
  for (var i = 0; i < bip39Words.length; i++) bip39Words[i]: i,
};

final Map<String, int> _prefixIndex = <String, int>{
  for (var i = 0; i < bip39Words.length; i++) bip39Words[i].substring(0, min(4, bip39Words[i].length)): i,
};

/// Why typed recovery words were refused.
enum RecoveryWordsProblem { wrongCount, unknownWord, checksum }

class RecoveryWordsException implements Exception {
  const RecoveryWordsException(this.problem, {this.position});

  final RecoveryWordsProblem problem;

  /// 1-based position of the word that isn't in the list.
  final int? position;

  @override
  String toString() => 'RecoveryWordsException(${problem.name}${position == null ? '' : ' at $position'})';
}

/// [key] (32 bytes) as 24 words.
List<String> encodeRecoveryWords(List<int> key) {
  if (key.length != 32) throw ArgumentError.value(key.length, 'key', 'must be 32 bytes');
  final checksum = sha256.convert(key).bytes.first;
  final bits = <int>[
    for (final byte in <int>[...key, checksum])
      for (var bit = 7; bit >= 0; bit--) (byte >> bit) & 1,
  ];
  return <String>[
    for (var word = 0; word < recoveryWordCount; word++)
      bip39Words[bits.sublist(word * 11, word * 11 + 11).fold<int>(0, (value, bit) => (value << 1) | bit)],
  ];
}

/// Splits what someone typed or pasted into words: any whitespace, commas,
/// dots, hyphens and list numbers ("1." "2)") are separators.
List<String> splitRecoveryInput(String input) => input
    .toLowerCase()
    .split(RegExp(r'[^a-z]+'))
    .where((word) => word.isNotEmpty)
    .toList();

/// The word list index of [word], also when only its first four letters
/// were typed. Null when it isn't a list word.
int? recoveryWordIndex(String word) {
  final lower = word.trim().toLowerCase();
  final exact = _index[lower];
  if (exact != null) return exact;
  if (lower.length < 4) return null;
  final byPrefix = _prefixIndex[lower.substring(0, 4)];
  if (byPrefix == null) return null;
  // "aban" is "abandon"; a longer typo ("abandonn") is not.
  return bip39Words[byPrefix].startsWith(lower) ? byPrefix : null;
}

/// The 32-byte key behind [words]. Throws [RecoveryWordsException].
Uint8List decodeRecoveryWords(List<String> words) {
  if (words.length != recoveryWordCount) throw const RecoveryWordsException(RecoveryWordsProblem.wrongCount);
  final bits = <int>[];
  for (var i = 0; i < words.length; i++) {
    final index = recoveryWordIndex(words[i]);
    if (index == null) throw RecoveryWordsException(RecoveryWordsProblem.unknownWord, position: i + 1);
    for (var bit = 10; bit >= 0; bit--) {
      bits.add((index >> bit) & 1);
    }
  }
  final bytes = Uint8List(33);
  for (var i = 0; i < 33; i++) {
    bytes[i] = bits.sublist(i * 8, i * 8 + 8).fold<int>(0, (value, bit) => (value << 1) | bit);
  }
  final key = Uint8List.sublistView(bytes, 0, 32);
  if (sha256.convert(key).bytes.first != bytes[32]) throw const RecoveryWordsException(RecoveryWordsProblem.checksum);
  return Uint8List.fromList(key);
}

/// [count] random list words joined by [separator]: 11 bits each, so six
/// words are 66 bits — enough for a vault passphrase that is stretched with
/// PBKDF2, and four are plenty for a throttled share-link password.
String generatePassphrase({int count = 6, String separator = '-'}) =>
    List<String>.generate(count, (_) => bip39Words[_random.nextInt(bip39Words.length)]).join(separator);

/// [count] distinct 0-based word positions to ask back, in order.
List<int> pickRecoveryCheckPositions({int count = 3, Random? random}) {
  final source = random ?? _random;
  final positions = <int>{};
  while (positions.length < count) {
    positions.add(source.nextInt(recoveryWordCount));
  }
  return positions.toList()..sort();
}
