'use strict';

// Deciding on suggestions. AI never renames, moves or tags on its own: the
// owner accepts (optionally edited) or rejects, and an accepted one can be
// undone while the file is still as it left it. With "auto-apply" on,
// high-confidence suggestions are applied at once and stay listed as
// `applied`, undoable like any other. Applying goes through the normal file
// operations (journal, live updates, name conflicts).
//
// A move into a folder that doesn't exist yet creates that folder only
// here, when the person accepts it, in the same transaction as the move
// (no folder without its file), and only while "new folders" is on. The
// folder is marked created_by_ai; undoing the move removes it again when it
// is left empty.

const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { validateName } = require('../../../utils/file_names');
const { announceChanges } = require('../../files/changes');
const { getLiveNode, serializeNode } = require('../../files/tree');
const operations = require('../../files/operations');
const { trashNode } = require('../../files/trash');
const { aiFeatures } = require('./features');
const { acceptedTags, saveTags } = require('./extraction');
const { normalizeTag } = require('./fields');
const { assertMoveTarget, folderPath } = require('./targets');
const { recordAcceptedMove } = require('./rules');
const {
  KINDS,
  announceSuggestions,
  isStale,
  listParam,
  parseJson,
  refreshStale,
  suggestionRow,
  withNode,
} = require('./suggestions');

const MAX_TAGS = 20;

// --- Applying ------------------------------------------------------------------------------

function staleError() {
  return httpError(409, 'The file changed since this was suggested.', 'AI_SUGGESTION_STALE');
}

function markStale(row) {
  db.prepare("UPDATE ai_suggestions SET status = 'stale', decided_at = datetime('now') WHERE id = ?").run(row.id);
}

// The file, live and unchanged since the suggestion was made, else stale.
function currentNode(userId, row) {
  let node;
  try {
    node = getLiveNode(userId, row.node_id);
  } catch (error) {
    if (error.status !== 404) throw error;
    node = null;
  }
  if (isStale(row, node)) {
    markStale(row);
    throw staleError();
  }
  return node;
}

function parseTagValue(value) {
  if (!Array.isArray(value)) throw httpError(400, 'value must be a list of tags.', 'INVALID_VALUE');
  return [...new Set(value.map(normalizeTag).filter(Boolean))].slice(0, MAX_TAGS);
}

function applyRename(userId, row, node, value) {
  const name = value === undefined ? parseJson(row.to_json, '') : validateName(value);
  const result = operations.updateNode(userId, node.id, { name, conflict: 'rename' });
  return { to: result.name, applied: { name: result.name, parentId: result.parentId }, node: result };
}

function moveTarget(userId, row, node, value) {
  if (value !== undefined) {
    if (typeof value !== 'string' || !value) throw httpError(400, 'value must be a folder id.', 'INVALID_VALUE');
    return assertMoveTarget(userId, node, value).id;
  }
  return assertMoveTarget(userId, node, parseJson(row.to_json, {}).id).id;
}

// The new folder (or an existing one of that name) and the move, in one
// transaction. updateNode's own transaction runs as a savepoint inside it,
// so a failing move leaves no folder behind.
function moveIntoNewFolder(userId, row, node, to) {
  if (!aiFeatures(userId).newFolders) {
    markStale(row);
    throw httpError(409, 'Suggesting new folders is turned off in Settings › AI.', 'AI_NEW_FOLDERS_OFF');
  }
  assertMoveTarget(userId, node, to.parentId);
  return db.transaction(() => {
    const { node: folder, created } = operations.createFolderInTransaction(userId, to.parentId, to.name, 'replace');
    if (created) db.prepare('UPDATE nodes SET created_by_ai = 1 WHERE id = ?').run(folder.id);
    const result = operations.updateNode(userId, node.id, { parentId: folder.id, conflict: 'rename' });
    return { targetId: folder.id, result };
  })();
}

