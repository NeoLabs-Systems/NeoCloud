import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/notes/note_frontmatter.dart';
import 'package:neocloud/src/notes/note_models.dart';
import 'package:neocloud/src/notes/note_store.dart';
import 'package:neocloud/src/vault/vault_keys.dart';
import 'package:neocloud/src/vault/vault_sessions.dart';

const String _vaultId = 'v1';
const String _folderId = 'f1';

/// A server that only stores what it is given: encrypted names and bytes.
class _VaultServer extends BackendClient {
  _VaultServer(this.info) : super(baseUrl: 'http://localhost:1');

  final VaultKeyInfo info;
  final Map<String, FileNode> nodes = <String, FileNode>{};
  final Map<String, Uint8List> contents = <String, Uint8List>{};
  int _next = 0;

  @override
  Future<VaultKeyInfo> vault(String vaultId) async => info;

  @override
  Future<void> updateVaultKey(String vaultId, {required String salt, required int iterations, required String wrappedKey}) async {}

  FileNode _node(String id, String name, int size, String etag) => FileNode(
    id: id,
    parentId: _folderId,
    type: 'file',
    name: name,
    size: size,
    mime: 'application/octet-stream',
    etag: etag,
    version: 1,
    modifiedAt: DateTime(2026, 11, 22, 10, _next),
    vaultId: _vaultId,
  );

  @override
  Future<FileNode> uploadSmall(String parentId, String name, Uint8List bytes, {required String conflict, String? ifMatch, String? sha256, DateTime? created, DateTime? modified}) async {
    final existing = nodes.values.where((node) => node.name == name).firstOrNull;
    if (existing != null && ifMatch != null && ifMatch != existing.etag) throw const ApiException(412, 'changed', code: 'ETAG_MISMATCH');
    _next += 1;
    final id = existing?.id ?? 'n$_next';
    final node = _node(id, name, bytes.length, 'e$_next');
    nodes[id] = node;
    contents[id] = bytes;
    return node;
  }

  @override
  Future<FolderListing> listFolder(String folderId, {String sort = 'name', String order = 'asc', int? limit, String? cursor}) async => FolderListing(
    folder: FileNode(id: _folderId, parentId: 'root', type: 'folder', name: 'Private', size: 0, mime: null, etag: 'f', version: 1, modifiedAt: null, vaultId: _vaultId),
    path: const <PathSegment>[],
    items: nodes.values.toList(),
  );

  @override
  Future<Uint8List> fileBytes(String fileId) async => contents[fileId]!;

  @override
  Future<FileNode> getNode(String id) async => nodes[id]!;

  @override
  Future<FileNode> updateNode(String id, {String? name, String? parentId}) async {
    final node = nodes[id]!;
    _next += 1;
    return nodes[id] = _node(id, name ?? node.name, node.size, 'e$_next');
  }

  @override
  Future<void> trashNode(String id) async {
    nodes.remove(id);
    contents.remove(id);
  }
}

void main() {
  test('frontmatter: known keys parsed, other lines kept, written back the server’s way', () {
    final parsed = parseNoteText('---\ntitle: "From Obsidian"\ntags:\n  - work\n  - "Küche"\npinned: true\ncolor: teal\n---\nBody');
    expect(parsed.properties.tags, <String>['work', 'Küche']);
    expect(parsed.properties.pinned, isTrue);
    expect(parsed.properties.color, NoteColor.teal);
    expect(parsed.body, 'Body');
    final written = serializeNoteText(parsed.properties.copyWith(pinned: false, archived: true), parsed.extra, parsed.body);
    expect(written, '---\ntags:\n  - work\n  - "Küche"\narchived: true\ncolor: teal\ntitle: "From Obsidian"\n---\nBody');
    expect(serializeNoteText(const NoteProperties(), const <String>[], 'plain'), 'plain');
    final (snippet, checklist) = summarizeNoteBody('# Trip\n- [x] **Book**\n- [ ] Pack');
    expect(snippet, 'Trip · Book · Pack');
    expect(checklist.done, 1);
    expect(checklist.total, 2);
  });

  test('notes in an encrypted notebook are encrypted in the app and saved with revisions', () async {
    final key = newVaultKey();
    final wrapped = await wrapVaultKey(key, 'short', iterations: 1000);
    final server = _VaultServer(VaultKeyInfo(id: _vaultId, folderId: _folderId, salt: wrapped.salt, iterations: wrapped.iterations, wrappedKey: wrapped.wrappedKey));
    final vaults = VaultSessions(server);
    expect(await vaults.unlock(_vaultId, 'short'), isTrue);
    final store = VaultNoteStore(server, vaults, const EncryptedNotebook(vaultId: _vaultId, folderId: _folderId, name: 'Private'));

    final note = await store.create(body: '# Diary\nDear diary', properties: const NoteProperties(tags: <String>['me']));
    expect(note.title, 'Diary');
    expect(note.encrypted, isTrue);
    expect(note.tags, <String>['me']);
    expect(store.aiAllowed, isFalse);

    // The server holds no plaintext: neither the name nor the text.
    final stored = server.nodes[note.id]!;
    expect(stored.name.contains('Diary'), isFalse);
    expect(utf8.decode(server.contents[note.id]!, allowMalformed: true).contains('Dear diary'), isFalse);

    final listed = await store.list();
    expect(listed.single.title, 'Diary');
    expect(listed.single.snippet, 'Diary · Dear diary');

    final saved = await store.update(note.id, const NoteChange(body: '- [ ] Write more', pinned: true), revision: note.revision);
    expect(saved.body, '- [ ] Write more');
    expect(saved.pinned, isTrue);
    expect(saved.tags, <String>['me'], reason: 'untouched properties stay');
    expect(saved.revision, isNot(note.revision));
    await expectLater(store.update(note.id, const NoteChange(body: 'lost'), revision: note.revision), throwsA(isA<ApiException>().having((error) => error.code, 'code', 'CONFLICT')));

    final renamed = await store.update(note.id, const NoteChange(title: 'Secret plans'), revision: saved.revision);
    expect(renamed.title, 'Secret plans');
    expect(server.nodes[note.id]!.name.contains('Secret'), isFalse);

    await store.trash(note.id);
    expect(await store.list(), isEmpty);
  });
}
