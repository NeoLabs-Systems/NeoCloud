'use strict';

// Sharing with other accounts on this server: the owner adds members by
// their exact email or username, changes or removes them; a member lists
// what is shared with them and may leave. There is deliberately no way to
// search or list accounts: an unknown recipient, the owner themselves and an
// account that can't receive shares all get the same 404.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { emitToUser } = require('../events');
const { getLiveNode } = require('../files/tree');
const { parseExpiry } = require('../shares/links');
const { activeGrant, memberView, sharedAccess } = require('./access');

const PERMISSIONS = ['read', 'write'];
const MAX_RECIPIENT_LENGTH = 254;

function recipientNotFound() {
  return httpError(404, 'No account can receive this share.', 'RECIPIENT_NOT_FOUND');
}

function memberNotFound() {
  return httpError(404, 'Member not found', 'MEMBER_NOT_FOUND');
}

function parsePermission(value, nodeType) {
  const permission = value === undefined || value === null || value === '' ? 'read' : value;
  if (!PERMISSIONS.includes(permission)) {
    throw httpError(400, 'permission must be read or write.', 'INVALID_PERMISSION');
  }
  // Members can't rename, move or delete the shared node itself, so write on
  // a single file would grant nothing more than read.
  if (permission === 'write' && nodeType !== 'folder') {
    throw httpError(400, 'Only folders can be shared with write access.', 'WRITE_NEEDS_FOLDER');
  }
  return permission;
}

function parseRecipient(value) {
  const text = typeof value === 'string' ? value.trim() : '';
  if (text.length === 0 || text.length > MAX_RECIPIENT_LENGTH) {
    throw httpError(400, 'Enter the exact email address or username.', 'INVALID_RECIPIENT');
  }
  return text;
}

/**
 * The account `text` names exactly (email when it has an @, else username;
 * case-insensitive), or null. The only rule for who can receive a share is
 * here: any other account than the owner's.
 */
function findRecipient(ownerId, text) {
  const column = text.includes('@') ? 'email' : 'username';
  // The column name comes from the two literals above, never from input.
  const account = db.prepare(`SELECT id FROM users WHERE lower(${column}) = lower(?)`).get(text);
  if (!account || account.id === ownerId) return null;
  return account;
}

// The owner's view of a member: the name the member chose, and what the
// owner typed. Never the member's email or username unless the owner typed it.
function serializeMember(row) {
  return {
    id: row.id,
    nodeId: row.node_id,
    recipient: row.recipient,
    displayName: row.display_name || null,
    permission: row.permission,
    createdAt: row.created_at,
    expiresAt: row.expires_at,
    expired: row.is_expired === 1,
  };
}

const SELECT_MEMBER = `
  SELECT s.*, u.display_name, NOT ${activeGrant('s.expires_at')} AS is_expired
  FROM user_shares s JOIN users u ON u.id = s.grantee_id
`;

function shareableNode(ownerId, nodeId) {
  const node = getLiveNode(ownerId, nodeId);
  if (node.parent_id === null) throw httpError(400, 'The root folder can’t be shared.', 'ROOT_IMMUTABLE');
  if (node.vault_id) throw httpError(409, 'Vault folders can’t be shared.', 'VAULT_NOT_SHAREABLE');
  return node;
}

function getMemberRow(ownerId, nodeId, shareId) {
  const row = db.prepare(`${SELECT_MEMBER} WHERE s.id = ? AND s.owner_id = ? AND s.node_id = ?`)
    .get(shareId, ownerId, nodeId);
  if (!row) throw memberNotFound();
  return row;
}

function notifyShareChange(...userIds) {
  for (const userId of userIds) emitToUser(userId, 'shares:changed', {});
}

