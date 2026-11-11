import 'dart:math';

import 'bip39_words.dart';

// A rough entropy estimate for vault passphrases, which can't be reset by
// anyone if they are lost or guessed. Deliberately pessimistic: list words
// count 11 bits, years and well-known passwords a few bits, repeats and
// runs ("aaaa", "1234", "qwer") one bit per extra character.

/// Shortest passphrase a person may choose. Generated ones are exempt.
const int minVaultPassphraseLength = 12;

/// Bits a chosen passphrase needs to be accepted.
const double minVaultPassphraseBits = 50;

const List<String> _common = <String>[
  'password',
  'passwort',
  'iloveyou',
  'letmein',
  'welcome',
  'qwertz',
  'qwerty',
  'monkey',
  'dragon',
  'master',
  'secret',
  'admin',
  'login',
  'neocloud',
  'abc123',
  '123456',
];

const List<String> _rows = <String>['abcdefghijklmnopqrstuvwxyz', '0123456789', 'qwertyuiopasdfghjklzxcvbnm', 'qwertzuiopasdfghjklyxcvbnm'];

final Set<String> _wordSet = bip39Words.toSet();

enum PassphraseLevel { tooWeak, weak, good, strong }

class PassphraseStrength {
  const PassphraseStrength({required this.bits, required this.length, required this.generated});

  /// Estimated entropy in bits.
  final double bits;
  final int length;
  final bool generated;

  bool get tooShort => !generated && length < minVaultPassphraseLength;

  bool get acceptable => generated || (!tooShort && bits >= minVaultPassphraseBits);

  PassphraseLevel get level {
    if (generated || bits >= 64) return PassphraseLevel.strong;
    if (bits >= minVaultPassphraseBits) return PassphraseLevel.good;
    if (bits >= 28) return PassphraseLevel.weak;
    return PassphraseLevel.tooWeak;
  }

  /// 0–4 for a four-segment meter.
  int get score => length == 0 ? 0 : level.index + 1;
}

/// The estimate for [passphrase]. [generated] marks one the app made, which
/// is accepted whatever its length.
PassphraseStrength estimatePassphrase(String passphrase, {bool generated = false}) {
  final length = passphrase.runes.length;
  if (length == 0) return PassphraseStrength(bits: 0, length: 0, generated: generated);
  final pool = _poolSize(passphrase);
  final perChar = log(pool) / ln2;
  var bits = 0.0;
  final tokens = passphrase.split(RegExp(r'[\s\-_.,;:+/]+')).where((token) => token.isNotEmpty).toList();
  for (final token in tokens) {
    bits += _tokenBits(token, perChar);
  }
  // Separators add little: a guesser tries the usual few.
  if (tokens.length > 1) bits += 2;
  return PassphraseStrength(bits: bits, length: length, generated: generated);
}

double _tokenBits(String token, double perChar) {
  final lower = token.toLowerCase();
  if (_wordSet.contains(lower)) return 11 + (token == lower ? 0 : 1);
  if (RegExp(r'^(19|20)\d\d$').hasMatch(token)) return 7;
  var rest = _unleet(lower);
  var bits = 0.0;
  for (final pattern in _common) {
    while (rest.contains(pattern)) {
      rest = rest.replaceFirst(pattern, '\u0000');
      bits += 3;
    }
  }
  // Years and dictionary words inside a longer token.
  rest = rest.replaceAllMapped(RegExp(r'(19|20)\d\d'), (_) {
    bits += 7;
    return '\u0000';
  });
  final chars = rest.split('');
  for (var i = 0; i < chars.length; i++) {
    final char = chars[i];
    if (char == '\u0000') continue;
    if (i > 0 && (chars[i - 1] == char || _isRun(chars[i - 1], char))) {
      bits += 1;
    } else {
      bits += perChar;
    }
  }
  return bits;
}

bool _isRun(String previous, String current) {
  for (final row in _rows) {
    final a = row.indexOf(previous);
    final b = row.indexOf(current);
    if (a >= 0 && b >= 0 && (b - a).abs() == 1) return true;
  }
  return false;
}

String _unleet(String text) {
  const map = <String, String>{'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '@': 'a', r'$': 's', '!': 'i'};
  // Only swap when the result reads as a known pattern, so "1234" stays digits.
  final swapped = text.split('').map((char) => map[char] ?? char).join();
  return _common.any(swapped.contains) && !_common.any(text.contains) ? swapped : text;
}

int _poolSize(String text) {
  var pool = 0;
  if (RegExp('[a-z]').hasMatch(text)) pool += 26;
  if (RegExp('[A-Z]').hasMatch(text)) pool += 26;
  if (RegExp('[0-9]').hasMatch(text)) pool += 10;
  if (RegExp(r'[!-/:-@\[-`{-~ ]').hasMatch(text)) pool += 33;
  if (text.runes.any((rune) => rune > 127)) pool += 100;
  return max(pool, 10);
}
