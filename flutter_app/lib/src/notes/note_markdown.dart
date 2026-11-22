import 'package:flutter/services.dart';

// The Markdown a note is written in (docs/notes.md): blocks for the read
// view, checklist ticks, and the edits the formatting bar and the `/` menu
// make on the text. Pure, so it is tested without widgets.

enum NoteBlockKind { heading1, heading2, heading3, paragraph, bullet, numbered, task, quote, code, divider, blank }

class NoteBlock {
  const NoteBlock(this.kind, this.text, {this.line = 0, this.done = false, this.number = 0});

  final NoteBlockKind kind;

  /// The text without its Markdown marker (a code block: its lines joined).
  final String text;

  /// The first line of the block in the body, for ticking a task.
  final int line;
  final bool done;
  final int number;
}

final RegExp _task = RegExp(r'^\s*[-*+]\s+\[([ xX])\]\s?(.*)$');
final RegExp _bullet = RegExp(r'^\s*[-*+]\s+(.*)$');
final RegExp _numbered = RegExp(r'^\s*(\d+)[.)]\s+(.*)$');
final RegExp _heading = RegExp(r'^(#{1,6})\s+(.*)$');
final RegExp _divider = RegExp(r'^\s*(-{3,}|\*{3,}|_{3,})\s*$');

/// The blocks of [body], one per line except fenced code.
List<NoteBlock> parseNoteBlocks(String body) {
  final lines = body.split('\n');
  final blocks = <NoteBlock>[];
  for (var index = 0; index < lines.length; index += 1) {
    final line = lines[index].replaceAll('\r', '');
    if (line.trimLeft().startsWith('```')) {
      final start = index;
      final code = <String>[];
      index += 1;
      while (index < lines.length && !lines[index].trimLeft().startsWith('```')) {
        code.add(lines[index].replaceAll('\r', ''));
        index += 1;
      }
      blocks.add(NoteBlock(NoteBlockKind.code, code.join('\n'), line: start));
      continue;
    }
    final task = _task.firstMatch(line);
    final heading = _heading.firstMatch(line);
    final numbered = _numbered.firstMatch(line);
    final bullet = _bullet.firstMatch(line);
    if (line.trim().isEmpty) {
      blocks.add(NoteBlock(NoteBlockKind.blank, '', line: index));
    } else if (_divider.hasMatch(line)) {
      blocks.add(NoteBlock(NoteBlockKind.divider, '', line: index));
    } else if (task != null) {
      blocks.add(NoteBlock(NoteBlockKind.task, task.group(2)!, line: index, done: task.group(1) != ' '));
    } else if (heading != null) {
      final level = heading.group(1)!.length;
      final kind = level == 1 ? NoteBlockKind.heading1 : (level == 2 ? NoteBlockKind.heading2 : NoteBlockKind.heading3);
      blocks.add(NoteBlock(kind, heading.group(2)!, line: index));
    } else if (line.startsWith('>')) {
      blocks.add(NoteBlock(NoteBlockKind.quote, line.replaceFirst(RegExp(r'^>\s?'), ''), line: index));
    } else if (numbered != null) {
      blocks.add(NoteBlock(NoteBlockKind.numbered, numbered.group(2)!, line: index, number: int.parse(numbered.group(1)!)));
    } else if (bullet != null) {
      blocks.add(NoteBlock(NoteBlockKind.bullet, bullet.group(1)!, line: index));
    } else {
      blocks.add(NoteBlock(NoteBlockKind.paragraph, line, line: index));
    }
  }
  return blocks;
}

/// [body] with the task on [line] ticked or unticked.
String toggleTaskLine(String body, int line) {
  final lines = body.split('\n');
  if (line < 0 || line >= lines.length) return body;
  final match = _task.firstMatch(lines[line]);
  if (match == null) return body;
  final open = lines[line].indexOf('[');
  final mark = match.group(1) == ' ' ? 'x' : ' ';
  lines[line] = '${lines[line].substring(0, open + 1)}$mark${lines[line].substring(open + 2)}';
  return lines.join('\n');
}

/// One run of inline text: bold, italic, struck through, code or a link.
class InlineRun {
  const InlineRun(this.text, {this.bold = false, this.italic = false, this.strike = false, this.code = false, this.link});

  final String text;
  final bool bold;
  final bool italic;
  final bool strike;
  final bool code;
  final String? link;
}

