'use strict';

// Bearer tokens for the desktop, mobile and lite clients. A token is shown
// once when created; only its SHA-256 is stored. Tokens reach file and sync
// endpoints; the phone app's (android/ios) also reaches account, settings and
// admin (middleware/auth.js).

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { assertAccountActive } = require('../account/status');

const TOKEN_PREFIX = 'nc_';
const MAX_NAME_LENGTH = 80;
// `webdav` tokens are app passwords for WebDAV clients (Basic auth).
const PLATFORMS = new Set(['', 'windows', 'macos', 'linux', 'android', 'ios', 'lite', 'webdav']);

function hashToken(token) {
  return crypto.createHash('sha256').update(String(token)).digest('hex');
}

function serialize(row) {
  return {
    id: row.id,
    name: row.name,
    platform: row.platform,
    createdAt: row.created_at,
    lastSeenAt: row.last_seen_at,
  };
}

function cleanPlatform(platform) {
  const clean = String(platform || '').trim().toLowerCase();
  if (!PLATFORMS.has(clean)) throw httpError(400, 'Unknown platform.', 'PLATFORM_INVALID');
  return clean;
}

// A device's name and platform as stored, or a 400 for bad input.
function cleanDeviceInfo({ name, platform = '' } = {}) {
  const cleanName = String(name || '').trim().slice(0, MAX_NAME_LENGTH);
  if (!cleanName) throw httpError(400, 'Give the device a name.', 'NAME_REQUIRED');
  return { name: cleanName, platform: cleanPlatform(platform) };
}

// Every route that hands out a token (device code, QR pairing, security key,
// app passwords) comes through here, so a disabled account gets none.
function createDeviceToken(userId, info) {
  assertAccountActive(userId);
  const { name, platform } = cleanDeviceInfo(info);
  const token = TOKEN_PREFIX + crypto.randomBytes(32).toString('base64url');
  const result = db.prepare(`
    INSERT INTO device_tokens (user_id, name, platform, token_hash) VALUES (?, ?, ?, ?)
  `).run(userId, name, platform, hashToken(token));
  const row = db.prepare('SELECT * FROM device_tokens WHERE id = ?').get(result.lastInsertRowid);
  return { device: serialize(row), token };
}

function listDeviceTokens(userId) {
  return db.prepare(`
    SELECT * FROM device_tokens WHERE user_id = ? AND revoked_at IS NULL ORDER BY id DESC
  `).all(userId).map(serialize);
}

function revokeDeviceToken(userId, id) {
  const result = db.prepare(`
    UPDATE device_tokens SET revoked_at = datetime('now')
    WHERE id = ? AND user_id = ? AND revoked_at IS NULL
  `).run(id, userId);
  if (result.changes === 0) throw httpError(404, 'Device not found', 'NOT_FOUND');
  return { success: true };
}

/**
 * Signs every client of the account out: all bearer tokens and WebDAV app
 * passwords, plus approved device-code sign-ins, QR sign-ins and QR pairing
 * codes not yet redeemed, which would otherwise still turn into a token or a
 * session. Used when the password changes or an admin resets it. With
 * `exceptDeviceId` the phone that asked stays signed in.
 */
function revokeAllDeviceTokens(userId, { exceptDeviceId = null } = {}) {
  return db.transaction(() => {
    const { changes } = db.prepare(`
      UPDATE device_tokens SET revoked_at = datetime('now')
      WHERE user_id = ? AND revoked_at IS NULL AND id IS NOT ?
    `).run(userId, exceptDeviceId);
    db.prepare(`
      UPDATE device_authorizations SET status = 'denied' WHERE user_id = ? AND status = 'approved'
    `).run(userId);
    db.prepare(`
      UPDATE user_qr_login_challenges SET status = 'declined' WHERE approved_by_user_id = ? AND status = 'approved'
    `).run(userId);
    db.prepare(`
      UPDATE device_pairings SET expires_at = datetime('now', '-1 second') WHERE user_id = ? AND status = 'pending'
    `).run(userId);
    return changes;
  })();
}

function getDeviceToken(userId, id) {
  const row = db.prepare(`
    SELECT * FROM device_tokens WHERE id = ? AND user_id = ? AND revoked_at IS NULL
  `).get(id, userId);
  if (!row) throw httpError(404, 'Device not found', 'NOT_FOUND');
  return serialize(row);
}

// The account id a bearer token belongs to, or null. A disabled account's
// tokens resolve to nothing.
function resolveDeviceToken(token) {
  if (!String(token || '').startsWith(TOKEN_PREFIX)) return null;
  const row = db.prepare(`
    SELECT d.id, d.user_id, d.platform FROM device_tokens d JOIN users u ON u.id = d.user_id
    WHERE d.token_hash = ? AND d.revoked_at IS NULL AND u.disabled_at IS NULL
  `).get(hashToken(token));
  if (!row) return null;
  db.prepare(`
    UPDATE device_tokens SET last_seen_at = datetime('now')
    WHERE id = ? AND (last_seen_at IS NULL OR last_seen_at < datetime('now', '-1 minute'))
  `).run(row.id);
  return { userId: row.user_id, deviceId: row.id, platform: row.platform };
}

module.exports = {
  cleanDeviceInfo,
  cleanPlatform,
  createDeviceToken,
  getDeviceToken,
  listDeviceTokens,
  resolveDeviceToken,
  revokeAllDeviceTokens,
  revokeDeviceToken,
};
