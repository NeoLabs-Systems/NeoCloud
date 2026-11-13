'use strict';

// What one job does for one file: the shared extraction, then a rename, tag
// and move suggestion as asked and switched on, then auto-apply when the
// person wants it. Renames need a meaningless name (classifyName) unless the
// person asked for this file; camera photos get tags (and their caption in
// the metadata) but no new name unless asked. File types the person left
// out of AI are skipped before anything is read. A move into a new folder
// is held until enough files of the job would go there (new_folders.js);
// nothing is created or moved here.

const db = require('../../../db/database');
const { readSettings } = require('../../settings/store');
const { assertAiAllowedForNode } = require('../exclusions');
const { freeName } = require('../../files/operations');
const { getLiveNode } = require('../../files/tree');
const { acceptedTags, extractFile } = require('./extraction');
const { aiFeatures, isTypeSkipped } = require('./features');
const { classifyName, renderName, templateFields } = require('./naming');
const { decideMove } = require('./sorting');
const { holdNewFolderMove } = require('./new_folders');
const { autoApply } = require('./review');
const { createSuggestion } = require('./suggestions');
const { folderPath } = require('./targets');

// Template fields whose confidence bounds a name's.
const CONFIDENCE_OF = { date: 'date', year: 'date', month: 'date', issuer: 'issuer', subject: 'subject', type: 'type', amount: 'amount', currency: 'amount' };

function nameConfidence(template, fields) {
  const used = templateFields(template)
    .filter((field) => CONFIDENCE_OF[field])
    .filter((field) => {
      const value = field === 'year' || field === 'month' ? fields.date : fields[field];
      return value !== null && value !== undefined && value !== '';
    })
    .map((field) => fields.confidence[CONFIDENCE_OF[field]]);
  if (used.length === 0) return 0;
  return Math.min(fields.overall, ...used);
}

function renameReason(fields) {
  const what = [fields.type, fields.issuer ? `from ${fields.issuer}` : null].filter(Boolean).join(' ');
  const when = fields.dateSource ? `date from ${fields.dateSource}` : null;
  return [what, when].filter(Boolean).join(', ');
}

function renameSuggestion(userId, node, fields, model) {
  const template = readSettings(userId).ai_naming_template;
  const rendered = renderName(template, fields, node.name);
  if (!rendered || rendered === node.name) return null;
  const name = freeName(node.parent_id, rendered, node.id);
  return {
    kind: 'rename',
    from: node.name,
    to: name,
    reason: renameReason(fields),
    confidence: nameConfidence(template, fields),
    model,
    signal: { type: fields.type, issuer: fields.issuer },
  };
}

function tagSuggestion(userId, node, fields, model, allowNew, knownTags) {
  const current = acceptedTags(node.id);
  const offered = (fields.tags || []).filter((tag) => allowNew || knownTags.has(tag));
  const merged = [...new Set([...current, ...offered])];
  if (merged.length === current.length) return null;
  return {
    kind: 'tags',
    from: current,
    to: merged,
    reason: fields.type ? `${fields.type}` : '',
    confidence: Math.min(fields.confidence.tags, fields.overall),
    model,
    signal: { type: fields.type, issuer: fields.issuer },
  };
}

async function moveSuggestion(userId, node, fields, allowNewFolders, signal) {
  const decision = await decideMove(userId, node, fields, { allowNewFolders, signal });
  if (!decision) return null;
  const from = { id: node.parent_id, path: folderPath(userId, node.parent_id) };
  return {
    kind: 'move',
    from,
    to: decision.to,
    reason: decision.reason,
    confidence: decision.confidence,
    model: decision.model,
    source: decision.source,
    ruleId: decision.ruleId,
    signal: { type: fields.type, issuer: fields.issuer },
  };
}

function knownTagsOf(userId) {
  return new Set(db.prepare(`
    SELECT DISTINCT j.value AS tag FROM ai_node_meta m, json_each(m.tags_json) j WHERE m.user_id = ?
  `).all(userId).map((row) => row.tag));
}

/**
 * Suggestions for one of the owner's files. `kinds`: names, folders, tags;
 * `batchKey` groups new-folder votes (the job's batch, or uploads).
 * Returns { created: [rows] }, { wait: true } (the indexer reads it soon) or
 * { skipped: code }. Throws the guard's errors for vault, excluded or
 * missing files and the facade's for AI that can't run.
 */
async function suggestForFile(userId, nodeId, { kinds, explicit = false, waitForIndex = false, batchKey = null, signal } = {}) {
  assertAiAllowedForNode(userId, nodeId);
  const node = getLiveNode(userId, nodeId);
  if (node.type !== 'file') return { skipped: 'NOT_A_FILE' };
  if (isTypeSkipped(userId, node.name)) return { skipped: 'AI_TYPE_SKIPPED' };
  const features = aiFeatures(userId);
  const wanted = kinds.filter((kind) => features[kind]);
  if (wanted.length === 0) return { skipped: 'AI_FEATURE_OFF' };
  const nameClass = classifyName(node.name);
  const wantName = wanted.includes('names') && (explicit || nameClass.meaningless);
  if (!wantName && !wanted.includes('tags') && !wanted.includes('folders')) return { created: [] };

  let feature = 'sort';
  if (wantName) feature = 'title';
  else if (wanted.includes('tags')) feature = 'tags';
  const extraction = await extractFile(userId, node, { feature, waitForIndex, signal });
  if (extraction.wait || extraction.skipped) return extraction;
  const { fields, model } = extraction;

  const drafts = [];
  const cameraPhoto = fields.camera && nameClass.camera;
  if (wantName && (explicit || !cameraPhoto)) drafts.push(renameSuggestion(userId, node, fields, model));
  if (wanted.includes('tags')) drafts.push(tagSuggestion(userId, node, fields, model, features.newTags, knownTagsOf(userId)));
  if (wanted.includes('folders')) drafts.push(await moveSuggestion(userId, node, fields, features.newFolders, signal));

  // The file may have changed while the model was thinking.
  const current = getLiveNode(userId, node.id);
  if (current.etag !== node.etag || current.name !== node.name || current.parent_id !== node.parent_id) {
    return { skipped: 'CHANGED' };
  }
  const ready = [];
  for (const draft of drafts.filter(Boolean)) {
    if (draft.kind === 'move' && draft.to.id === null) ready.push(...holdNewFolderMove(userId, node, draft, { batchKey }));
    else ready.push({ node, draft });
  }
  const created = ready.map((entry) => createSuggestion(userId, entry.node, entry.draft)).filter(Boolean);
  // Auto-apply never creates a folder: those moves are never high-tier (suggestions.js).
  if (features.autoApplyHighConfidence) autoApply(userId, created);
  return { created };
}

module.exports = { suggestForFile };
