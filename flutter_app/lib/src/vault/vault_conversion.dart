import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:crypto/crypto.dart' as hashes;
import 'package:flutter/foundation.dart';
import 'package:neocloud_sync/vault.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/backend_client.dart';
import '../api/models.dart';
import 'vault_stream.dart';

// Turning a plain folder into a vault (docs/clients.md §4). Only this device
// holds the vault key, so it does the work: every file is read, encrypted
// as a stream and uploaded into the vault under the same relative path, each
// upload checked by the SHA-256 of its ciphertext and its size. Only when
// all of it is in does the server swap the folder for the vault in one step,
// moving the originals to the trash. Nothing is ever deleted for good.
//
// The run can stop at any point (cancel, closed app, lost connection) and
// continue later: what is already in the vault is found again by its
// decrypted name and kept when the original is unchanged.

/// The server calls a conversion makes; [ClientVaultConversionBackend] in
/// the app, a fake in tests.
abstract class VaultConversionBackend {
  Future<VaultConversionCheck> check(String folderId);

  /// Every item in [folderId], all pages.
  Future<List<FileNode>> children(String folderId);

  Future<FileNode> createFolder(String parentId, String name);

  /// A file's content, in chunks.
  Stream<List<int>> read(FileNode file);

  /// Bytes [start] up to (not including) [end] of a file.
  Future<Uint8List> readRange(String fileId, int start, int end);

  /// One request; the server refuses content whose SHA-256 isn't [sha256].
  Future<FileNode> uploadWhole(String parentId, String name, Uint8List bytes, {required String sha256});

  /// A resumable upload: its id and part size.
  Future<(String, int)> startUpload(String parentId, String name, int size);

  /// Answers the SHA-256 (hex) the server stored for the part.
  Future<String> uploadPart(String uploadId, int index, Uint8List bytes);

  Future<FileNode> completeUpload(String uploadId);

  Future<void> cancelUpload(String uploadId);

  Future<void> trash(String nodeId);

  Future<FileNode> replace(String vaultId, {required String folderId, required int cursor});
}

/// The vault key in the two forms a conversion needs: the package's name
/// cipher and the raw key for streamed content.
class VaultConversionCipher {
  VaultConversionCipher(this._key) : _names = VaultCrypto.fromKey(Uint8List.fromList(_key));

  final List<int> _key;
  final VaultCrypto _names;

  Future<String> encryptName(String name) => _names.encryptName(name);

  /// Null for a name this key didn't write.
  Future<String?> decryptName(String stored) async {
    try {
      return await _names.decryptName(stored);
    } on VaultAuthenticationException {
      return null;
    }
  }

  Stream<Uint8List> encrypt(Stream<List<int>> plaintext, int size) => encryptVaultStream(_key, plaintext, size);

  Future<Uint8List> decryptSegment(List<int> header, int index, List<int> sealed, {required bool last}) =>
      decryptVaultSegment(_key, header, index, sealed, last: last);
}

/// What the device remembers about a conversion between runs: no keys, no
/// names inside the folder, only ids.
class VaultConversionState {
  VaultConversionState({
    required this.folderId,
    required this.folderName,
    required this.vaultId,
    required this.vaultFolderId,
    Map<String, String>? done,
  }) : done = done ?? <String, String>{};

  factory VaultConversionState.fromJson(Map<String, dynamic> json) => VaultConversionState(
    folderId: '${json['folderId']}',
    folderName: '${json['folderName'] ?? ''}',
    vaultId: '${json['vaultId']}',
    vaultFolderId: '${json['vaultFolderId']}',
    done: ((json['done'] as Map?) ?? const <String, Object?>{}).map((key, value) => MapEntry('$key', '$value')),
  );

  final String folderId;
  final String folderName;
  final String vaultId;
  final String vaultFolderId;

  /// Original file id → its etag when it went into the vault.
  final Map<String, String> done;

