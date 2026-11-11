'use strict';

// The content half of `GET /api/search?content=1`: files whose text matches,
// best BM25 first, with snippets cut from the decrypted text. The caller
// (files/search.js) passes its filter SQL so every filter applies here too.

const db = require('../../db/database');
const { readSettings } = require('../settings/store');
const { NODE_COLUMNS } = require('../files/tree');
const { isIndexEnabled } = require('./indexer');
const { makeSnippet, matchExpression, matchesAll, needsVerification } = require('./query_text');
const { decryptTexts } = require('./store');

// Phrases are checked in the text after the index query, so fetch extra
// candidates to make up for the ones that fail the check.
const PHRASE_OVERFETCH = 4;

/** Whether content search applies to this account right now. */
function contentSearchEnabled(userId) {
  return isIndexEnabled() && readSettings(userId).fullTextSearch === true;
}

/**
 * Content matches for parsed `terms`.
 *   scope, where, params  the name search's CTE, conditions over `n` and values
 *   nameIds               ids the name search already returned, in its order
 *   limit                 the page size
 * Returns { snippets: Map(nameId → snippet) for name results whose content
 * matches too, rows: [{ row, snippet }] further content-only results }.
 */
function findContentMatches(userId, { terms, scope, where, params, nameIds, limit }) {
  const match = matchExpression(terms);
  const result = { snippets: new Map(), rows: [] };
  if (!match) return result;
  const verify = needsVerification(terms);
  const wanted = Math.max(limit - nameIds.length, 0);

  const nameDocs = nameIds.length === 0 ? [] : db.prepare(`
    SELECT d.node_id, d.text_enc
    FROM search_fts JOIN search_docs d ON d.id = search_fts.rowid
    JOIN nodes n ON n.id = d.node_id AND n.blob_id = d.blob_id
    WHERE search_fts MATCH @match AND d.user_id = @userId AND d.status = 'indexed'
      AND d.node_id IN (SELECT value FROM json_each(@nameIds))
  `).all({ match, userId, nameIds: JSON.stringify(nameIds) });

  // Every fragment is fixed text or the caller's fixed text; values are bound.
  const contentRows = wanted === 0 ? [] : db.prepare(`
    ${scope}
    SELECT n.*, d.text_enc, bm25(search_fts) AS score
    FROM search_fts
    JOIN search_docs d ON d.id = search_fts.rowid
    JOIN (SELECT ${NODE_COLUMNS} FROM nodes) n ON n.id = d.node_id AND n.blob_id = d.blob_id
    WHERE search_fts MATCH @match AND d.user_id = @userId AND d.status = 'indexed'
      AND n.id NOT IN (SELECT value FROM json_each(@nameIds))
      AND ${where.join(' AND ')}
    ORDER BY score, n.modified_at DESC
    LIMIT @candidates
  `).all({
    ...params,
    match,
    nameIds: JSON.stringify(nameIds),
    candidates: verify ? wanted * PHRASE_OVERFETCH : wanted,
  });

  const texts = decryptTexts(userId, [...nameDocs, ...contentRows.map((row) => ({ node_id: row.id, text_enc: row.text_enc }))]);
  const fits = (id) => !verify || matchesAll(texts.get(id) || '', terms);
  for (const doc of nameDocs) {
    if (fits(doc.node_id)) result.snippets.set(doc.node_id, makeSnippet(texts.get(doc.node_id), terms));
  }
  for (const row of contentRows) {
    if (result.rows.length >= wanted) break;
    if (!fits(row.id)) continue;
    const { text_enc: _text, score: _score, ...node } = row;
    result.rows.push({ row: node, snippet: makeSnippet(texts.get(row.id), terms) });
  }
  return result;
}

module.exports = { contentSearchEnabled, findContentMatches };
