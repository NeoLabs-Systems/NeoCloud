import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:neocloud_sync/vault.dart';
import 'package:test/test.dart';

// Low iteration counts keep the suite fast; one test runs the real minimum.
const _fastIterations = 1000;

Uint8List _bytes(int length, [int seed = 1]) {
  final random = Random(seed);
  return Uint8List.fromList(List<int>.generate(length, (_) => random.nextInt(256)));
}

Future<Map<String, dynamic>> _node(Map<String, dynamic> request) async {
  final process = await Process.start('node', ['test/fixtures/ncv1.js']);
  process.stdin.write(jsonEncode(request));
  await process.stdin.close();
  final output = await process.stdout.transform(utf8.decoder).join();
  final errors = await process.stderr.transform(utf8.decoder).join();
  final code = await process.exitCode;
  if (code != 0) throw StateError('node failed ($code): $errors');
  return jsonDecode(output) as Map<String, dynamic>;
}

void main() {
  test('content round-trips at segment boundaries and has the documented size', () async {
    final setup = await VaultCrypto.create('correct horse', iterations: _fastIterations);
    for (final size in [0, 1, 65535, 65536, 65537, 131072, 200000]) {
      final plain = _bytes(size, size);
      final sealed = await setup.crypto.encryptContent(plain);
      final segments = size == 0 ? 1 : (size + 65535) ~/ 65536;
      expect(sealed.length, 12 + segments * 16 + size, reason: 'size $size');
      expect(ascii.decode(sealed.sublist(0, 4)), 'NCV1');
      expect(await setup.crypto.decryptContent(sealed), plain, reason: 'size $size');
    }
  });

  test('fromKey rebuilds the ciphers from the raw key (recovery)', () async {
    final key = _bytes(32, 9);
    final recovered = VaultCrypto.fromKey(key, iterations: _fastIterations);
    final sealed = await recovered.encryptContent(_bytes(1000, 1));
    final name = await recovered.encryptName('plan.txt');
    final again = VaultCrypto.fromKey(key);
    expect(await again.decryptContent(sealed), _bytes(1000, 1));
    expect(await again.decryptName(name), 'plan.txt');
    expect(again.iterations, minVaultIterations);
    expect(() => VaultCrypto.fromKey(_bytes(31, 1)), throwsArgumentError);
    await expectLater(
      VaultCrypto.fromKey(_bytes(32, 10)).decryptName(name),
      throwsA(isA<VaultAuthenticationException>()),
    );

    // A new passphrase after recovery: rewrap, then unlock as usual.
    final wrapped = await recovered.rewrap('new passphrase');
    final unlocked = await VaultCrypto.unlock(
      passphrase: 'new passphrase',
      salt: recovered.salt,
      iterations: recovered.iterations,
      wrappedKey: wrapped,
    );
    expect(await unlocked.decryptName(name), 'plan.txt');
  });

  test('names round-trip as unpadded base64url', () async {
    final setup = await VaultCrypto.create('pw', iterations: _fastIterations);
    const name = 'Steuer 2026 – Belege/ä.pdf';
    final stored = await setup.crypto.encryptName(name);
    expect(stored, isNot(contains('=')));
    expect(stored, matches(RegExp(r'^[A-Za-z0-9_-]+$')));
    expect(await setup.crypto.decryptName(stored), name);
  });

  test('wrong passphrase, tampering and truncation are refused', () async {
    final setup = await VaultCrypto.create('right', iterations: _fastIterations);
    await expectLater(
      VaultCrypto.unlock(
        passphrase: 'wrong',
        salt: setup.salt,
        iterations: setup.iterations,
        wrappedKey: setup.wrappedKey,
      ),
      throwsA(isA<VaultAuthenticationException>()),
    );
    final sealed = await setup.crypto.encryptContent(_bytes(140000));
    final flipped = Uint8List.fromList(sealed)..[20] ^= 1;
    await expectLater(setup.crypto.decryptContent(flipped), throwsA(isA<VaultAuthenticationException>()));
    // Dropping the final segment must not decrypt to a shorter file.
    final truncated = Uint8List.sublistView(sealed, 0, 12 + 2 * (65536 + 16));
    await expectLater(setup.crypto.decryptContent(truncated), throwsA(isA<VaultAuthenticationException>()));
  });

  test('rewrap opens with the new passphrase only', () async {
    final setup = await VaultCrypto.create('old', iterations: _fastIterations);
    final name = await setup.crypto.encryptName('x.txt');
    final wrapped = await setup.crypto.rewrap('new');
    final reopened = await VaultCrypto.unlock(
      passphrase: 'new',
      salt: setup.salt,
      iterations: setup.iterations,
      wrappedKey: wrapped,
    );
    expect(await reopened.decryptName(name), 'x.txt');
    await expectLater(
      VaultCrypto.unlock(passphrase: 'old', salt: setup.salt, iterations: setup.iterations, wrappedKey: wrapped),
      throwsA(isA<VaultAuthenticationException>()),
    );
  });

  test('Node encrypts, Dart decrypts (default 600000 iterations)', () async {
    final plain = _bytes(150000, 7);
    final node = await _node({
      'op': 'create',
      'passphrase': 'Plum-Orbit-73!',
      'iterations': minVaultIterations,
      'name': 'Bericht.docx',
      'content': base64.encode(plain),
    });
    final vault = await VaultCrypto.unlock(
      passphrase: 'Plum-Orbit-73!',
      salt: node['salt'] as String,
      iterations: node['iterations'] as int,
      wrappedKey: node['wrappedKey'] as String,
    );
    expect(await vault.decryptName(node['name'] as String), 'Bericht.docx');
    expect(await vault.decryptContent(base64.decode(node['content'] as String)), plain);
  });

  test('Dart encrypts, Node decrypts', () async {
    for (final size in [0, 65536, 70000]) {
      final plain = _bytes(size, 3);
      final setup = await VaultCrypto.create('zwölf Äpfel', iterations: _fastIterations);
      final node = await _node({
        'op': 'open',
        'passphrase': 'zwölf Äpfel',
        'salt': setup.salt,
        'iterations': setup.iterations,
        'wrappedKey': setup.wrappedKey,
        'name': await setup.crypto.encryptName('Ordner/Datei ü.txt'),
        'content': base64.encode(await setup.crypto.encryptContent(plain)),
      });
      expect(node['name'], 'Ordner/Datei ü.txt');
      expect(base64.decode(node['content'] as String), plain, reason: 'size $size');
    }
  });
}
