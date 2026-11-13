import 'dart:async';
import 'dart:math';
import 'dart:typed_data';

import 'package:crypto/crypto.dart' as hashes;
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/vault/vault_conversion.dart';
import 'package:neocloud/src/vault/vault_keys.dart';
import 'package:neocloud/src/vault/vault_stream.dart';
import 'package:neocloud_sync/vault.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _Node {
  _Node(this.id, this.parentId, this.type, this.name, {Uint8List? content, this.vault = false})
    : content = content ?? Uint8List(0);

  final String id;
  String parentId;
  final String type;
  String name;
  Uint8List content;
  int etag = 1;
  bool trashed = false;
  bool vault;

  FileNode toNode() => FileNode(
    id: id,
    parentId: parentId,
    type: type,
    name: name,
    size: type == 'file' ? content.length : 0,
    mime: null,
    etag: '$etag',
    version: 1,
    modifiedAt: null,
    vaultId: vault ? 'v1' : null,
  );
}

/// An in-memory server with the conversion routes, for the engine.
class _FakeServer implements VaultConversionBackend {
  final Map<String, _Node> nodes = <String, _Node>{'root': _Node('root', '', 'folder', '')};
  final Set<String> shared = <String>{};
  final Set<String> unreadable = <String>{};
  final List<(int, String)> changes = <(int, String)>[];
  int uploads = 0;
  int parts = 0;
  int _ids = 0;
  int _seq = 0;
  void Function(String name)? onUpload;
  final Map<String, (String, String, int, List<Uint8List>)> _sessions = <String, (String, String, int, List<Uint8List>)>{};

  String add(String parentId, String type, String name, {Uint8List? content, bool vault = false}) {
    final id = 'n${++_ids}';
    nodes[id] = _Node(id, parentId, type, name, content: content, vault: vault || (nodes[parentId]?.vault ?? false));
    changes.add((++_seq, id));
    return id;
  }

  bool _live(_Node node) {
    _Node? current = node;
    while (current != null) {
      if (current.trashed) return false;
      current = nodes[current.parentId];
    }
    return true;
  }

  Iterable<_Node> _subtree(String id) sync* {
    for (final node in nodes.values.where((node) => node.parentId == id && !node.trashed)) {
      yield node;
      yield* _subtree(node.id);
    }
  }

  @override
  Future<VaultConversionCheck> check(String folderId) async {
    final below = _subtree(folderId).toList();
    final files = below.where((node) => node.type == 'file').toList();
    final bytes = files.fold<int>(0, (sum, node) => sum + node.content.length);
    final problems = <String>[
      if (nodes[folderId]!.vault) 'IN_VAULT',
      if (below.any((node) => node.vault)) 'CONTAINS_VAULT',
      if (shared.contains(folderId) || below.any((node) => shared.contains(node.id))) 'SHARED',
    ];
    return VaultConversionCheck(
      folderId: folderId,
      name: nodes[folderId]!.name,
      files: files.length,
      folders: below.length - files.length,
      bytes: bytes,
      vaultBytes: files.fold<int>(0, (sum, node) => sum + vaultCiphertextSize(node.content.length)),
      freeBytes: null,
      cursor: _seq,
      problems: problems,
    );
  }

  @override
  Future<List<FileNode>> children(String folderId) async =>
      nodes.values.where((node) => node.parentId == folderId && !node.trashed).map((node) => node.toNode()).toList();

  @override
  Future<FileNode> createFolder(String parentId, String name) async => nodes[add(parentId, 'folder', name)]!.toNode();

  @override
  Stream<List<int>> read(FileNode file) async* {
    if (unreadable.contains(file.id)) throw const ApiException(500, 'Unreadable');
    final content = nodes[file.id]!.content;
    for (var start = 0; start < content.length; start += 50000) {
      yield content.sublist(start, min(start + 50000, content.length));
    }
  }

  @override
  Future<Uint8List> readRange(String fileId, int start, int end) async {
    final content = nodes[fileId]!.content;
    return content.sublist(start, min(end, content.length));
  }

  @override
  Future<FileNode> uploadWhole(String parentId, String name, Uint8List bytes, {required String sha256}) async {
    expect(hashes.sha256.convert(bytes).toString(), sha256);
    uploads += 1;
    onUpload?.call(name);
    return nodes[add(parentId, 'file', name, content: bytes)]!.toNode();
  }

  @override
  Future<(String, int)> startUpload(String parentId, String name, int size) async {
    final id = 'u${_sessions.length}';
    _sessions[id] = (parentId, name, size, <Uint8List>[]);
    return (id, 100000);
  }

  @override
  Future<String> uploadPart(String uploadId, int index, Uint8List bytes) async {
    parts += 1;
    _sessions[uploadId]!.$4.add(bytes);
    return hashes.sha256.convert(bytes).toString();
  }

