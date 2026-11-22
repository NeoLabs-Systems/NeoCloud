'use strict';

// The parsed card of each note, in memory only, keyed by node id and etag:
// listing notes then reads (and decrypts) only the notes that changed since
// the last list. Nothing about a note's text is written anywhere but its file.

const { readBlobBuffer } = require('../signing/blobs');
const { parseNote, summarizeBody } = require('../../utils/note_markdown');

const MAX_ENTRIES = 5000;
const entries = new Map();

function titleOf(name) {
  return name.replace(/\.md$/i, '');
}

function cardOf(node, notebook, parsed) {
  const { snippet, checklist } = summarizeBody(parsed.body);
  return {
    id: node.id,
    title: titleOf(node.name),
    notebook,
    snippet,
    checklist,
    ...parsed.properties,
    size: node.size,
    modifiedAt: node.modified_at,
    revision: node.etag,
  };
}

async function readNote(userId, node) {
  const bytes = await readBlobBuffer(userId, node.blob_id, node.size);
  return parseNote(bytes.toString('utf8'));
}

/** The card for `node` (a live note file of `userId`), from memory when its etag is unchanged. */
async function cardFor(userId, node, notebook) {
  const kept = entries.get(node.id);
  if (kept && kept.userId === userId && kept.etag === node.etag) {
    // Keep the most recently used entries at the end.
    entries.delete(node.id);
    entries.set(node.id, kept);
    return { ...kept.card, notebook, modifiedAt: node.modified_at, title: titleOf(node.name) };
  }
  const card = cardOf(node, notebook, await readNote(userId, node));
  remember(userId, node, card);
  return card;
}

function remember(userId, node, card) {
  entries.delete(node.id);
  entries.set(node.id, { userId, etag: node.etag, card });
  while (entries.size > MAX_ENTRIES) entries.delete(entries.keys().next().value);
}

function clearForTests() {
  entries.clear();
}

module.exports = { cardFor, cardOf, clearForTests, readNote, remember, titleOf };
