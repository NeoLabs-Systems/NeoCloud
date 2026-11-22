import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/notes/note_markdown.dart';
import 'package:neocloud/src/notes/note_models.dart';

TextEditingValue _at(String text, int start, [int? end]) => TextEditingValue(text: text, selection: TextSelection(baseOffset: start, extentOffset: end ?? start));

void main() {
  test('blocks: headings, tasks, lists, quotes, code and dividers', () {
    final blocks = parseNoteBlocks('# Trip\n- [x] Book\n- [ ] Pack\n* item\n2. second\n> said\n---\n```\ncode\n```\ntext');
    expect(blocks.map((block) => block.kind).toList(), <NoteBlockKind>[
      NoteBlockKind.heading1,
      NoteBlockKind.task,
      NoteBlockKind.task,
      NoteBlockKind.bullet,
      NoteBlockKind.numbered,
      NoteBlockKind.quote,
      NoteBlockKind.divider,
      NoteBlockKind.code,
      NoteBlockKind.paragraph,
    ]);
    expect(blocks[1].done, isTrue);
    expect(blocks[2].line, 2);
    expect(blocks[4].number, 2);
    expect(blocks[7].text, 'code');
  });

  test('ticking a task changes only its mark', () {
    expect(toggleTaskLine('- [ ] a\n- [x] b', 0), '- [x] a\n- [x] b');
    expect(toggleTaskLine('- [ ] a\n- [x] b', 1), '- [ ] a\n- [ ] b');
    expect(toggleTaskLine('plain', 0), 'plain');
  });

  test('inline runs', () {
    final runs = parseInline('a **b** *c* ~~d~~ `e` [f](https://x.test)');
    expect(runs.where((run) => run.bold).single.text, 'b');
    expect(runs.where((run) => run.italic).single.text, 'c');
    expect(runs.where((run) => run.strike).single.text, 'd');
    expect(runs.where((run) => run.code).single.text, 'e');
    expect(runs.where((run) => run.link != null).single.link, 'https://x.test');
  });

  test('bold wraps the selection and unwraps it again', () {
    final bold = toggleInlineMarker(_at('say hi', 4, 6), '**');
    expect(bold.text, 'say **hi**');
    expect(bold.selection.textInside(bold.text), 'hi');
    expect(toggleInlineMarker(bold, '**').text, 'say hi');
  });

  test('line prefixes replace each other and toggle off', () {
    final list = toggleLinePrefix(_at('one\ntwo', 0, 7), '- [ ] ');
    expect(list.text, '- [ ] one\n- [ ] two');
    expect(toggleLinePrefix(_at(list.text, 0, list.text.length), '- [ ] ').text, 'one\ntwo');
    expect(toggleLinePrefix(_at('- a', 1), '# ').text, '# a');
  });

  test('slash commands', () {
    expect(slashQuery(_at('x\n/che', 6)), 'che');
    expect(slashQuery(_at('a /che', 6)), isNull);
    final done = replaceSlashCommand(_at('x\n/che', 6), '- [ ] ');
    expect(done.text, 'x\n- [ ] ');
    expect(done.selection.baseOffset, done.text.length);
  });

  test('Enter continues a list and ends it on an empty item', () {
    expect(continueList(_at('- [x] a', 7))!.text, '- [x] a\n- [ ] ');
    expect(continueList(_at('1. a', 4))!.text, '1. a\n2. ');
    expect(continueList(_at('- a\n- ', 6))!.text, '- a\n');
    expect(continueList(_at('plain', 5)), isNull);
  });

  test('AI answers go below the cursor or over the selection', () {
    expect(insertBelow(_at('one\ntwo', 1), '- [ ] task').text, 'one\n- [ ] task\ntwo');
    expect(replaceSelection(_at('a long text', 2, 6), 'short').text, 'a short text');
  });

  test('a made-up title is told from one the person chose', () {
    expect(isAutomaticTitle('Note 2026-11-22 10.15', ''), isTrue);
    expect(isAutomaticTitle('Meeting notes (2)', '# Meeting notes\nAgenda'), isTrue);
    expect(isAutomaticTitle('Plan for Q3', 'Call Anna'), isFalse);
  });

  test('note json', () {
    final note = Note.fromJson(<String, dynamic>{
      'id': 'n1',
      'title': 'T',
      'notebook': 'Work',
      'snippet': 's',
      'checklist': <String, int>{'done': 1, 'total': 2},
      'tags': <String>['a'],
      'pinned': true,
      'archived': false,
      'color': 'teal',
      'modifiedAt': '2026-11-22 10:00:00',
      'revision': 'r1',
      'body': 'b',
    });
    expect(note.color, NoteColor.teal);
    expect(note.checklist.total, 2);
    expect(note.modifiedAt, isNotNull);
    expect(note.withRevision('r2').revision, 'r2');
    expect(NoteColor.plain.wire, 'default');
  });
}