function applyMove(userId, row, node, value) {
  const to = parseJson(row.to_json, {});
  let targetId;
  let result;
  if (value === undefined && !to.id) {
    ({ targetId, result } = moveIntoNewFolder(userId, row, node, to));
    announceChanges(userId, [to.parentId]);
  } else {
    targetId = moveTarget(userId, row, node, value);
    result = operations.updateNode(userId, node.id, { parentId: targetId, conflict: 'rename' });
  }
  return {
    to: { id: targetId, path: folderPath(userId, targetId) },
    applied: { name: result.name, parentId: result.parentId },
    node: result,
  };
}

function applyTags(userId, row, node, value) {
  const tags = value === undefined ? parseJson(row.to_json, []) : parseTagValue(value);
  saveTags(userId, node, tags);
  announceChanges(userId, [node.parent_id]);
  return { to: tags, applied: { tags }, node: serializeNode(getLiveNode(userId, node.id)) };
}

const APPLY = { rename: applyRename, move: applyMove, tags: applyTags };

/** Applies a pending suggestion (as `accepted` by the owner, or `applied` automatically). */
function applySuggestion(userId, row, { value, status = 'accepted' } = {}) {
  if (row.status !== 'pending') throw httpError(409, `This suggestion is ${row.status}.`, 'AI_SUGGESTION_DECIDED');
  const node = currentNode(userId, row);
  const result = APPLY[row.kind](userId, row, node, value);
  // Only the owner's own accepts teach rule proposals.
  if (row.kind === 'move' && status === 'accepted') recordAcceptedMove(userId, parseJson(row.signal_json, {}), result.to.id);
  const updated = db.prepare(`
    UPDATE ai_suggestions SET status = ?, to_json = ?, applied_json = ?, decided_at = datetime('now')
    WHERE id = ? RETURNING *
  `).get(status, JSON.stringify(result.to), JSON.stringify(result.applied), row.id);
  return { suggestion: updated, node: result.node };
}

/** Accepts one suggestion; `value` replaces its proposal (a name, a folder id, a tag list). */
function acceptSuggestion(userId, id, { value } = {}) {
  const row = suggestionRow(userId, id);
  try {
    const { suggestion } = applySuggestion(userId, row, { value });
    return { suggestion: withNode(userId, suggestion) };
  } finally {
    announceSuggestions(userId);
  }
}

function rejectSuggestion(userId, id) {
  const row = suggestionRow(userId, id);
  if (row.status !== 'pending' && row.status !== 'stale') {
    throw httpError(409, `This suggestion is ${row.status}.`, 'AI_SUGGESTION_DECIDED');
  }
  const updated = db.prepare(`
    UPDATE ai_suggestions SET status = 'rejected', decided_at = datetime('now') WHERE id = ? RETURNING *
  `).get(row.id);
  announceSuggestions(userId);
  return { suggestion: withNode(userId, updated) };
}

/** Accepts every pending high-tier suggestion (of `kind`, if given). Failures are reported, not thrown. */
function acceptAllHigh(userId, { kind } = {}) {
  const kinds = listParam(kind, KINDS, 'kind') || KINDS;
  refreshStale(userId);
  const rows = db.prepare(`
    SELECT * FROM ai_suggestions
    WHERE user_id = ? AND status = 'pending' AND tier = 'high' AND kind IN (SELECT value FROM json_each(?))
    ORDER BY created_at, CASE kind WHEN 'rename' THEN 0 WHEN 'tags' THEN 1 ELSE 2 END
  `).all(userId, JSON.stringify(kinds));
  let accepted = 0;
  const failed = [];
  for (const row of rows) {
    try {
      // A rename or move accepted just before may have changed this file.
      applySuggestion(userId, suggestionRow(userId, row.id));
      accepted += 1;
    } catch (error) {
      failed.push({ id: row.id, code: error.code || 'FAILED' });
    }
  }
  announceSuggestions(userId);
  return { accepted, failed };
}

/**
 * Accepts or rejects many suggestions at once (the Review tab's "Accept
 * all" / "Reject all"), one by one through the same steps as a single
 * decision. Failures are reported, not thrown.
 */
