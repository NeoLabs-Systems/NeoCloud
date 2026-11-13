'use strict';

// A summary of one file on demand, from the smart role (images: the vision
// role). Kept per content (etag), encrypted under the owner's key like the
// search text, and labelled with the model that wrote it; asking again
// returns the kept one unless `refresh` is set.

const db = require('../../../db/database');
const ai = require('../client');
const { getUserDek, openBytes, sealBytes } = require('../../crypto/keys');
const { httpError } = require('../../../utils/http_error');
const { getLiveNode } = require('../../files/tree');
const { assertAiAllowedForNode } = require('../exclusions');
const { assertAiUsable } = require('../policy');
const { assertFeature, assertTypeAllowed } = require('./features');
const { readFileInput } = require('./content');
const { SUMMARY_INSTRUCTIONS: INSTRUCTIONS } = require('./prompts');

const MAX_SUMMARY_CHARS = 4000;
function summaryAad(nodeId) {
  return `ai-summary:${nodeId}`;
}

function serialize(row, text, cached) {
  return { summary: text, model: row.model, etag: row.etag, createdAt: row.created_at, cached };
}

function keptSummary(userId, node) {
  const row = db.prepare('SELECT * FROM ai_summaries WHERE node_id = ? AND user_id = ? AND etag = ?').get(node.id, userId, node.etag);
  if (!row) return null;
  return serialize(row, openBytes(getUserDek(userId), row.summary_enc, summaryAad(node.id)).toString('utf8'), true);
}

/** The summary of one of the owner's files: `{ summary, model, etag, createdAt, cached }`. */
async function summarizeFile(userId, fileId, { refresh = false } = {}) {
  assertAiUsable(userId);
  assertFeature(userId, 'summaries');
  const node = getLiveNode(userId, fileId, { type: 'file' });
  assertAiAllowedForNode(userId, node.id);
  assertTypeAllowed(userId, node.name);
  if (refresh !== true) {
    const kept = keptSummary(userId, node);
    if (kept) return kept;
  }
  const input = await readFileInput(userId, node);
  if (input.kind !== 'text' && input.kind !== 'image') {
    throw httpError(422, 'There is no text in this file to summarize.', 'AI_NO_CONTENT');
  }
  const answer = input.kind === 'text'
    ? await ai.chat({
      userId,
      feature: 'summary',
      role: 'smart',
      messages: [
        { role: 'system', content: INSTRUCTIONS },
        { role: 'user', content: `File name: ${node.name}\n\nContent:\n${input.text}` },
      ],
    })
    : await ai.vision({ userId, feature: 'summary', image: input.image, system: INSTRUCTIONS, prompt: `File name: ${node.name}` });
  const text = String(answer.content || '').trim().slice(0, MAX_SUMMARY_CHARS);
  if (!text) throw httpError(502, 'The AI model returned no summary.', 'AI_PROVIDER_ERROR');
  const sealed = sealBytes(getUserDek(userId), Buffer.from(text, 'utf8'), summaryAad(node.id));
  const row = db.prepare(`
    INSERT INTO ai_summaries (node_id, user_id, etag, summary_enc, model, created_at) VALUES (?, ?, ?, ?, ?, datetime('now'))
    ON CONFLICT(node_id) DO UPDATE SET etag = excluded.etag, summary_enc = excluded.summary_enc,
      model = excluded.model, created_at = excluded.created_at
    RETURNING *
  `).get(node.id, userId, node.etag, sealed, answer.model);
  return serialize(row, text, false);
}

module.exports = { summarizeFile };
