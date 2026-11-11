import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/security/bip39_words.dart';
import 'package:neocloud/src/security/passphrase_strength.dart';
import 'package:neocloud/src/security/recovery_words.dart';
import 'package:neocloud/src/vault/vault_keys.dart';
import 'package:neocloud_sync/vault.dart';

void main() {
  group('recovery words', () {
    test('the word list is BIP-39 English', () {
      expect(bip39Words, hasLength(2048));
      expect(bip39Words.first, 'abandon');
      expect(bip39Words.last, 'zoo');
      expect(bip39Words.map((word) => word.substring(0, min(4, word.length))).toSet(), hasLength(2048));
    });

    test('matches the BIP-39 test vectors', () {
      expect(encodeRecoveryWords(Uint8List(32)), <String>[...List<String>.filled(23, 'abandon'), 'art']);
      expect(encodeRecoveryWords(Uint8List(32)..fillRange(0, 32, 0xff)), <String>[...List<String>.filled(23, 'zoo'), 'vote']);
    });

    test('round-trips random keys, also from 4-letter prefixes and messy input', () {
      final random = Random(7);
      for (var i = 0; i < 50; i++) {
        final key = Uint8List.fromList(List<int>.generate(32, (_) => random.nextInt(256)));
        final words = encodeRecoveryWords(key);
        expect(words, hasLength(24));
        expect(decodeRecoveryWords(words), key);
        expect(decodeRecoveryWords(words.map((w) => w.length > 4 ? w.substring(0, 4) : w).toList()), key);
        final pasted = [for (var n = 0; n < 24; n++) '${n + 1}. ${words[n].toUpperCase()}'].join('\n');
        expect(decodeRecoveryWords(splitRecoveryInput(pasted)), key);
      }
    });

    test('refuses a wrong count, unknown words and a bad checksum', () {
      final words = encodeRecoveryWords(newVaultKey());
      expect(
        () => decodeRecoveryWords(words.sublist(1)),
        throwsA(isA<RecoveryWordsException>().having((e) => e.problem, 'problem', RecoveryWordsProblem.wrongCount)),
      );
      expect(
        () => decodeRecoveryWords(<String>[...words.sublist(0, 5), 'notaword', ...words.sublist(6)]),
        throwsA(isA<RecoveryWordsException>().having((e) => e.position, 'position', 6)),
      );
      final swapped = List<String>.of(words);
      final first = swapped[0];
      swapped[0] = swapped[1];
      swapped[1] = first;
      if (swapped[0] != swapped[1]) {
        expect(
          () => decodeRecoveryWords(swapped),
          throwsA(isA<RecoveryWordsException>().having((e) => e.problem, 'problem', RecoveryWordsProblem.checksum)),
        );
      }
    });

    test('check positions are distinct and sorted', () {
      final positions = pickRecoveryCheckPositions(random: Random(1));
      expect(positions, hasLength(3));
      expect(positions.toSet(), hasLength(3));
      expect(positions, orderedEquals(List<int>.of(positions)..sort()));
      expect(positions.every((p) => p >= 0 && p < 24), isTrue);
    });
  });

  group('passphrase strength', () {
    test('common and short passphrases are refused', () {
      for (final weak in <String>['password123', 'Password1234', 'qwertyuiop12', 'aaaaaaaaaaaaaaaa', 'abcdefghijklmnop', 'p@ssw0rd2024!', 'correct horse']) {
        expect(estimatePassphrase(weak).acceptable, isFalse, reason: weak);
      }
      expect(estimatePassphrase('Xk9#vQ2m').tooShort, isTrue);
    });

    test('long random or multi-word passphrases are accepted', () {
      expect(estimatePassphrase('kfjwoeiruthdmz').acceptable, isTrue);
      expect(estimatePassphrase('river planet copper misery jungle').acceptable, isTrue);
      expect(estimatePassphrase('Mein Hund heißt Bello und frisst 3 Äpfel').acceptable, isTrue);
    });

    test('generated passphrases are strong and exempt from the length rule', () {
      final phrase = generatePassphrase();
      expect(phrase.split('-'), hasLength(6));
      expect(estimatePassphrase(phrase).level, PassphraseLevel.strong);
      expect(estimatePassphrase('ab', generated: true).acceptable, isTrue);
    });

    test('the meter rises with entropy', () {
      expect(estimatePassphrase('').score, 0);
      expect(estimatePassphrase('password').score, 1);
      expect(estimatePassphrase('river planet copper misery jungle orbit').score, 4);
    });
  });

  group('vault keys', () {
    test('wrap and unwrap round-trip and match the package format', () async {
      final key = newVaultKey();
      final wrapped = await wrapVaultKey(key, 'correct horse battery staple', iterations: 1000);
      expect(wrapped.iterations, 1000);
      final unwrapped = await unwrapVaultKey(
        passphrase: 'correct horse battery staple',
        salt: wrapped.salt,
        iterations: wrapped.iterations,
        wrappedKey: wrapped.wrappedKey,
      );
      expect(unwrapped, key);
      await expectLater(
        unwrapVaultKey(passphrase: 'wrong', salt: wrapped.salt, iterations: 1000, wrappedKey: wrapped.wrappedKey),
        throwsA(isA<VaultAuthenticationException>()),
      );
      // The package opens what the app wrapped, and both see the same key.
      final packaged = await VaultCrypto.unlock(
        passphrase: 'correct horse battery staple',
        salt: wrapped.salt,
        iterations: 1000,
        wrappedKey: wrapped.wrappedKey,
      );
      final fromKey = await vaultCryptoFromKey(key);
      expect(await packaged.decryptName(await fromKey.encryptName('Steuer 2026.pdf')), 'Steuer 2026.pdf');
    });

    test('a recovery key restores access to the same vault key', () async {
      final key = newVaultKey();
      final restored = decodeRecoveryWords(encodeRecoveryWords(key));
      final original = await vaultCryptoFromKey(key);
      final recovered = await vaultCryptoFromKey(restored);
      expect(await recovered.decryptName(await original.encryptName('a.txt')), 'a.txt');
    });
  });
}
