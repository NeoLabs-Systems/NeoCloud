'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { parseNote, serializeNote, summarizeBody } = require('../../server/utils/note_markdown');

test('a note without frontmatter is a plain note', () => {
  const parsed = parseNote('Hello\nworld');
  assert.deepEqual(parsed.properties, { tags: [], pinned: false, archived: false, color: 'default' });
  assert.equal(parsed.body, 'Hello\nworld');
  assert.equal(serializeNote(parsed), 'Hello\nworld');
});

test('frontmatter: known keys are read, other keys are kept as they were', () => {
  const text = '---\ntitle: "From Obsidian"\ntags:\n  - work\n  - "Küche"\npinned: true\ncolor: teal\naliases: [one, two]\ncustom:\n  - a\n---\nBody';
  const parsed = parseNote(text);
  assert.deepEqual(parsed.properties, { tags: ['work', 'Küche'], pinned: true, archived: false, color: 'teal' });
  assert.equal(parsed.body, 'Body');
  const written = serializeNote({ ...parsed, properties: { ...parsed.properties, pinned: false, archived: true } });
  assert.match(written, /^---\ntags:\n {2}- work\n {2}- "Küche"\narchived: true\ncolor: teal\ntitle: "From Obsidian"\naliases: \[one, two\]\ncustom:\n {2}- a\n---\nBody$/);
  assert.deepEqual(parseNote(written).properties, { tags: ['work', 'Küche'], pinned: false, archived: true, color: 'teal' });
});

test('inline tag lists, unknown colors and duplicate tags are cleaned up', () => {
  const parsed = parseNote('---\ntags: [a, "#b", A]\ncolor: neon\n---\n');
  assert.deepEqual(parsed.properties.tags, ['a', 'b']);
  assert.equal(parsed.properties.color, 'default');
});

test('a card snippet drops Markdown marks and counts the checklist', () => {
  const { snippet, checklist } = summarizeBody('# Trip\n\n- [x] **Book** flight\n- [ ] Pack [list](https://x)\n---\n> note');
  assert.equal(snippet, 'Trip · Book flight · Pack list · note');
  assert.deepEqual(checklist, { done: 1, total: 2 });
});