  @override
  Future<FileNode> completeUpload(String uploadId) async {
    final (parentId, name, size, chunks) = _sessions.remove(uploadId)!;
    final content = Uint8List.fromList(chunks.expand((chunk) => chunk).toList());
    expect(content.length, size);
    uploads += 1;
    onUpload?.call(name);
    return nodes[add(parentId, 'file', name, content: content)]!.toNode();
  }

  @override
  Future<void> cancelUpload(String uploadId) async {
    _sessions.remove(uploadId);
  }

  @override
  Future<void> trash(String nodeId) async {
    nodes[nodeId]!.trashed = true;
  }

  @override
  Future<FileNode> replace(String vaultId, {required String folderId, required int cursor}) async {
    final folder = nodes[folderId]!;
    final below = _subtree(folderId).map((node) => node.id).toSet();
    if (changes.any((change) => change.$1 > cursor && below.contains(change.$2))) {
      throw const ApiException(409, 'changed', code: 'VAULT_SOURCE_CHANGED');
    }
    final vaultFolder = nodes.values.firstWhere((node) => node.vault && nodes[node.parentId]?.vault != true);
    folder.trashed = true;
    vaultFolder
      ..name = folder.name
      ..parentId = folder.parentId;
    return vaultFolder.toNode();
  }

  bool isLive(String id) => _live(nodes[id]!);
}

Uint8List _bytes(int length, int seed) {
  final random = Random(seed);
  return Uint8List.fromList(List<int>.generate(length, (_) => random.nextInt(256)));
}

