'use strict';

// What AI may never read: vault files and folders (end-to-end encrypted; the
// server could not read them anyway), and anything inside a folder its owner
// marked "Exclude from AI" (subfolders included). Every AI feature checks a
// node here before its content goes anywhere, the way the search indexer
// skips vaults.

const db = require('../../db/database');
const { getLiveNode, serializeNode } = require('../files/tree');
const { httpError } = require('../../utils/http_error');

/** The node and its ancestors with the columns that decide AI access, or []. */
function aiChain(userId, nodeId) {
  return db.prepare(`
    WITH RECURSIVE up(id, parent_id, trashed_at, vault_id, ai_excluded) AS (
      SELECT id, parent_id, trashed_at, vault_id, ai_excluded FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL
      SELECT n.id, n.parent_id, n.trashed_at, n.vault_id, n.ai_excluded
      FROM nodes n JOIN up ON n.id = up.parent_id
    )
    SELECT * FROM up
  `).all(String(nodeId || ''), userId);
}

/**
 * Throws unless the owner's node may be sent to an AI provider: 404 for a node
 * that isn't the account's (or is in the trash), 403 AI_VAULT for vault
 * content, 403 AI_EXCLUDED inside an excluded folder. Shared items are
 * checked with the owner's id, never the recipient's.
 */
function assertAiAllowedForNode(userId, nodeId) {
  const chain = aiChain(userId, nodeId);
  if (chain.length === 0 || chain.some((node) => node.trashed_at)) {
    throw httpError(404, 'File or folder not found', 'NOT_FOUND');
  }
  if (chain.some((node) => node.vault_id)) {
    throw httpError(403, 'Vault files are never processed by AI.', 'AI_VAULT');
  }
  if (chain.some((node) => node.ai_excluded === 1)) {
    throw httpError(403, 'This item is in a folder excluded from AI.', 'AI_EXCLUDED');
  }
}

/** Marks or unmarks one of the owner's folders as excluded from AI. */
function setFolderAiExcluded(userId, folderId, excluded) {
  if (typeof excluded !== 'boolean') throw httpError(400, 'excluded must be true or false.', 'INVALID_VALUE');
  const folder = getLiveNode(userId, folderId, { type: 'folder' });
  if (folder.parent_id === null) throw httpError(400, 'Turn AI off in Settings to exclude everything.', 'ROOT_FOLDER');
  db.prepare('UPDATE nodes SET ai_excluded = ? WHERE id = ? AND user_id = ?').run(excluded ? 1 : 0, folder.id, userId);
  return { node: serializeNode(getLiveNode(userId, folder.id)) };
}

module.exports = { assertAiAllowedForNode, setFolderAiExcluded };
