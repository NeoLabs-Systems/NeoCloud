'use strict';

const bcrypt = require('bcrypt');
const { verifyPassword: verifyCurrentPassword } = require('./credentials');
const crypto = require('crypto');
const {
  generateSecret,
  generateURI,
  verifySync,
} = require('otplib');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const throttle = require('./login_throttle');
const {
  decryptValue,
  encryptValue,
} = require('../crypto/keys');

const TOTP_OPTIONS = {
  strategy: 'totp',
  digits: 6,
  period: 30,
  epochTolerance: 30,
};

const RECOVERY_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';

function normalizeCode(value) {
  return String(value || '').trim().replace(/\s+/g, '').toUpperCase();
}

function normalizeRecoveryCode(value) {
  return normalizeCode(value).replace(/-/g, '');
}

function generateRecoveryCode() {
  let raw = '';
  while (raw.length < 10) {
    raw += RECOVERY_ALPHABET[crypto.randomInt(RECOVERY_ALPHABET.length)];
  }
  return `${raw.slice(0, 5)}-${raw.slice(5)}`;
}

function getTwoFactorRow(userId) {
  return db.prepare('SELECT * FROM user_two_factor WHERE user_id = ?').get(userId) || null;
}

function getTwoFactorStatus(userId) {
  const row = getTwoFactorRow(userId);
  const recoveryCodesRemaining = db.prepare(`
    SELECT COUNT(*) AS count
    FROM user_recovery_codes
    WHERE user_id = ? AND used_at IS NULL
  `).get(userId).count;
  return {
    enabled: row?.enabled === 1,
    pending: Boolean(row?.pending_secret),
    enabledAt: row?.enabled_at || null,
    recoveryCodesRemaining,
  };
}

// Replacing a factor that is already on takes that factor (a TOTP or a
// recovery code), like turning it off does; the password alone is not enough.
async function assertCurrentFactor(userId, code) {
  if (getTwoFactorRow(userId)?.enabled !== 1) return;
  if (!String(code || '').trim()) {
    throw httpError(401, 'Enter a code from your current authenticator or a recovery code.', 'TWO_FACTOR_REQUIRED');
  }
  if (!(await verifyLoginCode(userId, code))) throw httpError(401, 'Invalid 2FA code', 'INVALID_CODE');
}

/**
 * Asks again who is there before something that can't be undone (exporting
 * or deleting the account): the password, plus a code when 2FA is on.
 */
async function confirmIdentity(userId, { currentPassword, code }) {
  await verifyCurrentPassword(userId, currentPassword);
  await assertCurrentFactor(userId, code);
}

async function beginSetup(userId, currentPassword, currentCode) {
  await verifyCurrentPassword(userId, currentPassword);
  await assertCurrentFactor(userId, currentCode);
  const user = db.prepare('SELECT username, email FROM users WHERE id = ?').get(userId);
  if (!user) {
    const error = new Error('User not found');
    error.statusCode = 404;
    throw error;
  }

  const secret = generateSecret();
  const accountName = user.email || user.username || `user-${userId}`;
  const otpauthUrl = generateURI({
    ...TOTP_OPTIONS,
    issuer: 'NeoCloud',
    label: accountName,
    secret,
  });
  db.prepare(`
    INSERT INTO user_two_factor (user_id, pending_secret, enabled, created_at, updated_at)
    VALUES (?, ?, COALESCE((SELECT enabled FROM user_two_factor WHERE user_id = ?), 0), datetime('now'), datetime('now'))
    ON CONFLICT(user_id) DO UPDATE SET
      pending_secret = excluded.pending_secret,
      updated_at = datetime('now')
  `).run(userId, encryptValue(secret), userId);

  return {
    otpauthUrl,
    manualKey: secret,
  };
}

function verifyTotp(secret, code) {
  const token = normalizeCode(code);
  if (!/^\d{6}$/.test(token)) return false;
  const result = verifySync({
    ...TOTP_OPTIONS,
    secret,
    token,
  });
  return result?.valid === true;
}

async function storeRecoveryCodes(userId) {
  const codes = Array.from({ length: 10 }, generateRecoveryCode);
  const hashes = await Promise.all(codes.map((code) => bcrypt.hash(normalizeRecoveryCode(code), 12)));
  const insert = db.prepare(`
    INSERT INTO user_recovery_codes (user_id, code_hash, created_at)
    VALUES (?, ?, datetime('now'))
  `);
  const tx = db.transaction(() => {
    db.prepare('DELETE FROM user_recovery_codes WHERE user_id = ?').run(userId);
    for (const hash of hashes) {
      insert.run(userId, hash);
    }
  });
  tx();
  return codes;
}