  Map<String, Object?> toJson() => <String, Object?>{
    'folderId': folderId,
    'folderName': folderName,
    'vaultId': vaultId,
    'vaultFolderId': vaultFolderId,
    'done': done,
  };
}

/// Conversions in progress, kept on the device by original folder id.
class VaultConversionStore {
  VaultConversionStore(this._preferences);

  static const String _key = 'vault.conversions';

  final SharedPreferences _preferences;

  Map<String, VaultConversionState> _all() {
    try {
      final raw = _preferences.getString(_key);
      if (raw == null || raw.isEmpty) return <String, VaultConversionState>{};
      return (jsonDecode(raw) as Map).map(
        (key, value) => MapEntry('$key', VaultConversionState.fromJson((value as Map).cast<String, dynamic>())),
      );
    } catch (_) {
      return <String, VaultConversionState>{};
    }
  }

  VaultConversionState? read(String folderId) => _all()[folderId];

  Future<void> write(VaultConversionState state) async {
    final all = _all()..[state.folderId] = state;
    await _preferences.setString(_key, jsonEncode(all.map((key, value) => MapEntry(key, value.toJson()))));
  }

  Future<void> remove(String folderId) async {
    final all = _all();
    if (all.remove(folderId) == null) return;
    await _preferences.setString(_key, jsonEncode(all.map((key, value) => MapEntry(key, value.toJson()))));
  }
}

enum VaultConversionPhase {
  ready,
  copying,
  finishing,

  /// The folder is a vault now; [VaultConversion.result] is its folder.
  done,

  /// Stopped by the user; originals untouched, the partial vault kept.
  cancelled,

  /// Stopped by the connection, the quota, a folder that keeps changing or
  /// an error from the server ([VaultConversion.error]);
  /// [VaultConversion.stopReason] says which. Can continue later.
  interrupted,

  /// Everything else is in; the files in [VaultConversion.failures] could
  /// not be copied. The user retries or finishes without them.
  review,

  /// The folder can't be converted ([VaultConversion.problems]) or the
  /// check of a converted file failed.
  refused,
}

enum VaultConversionStop { offline, quota, changing, verify, error }

/// A file that could not go into the vault, by its path in the folder.
class VaultConversionFailure {
  const VaultConversionFailure(this.path, this.reason);

  final String path;
  final String reason;
}

/// Files up to this size go in one request; larger ones in parts.
const int vaultConversionWholeLimit = 8 * 1024 * 1024;

/// How big the reads of an original are.
const int vaultConversionReadChunk = 4 * 1024 * 1024;

class _Cancelled implements Exception {
  const _Cancelled();
}

class _Stop implements Exception {
  const _Stop(this.reason);

  final VaultConversionStop reason;
}

class _UploadMismatch implements Exception {
  const _UploadMismatch();
}

/// One conversion run, watched by the progress sheet.
class VaultConversion extends ChangeNotifier {
  VaultConversion({
    required this.backend,
    required this.cipher,
    required this.store,
    required this.state,
    this.retryDelays = const <Duration>[Duration(seconds: 2), Duration(seconds: 5), Duration(seconds: 15)],
    this.saveInterval = const Duration(seconds: 2),
    this.wholeLimit = vaultConversionWholeLimit,
  });

  final VaultConversionBackend backend;
  final VaultConversionCipher cipher;
  final VaultConversionStore store;
  final VaultConversionState state;

  /// Pauses before each retry of a file after a dropped connection.
  final List<Duration> retryDelays;

  /// How often the progress is written to the device while copying.
  final Duration saveInterval;

  /// Encrypted files up to this size go in one request, larger ones in parts.
  final int wholeLimit;

  VaultConversionPhase phase = VaultConversionPhase.ready;
  VaultConversionStop? stopReason;

  /// The server's answer that stopped the run ([VaultConversionStop.error]).
  Object? error;
  List<String> problems = const <String>[];
  int totalFiles = 0;
  int totalBytes = 0;
  int doneFiles = 0;
  int doneBytes = 0;
  String currentPath = '';
  final List<VaultConversionFailure> failures = <VaultConversionFailure>[];
  FileNode? result;