/** Shares a node with the account `recipient` names. */
function addMember(ownerId, nodeId, { recipient, permission, expiresAt }) {
  const node = shareableNode(ownerId, nodeId);
  const text = parseRecipient(recipient);
  const level = parsePermission(permission, node.type);
  const expiry = parseExpiry(expiresAt);
  const grantee = findRecipient(ownerId, text);
  if (!grantee) throw recipientNotFound();
  let id;
  try {
    id = db.prepare(`
      INSERT INTO user_shares (owner_id, node_id, grantee_id, permission, recipient, expires_at)
      VALUES (?, ?, ?, ?, ?, ?)
    `).run(ownerId, node.id, grantee.id, level, text, expiry).lastInsertRowid;
  } catch (err) {
    if (err.code === 'SQLITE_CONSTRAINT_UNIQUE') {
      throw httpError(409, 'Already shared with this account.', 'ALREADY_MEMBER');
    }
    throw err;
  }
  notifyShareChange(grantee.id);
  return serializeMember(getMemberRow(ownerId, node.id, id));
}

function listMembers(ownerId, nodeId) {
  const node = getLiveNode(ownerId, nodeId);
  const rows = db.prepare(`${SELECT_MEMBER} WHERE s.owner_id = ? AND s.node_id = ? ORDER BY s.id`).all(ownerId, node.id);
  return { members: rows.map(serializeMember) };
}

/** Changes a member's permission and/or expiry; null clears expiresAt. */
function updateMember(ownerId, nodeId, shareId, { permission, expiresAt }) {
  const node = getLiveNode(ownerId, nodeId);
  const row = getMemberRow(ownerId, node.id, shareId);
  const level = permission === undefined ? row.permission : parsePermission(permission, node.type);
  const expiry = expiresAt === undefined ? row.expires_at : parseExpiry(expiresAt);
  db.prepare('UPDATE user_shares SET permission = ?, expires_at = ? WHERE id = ?').run(level, expiry, row.id);
  notifyShareChange(row.grantee_id);
  return serializeMember(getMemberRow(ownerId, node.id, row.id));
}

function removeMember(ownerId, nodeId, shareId) {
  const node = getLiveNode(ownerId, nodeId);
  const row = getMemberRow(ownerId, node.id, shareId);
  db.prepare('DELETE FROM user_shares WHERE id = ?').run(row.id);
  notifyShareChange(row.grantee_id);
  return { success: true };
}

/**
 * What other accounts share with `userId`: each shared node with its
 * owner's name and the permission. Shares whose node is in the owner's
 * trash (or below a trashed folder) or expired are left out.
 */
function listSharedWithMe(userId) {
  const rows = db.prepare(`
    SELECT s.id, s.owner_id, s.node_id, s.permission, s.created_at, s.expires_at,
           COALESCE(NULLIF(u.display_name, ''), u.username) AS owner_name
    FROM user_shares s JOIN users u ON u.id = s.owner_id
    WHERE s.grantee_id = ? AND ${activeGrant('s.expires_at')}
    ORDER BY s.created_at DESC, s.id DESC
  `).all(userId);
  const shares = [];
  for (const row of rows) {
    const access = sharedAccess(userId, row.owner_id, row.node_id);
    if (!access) continue;
    shares.push({
      id: row.id,
      permission: row.permission,
      ownerName: row.owner_name,
      sharedAt: row.created_at,
      expiresAt: row.expires_at,
      node: memberView(access.node, { rootId: row.node_id }),
    });
  }
  return { shares };
}

/** The member gives a share up; access ends at once. */
function leaveShare(userId, shareId) {
  const row = db.prepare('SELECT id, owner_id FROM user_shares WHERE id = ? AND grantee_id = ?').get(shareId, userId);
  if (!row) throw memberNotFound();
  db.prepare('DELETE FROM user_shares WHERE id = ?').run(row.id);
  notifyShareChange(row.owner_id, userId);
  return { success: true };
}

module.exports = { addMember, leaveShare, listMembers, listSharedWithMe, removeMember, updateMember };