async function enable(userId, code, currentCode) {
  const row = getTwoFactorRow(userId);
  if (!row?.pending_secret) {
    const error = new Error('No 2FA setup is pending');
    error.statusCode = 400;
    throw error;
  }
  const secret = decryptValue(row.pending_secret);
  if (!verifyTotp(secret, code)) {
    const error = new Error('Invalid 2FA code');
    error.statusCode = 401;
    throw error;
  }
  await assertCurrentFactor(userId, currentCode);
  const recoveryCodes = await storeRecoveryCodes(userId);
  db.prepare(`
    UPDATE user_two_factor
    SET secret = pending_secret,
        pending_secret = NULL,
        enabled = 1,
        enabled_at = datetime('now'),
        updated_at = datetime('now')
    WHERE user_id = ?
  `).run(userId);
  return {
    success: true,
    recoveryCodes,
    status: getTwoFactorStatus(userId),
  };
}

async function consumeRecoveryCode(userId, code) {
  const normalized = normalizeRecoveryCode(code);
  if (normalized.length < 6) return false;
  const rows = db.prepare(`
    SELECT id, code_hash
    FROM user_recovery_codes
    WHERE user_id = ? AND used_at IS NULL
    ORDER BY id ASC
  `).all(userId);
  for (const row of rows) {
    if (await bcrypt.compare(normalized, row.code_hash)) {
      db.prepare('UPDATE user_recovery_codes SET used_at = datetime(\'now\') WHERE id = ? AND user_id = ?')
        .run(row.id, userId);
      return true;
    }
  }
  return false;
}

async function verifyLoginCode(userId, code) {
  const row = getTwoFactorRow(userId);
  if (!row || row.enabled !== 1 || !row.secret) return true;
  const secret = decryptValue(row.secret);
  if (verifyTotp(secret, code)) return true;
  return consumeRecoveryCode(userId, code);
}

/**
 * The second step of a sign-in: one authenticator or recovery code for the
 * pending sign-in `challengeId`, at most five per challenge on top of the
 * account-wide throttle. Once throttle.challengeExhausted(challengeId) the
 * caller drops the pending sign-in; `restart` says how to begin again.
 */
async function verifySignInCode({ userId, challengeId, code, restart }) {
  if (throttle.challengeExhausted(challengeId)) {
    throw httpError(401, `Too many wrong codes. Sign in with ${restart} again.`, 'TWO_FACTOR_ATTEMPTS_EXCEEDED');
  }
  const key = throttle.accountKey({ id: userId });
  throttle.assertSignInAllowed(key);
  throttle.recordFailure(key, { kind: 'second_factor', challengeId });
  if (!(await verifyLoginCode(userId, String(code)))) {
    throw httpError(401, 'Invalid 2FA code', 'INVALID_CODE');
  }
  throttle.clearFailures(key);
}

async function disable(userId, { currentPassword, code }) {
  await verifyCurrentPassword(userId, currentPassword);
  const row = getTwoFactorRow(userId);
  if (!row || row.enabled !== 1) {
    return { success: true, status: getTwoFactorStatus(userId) };
  }
  if (!(await verifyLoginCode(userId, code))) {
    const error = new Error('Invalid 2FA code');
    error.statusCode = 401;
    throw error;
  }
  db.transaction(() => {
    db.prepare(`
      UPDATE user_two_factor
      SET enabled = 0,
          secret = NULL,
          pending_secret = NULL,
          enabled_at = NULL,
          updated_at = datetime('now')
      WHERE user_id = ?
    `).run(userId);
    db.prepare('DELETE FROM user_recovery_codes WHERE user_id = ?').run(userId);
  })();
  return { success: true, status: getTwoFactorStatus(userId) };
}

async function regenerateRecoveryCodes(userId, { currentPassword, code }) {
  await verifyCurrentPassword(userId, currentPassword);
  if (!(await verifyLoginCode(userId, code))) {
    const error = new Error('Invalid 2FA code');
    error.statusCode = 401;
    throw error;
  }
  const recoveryCodes = await storeRecoveryCodes(userId);
  return { success: true, recoveryCodes, status: getTwoFactorStatus(userId) };
}

module.exports = {
  beginSetup,
  confirmIdentity,
  disable,
  enable,
  getTwoFactorStatus,
  regenerateRecoveryCodes,
  verifyLoginCode,
  verifySignInCode,
};
