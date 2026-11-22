import 'dart:convert';
import 'dart:typed_data';

import '../api/backend_client.dart';
import '../api/models.dart';
import '../vault/vault_sessions.dart';
import 'note_frontmatter.dart';
import 'note_models.dart';

/// What a save changes; null fields stay as they are.
class NoteChange {
  const NoteChange({this.body, this.title, this.tags, this.pinned, this.archived, this.color, this.notebook});

  final String? body;
  final String? title;
  final List<String>? tags;
  final bool? pinned;
  final bool? archived;
  final NoteColor? color;
  final String? notebook;

  bool get touchesText => body != null || tags != null || pinned != null || archived != null || color != null;
}

/// Where a note is read from and saved to: the server for ordinary notes,
/// the app's vault crypto for notes in an encrypted notebook. A save with a
/// stale [revision] fails with `ApiException` code `CONFLICT`.
abstract class NoteStore {
  Future<Note> load(String id);

  Future<Note> update(String id, NoteChange change, {String? revision});

  Future<void> trash(String id);

  /// Whether AI may read notes kept here (never for a vault).
  bool get aiAllowed;
}

/// Notes the server reads and writes (`/api/notes`).
class ServerNoteStore implements NoteStore {
  const ServerNoteStore(this._client);

  final BackendClient _client;

  @override
  bool get aiAllowed => true;

  @override
  Future<Note> load(String id) => _client.note(id);

  @override
  Future<Note> update(String id, NoteChange change, {String? revision}) => _client.updateNote(
    id,
    body: change.body,
    title: change.title,
    notebook: change.notebook,
    tags: change.tags,
    pinned: change.pinned,
    archived: change.archived,
    color: change.color,
    baseRevision: revision,
  );

  @override
  Future<void> trash(String id) => _client.deleteNote(id);
}

/// Notes in an encrypted notebook: `.md` files in a vault folder, their
/// names and content encrypted and decrypted here with the vault key
/// (docs/clients.md §4). The server only ever stores ciphertext.
class VaultNoteStore implements NoteStore {
  VaultNoteStore(this._client, this._vaults, this.notebook);

  final BackendClient _client;
  final VaultSessions _vaults;
  final EncryptedNotebook notebook;

  static const int _maxNoteBytes = 1024 * 1024;

  /// The files last listed, by id (plain names).
  final Map<String, FileNode> _nodes = <String, FileNode>{};

  /// Parsed cards by node id and etag, so a list decrypts only what changed.
  final Map<String, (String, NoteCard)> _cards = <String, (String, NoteCard)>{};

  @override
  bool get aiAllowed => false;

  bool get unlocked => _vaults.isUnlocked(notebook.vaultId);

  static String _titleOf(String name) => name.replaceFirst(RegExp(r'\.md$', caseSensitive: false), '');

  Note _noteOf(FileNode node, ParsedNote parsed) {
    final (snippet, checklist) = summarizeNoteBody(parsed.body);
    return Note(
      id: node.id,
      title: _titleOf(node.name),
      notebook: notebook.name,
      snippet: snippet,
      checklist: checklist,
      tags: parsed.properties.tags,
      pinned: parsed.properties.pinned,
      archived: parsed.properties.archived,
      color: parsed.properties.color,
      modifiedAt: node.modifiedAt,
      revision: node.etag,
      vaultId: notebook.vaultId,
      body: parsed.body,
    );
  }

  Future<ParsedNote> _read(FileNode node) async {
    final data = await _vaults.decrypt(notebook.vaultId, await _client.fileBytes(node.id));
    return parseNoteText(utf8.decode(data, allowMalformed: true));
  }

  /// Every note in the notebook as a card.
  Future<List<NoteCard>> list() async {
    final files = <FileNode>[];
    String? cursor;
    do {
      final page = await _client.listFolder(notebook.folderId, limit: 500, cursor: cursor);
      files.addAll(await _vaults.reveal(page.items));
      cursor = page.nextCursor;
    } while (cursor != null);
    final notes = files.where((node) => !node.isFolder && node.size <= _maxNoteBytes && node.name.toLowerCase().endsWith('.md')).toList();
    _nodes
      ..clear()
      ..addEntries(notes.map((node) => MapEntry(node.id, node)));
    final cards = <NoteCard>[];
    for (final node in notes) {
      final kept = _cards[node.id];
      if (kept != null && kept.$1 == node.etag) {
        cards.add(kept.$2);
        continue;
      }
      final card = _noteOf(node, await _read(node));
      _cards[node.id] = (node.etag, card);
      cards.add(card);
    }
    return cards;
  }