function bulkDecide(userId, { ids, action } = {}) {
  if (action !== 'accept' && action !== 'reject') throw httpError(400, 'action must be accept or reject.', 'INVALID_ACTION');
  if (!Array.isArray(ids) || ids.length === 0 || ids.length > 1000 || !ids.every((id) => typeof id === 'string' && id)) {
    throw httpError(400, 'ids must be a list of 1–1000 suggestion ids.', 'INVALID_REQUEST');
  }
  let done = 0;
  const failed = [];
  for (const id of [...new Set(ids)]) {
    try {
      const row = suggestionRow(userId, id);
      if (action === 'accept') {
        applySuggestion(userId, row);
      } else {
        if (row.status !== 'pending' && row.status !== 'stale') {
          throw httpError(409, `This suggestion is ${row.status}.`, 'AI_SUGGESTION_DECIDED');
        }
        db.prepare("UPDATE ai_suggestions SET status = 'rejected', decided_at = datetime('now') WHERE id = ?").run(row.id);
      }
      done += 1;
    } catch (error) {
      failed.push({ id, code: error.code || 'FAILED' });
    }
  }
  announceSuggestions(userId);
  return { done, failed };
}

/** Applies a job's new high-tier suggestions when the person turned auto-apply on. */
function autoApply(userId, rows) {
  for (const row of rows) {
    if (row.tier !== 'high') continue;
    try {
      applySuggestion(userId, suggestionRow(userId, row.id), { status: 'applied' });
    } catch {
      // It stays pending for review.
    }
  }
}

// --- Undo ------------------------------------------------------------------------------------

function undoConflict() {
  return httpError(409, 'The file changed since; it can’t be put back automatically.', 'AI_UNDO_CONFLICT');
}

// A folder AI made for a move that was just undone goes to the trash once nothing is left in it.
function removeIfEmptyAiFolder(userId, folderId) {
  const empty = db.prepare(`
    SELECT 1 FROM nodes f WHERE f.id = ? AND f.user_id = ? AND f.created_by_ai = 1 AND f.trashed_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM nodes c WHERE c.parent_id = f.id AND c.trashed_at IS NULL)
  `).get(folderId, userId);
  if (empty) trashNode(userId, folderId);
}

/**
 * Puts an accepted or auto-applied suggestion back: the old name, folder or
 * tags, as long as the file is still as the suggestion left it (else 409
 * AI_UNDO_CONFLICT).
 */
function undoSuggestion(userId, id) {
  const row = suggestionRow(userId, id);
  if (row.status !== 'accepted' && row.status !== 'applied') {
    throw httpError(409, `This suggestion is ${row.status}.`, 'AI_NOT_UNDOABLE');
  }
  const applied = parseJson(row.applied_json, {});
  const from = parseJson(row.from_json, null);
  let node;
  try {
    node = getLiveNode(userId, row.node_id);
  } catch (error) {
    if (error.status === 404) throw undoConflict();
    throw error;
  }
  if (row.kind === 'tags') {
    const current = acceptedTags(node.id);
    if (JSON.stringify(current) !== JSON.stringify(applied.tags)) throw undoConflict();
    saveTags(userId, node, Array.isArray(from) ? from : []);
    announceChanges(userId, [node.parent_id]);
  } else {
    if (node.name !== applied.name || node.parent_id !== applied.parentId) throw undoConflict();
    if (row.kind === 'rename') {
      operations.updateNode(userId, node.id, { name: from, conflict: 'fail' });
    } else {
      operations.updateNode(userId, node.id, { parentId: from.id, name: row.basis_name, conflict: 'fail' });
      if (row.status === 'accepted') recordAcceptedMove(userId, parseJson(row.signal_json, {}), applied.parentId, -1);
      removeIfEmptyAiFolder(userId, applied.parentId);
    }
  }
  const updated = db.prepare(`
    UPDATE ai_suggestions SET status = 'undone', decided_at = datetime('now') WHERE id = ? RETURNING *
  `).get(row.id);
  announceSuggestions(userId);
  return { suggestion: withNode(userId, updated) };
}

module.exports = { acceptAllHigh, acceptSuggestion, autoApply, bulkDecide, rejectSuggestion, undoSuggestion };
