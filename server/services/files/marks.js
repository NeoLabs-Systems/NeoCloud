'use strict';

// What an account remembers about its own nodes: favorites, and when it last
// opened a file (for Quick Access). Both rows go with the node when it is
// deleted for good; a trashed node keeps them and gets them back on restore.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { LIVE_NODES_CTE, NODE_COLUMNS, getLiveNode, nodeById, serializeNode, withFolderPaths } = require('./tree');

function liveNonRoot(userId, nodeId) {
  const node = getLiveNode(userId, nodeId);
  if (node.parent_id === null) throw httpError(400, 'The root folder can’t be marked.', 'ROOT_IMMUTABLE');
  return node;
}

function setFavorite(userId, nodeId) {
  const node = liveNonRoot(userId, nodeId);
  db.prepare(`
    INSERT OR IGNORE INTO favorites (user_id, node_id, created_at) VALUES (?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
  `).run(userId, node.id);
  return serializeNode(nodeById(node.id));
}

/** Also works for a node in the trash; another account's node is missing. */
function clearFavorite(userId, nodeId) {
  const node = db.prepare('SELECT id FROM nodes WHERE id = ? AND user_id = ?').get(String(nodeId || ''), userId);
  if (!node) throw httpError(404, 'File or folder not found', 'NOT_FOUND');
  db.prepare('DELETE FROM favorites WHERE user_id = ? AND node_id = ?').run(userId, node.id);
  return serializeNode(nodeById(node.id));
}

/**
 * The account's live favorites, most recently marked first, with their
 * folder `path`. Nodes inside a vault are left out (their names are
 * ciphertext); a vault's own folder is listed.
 */
function listFavorites(userId) {
  const rows = db.prepare(`
    ${LIVE_NODES_CTE}
    SELECT ${NODE_COLUMNS}, fav.created_at AS favorited_at
    FROM favorites fav JOIN nodes ON nodes.id = fav.node_id
    WHERE fav.user_id = @userId
      AND nodes.id NOT IN (SELECT id FROM hidden)
      AND (nodes.vault_id IS NULL OR EXISTS (SELECT 1 FROM vaults v WHERE v.folder_id = nodes.id))
    ORDER BY fav.created_at DESC, lower(nodes.name)
  `).all({ userId });
  const items = rows.map((row) => ({ ...serializeNode(row), favoritedAt: row.favorited_at }));
  return { items: withFolderPaths(userId, items) };
}

/** Notes that the account opened a node now (millisecond precision, for ordering). */
function recordOpen(userId, nodeId) {
  const node = liveNonRoot(userId, nodeId);
  db.prepare(`
    INSERT INTO node_opens (user_id, node_id, last_opened_at) VALUES (?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))
    ON CONFLICT (user_id, node_id) DO UPDATE SET
      last_opened_at = excluded.last_opened_at,
      open_count = open_count + 1
  `).run(userId, node.id);
}

module.exports = { clearFavorite, listFavorites, recordOpen, setFavorite };
