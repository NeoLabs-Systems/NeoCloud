'use strict';

// QR pairing: a signed-in web session mints a short-lived, single-use code;
// a phone scans it (neocloud://pair?server=…&code=…) and swaps the code for a
// device token. Only the code's SHA-256 is stored.

const crypto = require('crypto');
const db = require('../../db/database');
const { emitToUser } = require('../events');
const { httpError } = require('../../utils/http_error');
const { cleanDeviceInfo, cleanPlatform, createDeviceToken } = require('./tokens');

const LIFETIME_SECONDS = 300;
const MAX_OPEN_CODES = 5;

function hashCode(code) {
  return crypto.createHash('sha256').update(String(code)).digest('hex');
}

// Optional name/platform suggested by the web page; the phone's own win.
function cleanHints({ name, platform } = {}) {
  if (name) return cleanDeviceInfo({ name, platform });
  return { name: null, platform: cleanPlatform(platform) };
}

function startPairing(userId, hints, serverOrigin) {
  const { name, platform } = cleanHints(hints);
  db.prepare("DELETE FROM device_pairings WHERE datetime(expires_at) < datetime('now', '-1 day')").run();
  const open = db.prepare(`
    SELECT COUNT(*) AS count FROM device_pairings
    WHERE user_id = ? AND status = 'pending' AND datetime(expires_at) > datetime('now')
  `).get(userId).count;
  if (open >= MAX_OPEN_CODES) {
    throw httpError(429, 'Too many open pairing codes. Wait a few minutes and try again.', 'TOO_MANY_PAIRING_CODES');
  }
  const code = crypto.randomBytes(32).toString('base64url');
  const { expires_at: expiresAt } = db.prepare(`
    INSERT INTO device_pairings (user_id, code_hash, name, platform, expires_at)
    VALUES (?, ?, ?, ?, datetime('now', ?))
    RETURNING expires_at
  `).get(userId, hashCode(code), name, platform, `+${LIFETIME_SECONDS} seconds`);
  const query = new URLSearchParams({ server: serverOrigin, code });
  return { code, expiresAt, server: serverOrigin, qrPayload: `neocloud://pair?${query}` };
}

function isExpired(row) {
  return db.prepare("SELECT datetime(?) <= datetime('now') AS expired").get(row.expires_at).expired === 1;
}

/** The web page's poll: pending, used (with the device) or expired. */
function getPairingStatus(userId, code) {
  const row = db.prepare(`
    SELECT p.*, t.name AS device_name, t.platform AS device_platform
    FROM device_pairings p LEFT JOIN device_tokens t ON t.id = p.device_id
    WHERE p.code_hash = ? AND p.user_id = ?
  `).get(hashCode(code), userId);
  if (!row) throw httpError(404, 'Pairing code not found', 'NOT_FOUND');
  if (row.status === 'used') {
    const device = row.device_id ? { id: row.device_id, name: row.device_name, platform: row.device_platform } : null;
    return { status: 'used', device };
  }
  return { status: isExpired(row) ? 'expired' : 'pending' };
}

/** The phone's swap. Used, expired or unknown codes all answer 410. */
function redeemPairing({ code, name, platform }) {
  const result = db.transaction(() => {
    const row = typeof code === 'string' && code
      ? db.prepare('SELECT * FROM device_pairings WHERE code_hash = ?').get(hashCode(code))
      : null;
    if (!row || row.status !== 'pending' || isExpired(row)) {
      throw httpError(410, 'The pairing code has expired. Show a new one in the web app.', 'EXPIRED_TOKEN');
    }
    const info = { name: name || row.name, platform: platform || row.platform };
    const { device, token } = createDeviceToken(row.user_id, info);
    db.prepare("UPDATE device_pairings SET status = 'used', device_id = ?, used_at = datetime('now') WHERE id = ?")
      .run(device.id, row.id);
    const user = db.prepare('SELECT id, username FROM users WHERE id = ?').get(row.user_id);
    return {
      token,
      device: { id: device.id, name: device.name, platform: device.platform },
      user: { id: user.id, username: user.username },
    };
  })();
  emitToUser(result.user.id, 'devices:paired', { device: result.device });
  return result;
}

module.exports = { getPairingStatus, redeemPairing, startPairing };
