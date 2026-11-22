part of 'backend_client.dart';

/// Notes: Markdown files under /Notes (docs/api.md#notes).
extension BackendNotesCalls on BackendClient {
  Future<NotesListing> notes({String? query, String? tag, String? notebook, bool archived = false}) async => NotesListing.fromJson(
    _map(await _request('GET', '/api/notes', query: <String, String>{
      if (query != null && query.trim().isNotEmpty) 'q': query.trim(),
      'tag': ?tag,
      'notebook': ?notebook,
      if (archived) 'archived': 'true',
    })),
  );

  Future<Note> note(String id) async => Note.fromJson(_map(await _request('GET', '/api/notes/$id')));

  Future<Note> createNote({String? title, String? body, String? notebook, List<String>? tags, bool? pinned, NoteColor? color}) async =>
      Note.fromJson(_map(await _request('POST', '/api/notes', json: <String, Object?>{
        'title': ?title,
        'body': ?body,
        'notebook': ?notebook,
        'tags': ?tags,
        'pinned': ?pinned,
        if (color != null) 'color': color.wire,
      })));

  /// Changes a note; [baseRevision] refuses it (409 `CONFLICT`) when the
  /// note changed since it was read.
  Future<Note> updateNote(
    String id, {
    String? body,
    String? title,
    String? notebook,
    List<String>? tags,
    bool? pinned,
    bool? archived,
    NoteColor? color,
    String? baseRevision,
  }) async => Note.fromJson(_map(await _request('PATCH', '/api/notes/$id', json: <String, Object?>{
    'body': ?body,
    'title': ?title,
    'notebook': ?notebook,
    'tags': ?tags,
    'pinned': ?pinned,
    'archived': ?archived,
    if (color != null) 'color': color.wire,
    'baseRevision': ?baseRevision,
  })));

  Future<void> deleteNote(String id) => _request('DELETE', '/api/notes/$id');

  Future<void> createNotebook(String name) => _request('POST', '/api/notes/notebooks', json: <String, Object?>{'name': name});

  Future<NoteAiAnswer> noteAi(String id, NoteAiAction action, {String? selection, String? question}) async => NoteAiAnswer.fromJson(
    _map(await _request('POST', '/api/notes/$id/ai', json: <String, Object?>{
      'action': action.wire,
      if (selection != null && selection.trim().isNotEmpty) 'selection': selection,
      'question': ?question,
    })),
  );
}
