'use strict';

// Turning a plain folder into a vault. The client does the work, because only
// it holds the vault key: it creates a vault beside the folder, uploads every
// file encrypted into it and checks each upload. The server answers two
// questions around that: may this folder be converted (and is there room for
// a second, encrypted copy), and, once everything is in, replaces the folder
// by the vault in one step. See docs/clients.md, section 4.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { activeGrant } = require('../members/access');
const { remainingBytes } = require('../storage/quota');
const { announceChanges, currentCursor, recordChange } = require('./changes');
const { touchFolder } = require('./operations');
const { ancestry, getLiveNode, liveSubtree, nodeById, serializeNode } = require('./tree');

// NCV1: 12-byte header, then a 16-byte tag per 64 KiB segment (an empty file
// is one empty segment).
const SEGMENT_BYTES = 65536;
const HEADER_BYTES = 12;
const TAG_BYTES = 16;

function vaultSize(size) {
  return HEADER_BYTES + Math.max(1, Math.ceil(size / SEGMENT_BYTES)) * TAG_BYTES + size;
}

function placeholders(ids) {
  return ids.map(() => '?').join(', ');
}

// Why the folder can't become a vault, as codes, ignoring room. Members and
// public links count on the folder, anything in it and any folder above it:
// people who can see the folder would lose its contents.
function problemsOf(userId, folder, subtree) {
  const problems = [];
  if (folder.parent_id === null) problems.push('ROOT');
  if (folder.vault_id) problems.push('IN_VAULT');
  if (subtree.some((node) => node.depth > 0 && node.vault_id)) problems.push('CONTAINS_VAULT');
  const ids = [...subtree.map((node) => node.id), ...ancestry(userId, folder.id).map((node) => node.id)];
  const linked = (sql) => ids.length > 0 && Boolean(db.prepare(sql).get(...ids));
  if (linked(`SELECT 1 FROM user_shares WHERE node_id IN (${placeholders(ids)}) AND ${activeGrant()} LIMIT 1`)) {
    problems.push('SHARED');
  }
  if (linked(`
    SELECT 1 FROM shares WHERE node_id IN (${placeholders(ids)}) AND revoked_at IS NULL
    AND (expires_at IS NULL OR datetime(expires_at) > datetime('now')) LIMIT 1
  `)) {
    problems.push('PUBLIC_LINK');
  }
  return problems;
}

/**
 * What converting `folderId` involves: its files, folders and bytes, the
 * bytes the encrypted copy will take, the room left (null when unlimited),
 * the change-journal position to hand back when replacing, and the
 * `problems` that rule it out (empty when it may go ahead).
 */
function conversionCheck(userId, folderId) {
  const folder = getLiveNode(userId, folderId, { type: 'folder' });
  const subtree = liveSubtree(userId, folder.id);
  const files = subtree.filter((node) => node.type === 'file');
  const bytes = files.reduce((sum, node) => sum + node.size, 0);
  const vaultBytes = files.reduce((sum, node) => sum + vaultSize(node.size), 0);
  const free = remainingBytes(userId);
  const problems = problemsOf(userId, folder, subtree);
  if (free < vaultBytes) problems.push('QUOTA');
  return {
    folderId: folder.id,
    name: folder.name,
    parentId: folder.parent_id,
    files: files.length,
    folders: subtree.length - files.length - 1,
    bytes,
    vaultBytes,
    freeBytes: Number.isFinite(free) ? free : null,
    cursor: currentCursor(userId),
    problems,
  };
}

/**
 * Moves `folderId` to the trash and gives vault `vaultId`'s folder its name
 * and place, in one transaction. Refused when anything below the folder
 * changed after `cursor` (from the check that started the last pass), so a
 * file added or edited during the conversion is never trashed unconverted.
 */
function replaceFolderWithVault(userId, vaultId, { folderId, cursor }) {
  const after = Number(cursor);
  if (!Number.isInteger(after) || after < 0) throw httpError(400, 'cursor must be a whole number.', 'INVALID_CURSOR');
  const result = db.transaction(() => {
    const vault = db.prepare('SELECT * FROM vaults WHERE id = ? AND user_id = ?').get(String(vaultId || ''), userId);
    if (!vault) throw httpError(404, 'Vault not found', 'NOT_FOUND');
    const vaultFolder = getLiveNode(userId, vault.folder_id, { type: 'folder' });
    const folder = getLiveNode(userId, folderId, { type: 'folder' });
    if (ancestry(userId, vaultFolder.id).some((node) => node.id === folder.id)) {
      throw httpError(409, 'The vault is inside the folder it replaces.', 'VAULT_BOUNDARY');
    }
    const subtree = liveSubtree(userId, folder.id);
    if (problemsOf(userId, folder, subtree).length > 0) {
      throw httpError(409, 'This folder can’t be turned into a vault.', 'VAULT_CONVERT_REFUSED');
    }
    const below = subtree.filter((node) => node.depth > 0).map((node) => node.id);
    const changed = below.length > 0 && db.prepare(`
      SELECT 1 FROM changes WHERE user_id = ? AND seq > ? AND node_id IN (${placeholders(below)}) LIMIT 1
    `).get(userId, after, ...below);
    if (changed) throw httpError(409, 'The folder changed during the conversion.', 'VAULT_SOURCE_CHANGED');

    db.prepare("UPDATE nodes SET trashed_at = datetime('now') WHERE id = ?").run(folder.id);
    recordChange(userId, folder.id, 'trash');
    const moved = vaultFolder.parent_id !== folder.parent_id;
    db.prepare('UPDATE nodes SET parent_id = ?, name = ? WHERE id = ?').run(folder.parent_id, folder.name, vaultFolder.id);
    recordChange(userId, vaultFolder.id, moved ? 'move' : 'rename');
    touchFolder(folder.parent_id);
    if (moved) touchFolder(vaultFolder.parent_id);
    return { node: nodeById(vaultFolder.id), trashedId: folder.id, parents: [folder.parent_id, vaultFolder.parent_id] };
  })();
  announceChanges(userId, result.parents);
  return { folder: serializeNode(result.node), trashedId: result.trashedId };
}

module.exports = { conversionCheck, replaceFolderWithVault, vaultSize };