  bool _cancelRequested = false;
  DateTime _savedAt = DateTime.fromMillisecondsSinceEpoch(0);
  (FileNode, String)? _sample;

  bool get running => phase == VaultConversionPhase.copying || phase == VaultConversionPhase.finishing;

  /// Stops at the next file or part. The originals stay as they are.
  void cancel() {
    _cancelRequested = true;
  }

  void _set(VaultConversionPhase next) {
    phase = next;
    notifyListeners();
  }

  /// Copies what isn't in the vault yet and, when everything is, replaces
  /// the folder. With [finishWithoutFailed] files that keep failing go to
  /// the trash with the rest instead of holding the conversion up.
  Future<void> run({bool finishWithoutFailed = false}) async {
    if (running) return;
    _cancelRequested = false;
    stopReason = null;
    error = null;
    problems = const <String>[];
    try {
      // A folder that changes while it is copied is copied again (only what
      // changed); three passes are plenty for anything but constant writes.
      for (var pass = 0; pass < 3; pass++) {
        _set(VaultConversionPhase.copying);
        final check = await _transient(() => backend.check(state.folderId));
        // The room was checked before the vault was made; part of the copy
        // may already be in it, so QUOTA alone doesn't stop a continuation.
        final blocking = check.problems.where((problem) => problem != 'QUOTA').toList();
        if (blocking.isNotEmpty) {
          problems = blocking;
          _set(VaultConversionPhase.refused);
          return;
        }
        totalFiles = check.files;
        totalBytes = check.bytes;
        doneFiles = 0;
        doneBytes = 0;
        failures.clear();
        _sample = null;
        await _walk(state.folderId, state.vaultFolderId, '');
        await _save(force: true);
        if (failures.isNotEmpty && !finishWithoutFailed) {
          _set(VaultConversionPhase.review);
          return;
        }
        _set(VaultConversionPhase.finishing);
        currentPath = '';
        if (!await _sampleOpens()) throw const _Stop(VaultConversionStop.verify);
        if (_cancelRequested) throw const _Cancelled();
        try {
          result = await _transient(() => backend.replace(state.vaultId, folderId: state.folderId, cursor: check.cursor));
        } on ApiException catch (error) {
          if (error.code == 'VAULT_SOURCE_CHANGED' || error.code == 'VAULT_CONVERT_REFUSED') continue;
          rethrow;
        }
        await store.remove(state.folderId);
        _set(VaultConversionPhase.done);
        return;
      }
      throw const _Stop(VaultConversionStop.changing);
    } on _Cancelled {
      await _save(force: true);
      _set(VaultConversionPhase.cancelled);
    } on _Stop catch (stop) {
      await _save(force: true);
      stopReason = stop.reason;
      _set(stop.reason == VaultConversionStop.verify ? VaultConversionPhase.refused : VaultConversionPhase.interrupted);
    } catch (failure) {
      await _save(force: true);
      stopReason = _isQuota(failure) ? VaultConversionStop.quota : VaultConversionStop.error;
      error = failure;
      _set(VaultConversionPhase.interrupted);
    }
  }

  Future<void> _save({bool force = false}) async {
    final now = DateTime.now();
    if (!force && now.difference(_savedAt) < saveInterval) return;
    _savedAt = now;
    await store.write(state);
  }

  static bool _isTransient(Object error) =>
      error is ApiException && (error.code == 'NETWORK' || error.statusCode == 502 || error.statusCode == 503 || error.statusCode == 504);

  static bool _isQuota(Object error) => error is ApiException && (error.code == 'QUOTA_EXCEEDED' || error.statusCode == 507);

  /// Runs [call]; a dropped connection is retried after each of
  /// [retryDelays], then stops the run until the user continues it.
  Future<T> _transient<T>(Future<T> Function() call) async {
    for (var attempt = 0; ; attempt++) {
      try {
        return await call();
      } catch (error) {
        if (!_isTransient(error)) rethrow;
        if (attempt >= retryDelays.length) throw const _Stop(VaultConversionStop.offline);
        await Future<void>.delayed(retryDelays[attempt]);
      }
    }
  }