final RegExp _inline = RegExp(r'(\*\*[^*]+\*\*|__[^_]+__|~~[^~]+~~|`[^`]+`|\[[^\]]+\]\([^)\s]+\)|\*[^*\s][^*]*\*|_[^_\s][^_]*_)');

/// The inline runs of one line of Markdown.
List<InlineRun> parseInline(String text) {
  final runs = <InlineRun>[];
  var position = 0;
  for (final match in _inline.allMatches(text)) {
    if (match.start > position) runs.add(InlineRun(text.substring(position, match.start)));
    final token = match.group(0)!;
    if (token.startsWith('**') || token.startsWith('__')) {
      runs.add(InlineRun(token.substring(2, token.length - 2), bold: true));
    } else if (token.startsWith('~~')) {
      runs.add(InlineRun(token.substring(2, token.length - 2), strike: true));
    } else if (token.startsWith('`')) {
      runs.add(InlineRun(token.substring(1, token.length - 1), code: true));
    } else if (token.startsWith('[')) {
      final close = token.indexOf('](');
      runs.add(InlineRun(token.substring(1, close), link: token.substring(close + 2, token.length - 1)));
    } else {
      runs.add(InlineRun(token.substring(1, token.length - 1), italic: true));
    }
    position = match.end;
  }
  if (position < text.length) runs.add(InlineRun(text.substring(position)));
  return runs;
}

/// Puts [marker] around the selection (`**bold**`), or takes it away when
/// the selection already has it. A collapsed selection gets an empty pair
/// with the cursor in the middle.
TextEditingValue toggleInlineMarker(TextEditingValue value, String marker, {String? closing}) {
  final close = closing ?? marker;
  final selection = value.selection;
  if (!selection.isValid) return value;
  final text = value.text;
  final start = selection.start;
  final end = selection.end;
  final hasBefore = start >= marker.length && text.substring(start - marker.length, start) == marker;
  final hasAfter = end + close.length <= text.length && text.substring(end, end + close.length) == close;
  if (hasBefore && hasAfter && !selection.isCollapsed) {
    final updated = text.replaceRange(end, end + close.length, '').replaceRange(start - marker.length, start, '');
    return TextEditingValue(text: updated, selection: TextSelection(baseOffset: start - marker.length, extentOffset: end - marker.length));
  }
  final updated = text.replaceRange(end, end, close).replaceRange(start, start, marker);
  return TextEditingValue(
    text: updated,
    selection: TextSelection(baseOffset: start + marker.length, extentOffset: end + marker.length),
  );
}

/// Where the line holding [offset] starts.
int _lineStart(String text, int offset) => offset <= 0 ? 0 : text.lastIndexOf('\n', offset - 1) + 1;

/// The prefixes a line can start with; one replaces another.
const List<String> notePrefixes = <String>['# ', '## ', '### ', '- [ ] ', '- [x] ', '- ', '> ', '1. '];

String _prefixOf(String line) {
  for (final prefix in notePrefixes) {
    if (line.startsWith(prefix)) return prefix;
  }
  final numbered = RegExp(r'^\d+[.)]\s').firstMatch(line);
  return numbered?.group(0) ?? '';
}

/// Sets [prefix] on every line the selection touches; a second press with
/// the same prefix takes it away again.
TextEditingValue toggleLinePrefix(TextEditingValue value, String prefix) {
  final selection = value.selection;
  if (!selection.isValid) return value;
  final text = value.text;
  final firstLine = _lineStart(text, selection.start);
  var lastLineEnd = text.indexOf('\n', selection.end);
  if (lastLineEnd == -1) lastLineEnd = text.length;
  final lines = text.substring(firstLine, lastLineEnd).split('\n');
  final remove = lines.every((line) => _prefixOf(line) == prefix || (prefix == '- [ ] ' && _prefixOf(line) == '- [x] '));
  var delta = 0;
  final changed = <String>[];
  for (final line in lines) {
    final current = _prefixOf(line);
    final rest = line.substring(current.length);
    final next = remove ? rest : '$prefix$rest';
    delta += next.length - line.length;
    changed.add(next);
  }
  final updated = text.replaceRange(firstLine, lastLineEnd, changed.join('\n'));
  final cursor = (selection.end + delta).clamp(firstLine, updated.length);
  return TextEditingValue(text: updated, selection: TextSelection.collapsed(offset: cursor));
}

