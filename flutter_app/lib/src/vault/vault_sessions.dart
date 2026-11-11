import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:neocloud_sync/vault.dart';

import '../api/backend_client.dart';
import '../api/models.dart';
import '../files/file_format.dart';
import '../files/upload_queue.dart';
import '../security/recovery_words.dart';
import 'vault_keys.dart';

/// A recovery key that is well-formed but opens another vault.
class VaultRecoveryMismatch implements Exception {
  const VaultRecoveryMismatch();
}

/// The vaults unlocked in this session. Keys stay in memory only and are
/// gone when the app closes, the account signs out or the user locks them.
/// Everything inside a vault folder has an encrypted name; the vault folder
/// itself keeps its plain name.
class VaultSessions extends ChangeNotifier {
  VaultSessions(this._client);

  final BackendClient _client;
  final Map<String, VaultCrypto> _keys = <String, VaultCrypto>{};
  final Map<String, Future<VaultKeyInfo>> _info = <String, Future<VaultKeyInfo>>{};
  final Map<String, String> _names = <String, String>{};

  bool isUnlocked(String vaultId) => _keys.containsKey(vaultId);

  bool get hasUnlocked => _keys.isNotEmpty;

  Future<VaultKeyInfo> info(String vaultId) {
    return _info.putIfAbsent(vaultId, () => _client.vault(vaultId).catchError((Object error) {
      _info.remove(vaultId);
      throw error;
    }));
  }

  /// Whether [node] is the vault folder itself, whose name is not encrypted.
  Future<bool> isVaultRoot(FileNode node) async => node.vaultId != null && (await info(node.vaultId!)).folderId == node.id;

  /// False when the passphrase is wrong. A vault wrapped with fewer than
  /// [vaultIterations] PBKDF2 rounds is re-wrapped in the background.
  Future<bool> unlock(String vaultId, String passphrase) async {
    final key = await _rawKey(vaultId, passphrase);
    if (key == null) return false;
    _keys[vaultId] = await vaultCryptoFromKey(key);
    notifyListeners();
    final info = await this.info(vaultId);
    if (info.iterations < vaultIterations) unawaited(_rewrap(vaultId, key, passphrase).catchError((Object _) {}));
    return true;
  }

  Future<Uint8List?> _rawKey(String vaultId, String passphrase) async {
    final info = await this.info(vaultId);
    try {
      return await unwrapVaultKey(passphrase: passphrase, salt: info.salt, iterations: info.iterations, wrappedKey: info.wrappedKey);
    } on VaultAuthenticationException {
      return null;
    }
  }

  /// Stores [key] wrapped under [passphrase] with today's iteration count.
  Future<void> _rewrap(String vaultId, List<int> key, String passphrase) async {
    final wrapped = await wrapVaultKey(key, passphrase);
    await _client.updateVaultKey(vaultId, salt: wrapped.salt, iterations: wrapped.iterations, wrappedKey: wrapped.wrappedKey);
    _info.remove(vaultId);
  }

  /// The vault's recovery words, or null when [passphrase] is wrong.
  Future<List<String>?> recoveryWords(String vaultId, String passphrase) async {
    final key = await _rawKey(vaultId, passphrase);
    return key == null ? null : encodeRecoveryWords(key);
  }

  /// Sets [newPassphrase] with the vault [key] from its recovery words and
  /// unlocks the vault. Throws [VaultRecoveryMismatch] when [key] can't read
  /// what is in the vault, so a wrong key never replaces the right one.
  Future<void> recover(String vaultId, List<int> key, String newPassphrase) async {
    final crypto = await vaultCryptoFromKey(key);
    final listing = await _client.listFolder((await info(vaultId)).folderId);
    if (listing.items.isNotEmpty) {
      try {
        await crypto.decryptName(listing.items.first.name);
      } on VaultAuthenticationException {
        throw const VaultRecoveryMismatch();
      }
    }
    await _rewrap(vaultId, key, newPassphrase);
    _keys[vaultId] = crypto;
    notifyListeners();
  }

  void lock(String vaultId) {
    if (_keys.remove(vaultId) != null) notifyListeners();
  }

  void lockAll() {
    _keys.clear();
    _info.clear();
    _names.clear();
    notifyListeners();
  }

  /// Makes a vault folder under [parentId] for [key] (whose recovery words
  /// the user already saved) and keeps it unlocked.
  Future<FileNode> create({required String parentId, required String name, required String passphrase, required List<int> key}) async {
    final wrapped = await wrapVaultKey(key, passphrase);
    final folder = await _client.createVault(
      parentId: parentId,
      name: name,
      salt: wrapped.salt,
      iterations: wrapped.iterations,
      wrappedKey: wrapped.wrappedKey,
    );
    final vaultId = folder.vaultId;
    if (vaultId != null) {
      _keys[vaultId] = await vaultCryptoFromKey(key);
      notifyListeners();
    }
    return folder;
  }

  VaultCrypto _key(String vaultId) {
    final key = _keys[vaultId];
    if (key == null) throw StateError('Vault $vaultId is locked.');
    return key;
  }

  Future<String> _plainName(String vaultId, String stored) async {
    final cached = _names[stored];
    if (cached != null) return cached;
    try {
      final plain = await _key(vaultId).decryptName(stored);
      _names[stored] = plain;
      return plain;
    } on VaultAuthenticationException {
      // Not one of ours (or damaged): show what the server has.
      return stored;
    }
  }

  /// [nodes] as the user sees them: names inside unlocked vaults decrypted.
  /// Nodes of locked vaults and the vault folders themselves stay as they are.
  Future<List<FileNode>> reveal(List<FileNode> nodes) async {
    final result = <FileNode>[];
    for (final node in nodes) {
      final vaultId = node.vaultId;
      if (vaultId == null || !isUnlocked(vaultId) || await isVaultRoot(node)) {
        result.add(node);
        continue;
      }
      final plain = await _plainName(vaultId, node.name);
      result.add(node.withName(plain, mime: node.isFolder ? null : guessMimeType(plain)));
    }
    return result;
  }

  /// Breadcrumbs with the folders below the vault folder decrypted.
  Future<List<PathSegment>> revealPath(List<PathSegment> path, String vaultId) async {
    if (!isUnlocked(vaultId)) return path;
    final rootId = (await info(vaultId)).folderId;
    final rootIndex = path.indexWhere((segment) => segment.id == rootId);
    if (rootIndex < 0) return path;
    return <PathSegment>[
      for (var i = 0; i < path.length; i++)
        i <= rootIndex ? path[i] : PathSegment(id: path[i].id, name: await _plainName(vaultId, path[i].name)),
    ];
  }

  Future<String> encryptName(String vaultId, String name) => _key(vaultId).encryptName(name);

  Future<Uint8List> encrypt(String vaultId, Uint8List plaintext) => _key(vaultId).encryptContent(plaintext);

  Future<Uint8List> decrypt(String vaultId, Uint8List data) => _key(vaultId).decryptContent(data);

  /// What the upload queue needs to fill a folder of this vault.
  UploadCipher cipherFor(String vaultId) => _VaultUploadCipher(this, vaultId);
}

class _VaultUploadCipher implements UploadCipher {
  _VaultUploadCipher(this._sessions, this._vaultId);

  final VaultSessions _sessions;
  final String _vaultId;

  @override
  Future<String> encryptName(String name) => _sessions.encryptName(_vaultId, name);

  @override
  Future<Uint8List> encryptContent(Uint8List plaintext) => _sessions.encrypt(_vaultId, plaintext);
}
