'use strict';

// The one structured call per file that auto-title, tags and sorting share:
// date, issuer, subject, type, amount, currency, tags, language, a short
// summary (and a caption for photos), each with a confidence. NeoCloud
// checks the answer against the text (the issuer and amount must appear in
// it) and picks the date by precedence; the result is kept per node and
// content (ai_node_meta), so a second feature asks nothing again.

const db = require('../../../db/database');
const ai = require('../client');
const { normalizeExtraction } = require('./fields');
const { extractionInstructions } = require('./prompts');
const { readFileInput } = require('./content');

const MAX_EXISTING_TAGS = 60;

function existingTags(userId) {
  const rows = db.prepare(`
    SELECT DISTINCT lower(j.value) AS tag FROM ai_node_meta m, json_each(m.tags_json) j
    WHERE m.user_id = ? LIMIT ?
  `).all(userId, MAX_EXISTING_TAGS);
  return rows.map((row) => row.tag);
}

function describe(node) {
  return `File name: ${node.name}\nModified: ${String(node.modified_at).slice(0, 10)}`;
}

/** The kept extraction for the node's current content, or null. */
function cachedExtraction(userId, node) {
  const row = db.prepare('SELECT fields_json, model FROM ai_node_meta WHERE node_id = ? AND user_id = ? AND etag = ?')
    .get(node.id, userId, node.etag);
  if (!row) return null;
  const fields = JSON.parse(row.fields_json);
  return Object.keys(fields).length > 0 ? { fields, model: row.model } : null;
}

function searchText(fields, tags) {
  return [...tags, fields.issuer, fields.subject, fields.type, fields.caption]
    .filter(Boolean).join(' ').toLowerCase();
}

function saveExtraction(userId, node, fields, model) {
  const tags = JSON.parse(db.prepare('SELECT tags_json FROM ai_node_meta WHERE node_id = ?').get(node.id)?.tags_json || '[]');
  db.prepare(`
    INSERT INTO ai_node_meta (node_id, user_id, etag, fields_json, tags_json, search_text, model, updated_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
    ON CONFLICT(node_id) DO UPDATE SET etag = excluded.etag, fields_json = excluded.fields_json,
      search_text = excluded.search_text, model = excluded.model, updated_at = excluded.updated_at
  `).run(node.id, userId, node.etag, JSON.stringify(fields), JSON.stringify(tags), searchText(fields, tags), model);
}

/** Stores the tags the owner accepted for a node (aiTags) and refreshes its search words. */
function saveTags(userId, node, tags) {
  const row = db.prepare('SELECT fields_json FROM ai_node_meta WHERE node_id = ?').get(node.id);
  const fields = row ? JSON.parse(row.fields_json) : {};
  db.prepare(`
    INSERT INTO ai_node_meta (node_id, user_id, etag, tags_json, search_text, updated_at)
    VALUES (?, ?, ?, ?, ?, datetime('now'))
    ON CONFLICT(node_id) DO UPDATE SET tags_json = excluded.tags_json, search_text = excluded.search_text,
      updated_at = excluded.updated_at
  `).run(node.id, userId, node.etag, JSON.stringify(tags), searchText(fields, tags));
}

function acceptedTags(nodeId) {
  return JSON.parse(db.prepare('SELECT tags_json FROM ai_node_meta WHERE node_id = ?').get(nodeId)?.tags_json || '[]');
}

/**
 * The extraction for a file node: kept, or one model call now. `feature`
 * labels the call in the usage log. Returns { fields, model } or
 * { skipped: reason } when there is nothing a model can read (or the vision
 * role has no model), { wait: true } while the indexer is about to read it.
 */
async function extractFile(userId, node, { feature, waitForIndex = false, signal } = {}) {
  const cached = cachedExtraction(userId, node);
  if (cached) return cached;
  const input = await readFileInput(userId, node, { waitForIndex });
  if (input.kind === 'wait') return { wait: true };
  if (input.kind === 'none') return { skipped: input.reason };
  const system = extractionInstructions(existingTags(userId));
  let answer;
  if (input.kind === 'text') {
    answer = await ai.chat({
      userId,
      feature,
      role: 'text',
      json: true,
      signal,
      messages: [
        { role: 'system', content: system },
        { role: 'user', content: `${describe(node)}\n\nContent:\n${input.text}` },
      ],
    });
  } else {
    try {
      answer = await ai.vision({ userId, feature, image: input.image, system, prompt: describe(node), json: true, signal });
    } catch (error) {
      if (error.code === 'AI_MODELS_UNAVAILABLE') return { skipped: 'AI_NO_VISION' };
      throw error;
    }
  }
  const fields = normalizeExtraction(answer.data, { text: input.kind === 'text' ? input.text : null, node, exif: input.exif });
  saveExtraction(userId, node, fields, answer.model);
  return { fields, model: answer.model };
}

module.exports = { acceptedTags, extractFile, saveTags };
