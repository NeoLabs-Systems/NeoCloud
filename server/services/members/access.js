'use strict';

// Who may reach a node: its owner, or an account it (or a folder above it)
// is shared with. This is the one place that answers that question for the
// file routes; every route that takes a node id from a member goes through
// resolveAccess. A node someone else owns and hasn't shared with the caller
// behaves exactly like a missing one (404), and vault nodes are never
// reachable through a share.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { ROOT_ALIAS, ancestry, nodeById, serializeNode } = require('../files/tree');

const PERMISSION_RANK = { read: 1, write: 2 };

function notFound() {
  return httpError(404, 'File or folder not found', 'NOT_FOUND');
}

function readOnly() {
  return httpError(403, 'This folder is shared with you read-only.', 'SHARE_READ_ONLY');
}

function shareBoundary() {
  return httpError(409, 'Items can’t be moved across the border of a shared folder.', 'SHARE_BOUNDARY');
}

// SQL true while a grant counts: until its expiry (expired rows stay until
// the owner removes them). `column` is a literal from this folder, never input.
function activeGrant(column = 'expires_at') {
  return `(${column} IS NULL OR datetime(${column}) > datetime('now'))`;
}

/** The live grants to `userId` on any of `nodeIds`. */
function grantsOn(userId, ownerId, nodeIds) {
  return db.prepare(`
    SELECT id, node_id, permission FROM user_shares
    WHERE grantee_id = ? AND owner_id = ? AND node_id IN (SELECT value FROM json_each(?)) AND ${activeGrant()}
  `).all(userId, ownerId, JSON.stringify(nodeIds));
}

function assertType(node, type) {
  if (!type || node.type === type) return;
  throw type === 'folder'
    ? httpError(400, 'That is not a folder.', 'NOT_A_FOLDER')
    : httpError(400, 'That is not a file.', 'NOT_A_FILE');
}

/**
 * Access to a node someone else owns, through grants to `userId` on it or a
 * folder above it. The chain must be live (nothing up to the owner's root in
 * the trash). Returns null when nothing grants access.
 *
 *   rootId       the outermost shared folder the member can see (breadcrumbs stop there)
 *   writeRootId  the outermost one granting write (moves stay inside it), or null
 */
function sharedAccess(userId, ownerId, nodeId) {
  const chain = ancestry(ownerId, nodeId);
  if (chain.length === 0 || chain.some((node) => node.trashed_at)) return null;
  const grants = grantsOn(userId, ownerId, chain.map((node) => node.id));
  if (grants.length === 0) return null;
  const node = nodeById(nodeId);
  if (node.vault_id) return null;
  const depth = new Map(chain.map((entry, index) => [entry.id, index]));
  const outermost = (rows) => rows.reduce((best, row) => (
    best === null || depth.get(row.node_id) > depth.get(best.node_id) ? row : best
  ), null);
  const writeRoot = outermost(grants.filter((row) => row.permission === 'write'));
  return {
    node,
    ownerId,
    shared: true,
    permission: writeRoot ? 'write' : 'read',
    rootId: outermost(grants).node_id,
    writeRootId: writeRoot ? writeRoot.node_id : null,
    chain,
  };
}

/**
 * Who acts on `nodeId` for `userId`, and with what right.
 *   - The caller's own node, a missing id or `root`: { ownerId: userId,
 *     shared: false }. The caller then uses the account-scoped services as
 *     before, which also produce their usual 404s.
 *   - A node shared with the caller: { node, ownerId, shared: true,
 *     permission, rootId, writeRootId, chain }.
 *   - Anything else: 404, exactly like a missing node.
 * `need: 'write'` refuses a read-only member (403 SHARE_READ_ONLY); `type`
 * checks a shared node's type like getLiveNode does.
 */
function resolveAccess(userId, nodeId, { need = 'read', type = null } = {}) {
  const own = { ownerId: userId, shared: false };
  if (nodeId === ROOT_ALIAS || nodeId === undefined || nodeId === null) return own;
  const id = String(nodeId);
  const row = db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(id);
  if (!row || row.user_id === userId) return own;
  const access = sharedAccess(userId, row.user_id, id);
  if (!access) throw notFound();
  if (PERMISSION_RANK[access.permission] < PERMISSION_RANK[need]) throw readOnly();
  assertType(access.node, type);
  return access;
}

/**
 * A node as a member sees it: no owner favorites, nothing about whom else it
 * is shared with, no way up past the share.
 */
function memberView(row, access) {
  // The owner's AI tags and metadata stay the owner's.
  const { shared, aiTags, aiMeta, ...node } = serializeNode(row);
  return {
    ...node,
    ...(node.favorite === undefined ? {} : { favorite: false }),
    ...(row.id === access.rootId ? { parentId: null } : {}),
  };
}

/** Breadcrumbs from the shared folder the member can see down to the node. */
function memberPath(access) {
  const top = access.chain.findIndex((node) => node.id === access.rootId);
  return access.chain.slice(0, top + 1).reverse().map((node) => ({ id: node.id, name: node.name }));
}

/**
 * The members to tell when folders of `ownerId` changed: a map of grantee id
 * to the changed folder ids each of them can see.
 */
function membersWatching(ownerId, folderIds) {
  const anyShare = db.prepare(`SELECT 1 FROM user_shares WHERE owner_id = ? AND ${activeGrant()} LIMIT 1`).get(ownerId);
  const watching = new Map();
  if (!anyShare) return watching;
  for (const folderId of folderIds) {
    const chain = ancestry(ownerId, folderId);
    if (chain.length === 0 || chain.some((node) => node.trashed_at)) continue;
    if (nodeById(folderId).vault_id) continue;
    const grantees = db.prepare(`
      SELECT DISTINCT grantee_id FROM user_shares
      WHERE owner_id = ? AND node_id IN (SELECT value FROM json_each(?)) AND ${activeGrant()}
    `).all(ownerId, JSON.stringify(chain.map((node) => node.id)));
    for (const { grantee_id: granteeId } of grantees) {
      if (!watching.has(granteeId)) watching.set(granteeId, []);
      watching.get(granteeId).push(folderId);
    }
  }
  return watching;
}

module.exports = {
  activeGrant,
  memberPath,
  memberView,
  membersWatching,
  resolveAccess,
  shareBoundary,
  sharedAccess,
};
