'use strict';

// Where AI may suggest moving a file: one of the owner's own live folders,
// never in a vault or inside a folder excluded from AI, and never across the
// border of a shared folder (out of one, into one, or between two): a move
// suggestion must not change who can see the file. "Shared" is a folder with
// a grant to another account or a share link.

const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { ancestry, getLiveNode } = require('../../files/tree');
const { assertAiAllowedForNode } = require('../exclusions');

const MAX_CANDIDATE_FOLDERS = 1000;
const MAX_DEPTH = 64;

const SHARED_SQL = `(EXISTS (SELECT 1 FROM user_shares s WHERE s.node_id = n.id)
  OR EXISTS (SELECT 1 FROM shares l WHERE l.node_id = n.id AND l.revoked_at IS NULL))`;

/** The shared folders above (and including) a folder, root first, as one key. */
function shareScope(userId, folderId) {
  const ids = ancestry(userId, folderId).map((entry) => entry.id).reverse();
  if (ids.length === 0) return '';
  const shared = new Set(db.prepare(`
    SELECT n.id FROM nodes n WHERE n.id IN (SELECT value FROM json_each(?)) AND ${SHARED_SQL}
  `).all(JSON.stringify(ids)).map((row) => row.id));
  return ids.filter((id) => shared.has(id)).join(',');
}

/**
 * Every folder a file in `fromFolderId` may be moved to: { id, path, name,
 * parentId }, root first, at most 1000. `path` is "/" for the root.
 */
function candidateFolders(userId, fromFolderId) {
  const scope = shareScope(userId, fromFolderId);
  const rows = db.prepare(`
    WITH RECURSIVE down(id, parent_id, name, path, scope, depth) AS (
      SELECT n.id, n.parent_id, n.name, '', CASE WHEN ${SHARED_SQL} THEN n.id ELSE '' END, 0
      FROM nodes n WHERE n.user_id = @userId AND n.parent_id IS NULL
      UNION ALL
      SELECT n.id, n.parent_id, n.name, down.path || '/' || n.name,
             CASE WHEN ${SHARED_SQL} THEN (CASE WHEN down.scope = '' THEN n.id ELSE down.scope || ',' || n.id END)
                  ELSE down.scope END,
             down.depth + 1
      FROM nodes n JOIN down ON n.parent_id = down.id
      WHERE n.type = 'folder' AND n.trashed_at IS NULL AND n.vault_id IS NULL AND n.ai_excluded = 0
        AND down.depth < @maxDepth
    )
    SELECT id, parent_id, name, path, scope FROM down ORDER BY depth, lower(path) LIMIT @limit
  `).all({ userId, maxDepth: MAX_DEPTH, limit: MAX_CANDIDATE_FOLDERS });
  return rows
    .filter((row) => row.scope === scope)
    .map((row) => ({ id: row.id, parentId: row.parent_id, name: row.name, path: row.path || '/' }));
}

function notAllowed() {
  return httpError(409, 'AI can’t move the file there.', 'AI_TARGET_NOT_ALLOWED');
}

/** A live folder of the owner that AI may file into (no vault, not excluded), else throws. */
function assertFilingFolder(userId, folderId) {
  const folder = getLiveNode(userId, folderId, { type: 'folder' });
  try {
    assertAiAllowedForNode(userId, folder.id);
  } catch (error) {
    if (error.status === 403) throw notAllowed();
    throw error;
  }
  return folder;
}

/**
 * Throws unless `node` (the owner's file) may move to `folderId`: a live
 * folder of the owner, readable by AI (no vault, not excluded), within the
 * same shared folders. Returns the folder row.
 */
function assertMoveTarget(userId, node, folderId) {
  const folder = assertFilingFolder(userId, folderId);
  if (shareScope(userId, folder.id) !== shareScope(userId, node.parent_id)) {
    throw httpError(409, 'Items can’t be moved across the border of a shared folder.', 'SHARE_BOUNDARY');
  }
  return folder;
}

/** The folder path ("/Finance/Telekom", "/" for the root) of a folder. */
function folderPath(userId, folderId) {
  const names = ancestry(userId, folderId).reverse().slice(1).map((entry) => entry.name);
  return `/${names.join('/')}`;
}

module.exports = { assertFilingFolder, assertMoveTarget, candidateFolders, folderPath };