  static String _key(FileNode node) => '${node.type}:${node.name.toLowerCase()}';

  Future<void> _walk(String sourceId, String vaultFolderId, String path) async {
    final items = await _transient(() => backend.children(sourceId));
    // What an earlier run already put in this vault folder, by plain name.
    final existing = <String, FileNode>{};
    for (final node in await _transient(() => backend.children(vaultFolderId))) {
      final plain = await cipher.decryptName(node.name);
      if (plain != null) existing[_key(node.withName(plain))] = node;
    }
    for (final item in items) {
      if (_cancelRequested) throw const _Cancelled();
      final itemPath = path.isEmpty ? item.name : '$path/${item.name}';
      final copy = existing[_key(item)];
      if (item.isFolder) {
        final FileNode target;
        if (copy != null) {
          target = copy;
        } else {
          final encrypted = await cipher.encryptName(item.name);
          target = await _transient<FileNode>(() => backend.createFolder(vaultFolderId, encrypted));
        }
        await _walk(item.id, target.id, itemPath);
      } else {
        await _file(item, vaultFolderId, copy, itemPath);
      }
    }
  }

  void _count(FileNode file) {
    doneFiles += 1;
    doneBytes += file.size;
    notifyListeners();
  }

  Future<void> _file(FileNode file, String vaultFolderId, FileNode? copy, String path) async {
    final expected = vaultCiphertextSize(file.size);
    if (copy != null && state.done[file.id] == file.etag && copy.size == expected) {
      _count(file);
      return;
    }
    currentPath = path;
    notifyListeners();
    try {
      // A copy nobody recorded (the app closed mid-way) or of an older
      // version: replaced by a fresh one. It is vault content, so the trash
      // holds nothing readable.
      if (copy != null) await _transient(() => backend.trash(copy.id));
      final node = await _transient(() => _upload(file, vaultFolderId));
      if (node.size != expected) {
        await _transient(() => backend.trash(node.id));
        throw const _UploadMismatch();
      }
      state.done[file.id] = file.etag;
      _sample ??= file.size > 0 ? (file, node.id) : null;
      await _save();
    } on _Cancelled {
      rethrow;
    } on _Stop {
      rethrow;
    } catch (error) {
      if (_isQuota(error)) throw const _Stop(VaultConversionStop.quota);
      failures.add(VaultConversionFailure(path, _reason(error)));
    }
    _count(file);
  }

  static String _reason(Object error) {
    if (error is _UploadMismatch) return 'mismatch';
    if (error is VaultSourceSizeMismatch) return 'changed';
    if (error is ApiException) return error.code ?? 'HTTP ${error.statusCode}';
    return 'error';
  }

  Future<FileNode> _upload(FileNode file, String vaultFolderId) async {
    final name = await cipher.encryptName(file.name);
    final size = vaultCiphertextSize(file.size);
    final ciphertext = cipher.encrypt(backend.read(file), file.size);
    if (size <= wholeLimit) {
      final builder = BytesBuilder(copy: false);
      await for (final chunk in ciphertext) {
        builder.add(chunk);
      }
      final bytes = builder.takeBytes();
      if (_cancelRequested) throw const _Cancelled();
      return backend.uploadWhole(vaultFolderId, name, bytes, sha256: hashes.sha256.convert(bytes).toString());
    }
    final (uploadId, partSize) = await backend.startUpload(vaultFolderId, name, size);
    try {
      var index = 0;
      var buffer = BytesBuilder(copy: false);
      Future<void> flush() async {
        if (_cancelRequested) throw const _Cancelled();
        final bytes = buffer.takeBytes();
        buffer = BytesBuilder(copy: false);
        final stored = await backend.uploadPart(uploadId, index, bytes);
        if (stored != hashes.sha256.convert(bytes).toString()) throw const _UploadMismatch();
        index += 1;
      }

      await for (final chunk in ciphertext) {
        var offset = 0;
        while (offset < chunk.length) {
          final take = math.min(partSize - buffer.length, chunk.length - offset);
          buffer.add(Uint8List.sublistView(chunk, offset, offset + take));
          offset += take;
          if (buffer.length == partSize) await flush();
        }
      }
      if (buffer.length > 0) await flush();
      return await backend.completeUpload(uploadId);
    } catch (_) {
      unawaited(backend.cancelUpload(uploadId).catchError((Object _) {}));
      rethrow;
    }
  }