void main() {
  late _FakeServer server;
  late Uint8List key;
  late VaultConversionStore store;
  late String source;
  late String vaultFolder;

  VaultConversionState freshState() =>
      VaultConversionState(folderId: source, folderName: 'Taxes', vaultId: 'v1', vaultFolderId: vaultFolder);

  VaultConversion conversion(VaultConversionState state) => VaultConversion(
    backend: server,
    cipher: VaultConversionCipher(key),
    store: store,
    state: state,
    retryDelays: const <Duration>[],
    saveInterval: Duration.zero,
    wholeLimit: 100000,
  );

  /// The vault's contents as plain path → plaintext, read back with the key.
  Future<Map<String, Uint8List>> vaultContents(String folderId, [String path = '']) async {
    final crypto = VaultCrypto.fromKey(key);
    final result = <String, Uint8List>{};
    for (final node in await server.children(folderId)) {
      final name = await crypto.decryptName(node.name);
      final itemPath = path.isEmpty ? name : '$path/$name';
      if (node.isFolder) {
        result.addAll(await vaultContents(node.id, itemPath));
      } else {
        result[itemPath] = await crypto.decryptContent(server.nodes[node.id]!.content);
      }
    }
    return result;
  }

  setUp(() async {
    SharedPreferences.setMockInitialValues(<String, Object>{});
    store = VaultConversionStore(await SharedPreferences.getInstance());
    server = _FakeServer();
    key = newVaultKey();
    source = server.add('root', 'folder', 'Taxes');
    final year = server.add(source, 'folder', '2026');
    server.add(source, 'file', 'empty.txt');
    server.add(source, 'file', 'note.txt', content: _bytes(1000, 1));
    server.add(year, 'file', 'scan.pdf', content: _bytes(250000, 2));
    vaultFolder = server.add('root', 'folder', 'Taxes (1)', vault: true);
  });

  test('the stream cipher writes what VaultCrypto reads', () async {
    for (final size in <int>[0, 1, vaultSegmentSize, vaultSegmentSize + 1, 3 * vaultSegmentSize + 17]) {
      final plain = _bytes(size, size);
      final chunks = <List<int>>[for (var i = 0; i < size; i += 7000) plain.sublist(i, min(i + 7000, size))];
      final sealed = (await encryptVaultStream(key, Stream<List<int>>.fromIterable(chunks), size).toList()).expand((c) => c).toList();
      expect(sealed.length, vaultCiphertextSize(size));
      expect(await VaultCrypto.fromKey(key).decryptContent(Uint8List.fromList(sealed)), plain);
    }
    await expectLater(
      encryptVaultStream(key, Stream<List<int>>.value(_bytes(10, 3)), 9).toList(),
      throwsA(isA<VaultSourceSizeMismatch>()),
    );
  });

  test('converts the folder, keeps its structure and trashes the originals', () async {
    final run = conversion(freshState());
    await run.run();

    expect(run.phase, VaultConversionPhase.done);
    expect(run.doneFiles, 3);
    expect(run.totalBytes, 251000);
    expect(server.isLive(source), isFalse, reason: 'originals in the trash');
    expect(server.nodes.containsKey(source), isTrue, reason: 'never deleted for good');
    expect(run.result!.name, 'Taxes');
    final contents = await vaultContents(vaultFolder);
    expect(contents.keys.toSet(), <String>{'empty.txt', 'note.txt', '2026/scan.pdf'});
    expect(contents['2026/scan.pdf'], _bytes(250000, 2));
    expect(server.parts, greaterThan(1), reason: 'a large file goes in verified parts');
    expect(store.read(source), isNull);
  });

  test('cancel leaves the originals and the partial vault, and a new run continues', () async {
    final first = conversion(freshState());
    await store.write(first.state);
    server.onUpload = (_) => first.cancel();
    await first.run();

    expect(first.phase, VaultConversionPhase.cancelled);
    expect(server.isLive(source), isTrue);
    expect(server.uploads, 1);
    final saved = store.read(source)!;
    expect(saved.done, hasLength(1));

    server.onUpload = null;
    final second = conversion(saved);
    await second.run();
    expect(second.phase, VaultConversionPhase.done);
    expect(server.uploads, 3, reason: 'the file already in the vault is not sent again');
    expect((await vaultContents(vaultFolder)).keys, hasLength(3));
  });

  test('a copy nobody recorded is replaced, not duplicated', () async {
    final first = conversion(freshState());
    server.onUpload = (_) => first.cancel();
    await first.run();
    // The app closed before it could note the upload.
    final lost = VaultConversionState(folderId: source, folderName: 'Taxes', vaultId: 'v1', vaultFolderId: vaultFolder);
    server.onUpload = null;
    await conversion(lost).run();
    final contents = await vaultContents(vaultFolder);
    expect(contents.keys.toSet(), <String>{'empty.txt', 'note.txt', '2026/scan.pdf'});
    expect((await server.children(vaultFolder)).where((node) => !node.isFolder), hasLength(2), reason: 'no duplicate copies');
  });

  test('an original edited after it was copied is copied again on the next run', () async {
    final first = conversion(freshState());
    server.onUpload = (_) => first.cancel();
    await first.run();
    final copiedId = first.state.done.keys.single;
    server.nodes[copiedId]!
      ..content = _bytes(77, 5)
      ..etag += 1;
    server.onUpload = null;
    final second = conversion(store.read(source)!);
    await second.run();
    expect(second.phase, VaultConversionPhase.done);
    final name = server.nodes[copiedId]!.name;
    final path = name == 'scan.pdf' ? '2026/scan.pdf' : name;
    expect((await vaultContents(vaultFolder))[path], _bytes(77, 5));
  });

  test('a folder that changes during the conversion is copied again before it is replaced', () async {
    final run = conversion(freshState());
    var added = false;
    server.onUpload = (_) {
      if (added) return;
      added = true;
      server.add(source, 'file', 'late.txt', content: _bytes(10, 9));
    };
    await run.run();
    expect(run.phase, VaultConversionPhase.done);
    expect((await vaultContents(vaultFolder)).keys, contains('late.txt'));
  });

  test('refuses a shared folder without touching anything', () async {
    server.shared.add(source);
    final run = conversion(freshState());
    await run.run();
    expect(run.phase, VaultConversionPhase.refused);
    expect(run.problems, <String>['SHARED']);
    expect(server.uploads, 0);
    expect(server.isLive(source), isTrue);
  });

  test('a file that fails is skipped and reported; finishing without it is the user’s call', () async {
    final noteId = server.nodes.values.firstWhere((node) => node.name == 'note.txt').id;
    server.unreadable.add(noteId);
    final run = conversion(freshState());
    await run.run();

    expect(run.phase, VaultConversionPhase.review);
    expect(run.failures.map((failure) => failure.path), <String>['note.txt']);
    expect(server.isLive(source), isTrue, reason: 'nothing trashed while a file is missing');
    expect((await vaultContents(vaultFolder)).keys.toSet(), <String>{'empty.txt', '2026/scan.pdf'});

    await run.run(finishWithoutFailed: true);
    expect(run.phase, VaultConversionPhase.done);
    expect(server.isLive(source), isFalse);
    expect(server.nodes[noteId]!.content, hasLength(1000), reason: 'the skipped original is in the trash, not gone');
  });

  test('a lost connection stops the run so it can continue later', () async {
    final offline = _OfflineServer();
    final run = VaultConversion(
      backend: offline,
      cipher: VaultConversionCipher(key),
      store: store,
      state: VaultConversionState(folderId: 'f', folderName: 'F', vaultId: 'v', vaultFolderId: 'vf'),
      retryDelays: const <Duration>[Duration.zero],
    );
    await run.run();
    expect(run.phase, VaultConversionPhase.interrupted);
    expect(run.stopReason, VaultConversionStop.offline);
    expect(store.read('f'), isNotNull);
  });
}

class _OfflineServer extends _FakeServer {
  @override
  Future<VaultConversionCheck> check(String folderId) async => throw const ApiException(0, '', code: 'NETWORK');
}
