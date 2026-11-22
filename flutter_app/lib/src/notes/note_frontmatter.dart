import 'note_models.dart';

// A note's YAML frontmatter, read and written in the app for notes in an
// encrypted notebook, which the server cannot read. The same subset and
// rules as the server's (server/utils/note_markdown.js, docs/notes.md):
// tags, pinned, archived and color; every other line kept as it was.

const int _maxTags = 20;
const int _maxTagLength = 40;
const int _snippetChars = 280;

final RegExp _frontmatter = RegExp(r'^---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)');
final RegExp _keyLine = RegExp(r'^([A-Za-z_][\w-]*):(.*)$');
const List<String> _known = <String>['tags', 'pinned', 'archived', 'color'];

class NoteProperties {
  const NoteProperties({this.tags = const <String>[], this.pinned = false, this.archived = false, this.color = NoteColor.plain});

  final List<String> tags;
  final bool pinned;
  final bool archived;
  final NoteColor color;

  NoteProperties copyWith({List<String>? tags, bool? pinned, bool? archived, NoteColor? color}) => NoteProperties(
    tags: tags == null ? this.tags : cleanNoteTags(tags),
    pinned: pinned ?? this.pinned,
    archived: archived ?? this.archived,
    color: color ?? this.color,
  );
}

class ParsedNote {
  const ParsedNote(this.properties, this.extra, this.body);

  final NoteProperties properties;

  /// Frontmatter lines NeoCloud doesn't use, kept verbatim.
  final List<String> extra;
  final String body;
}

String _unquote(String text) {
  final value = text.trim();
  if (value.length >= 2 && ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))) {
    return value.substring(1, value.length - 1);
  }
  return value;
}

List<String> cleanNoteTags(Iterable<Object?> raw) {
  final seen = <String>{};
  final tags = <String>[];
  for (final item in raw) {
    var tag = '$item'.trim().replaceFirst(RegExp('^#'), '').replaceAll(RegExp(r'[\r\n]'), ' ');
    if (tag.length > _maxTagLength) tag = tag.substring(0, _maxTagLength);
    tag = tag.trim();
    if (tag.isEmpty || !seen.add(tag.toLowerCase())) continue;
    tags.add(tag);
    if (tags.length >= _maxTags) break;
  }
  return tags;
}

ParsedNote parseNoteText(String text) {
  final source = text.startsWith('﻿') ? text.substring(1) : text;
  final match = _frontmatter.firstMatch(source);
  if (match == null) return ParsedNote(const NoteProperties(), const <String>[], source);
  final values = <String, Object>{};
  final extra = <String>[];
  final lines = match.group(1)!.split(RegExp(r'\r?\n'));
  for (var index = 0; index < lines.length; index += 1) {
    final key = _keyLine.firstMatch(lines[index]);
    if (key == null) {
      extra.add(lines[index]);
      continue;
    }
    final block = <String>[lines[index]];
    while (index + 1 < lines.length && RegExp(r'^(\s+|-\s)').hasMatch(lines[index + 1])) {
      index += 1;
      block.add(lines[index]);
    }
    final name = key.group(1)!;
    final rest = key.group(2)!;
    if (!_known.contains(name)) {
      extra.addAll(block);
      continue;
    }
    if (rest.trim().startsWith('[')) {
      final inner = rest.trim().substring(1, rest.trim().length - 1).trim();
      values[name] = inner.isEmpty ? <String>[] : inner.split(',').map(_unquote).where((tag) => tag.isNotEmpty).toList();
    } else if (block.length > 1) {
      values[name] = block.skip(1).map((item) => _unquote(item.replaceFirst(RegExp(r'^\s*-\s?'), ''))).where((tag) => tag.isNotEmpty).toList();
    } else {
      values[name] = rest.trim() == 'true' ? true : (rest.trim() == 'false' ? false : _unquote(rest));
    }
  }
  final tags = values['tags'];
  return ParsedNote(
    NoteProperties(
      tags: cleanNoteTags(tags is List ? tags : (tags is String && tags.isNotEmpty ? <String>[tags] : const <String>[])),
      pinned: values['pinned'] == true,
      archived: values['archived'] == true,
      color: NoteColorWire.parse(values['color']),
    ),
    extra,
    source.substring(match.end),
  );
}

String _quoteIfNeeded(String text) {
  final plain = RegExp(r'^[\w][\w .\-/&+]*$').hasMatch(text) && !<String>['true', 'false', 'null'].contains(text);
  if (plain) return text;
  return '"${text.replaceAll(r'\', r'\\').replaceAll('"', r'\"')}"';
}

String serializeNoteText(NoteProperties properties, List<String> extra, String body) {
  final lines = <String>[];
  if (properties.tags.isNotEmpty) {
    lines.add('tags:');
    for (final tag in properties.tags) {
      lines.add('  - ${_quoteIfNeeded(tag)}');
    }
  }
  if (properties.pinned) lines.add('pinned: true');
  if (properties.archived) lines.add('archived: true');
  if (properties.color != NoteColor.plain) lines.add('color: ${properties.color.wire}');
  lines.addAll(extra.where((line) => line.trim().isNotEmpty));
  if (lines.isEmpty) return body;
  return '---\n${lines.join('\n')}\n---\n$body';
}

/// A card's snippet and checklist progress, like the server's.
(String, NoteChecklist) summarizeNoteBody(String body) {
  var total = 0;
  var done = 0;
  final plain = <String>[];
  for (final line in body.split(RegExp(r'\r?\n'))) {
    final task = RegExp(r'^\s*[-*+]\s+\[([ xX])\]\s?(.*)$').firstMatch(line);
    if (task != null) {
      total += 1;
      if (task.group(1) != ' ') done += 1;
    }
    final text = (task != null ? task.group(2)! : line)
        .replaceFirst(RegExp(r'^\s*(#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+)'), '')
        .replaceAll(RegExp(r'(\*\*|__|\*|_|~~|`)'), '')
        .replaceAllMapped(RegExp(r'!?\[([^\]]*)\]\([^)]*\)'), (match) => match.group(1)!)
        .trim();
    if (text.isNotEmpty && !RegExp(r'^(-{3,}|\*{3,}|```.*)$').hasMatch(line.trim())) plain.add(text);
  }
  final snippet = plain.join(' · ');
  return (snippet.length > _snippetChars ? '${snippet.substring(0, _snippetChars - 1)}…' : snippet, NoteChecklist(done: done, total: total));
}
