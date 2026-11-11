'use strict';

// The search index tables: search_docs (one row per file, the extracted text
// encrypted for snippets) and search_fts (contentless FTS5, the words only).
// A search_docs row going away removes its FTS entry by trigger, so deleting
// a node or an account needs nothing from here.

const zlib = require('zlib');
const db = require('../../db/database');
const { getUserDek, openBytes, sealBytes } = require('../crypto/keys');

// The text is bound to its file: a row copied onto another node won't decrypt.
function textAad(nodeId) {
  return `search-text:${nodeId}`;
}

function encryptText(dek, nodeId, text) {
  return sealBytes(dek, zlib.deflateRawSync(Buffer.from(text, 'utf8')), textAad(nodeId));
}

/** The decrypted texts of doc rows ({ node_id, text_enc }), by node id. */
function decryptTexts(userId, rows) {
  const texts = new Map();
  if (rows.length === 0) return texts;
  const dek = getUserDek(userId);
  for (const row of rows) {
    if (!row.text_enc) continue;
    texts.set(row.node_id, zlib.inflateRawSync(openBytes(dek, row.text_enc, textAad(row.node_id))).toString('utf8'));
  }
  return texts;
}

// Inside a transaction: the doc row for the node, created or reset, with no
// FTS entry. Returns its id.
function resetDoc(userId, node, status, textEnc, textBytes) {
  const existing = db.prepare('SELECT id FROM search_docs WHERE node_id = ?').get(node.id);
  if (existing) {
    db.prepare(`
      UPDATE search_docs SET blob_id = ?, status = ?, text_enc = ?, text_bytes = ?, indexed_at = datetime('now')
      WHERE id = ?
    `).run(node.blob_id, status, textEnc, textBytes, existing.id);
    db.prepare('DELETE FROM search_fts WHERE rowid = ?').run(existing.id);
    return existing.id;
  }
  return db.prepare(`
    INSERT INTO search_docs (user_id, node_id, blob_id, status, text_enc, text_bytes) VALUES (?, ?, ?, ?, ?, ?)
    RETURNING id
  `).get(userId, node.id, node.blob_id, status, textEnc, textBytes).id;
}

// The file may have changed while its text was extracted; then the result
// is for content that is gone, and the next pass picks up the new one.
function stillCurrent(userId, node) {
  const row = db.prepare('SELECT blob_id FROM nodes WHERE id = ? AND user_id = ?').get(node.id, userId);
  return Boolean(row) && row.blob_id === node.blob_id;
}

/** Stores the extracted text of a file node (row with id and blob_id). */
function saveIndexed(userId, node, text) {
  const textEnc = text ? encryptText(getUserDek(userId), node.id, text) : null;
  db.transaction(() => {
    if (!stillCurrent(userId, node)) return;
    const id = resetDoc(userId, node, 'indexed', textEnc, Buffer.byteLength(text || ''));
    if (text) db.prepare('INSERT INTO search_fts (rowid, body) VALUES (?, ?)').run(id, text);
  })();
}

/** Records that a file could not be read, so it isn't tried again until it changes. */
function saveFailed(userId, node) {
  db.transaction(() => {
    if (stillCurrent(userId, node)) resetDoc(userId, node, 'failed', null, 0);
  })();
}

/** Drops the index entries of a node and everything below it. */
function removeSubtree(userId, nodeId) {
  db.prepare(`
    WITH RECURSIVE down(id) AS (
      SELECT id FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL SELECT n.id FROM nodes n JOIN down ON n.parent_id = down.id
    )
    DELETE FROM search_docs WHERE node_id IN (SELECT id FROM down)
  `).run(nodeId, userId);
}

function removeNode(nodeId) {
  db.prepare('DELETE FROM search_docs WHERE node_id = ?').run(nodeId);
}

/** Drops everything indexed for accounts that turned full-text search off. */
function removeOptedOut() {
  return db.prepare(`
    DELETE FROM search_docs WHERE user_id IN (
      SELECT user_id FROM user_settings WHERE key = 'fullTextSearch' AND value = 'false'
    )
  `).run().changes;
}

/** Empties the index; the indexer then reads every file again. */
function clearIndex() {
  db.transaction(() => {
    db.prepare("INSERT INTO search_fts (search_fts) VALUES ('delete-all')").run();
    db.prepare('DELETE FROM search_docs').run();
  })();
}

function readState(key) {
  return db.prepare('SELECT value FROM search_index_state WHERE key = ?').get(key)?.value ?? null;
}

function writeState(key, value) {
  db.prepare(`
    INSERT INTO search_index_state (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value
  `).run(key, String(value));
}

/** Counts and the on-disk size of the index (encrypted texts plus FTS pages). */
function indexTotals() {
  return db.prepare(`
    SELECT
      (SELECT COUNT(*) FROM search_docs WHERE status = 'indexed') AS indexed,
      (SELECT COUNT(*) FROM search_docs WHERE status = 'failed') AS failed,
      (SELECT COALESCE(SUM(length(text_enc)), 0) FROM search_docs)
        + (SELECT COALESCE(SUM(length(block)), 0) FROM search_fts_data)
        + (SELECT COALESCE(SUM(length(sz)), 0) FROM search_fts_docsize) AS bytes
  `).get();
}

module.exports = {
  clearIndex,
  decryptTexts,
  indexTotals,
  readState,
  removeNode,
  removeOptedOut,
  removeSubtree,
  saveFailed,
  saveIndexed,
  writeState,
};
