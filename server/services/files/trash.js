'use strict';

// Deleting moves a node to the trash: only the node itself is marked, its
// descendants go with it implicitly and come back with it on restore.
// Removing from the trash deletes the whole subtree, every kept version, and
// any blob nothing else points at.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { deleteBlobFiles } = require('../storage/blob_store');
const { releaseBlobs } = require('../storage/blob_refs');
const { announceChanges, recordChange } = require('./changes');
const { freeName, touchFolder } = require('./operations');
const { getStorageConfig } = require('../../config/storage');
const {
  NODE_COLUMNS,
  ancestry,
  getLiveNode,
  getRoot,
  nodeById,
  serializeNode,
  withFolderPaths,
} = require('./tree');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Trash');

function trashNode(userId, nodeId) {
  const node = db.transaction(() => {
    const target = getLiveNode(userId, nodeId);
    if (target.parent_id === null) throw httpError(400, 'The root folder can’t be deleted.', 'ROOT_IMMUTABLE');
    db.prepare("UPDATE nodes SET trashed_at = datetime('now') WHERE id = ?").run(target.id);
    recordChange(userId, target.id, 'trash');
    touchFolder(target.parent_id);
    return target;
  })();
  announceChanges(userId, [node.parent_id]);
  return { success: true };
}

function isVaultFolder(node) {
  return Boolean(db.prepare('SELECT 1 FROM vaults WHERE folder_id = ?').get(node.id));
}

function getTrashedNode(userId, nodeId) {
  const node = db.prepare(`
    SELECT ${NODE_COLUMNS} FROM nodes WHERE id = ? AND user_id = ? AND trashed_at IS NOT NULL
  `).get(String(nodeId || ''), userId);
  if (!node) throw httpError(404, 'Not in the trash', 'NOT_FOUND');
  return node;
}

/**
 * The account's trashed nodes, newest first. `originalPath` is the folder
 * each one came from; `expiresAt` is when the retention policy removes it
 * (null when trash is kept until emptied by hand).
 */
function listTrash(userId) {
  const { trashRetentionDays } = getStorageConfig();
  const keepFor = trashRetentionDays > 0 ? `+${Math.floor(trashRetentionDays)} days` : null;
  const rows = db.prepare(`
    SELECT ${NODE_COLUMNS}, CASE WHEN @keepFor IS NULL THEN NULL ELSE datetime(trashed_at, @keepFor) END AS expires_at
    FROM nodes WHERE user_id = @userId AND trashed_at IS NOT NULL ORDER BY trashed_at DESC
  `).all({ userId, keepFor });
  const items = withFolderPaths(userId, rows.map((row) => ({ ...serializeNode(row), expiresAt: row.expires_at })));
  return { items: items.map(({ path, ...item }) => ({ ...item, originalPath: path })) };
}

/**
 * Puts a node back where it was. If that folder is gone or itself in the
 * trash, it lands in the root; a name now taken gets "(1)" appended.
 */
function restoreNode(userId, nodeId) {
  const node = db.transaction(() => {
    const target = getTrashedNode(userId, nodeId);
    const parentChain = ancestry(userId, target.parent_id);
    const parentLive = parentChain.length > 0 && parentChain.every((n) => !n.trashed_at);
    // Ciphertext can't land in the root: restore the vault folder first.
    if (!parentLive && target.parent_id && target.vault_id && !isVaultFolder(target)) {
      throw httpError(409, 'Restore the vault folder first.', 'VAULT_BOUNDARY');
    }
    const parentId = parentLive ? target.parent_id : getRoot(userId).id;
    const name = freeName(parentId, target.name, target.id);
    db.prepare('UPDATE nodes SET trashed_at = NULL, parent_id = ?, name = ? WHERE id = ?').run(parentId, name, target.id);
    recordChange(userId, target.id, 'restore');
    touchFolder(parentId);
    return nodeById(target.id);
  })();
  announceChanges(userId, [node.parent_id]);
  return serializeNode(node);
}

// Inside a transaction: deletes a node and everything below it. Returns the
// blob ids that became unreferenced.
function purgeSubtree(userId, nodeId) {
  const subtree = db.prepare(`
    WITH RECURSIVE down(id) AS (
      SELECT id FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL SELECT n.id FROM nodes n JOIN down ON n.parent_id = down.id
    )
    SELECT nodes.id, nodes.blob_id FROM nodes JOIN down ON down.id = nodes.id
  `).all(nodeId, userId);
  const blobIds = subtree.filter((row) => row.blob_id).map((row) => row.blob_id);
  const versionBlobs = db.prepare(`
    SELECT blob_id FROM file_versions WHERE node_id IN (SELECT value FROM json_each(?))
  `).all(JSON.stringify(subtree.map((row) => row.id)));
  db.prepare('DELETE FROM nodes WHERE id = ?').run(nodeId);
  recordChange(userId, nodeId, 'delete');
  return releaseBlobs([...blobIds, ...versionBlobs.map((row) => row.blob_id)]);
}

async function purgeNode(userId, nodeId) {
  const orphaned = db.transaction(() => purgeSubtree(userId, getTrashedNode(userId, nodeId).id))();
  await deleteBlobFiles(userId, orphaned);
  return { success: true };
}

async function emptyTrash(userId) {
  const ids = db.prepare('SELECT id FROM nodes WHERE user_id = ? AND trashed_at IS NOT NULL').all(userId);
  const orphaned = db.transaction(() => {
    const blobs = [];
    for (const { id } of ids) {
      // A node inside a folder purged earlier in this loop is already gone.
      if (db.prepare('SELECT 1 FROM nodes WHERE id = ?').get(id)) blobs.push(...purgeSubtree(userId, id));
    }
    return blobs;
  })();
  await deleteBlobFiles(userId, orphaned);
  return { success: true, removed: ids.length };
}

/** Removes everything trashed more than `days` days ago, for every account. */
async function purgeExpiredTrash(days) {
  if (!(days > 0)) return 0;
  const rows = db.prepare(`
    SELECT id, user_id FROM nodes
    WHERE trashed_at IS NOT NULL AND datetime(trashed_at) <= datetime('now', ?)
  `).all(`-${Math.floor(days)} days`);
  for (const row of rows) {
    try {
      if (!db.prepare('SELECT 1 FROM nodes WHERE id = ?').get(row.id)) continue;
      const orphaned = db.transaction(() => purgeSubtree(row.user_id, row.id))();
      await deleteBlobFiles(row.user_id, orphaned);
    } catch (err) {
      logger.error(`Could not purge trashed node of account ${row.user_id}:`, err.message);
    }
  }
  return rows.length;
}

module.exports = {
  emptyTrash,
  listTrash,
  purgeExpiredTrash,
  purgeNode,
  purgeSubtree,
  restoreNode,
  trashNode,
};
