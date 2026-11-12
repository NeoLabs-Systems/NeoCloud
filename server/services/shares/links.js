'use strict';

// Share links as their owner manages them: create, list, edit, revoke. What
// a visitor with the link can do is in public.js.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { hashPassword } = require('../account/credentials');
const { getLiveNode } = require('../files/tree');

const MIN_PASSWORD_LENGTH = 8;
const MAX_PASSWORD_LENGTH = 200;
const MAX_NOTE_LENGTH = 500;
const MAX_DOWNLOAD_LIMIT = 1000000;
// view: browse and download. view_upload: also upload. upload_only: upload
// into the folder without seeing what is in it.
const SHARE_MODES = ['view', 'view_upload', 'upload_only'];

function shareUrl(origin, token) {
  return `${origin}/s/${token}`;
}

function serializeShare(row, origin) {
  return {
    id: row.id,
    token: row.token,
    url: shareUrl(origin, row.token),
    nodeId: row.node_id,
    nodeName: row.node_name,
    nodeType: row.node_type,
    mode: row.mode,
    allowUpload: row.mode !== 'view',
    expiresAt: row.expires_at,
    expired: row.is_expired === 1,
    hasPassword: Boolean(row.password_hash),
    note: row.note,
    maxDownloads: row.max_downloads,
    downloadCount: row.download_count,
    exhausted: row.is_exhausted === 1,
    maxUploadBytes: row.max_upload_bytes,
    uploadedBytes: row.uploaded_bytes,
    createdAt: row.created_at,
  };
}

const SELECT_SHARE = `
  SELECT s.*, n.name AS node_name, n.type AS node_type,
         (s.expires_at IS NOT NULL AND datetime(s.expires_at) <= datetime('now')) AS is_expired,
         (s.max_downloads IS NOT NULL AND s.download_count >= s.max_downloads) AS is_exhausted
  FROM shares s JOIN nodes n ON n.id = s.node_id
`;

function isUnset(value) {
  return value === undefined || value === null || value === '';
}

// An ISO date in the future, stored as SQLite UTC text; empty means never.
function parseExpiry(value) {
  if (isUnset(value)) return null;
  const time = Date.parse(String(value));
  if (!Number.isFinite(time)) throw httpError(400, 'expiresAt must be a date.', 'INVALID_EXPIRY');
  if (time <= Date.now()) throw httpError(400, 'expiresAt must be in the future.', 'INVALID_EXPIRY');
  return new Date(time).toISOString().slice(0, 19).replace('T', ' ');
}

// `mode` wins; the older `allowUpload: true` still means view_upload.
function parseMode(mode, allowUpload) {
  if (isUnset(mode)) return allowUpload === true ? 'view_upload' : 'view';
  if (!SHARE_MODES.includes(mode)) {
    throw httpError(400, `mode must be one of ${SHARE_MODES.join(', ')}.`, 'INVALID_MODE');
  }
  return mode;
}

function parseCount(value, max, message, code) {
  if (isUnset(value)) return null;
  const count = Number(value);
  if (!Number.isSafeInteger(count) || count < 1 || count > max) throw httpError(400, message, code);
  return count;
}

function parseMaxDownloads(value) {
  return parseCount(value, MAX_DOWNLOAD_LIMIT, `maxDownloads must be between 1 and ${MAX_DOWNLOAD_LIMIT}.`, 'INVALID_MAX_DOWNLOADS');
}

function parseMaxUploadBytes(value) {
  return parseCount(value, Number.MAX_SAFE_INTEGER, 'maxUploadBytes must be a positive byte count.', 'INVALID_SIZE');
}

function parseNote(value) {
  if (isUnset(value)) return null;
  if (typeof value !== 'string') throw httpError(400, 'note must be text.', 'INVALID_NOTE');
  const note = value.trim();
  if (note.length > MAX_NOTE_LENGTH) throw httpError(400, `note can be at most ${MAX_NOTE_LENGTH} characters.`, 'NOTE_TOO_LONG');
  return note || null;
}

// The bcrypt hash of a new link password; empty means none.
async function passwordHash(value) {
  if (isUnset(value)) return null;
  const text = String(value);
  if (text.length < MIN_PASSWORD_LENGTH) {
    throw httpError(400, `The password needs at least ${MIN_PASSWORD_LENGTH} characters.`, 'PASSWORD_TOO_SHORT');
  }
  if (text.length > MAX_PASSWORD_LENGTH) throw httpError(400, 'Password is too long.', 'PASSWORD_TOO_LONG');
  return hashPassword(text);
}

function assertModeFits(mode, nodeType) {
  if (mode !== 'view' && nodeType !== 'folder') throw httpError(400, 'Uploads need a shared folder.', 'NOT_A_FOLDER');
}

