// Notes as the server lists them (docs/notes.md, docs/api.md#notes): a card
// for the grid, a note with its Markdown body for the editor.

enum NoteColor { plain, red, orange, amber, green, teal, blue, purple, gray }

extension NoteColorWire on NoteColor {
  /// The value in the note's frontmatter (`default` for no color).
  String get wire => this == NoteColor.plain ? 'default' : name;

  static NoteColor parse(Object? value) =>
      NoteColor.values.where((color) => color.wire == value).firstOrNull ?? NoteColor.plain;
}

class NoteChecklist {
  const NoteChecklist({required this.done, required this.total});

  final int done;
  final int total;

  factory NoteChecklist.fromJson(Object? json) {
    final map = json is Map ? json : const <String, Object?>{};
    return NoteChecklist(done: _int(map['done']), total: _int(map['total']));
  }
}

class NoteCard {
  const NoteCard({
    required this.id,
    required this.title,
    required this.notebook,
    required this.snippet,
    required this.checklist,
    required this.tags,
    required this.pinned,
    required this.archived,
    required this.color,
    required this.modifiedAt,
    required this.revision,
  });

  final String id;
  final String title;

  /// The notebook (a folder under /Notes), '' for none.
  final String notebook;
  final String snippet;
  final NoteChecklist checklist;
  final List<String> tags;
  final bool pinned;
  final bool archived;
  final NoteColor color;
  final DateTime? modifiedAt;
  final String revision;

  factory NoteCard.fromJson(Map<String, dynamic> json) => NoteCard(
    id: '${json['id']}',
    title: '${json['title'] ?? ''}',
    notebook: '${json['notebook'] ?? ''}',
    snippet: '${json['snippet'] ?? ''}',
    checklist: NoteChecklist.fromJson(json['checklist']),
    tags: _strings(json['tags']),
    pinned: json['pinned'] == true,
    archived: json['archived'] == true,
    color: NoteColorWire.parse(json['color']),
    modifiedAt: DateTime.tryParse('${json['modifiedAt'] ?? ''}'.replaceFirst(' ', 'T')),
    revision: '${json['revision'] ?? ''}',
  );
}

class Note extends NoteCard {
  const Note({
    required super.id,
    required super.title,
    required super.notebook,
    required super.snippet,
    required super.checklist,
    required super.tags,
    required super.pinned,
    required super.archived,
    required super.color,
    required super.modifiedAt,
    required super.revision,
    required this.body,
  });

  /// The Markdown without the frontmatter.
  final String body;

  /// This note as if read at [value] (to save over a newer version).
  Note withRevision(String value) => Note(
    id: id,
    title: title,
    notebook: notebook,
    snippet: snippet,
    checklist: checklist,
    tags: tags,
    pinned: pinned,
    archived: archived,
    color: color,
    modifiedAt: modifiedAt,
    revision: value,
    body: body,
  );

  factory Note.fromJson(Map<String, dynamic> json) {
    final card = NoteCard.fromJson(json);
    return Note(
      id: card.id,
      title: card.title,
      notebook: card.notebook,
      snippet: card.snippet,
      checklist: card.checklist,
      tags: card.tags,
      pinned: card.pinned,
      archived: card.archived,
      color: card.color,
      modifiedAt: card.modifiedAt,
      revision: card.revision,
      body: '${json['body'] ?? ''}',
    );
  }
}

class NoteTag {
  const NoteTag(this.name, this.count);

  final String name;
  final int count;
}

class NotesListing {
  const NotesListing({required this.notes, required this.notebooks, required this.tags});

  final List<NoteCard> notes;
  final List<String> notebooks;
  final List<NoteTag> tags;

  static const empty = NotesListing(notes: <NoteCard>[], notebooks: <String>[], tags: <NoteTag>[]);

  factory NotesListing.fromJson(Map<String, dynamic> json) => NotesListing(
    notes: _maps(json['notes']).map(NoteCard.fromJson).toList(),
    notebooks: _strings(json['notebooks']),
    tags: _maps(json['tags']).map((tag) => NoteTag('${tag['name']}', _int(tag['count']))).toList(),
  );
}

/// What one AI action returned; exactly one of the three is set.
class NoteAiAnswer {
  const NoteAiAnswer({this.text, this.title, this.tags, this.model});

  final String? text;
  final String? title;
  final List<String>? tags;
  final String? model;

  factory NoteAiAnswer.fromJson(Map<String, dynamic> json) => NoteAiAnswer(
    text: json['text'] is String ? json['text'] as String : null,
    title: json['title'] is String ? json['title'] as String : null,
    tags: json['tags'] is List ? _strings(json['tags']) : null,
    model: json['model'] is String ? json['model'] as String : null,
  );
}

/// The AI actions of a note (`POST /api/notes/:id/ai`).
enum NoteAiAction { summarize, actionItems, improve, shorten, continueWriting, ask, title, tags }

extension NoteAiActionWire on NoteAiAction {
  String get wire => this == NoteAiAction.continueWriting ? 'continue' : name;

  /// Works on the selection when there is one.
  bool get onSelection => this == NoteAiAction.improve || this == NoteAiAction.shorten || this == NoteAiAction.continueWriting;
}

int _int(Object? value) => value is num ? value.toInt() : int.tryParse('$value') ?? 0;

List<String> _strings(Object? value) => value is List ? value.map((item) => '$item').toList() : const <String>[];

List<Map<String, dynamic>> _maps(Object? value) =>
    value is List ? value.whereType<Map>().map((item) => item.cast<String, dynamic>()).toList() : const <Map<String, dynamic>>[];
