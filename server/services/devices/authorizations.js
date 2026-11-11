'use strict';

// Device-code sign-in for clients without a browser session (desktop,
// mobile, lite). The client gets a secret device code and a short user code;
// the user approves the user code in the web app; the client then swaps the
// device code for a bearer token, exactly once. Only the device code's
// SHA-256 is stored.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { cleanDeviceInfo, createDeviceToken } = require('./tokens');

const LIFETIME_SECONDS = 600;
const POLL_INTERVAL_SECONDS = 3;
// No I/O/0/1, so a code read aloud or typed from a screen can't be misread.
const USER_CODE_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
const USER_CODE_LENGTH = 8;

function hashCode(code) {
  return crypto.createHash('sha256').update(String(code)).digest('hex');
}

function randomUserCode() {
  let code = '';
  for (let i = 0; i < USER_CODE_LENGTH; i += 1) {
    code += USER_CODE_ALPHABET[crypto.randomInt(USER_CODE_ALPHABET.length)];
  }
  return `${code.slice(0, 4)}-${code.slice(4)}`;
}

// "abcd efgh", "ABCDEFGH" and "ABCD-EFGH" all name the same code.
function normalizeUserCode(value) {
  const letters = String(value || '').toUpperCase().replace(/[^A-Z0-9]/g, '');
  if (letters.length !== USER_CODE_LENGTH) return '';
  return `${letters.slice(0, 4)}-${letters.slice(4)}`;
}

/** Starts a sign-in. `verificationOrigin` is where the user opens the web app. */
function startAuthorization(info, verificationOrigin) {
  const { name, platform } = cleanDeviceInfo(info);
  const deviceCode = crypto.randomBytes(32).toString('base64url');
  db.prepare("DELETE FROM device_authorizations WHERE datetime(expires_at) < datetime('now', '-1 day')").run();
  let userCode;
  for (let attempt = 0; ; attempt += 1) {
    userCode = randomUserCode();
    try {
      db.prepare(`
        INSERT INTO device_authorizations (device_code_hash, user_code, name, platform, expires_at)
        VALUES (?, ?, ?, ?, datetime('now', ?))
      `).run(hashCode(deviceCode), userCode, name, platform, `+${LIFETIME_SECONDS} seconds`);
      break;
    } catch (err) {
      // A user code still in the table; draw again.
      if (attempt >= 5 || err?.code !== 'SQLITE_CONSTRAINT_UNIQUE') throw err;
    }
  }
  return {
    deviceCode,
    userCode,
    verificationUrl: `${verificationOrigin}/app/device?code=${userCode}`,
    expiresIn: LIFETIME_SECONDS,
    interval: POLL_INTERVAL_SECONDS,
  };
}

function isExpired(row) {
  return db.prepare("SELECT datetime(?) <= datetime('now') AS expired").get(row.expires_at).expired === 1;
}

// A request still waiting for the user, or 404 (unknown, expired, decided).
function getPendingByUserCode(userCode) {
  const row = db.prepare('SELECT * FROM device_authorizations WHERE user_code = ?').get(normalizeUserCode(userCode));
  if (!row || row.status !== 'pending' || isExpired(row)) {
    throw httpError(404, 'This code is unknown or has expired. Start the sign-in on the device again.', 'NOT_FOUND');
  }
  return row;
}

/** What the web app shows before the user approves. */
function describeAuthorization(userCode) {
  const row = getPendingByUserCode(userCode);
  return { name: row.name, platform: row.platform, createdAt: row.created_at, expiresAt: row.expires_at };
}

function decideAuthorization(userId, userCode, approve) {
  if (typeof approve !== 'boolean') throw httpError(400, 'approve must be true or false.', 'INVALID_DECISION');
  db.transaction(() => {
    const row = getPendingByUserCode(userCode);
    db.prepare('UPDATE device_authorizations SET status = ?, user_id = ? WHERE id = ?')
      .run(approve ? 'approved' : 'denied', userId, row.id);
  })();
  return { success: true };
}

/**
 * The client's poll. Pending: 428, denied: 403, expired/unknown/used: 410.
 * Approved: a new device token, and the code is spent.
 */
function exchangeDeviceCode(deviceCode) {
  return db.transaction(() => {
    const row = deviceCode
      ? db.prepare('SELECT * FROM device_authorizations WHERE device_code_hash = ?').get(hashCode(deviceCode))
      : null;
    if (!row || row.status === 'consumed' || isExpired(row)) {
      throw httpError(410, 'The sign-in code has expired. Start again.', 'EXPIRED_TOKEN');
    }
    if (row.status === 'pending') throw httpError(428, 'Waiting for approval.', 'AUTHORIZATION_PENDING');
    if (row.status === 'denied') throw httpError(403, 'The sign-in was denied.', 'ACCESS_DENIED');

    db.prepare("UPDATE device_authorizations SET status = 'consumed' WHERE id = ?").run(row.id);
    const { device, token } = createDeviceToken(row.user_id, { name: row.name, platform: row.platform });
    const user = db.prepare('SELECT id, username FROM users WHERE id = ?').get(row.user_id);
    return {
      token,
      device: { id: device.id, name: device.name, platform: device.platform },
      user: { id: user.id, username: user.username },
    };
  })();
}

module.exports = {
  decideAuthorization,
  describeAuthorization,
  exchangeDeviceCode,
  startAuthorization,
};