  /// Reads one file of this run back from the vault and decrypts its first
  /// segment, which must match the original: proof that what the server
  /// holds opens with this vault's key.
  Future<bool> _sampleOpens() async {
    final sample = _sample;
    if (sample == null) return true;
    final (file, vaultNodeId) = sample;
    final length = file.size < vaultSegmentSize ? file.size : vaultSegmentSize;
    try {
      final sealed = await _transient(() => backend.readRange(vaultNodeId, 0, vaultHeaderSize + length + 16));
      final plain = await cipher.decryptSegment(
        Uint8List.sublistView(sealed, 0, vaultHeaderSize),
        0,
        Uint8List.sublistView(sealed, vaultHeaderSize),
        last: file.size <= vaultSegmentSize,
      );
      final original = await _transient(() => backend.readRange(file.id, 0, length));
      return listEquals(plain, original);
    } on VaultAuthenticationException {
      return false;
    }
  }
}

/// [VaultConversionBackend] over the app's [BackendClient].
class ClientVaultConversionBackend implements VaultConversionBackend {
  ClientVaultConversionBackend(this._client);

  final BackendClient _client;

  @override
  Future<VaultConversionCheck> check(String folderId) => _client.vaultConversionCheck(folderId);

  @override
  Future<List<FileNode>> children(String folderId) async {
    final items = <FileNode>[];
    String? cursor;
    do {
      final page = await _client.listFolder(folderId, limit: 2000, cursor: cursor);
      items.addAll(page.items);
      cursor = page.nextCursor;
    } while (cursor != null);
    return items;
  }

  @override
  Future<FileNode> createFolder(String parentId, String name) => _client.createFolder(parentId, name);

  @override
  Stream<List<int>> read(FileNode file) async* {
    for (var start = 0; start < file.size; start += vaultConversionReadChunk) {
      final end = start + vaultConversionReadChunk < file.size ? start + vaultConversionReadChunk : file.size;
      yield await _client.fileRange(file.id, start, end);
    }
  }

  @override
  Future<Uint8List> readRange(String fileId, int start, int end) =>
      end <= start ? Future<Uint8List>.value(Uint8List(0)) : _client.fileRange(fileId, start, end);

  @override
  Future<FileNode> uploadWhole(String parentId, String name, Uint8List bytes, {required String sha256}) =>
      _client.uploadSmall(parentId, name, bytes, conflict: 'rename', sha256: sha256);

  @override
  Future<(String, int)> startUpload(String parentId, String name, int size) async {
    final session = await _client.createUpload(parentId, name, size, conflict: 'rename');
    return ('${session['id']}', (session['partSize'] as num).toInt());
  }

  @override
  Future<String> uploadPart(String uploadId, int index, Uint8List bytes) => _client.uploadPartChecked(uploadId, index, bytes);

  @override
  Future<FileNode> completeUpload(String uploadId) => _client.completeUpload(uploadId);

  @override
  Future<void> cancelUpload(String uploadId) => _client.cancelUpload(uploadId);

  @override
  Future<void> trash(String nodeId) => _client.trashNode(nodeId);

  @override
  Future<FileNode> replace(String vaultId, {required String folderId, required int cursor}) =>
      _client.replaceFolderWithVault(vaultId, folderId: folderId, cursor: cursor);
}