/// What follows a `/` typed at the start of the cursor's line ("/che" →
/// "che"), or null when the cursor is not in such a command.
String? slashQuery(TextEditingValue value) {
  final selection = value.selection;
  if (!selection.isValid || !selection.isCollapsed) return null;
  final text = value.text;
  final lineStart = _lineStart(text, selection.baseOffset);
  final typed = text.substring(lineStart, selection.baseOffset);
  final match = RegExp(r'^/([\p{L}\d]{0,20})$', unicode: true).firstMatch(typed);
  return match?.group(1);
}

/// Replaces the `/command` on the cursor's line with [insert], placing the
/// cursor at its end (or at [cursorAt] within it).
TextEditingValue replaceSlashCommand(TextEditingValue value, String insert, {int? cursorAt}) {
  final text = value.text;
  final cursor = value.selection.baseOffset;
  final lineStart = _lineStart(text, cursor);
  final updated = text.replaceRange(lineStart, cursor, insert);
  return TextEditingValue(text: updated, selection: TextSelection.collapsed(offset: lineStart + (cursorAt ?? insert.length)));
}

/// Inserts [block] on its own line after the selection.
TextEditingValue insertBelow(TextEditingValue value, String block) {
  final text = value.text;
  final at = value.selection.isValid ? value.selection.end : text.length;
  var lineEnd = text.indexOf('\n', at);
  if (lineEnd == -1) lineEnd = text.length;
  final content = block.trim();
  final insert = lineEnd == 0 ? '$content${text.isEmpty ? '' : '\n'}' : '\n$content';
  final updated = text.replaceRange(lineEnd, lineEnd, insert);
  return TextEditingValue(text: updated, selection: TextSelection.collapsed(offset: lineEnd + (lineEnd == 0 ? content.length : insert.length)));
}

/// Replaces the selection with [replacement] (the whole text when nothing is selected).
TextEditingValue replaceSelection(TextEditingValue value, String replacement) {
  final selection = value.selection;
  if (!selection.isValid || selection.isCollapsed) {
    return TextEditingValue(text: replacement, selection: TextSelection.collapsed(offset: replacement.length));
  }
  final updated = value.text.replaceRange(selection.start, selection.end, replacement);
  return TextEditingValue(text: updated, selection: TextSelection.collapsed(offset: selection.start + replacement.length));
}

/// Pressing Enter on a list line continues the list; on an empty item it ends it.
/// Returns null when Enter should just insert a line break.
TextEditingValue? continueList(TextEditingValue value) {
  final selection = value.selection;
  if (!selection.isValid || !selection.isCollapsed) return null;
  final text = value.text;
  final cursor = selection.baseOffset;
  final lineStart = _lineStart(text, cursor);
  final line = text.substring(lineStart, cursor);
  final prefix = _prefixOf(line);
  if (prefix.isEmpty || prefix.startsWith('#')) return null;
  if (line.trim() == prefix.trim()) {
    final updated = text.replaceRange(lineStart, cursor, '');
    return TextEditingValue(text: updated, selection: TextSelection.collapsed(offset: lineStart));
  }
  var next = prefix == '- [x] ' ? '- [ ] ' : prefix;
  final number = RegExp(r'^(\d+)([.)])\s').firstMatch(prefix);
  if (number != null) next = '${int.parse(number.group(1)!) + 1}${number.group(2)} ';
  final insert = '\n$next';
  final updated = text.replaceRange(cursor, cursor, insert);
  return TextEditingValue(text: updated, selection: TextSelection.collapsed(offset: cursor + insert.length));
}

/// Whether [title] is one NeoCloud made up for an untitled note: the first
/// line of the text, or `Note <date>` (see the server's defaultTitle).
bool isAutomaticTitle(String title, String body) {
  if (RegExp(r'^Note \d{4}-\d{2}-\d{2} \d{2}\.\d{2}( \(\d+\))?$').hasMatch(title)) return true;
  final first = body.split('\n').map((line) => line.replaceAll(RegExp(r'^[#>\-*+\s]+|\[[ xX]\]\s*'), '').trim()).where((line) => line.isNotEmpty).firstOrNull;
  if (first == null) return false;
  final base = title.replaceFirst(RegExp(r' \(\d+\)$'), '');
  return base.isNotEmpty && first.startsWith(base);
}
