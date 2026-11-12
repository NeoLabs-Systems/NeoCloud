'use strict';

// The file tree for an account that may also be a member of other
// accounts' shared folders. A node of the caller's own goes to the
// account-scoped services exactly as before. A shared node is resolved
// through access.js and the same services then run as its owner: writes
// land in the owner's tree, on the owner's quota, under the owner's key.
// What a member may not do (re-share, versions, vaults, public links,
// favorites) stays behind the account-scoped services, which answer 404.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const operations = require('../files/operations');
const trash = require('../files/trash');
const tree = require('../files/tree');
const { copyAcrossAccounts } = require('../files/cross_copy');
const { memberPath, memberView, resolveAccess, shareBoundary } = require('./access');

function shareRootImmutable() {
  return httpError(403, 'The shared folder itself belongs to its owner.', 'SHARE_ROOT_IMMUTABLE');
}

// A member's write must stay inside one shared folder they can write to.
function assertSameShare(source, target) {
  if (!target.shared || target.ownerId !== source.ownerId || target.writeRootId !== source.writeRootId) {
    throw shareBoundary();
  }
}

// Re-reads a node after a change and shows it the way a member sees it.
function memberResult(userId, nodeId) {
  const access = resolveAccess(userId, nodeId);
  return memberView(access.node, access);
}

function listChildren(userId, folderId, options) {
  const access = resolveAccess(userId, folderId, { type: 'folder' });
  if (!access.shared) return tree.listChildren(userId, folderId, options);
  const result = tree.listChildren(access.ownerId, access.node.id, options);
  // A vault inside a shared folder stays the owner's alone.
  const { vaults } = db.prepare(`
    SELECT COUNT(*) AS vaults FROM nodes WHERE parent_id = ? AND trashed_at IS NULL AND vault_id IS NOT NULL
  `).get(access.node.id);
  return {
    ...result,
    folder: memberView(access.node, access),
    path: memberPath(access),
    items: result.items.filter((item) => !item.vaultId).map(({ shared, ...item }) => ({ ...item, favorite: false })),
    total: result.total - vaults,
  };
}

function getNode(userId, nodeId) {
  const access = resolveAccess(userId, nodeId);
  if (!access.shared) {
    const node = tree.getLiveNode(userId, nodeId);
    return { node: tree.serializeNode(node), path: tree.breadcrumbs(userId, node.id) };
  }
  return { node: memberView(access.node, access), path: memberPath(access) };
}

/** { parentId, name } or { parentId, path } as in POST /api/folders. */
function createFolder(userId, { parentId, name, path, conflict }) {
  const access = resolveAccess(userId, parentId, { need: 'write', type: 'folder' });
  const actor = access.ownerId;
  const target = access.shared ? access.node.id : parentId;
  const node = path
    ? operations.ensureFolderPath(actor, { parentId: target, path })
    : operations.createFolder(actor, { parentId: target, name, conflict });
  return access.shared ? memberResult(userId, node.id) : node;
}

/** Rename and/or move, inside the caller's own tree or inside one share. */
function updateNode(userId, nodeId, { name, parentId, conflict }) {
  const access = resolveAccess(userId, nodeId, { need: 'write' });
  const moving = parentId !== undefined && parentId !== null;
  const target = moving ? resolveAccess(userId, parentId, { need: 'write', type: 'folder' }) : null;
  if (!access.shared) {
    if (target?.shared) throw shareBoundary();
    return operations.updateNode(userId, nodeId, { name, parentId, conflict });
  }
  if (access.node.id === access.writeRootId) throw shareRootImmutable();
  if (target) assertSameShare(access, target);
  const node = operations.updateNode(access.ownerId, access.node.id, { name, parentId, conflict });
  return memberResult(userId, node.id);
}

/**
 * Copies within one account's tree (sharing blobs, as before) or, when
 * source and target belong to different accounts, as a real copy charged to
 * the account that receives it.
 */
async function copyNode(userId, nodeId, { parentId, name, conflict }) {
  const source = resolveAccess(userId, nodeId);
  const targetId = parentId ?? (source.shared ? source.node.parent_id : null);
  const target = targetId === null
    ? { ownerId: userId, shared: false }
    : resolveAccess(userId, targetId, { need: 'write', type: 'folder' });
  if (!source.shared && !target.shared) return operations.copyNode(userId, nodeId, { parentId, name, conflict });

  const targetNodeId = target.shared ? target.node.id : targetId;
  const sourceNodeId = source.shared ? source.node.id : nodeId;
  if (source.ownerId === target.ownerId) {
    const copy = operations.copyNode(source.ownerId, sourceNodeId, { parentId: targetNodeId, name, conflict });
    return target.shared ? memberResult(userId, copy.id) : copy;
  }
  const copy = await copyAcrossAccounts({
    fromUserId: source.ownerId,
    sourceId: sourceNodeId,
    toUserId: target.ownerId,
    targetId: targetNodeId,
    name,
    conflict,
  });
  return target.shared ? memberResult(userId, copy.id) : copy;
}

function trashNode(userId, nodeId) {
  const access = resolveAccess(userId, nodeId, { need: 'write' });
  if (!access.shared) return trash.trashNode(userId, nodeId);
  if (access.node.id === access.writeRootId) throw shareRootImmutable();
  return trash.trashNode(access.ownerId, access.node.id);
}

/**
 * Restores from the trash. A member may restore what was trashed inside a
 * folder shared with them for writing, back into that folder only.
 */
function restoreNode(userId, nodeId) {
  const row = db.prepare('SELECT user_id, parent_id, trashed_at, vault_id FROM nodes WHERE id = ?').get(String(nodeId || ''));
  if (!row || row.user_id === userId) return trash.restoreNode(userId, nodeId);
  if (!row.trashed_at || row.vault_id || !row.parent_id) throw httpError(404, 'Not in the trash', 'NOT_FOUND');
  // Someone else's node: its parent is theirs too, so this resolves through a share or 404s.
  resolveAccess(userId, row.parent_id, { need: 'write', type: 'folder' });
  const node = trash.restoreNode(row.user_id, String(nodeId));
  return memberResult(userId, node.id);
}

module.exports = { copyNode, createFolder, getNode, listChildren, restoreNode, trashNode, updateNode };