function getOwnShare(userId, id) {
  const row = db.prepare(`${SELECT_SHARE} WHERE s.id = ? AND s.user_id = ? AND s.revoked_at IS NULL`).get(id, userId);
  if (!row) throw httpError(404, 'Share not found', 'NOT_FOUND');
  return row;
}

async function createShare(userId, body, origin) {
  const node = getLiveNode(userId, body.nodeId);
  if (node.parent_id === null) throw httpError(400, 'The root folder can’t be shared.', 'ROOT_IMMUTABLE');
  if (node.vault_id) throw httpError(409, 'Vault folders can’t be shared.', 'VAULT_NOT_SHAREABLE');
  const mode = parseMode(body.mode, body.allowUpload);
  assertModeFits(mode, node.type);
  const values = {
    expiry: parseExpiry(body.expiresAt),
    maxDownloads: parseMaxDownloads(body.maxDownloads),
    maxUploadBytes: parseMaxUploadBytes(body.maxUploadBytes),
    note: parseNote(body.note),
    passwordHash: await passwordHash(body.password),
  };
  const token = crypto.randomBytes(24).toString('base64url');
  const result = db.prepare(`
    INSERT INTO shares (user_id, node_id, token, password_hash, mode, allow_upload, expires_at, max_downloads, max_upload_bytes, note)
    VALUES (@userId, @nodeId, @token, @passwordHash, @mode, @allowUpload, @expiry, @maxDownloads, @maxUploadBytes, @note)
  `).run({ userId, nodeId: node.id, token, mode, allowUpload: mode === 'view' ? 0 : 1, ...values });
  return serializeShare(getOwnShare(userId, result.lastInsertRowid), origin);
}

/**
 * Changes a link in place; the token (and so the URL) stays. Only the fields
 * present change; `null` clears expiresAt, password, maxDownloads, note and
 * maxUploadBytes. A new or removed password signs every visitor out.
 */
async function updateShare(userId, id, body, origin) {
  const row = getOwnShare(userId, id);
  const changes = {};
  if (body.expiresAt !== undefined) changes.expires_at = parseExpiry(body.expiresAt);
  if (body.mode !== undefined || body.allowUpload !== undefined) {
    changes.mode = parseMode(body.mode, body.allowUpload);
    assertModeFits(changes.mode, row.node_type);
    changes.allow_upload = changes.mode === 'view' ? 0 : 1;
  }
  if (body.maxDownloads !== undefined) changes.max_downloads = parseMaxDownloads(body.maxDownloads);
  if (body.maxUploadBytes !== undefined) changes.max_upload_bytes = parseMaxUploadBytes(body.maxUploadBytes);
  if (body.note !== undefined) changes.note = parseNote(body.note);
  if (body.password !== undefined) changes.password_hash = await passwordHash(body.password);

  const columns = Object.keys(changes);
  if (columns.length > 0) {
    db.transaction(() => {
      // Column names come from the fixed keys above, never from input.
      db.prepare(`UPDATE shares SET ${columns.map((column) => `${column} = @${column}`).join(', ')} WHERE id = @id`)
        .run({ ...changes, id: row.id });
      if (columns.includes('password_hash')) db.prepare('DELETE FROM share_unlock_failures WHERE share_id = ?').run(row.id);
    })();
  }
  return serializeShare(getOwnShare(userId, row.id), origin);
}

/**
 * The account's links that are not revoked, newest first; expired and used
 * up ones say so. With `nodeId`: only that node's links still usable (not
 * expired, downloads left), so a share dialog can show the existing link.
 */
function listShares(userId, origin, { nodeId } = {}) {
  const rows = isUnset(nodeId)
    ? db.prepare(`${SELECT_SHARE} WHERE s.user_id = ? AND s.revoked_at IS NULL ORDER BY s.id DESC`).all(userId)
    : db.prepare(`
      SELECT * FROM (${SELECT_SHARE} WHERE s.user_id = ? AND s.node_id = ? AND s.revoked_at IS NULL)
      WHERE is_expired = 0 AND is_exhausted = 0 ORDER BY id DESC
    `).all(userId, String(nodeId));
  return { shares: rows.map((row) => serializeShare(row, origin)) };
}

function revokeShare(userId, id) {
  const result = db.prepare(`
    UPDATE shares SET revoked_at = datetime('now') WHERE id = ? AND user_id = ? AND revoked_at IS NULL
  `).run(id, userId);
  if (result.changes === 0) throw httpError(404, 'Share not found', 'NOT_FOUND');
  return { success: true };
}

module.exports = { createShare, listShares, parseExpiry, revokeShare, updateShare };
