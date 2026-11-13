'use strict';

// Removing empty folders AI left behind. Two lists, each shown to the
// person with names before anything happens:
//   - aiCreated: empty folders made by accepting an AI move into a new
//     folder (nodes.created_by_ai), anywhere in the tree;
//   - recent: other empty folders at the top level made in the last seven
//     days. Folders AI made before created_by_ai existed can't be told
//     apart from the person's own, so these are offered, never removed
//     unasked.
// Only empty folders qualify (nothing live inside), never a vault, never a
// shared folder. Removing moves them to the trash, so it can be undone.

const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { getRoot } = require('../../files/tree');
const { trashNode } = require('../../files/trash');
const { folderPath } = require('./targets');

const RECENT_DAYS = 7;
const MAX_LISTED = 500;

const EMPTY_FOLDER_SQL = `
  f.user_id = @userId AND f.type = 'folder' AND f.trashed_at IS NULL AND f.parent_id IS NOT NULL
  AND f.vault_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM vaults v WHERE v.folder_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM nodes c WHERE c.parent_id = f.id AND c.trashed_at IS NULL)
  AND NOT EXISTS (SELECT 1 FROM user_shares s WHERE s.node_id = f.id)
  AND NOT EXISTS (SELECT 1 FROM shares l WHERE l.node_id = f.id AND l.revoked_at IS NULL)
`;

function aiCreatedRows(userId) {
  return db.prepare(`
    SELECT f.id, f.name, f.created_at FROM nodes f
    WHERE ${EMPTY_FOLDER_SQL} AND f.created_by_ai = 1
    ORDER BY f.created_at DESC LIMIT @limit
  `).all({ userId, limit: MAX_LISTED });
}

function recentRows(userId) {
  return db.prepare(`
    SELECT f.id, f.name, f.created_at FROM nodes f
    WHERE ${EMPTY_FOLDER_SQL} AND f.created_by_ai = 0 AND f.parent_id = @rootId
      AND f.created_at >= datetime('now', @since)
    ORDER BY f.created_at DESC LIMIT @limit
  `).all({ userId, rootId: getRoot(userId).id, since: `-${RECENT_DAYS} days`, limit: MAX_LISTED });
}

function serialize(userId, row) {
  return { id: row.id, name: row.name, path: folderPath(userId, row.id), createdAt: row.created_at };
}

/** The empty folders that may be removed: { aiCreated: [...], recent: [...] }, each { id, name, path, createdAt }. */
function listEmptyFolders(userId) {
  return {
    aiCreated: aiCreatedRows(userId).map((row) => serialize(userId, row)),
    recent: recentRows(userId).map((row) => serialize(userId, row)),
  };
}

/**
 * Moves the chosen folders to the trash, each only if it is still on one of
 * the two lists (so still empty). Returns { removed, failed: [{ id, code }] }.
 */
function removeEmptyFolders(userId, { ids } = {}) {
  if (!Array.isArray(ids) || ids.length === 0 || ids.length > MAX_LISTED || !ids.every((id) => typeof id === 'string' && id)) {
    throw httpError(400, `ids must be a list of 1–${MAX_LISTED} folder ids.`, 'INVALID_REQUEST');
  }
  let removed = 0;
  const failed = [];
  for (const id of [...new Set(ids)]) {
    // Read again for each: an earlier removal or an upload may have changed things.
    const allowed = new Set([...aiCreatedRows(userId), ...recentRows(userId)].map((row) => row.id));
    if (!allowed.has(id)) {
      failed.push({ id, code: 'NOT_EMPTY_OR_NOT_ALLOWED' });
      continue;
    }
    trashNode(userId, id);
    removed += 1;
  }
  return { removed, failed };
}

module.exports = { listEmptyFolders, removeEmptyFolders };
