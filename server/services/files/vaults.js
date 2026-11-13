'use strict';

// End-to-end encrypted folders. Clients encrypt names and contents with a
// vault key the server never sees; the server keeps that key only wrapped
// (AES-GCM under a PBKDF2 key from the user's passphrase) and enforces that
// nothing moves across the vault's border. See docs/clients.md, section 4.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { announceChanges } = require('./changes');
const { conflictMode, createFolderInTransaction, vaultBoundary } = require('./operations');
const { getLiveNode, nodeById, serializeNode } = require('./tree');

const SALT_BYTES = 16;
// nonce 12 B + AES-256-GCM of a 32-byte key + tag 16 B.
const WRAPPED_KEY_BYTES = 12 + 32 + 16;
const MIN_ITERATIONS = 600000;
const MAX_ITERATIONS = 10000000;
const BASE64_PATTERN = /^[A-Za-z0-9+/]+={0,2}$/;

function decodedLength(value, field) {
  const text = String(value || '');
  if (!BASE64_PATTERN.test(text) || text.length % 4 !== 0) {
    throw httpError(400, `${field} must be base64.`, 'VAULT_PARAMS_INVALID');
  }
  return Buffer.from(text, 'base64').length;
}

// The client's key material, checked for shape only.
function cleanKeyParams({ salt, iterations, wrappedKey }) {
  if (decodedLength(salt, 'salt') !== SALT_BYTES) {
    throw httpError(400, `salt must be ${SALT_BYTES} bytes.`, 'VAULT_PARAMS_INVALID');
  }
  if (decodedLength(wrappedKey, 'wrappedKey') !== WRAPPED_KEY_BYTES) {
    throw httpError(400, `wrappedKey must be ${WRAPPED_KEY_BYTES} bytes.`, 'VAULT_PARAMS_INVALID');
  }
  const rounds = Number(iterations);
  if (!Number.isInteger(rounds) || rounds < MIN_ITERATIONS || rounds > MAX_ITERATIONS) {
    throw httpError(400, `iterations must be between ${MIN_ITERATIONS} and ${MAX_ITERATIONS}.`, 'VAULT_PARAMS_INVALID');
  }
  return { salt: String(salt), iterations: rounds, wrappedKey: String(wrappedKey) };
}

function serializeVault(row) {
  return {
    id: row.id,
    folderId: row.folder_id,
    salt: row.salt,
    iterations: row.iterations,
    wrappedKey: row.wrapped_key,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function getVaultRow(userId, vaultId) {
  const row = db.prepare('SELECT * FROM vaults WHERE id = ? AND user_id = ?').get(String(vaultId || ''), userId);
  if (!row) throw httpError(404, 'Vault not found', 'NOT_FOUND');
  return row;
}

/** A new vault folder. Vaults don't nest. `conflict` is fail or rename. */
function createVault(userId, { parentId, name, salt, iterations, wrappedKey, conflict }) {
  const params = cleanKeyParams({ salt, iterations, wrappedKey });
  const mode = conflictMode(conflict);
  if (mode === 'replace') throw httpError(400, 'conflict must be fail or rename.', 'CONFLICT_MODE_INVALID');
  const result = db.transaction(() => {
    const parent = getLiveNode(userId, parentId, { type: 'folder' });
    if (parent.vault_id) throw vaultBoundary();
    const { node } = createFolderInTransaction(userId, parent.id, name, mode);
    const id = crypto.randomUUID();
    db.prepare(`
      INSERT INTO vaults (id, user_id, folder_id, salt, iterations, wrapped_key) VALUES (?, ?, ?, ?, ?, ?)
    `).run(id, userId, node.id, params.salt, params.iterations, params.wrappedKey);
    db.prepare('UPDATE nodes SET vault_id = ? WHERE id = ?').run(id, node.id);
    return {
      vault: getVaultRow(userId, id),
      folder: nodeById(node.id),
    };
  })();
  announceChanges(userId, [result.folder.parent_id]);
  return { ...serializeVault(result.vault), folder: serializeNode(result.folder) };
}

function getVault(userId, vaultId) {
  return serializeVault(getVaultRow(userId, vaultId));
}

/** A new wrapping of the same vault key (the passphrase changed). */
function updateVaultKey(userId, vaultId, { salt, iterations, wrappedKey }) {
  const params = cleanKeyParams({ salt, iterations, wrappedKey });
  const row = getVaultRow(userId, vaultId);
  db.prepare(`
    UPDATE vaults SET salt = ?, iterations = ?, wrapped_key = ?, updated_at = datetime('now') WHERE id = ?
  `).run(params.salt, params.iterations, params.wrappedKey, row.id);
  return getVault(userId, row.id);
}

module.exports = { createVault, getVault, updateVaultKey };