  Future<FileNode> _node(String id) async {
    final known = _nodes[id];
    if (known != null) return known;
    final node = (await _vaults.reveal(<FileNode>[await _client.getNode(id)])).single;
    if (node.vaultId != notebook.vaultId) throw const ApiException(404, 'Note not found', code: 'NOT_FOUND');
    _nodes[id] = node;
    return node;
  }

  @override
  Future<Note> load(String id) async {
    final node = await _node(id);
    return _noteOf(node, await _read(node));
  }

  Future<String> _cipherName(String title) => _vaults.encryptName(notebook.vaultId, '${_safeTitle(title)}.md');

  static String _safeTitle(String raw) {
    final title = raw.replaceAll(RegExp(r'[\u0000-\u001f\u007f/\\]'), ' ').replaceAll(RegExp(r'\s+'), ' ').trim();
    return title.length > 120 ? title.substring(0, 120).trim() : title;
  }

  /// A new note: the title is the first line when none is given.
  Future<Note> create({String? title, String? body, NoteProperties properties = const NoteProperties()}) async {
    final text = body ?? '';
    final first = text.split('\n').map((line) => line.replaceAll(RegExp(r'^[#>\-*+\s]+|\[[ xX]\]\s*'), '').trim()).where((line) => line.isNotEmpty).firstOrNull;
    final now = DateTime.now();
    final fallback = 'Note ${now.toIso8601String().substring(0, 16).replaceFirst('T', ' ').replaceFirst(':', '.')}';
    final name = _safeTitle(title ?? '').isNotEmpty ? title! : (first == null ? fallback : (first.length > 60 ? first.substring(0, 60) : first));
    final data = await _vaults.encrypt(notebook.vaultId, Uint8List.fromList(utf8.encode(serializeNoteText(properties, const <String>[], text))));
    final stored = await _client.uploadSmall(notebook.folderId, await _cipherName(name), data, conflict: 'rename');
    final node = (await _vaults.reveal(<FileNode>[stored])).single;
    _nodes[node.id] = node;
    return load(node.id);
  }

  @override
  Future<Note> update(String id, NoteChange change, {String? revision}) async {
    var node = await _node(id);
    if (revision != null && revision != node.etag) {
      // The list may be old: ask the server for the file's current etag.
      node = (await _vaults.reveal(<FileNode>[await _client.getNode(id)])).single;
      _nodes[id] = node;
      if (revision != node.etag) throw const ApiException(409, 'The note changed since it was read.', code: 'CONFLICT');
    }
    if (change.touchesText) {
      final current = await _read(node);
      final properties = current.properties.copyWith(tags: change.tags, pinned: change.pinned, archived: change.archived, color: change.color);
      final text = serializeNoteText(properties, current.extra, change.body ?? current.body);
      final bytes = Uint8List.fromList(utf8.encode(text));
      if (bytes.length > _maxNoteBytes) throw const ApiException(413, 'This note is too long (1 MB at most).', code: 'TOO_LARGE');
      final data = await _vaults.encrypt(notebook.vaultId, bytes);
      try {
        final stored = await _client.uploadSmall(node.parentId ?? notebook.folderId, node.cipherName ?? node.name, data, conflict: 'replace', ifMatch: node.etag);
        node = node.updatedFrom(stored);
      } on ApiException catch (error) {
        if (error.code == 'ETAG_MISMATCH' || error.statusCode == 412) throw const ApiException(409, 'The note changed since it was read.', code: 'CONFLICT');
        rethrow;
      }
    }
    final title = change.title == null ? '' : _safeTitle(change.title!);
    if (title.isNotEmpty && title != _titleOf(node.name)) {
      final renamed = await _client.updateNode(id, name: await _cipherName(title));
      node = (await _vaults.reveal(<FileNode>[renamed])).single;
    }
    _nodes[id] = node;
    final note = await load(id);
    _cards[id] = (node.etag, note);
    return note;
  }

  @override
  Future<void> trash(String id) async {
    await _client.trashNode(id);
    _nodes.remove(id);
    _cards.remove(id);
  }
}
