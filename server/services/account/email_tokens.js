'use strict';

// The single-use links that go out by mail: sign-up confirmation, email
// change (also used to confirm an address that was never confirmed) and
// password reset. A token is 32 random bytes; only its SHA-256 is stored, so
// a copy of the database can't spend one. Issuing a token spends the older
// ones of the same account and type, and a token is spent by an atomic
// UPDATE ... WHERE consumed_at IS NULL, so two clicks racing never both win.
//
// A signed-up account is "waiting for confirmation" while it holds an unspent
// sign-up token. An address that was never asked to confirm (an account made
// while mail was off) has none, so it is never locked out; it can still
// confirm through its settings (email_change_confirmation to its own address).

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { getEmailConfig } = require('./service_email');
const { hashPassword, validateNewPassword } = require('./credentials');
const { revokeAllSessionsForUser } = require('./sessions');
const { revokeAllDeviceTokens } = require('../devices/tokens');
const throttle = require('./login_throttle');

const TOKEN_BYTES = 32;
const SIGNUP = 'signup_confirmation';
const EMAIL_CHANGE = 'email_change_confirmation';
const PASSWORD_RESET = 'password_reset';

function tokenHash(token) {
  return crypto.createHash('sha256').update(String(token || '')).digest('hex');
}

// SQLite's own datetime format (UTC, no zone), so it compares with datetime('now').
function sqliteDateFromMs(ms) {
  return new Date(ms).toISOString().replace('T', ' ').replace(/\.\d{3}Z$/, '');
}

function linkTo(pathname, token) {
  const url = new URL(pathname, `${getEmailConfig().publicUrl}/`);
  url.searchParams.set('token', token);
  return url.toString();
}

function confirmationUrl(token) {
  return linkTo('/api/auth/email/confirm', token);
}

function passwordResetUrl(token) {
  return linkTo('/api/auth/password/reset', token);
}

/** Issues a token for the account and spends its older ones of the same type. */
function createEmailToken(userId, type, email) {
  const token = crypto.randomBytes(TOKEN_BYTES).toString('base64url');
  const expiresAt = sqliteDateFromMs(Date.now() + getEmailConfig().tokenTtlHours * 60 * 60 * 1000);
  db.transaction(() => {
    db.prepare(`
      UPDATE user_email_tokens SET consumed_at = datetime('now')
      WHERE user_id = ? AND type = ? AND consumed_at IS NULL
    `).run(userId, type);
    db.prepare(`
      INSERT INTO user_email_tokens (user_id, type, email, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)
    `).run(userId, type, email, tokenHash(token), expiresAt);
  })();
  return { token, expiresAt };
}

/** Tokens of this type issued to the account in the last `windowMs`: the send throttle's count. */
function recentTokenCount(userId, type, windowMs) {
  return db.prepare(`
    SELECT COUNT(*) AS count FROM user_email_tokens
    WHERE user_id = ? AND type = ? AND datetime(created_at) > datetime(?)
  `).get(userId, type, sqliteDateFromMs(Date.now() - windowMs)).count;
}

/** Withdraws every unspent token of the given types (the link itself is no longer wanted). */
function spendTokens(userId, types) {
  const marks = types.map(() => '?').join(', ');
  db.prepare(`
    UPDATE user_email_tokens SET consumed_at = datetime('now')
    WHERE user_id = ? AND type IN (${marks}) AND consumed_at IS NULL
  `).run(userId, ...types);
}

/** The new address an unspent, unexpired email change is waiting for, or null. */
function pendingEmailFor(userId) {
  const row = db.prepare(`
    SELECT t.email FROM user_email_tokens t JOIN users u ON u.id = t.user_id
    WHERE t.user_id = ? AND t.type = ? AND t.consumed_at IS NULL
      AND datetime(t.expires_at) > datetime('now') AND lower(t.email) != lower(COALESCE(u.email, ''))
    ORDER BY t.id DESC LIMIT 1
  `).get(userId, EMAIL_CHANGE);
  return row ? row.email : null;
}

function findLiveToken(token, types) {
  const marks = types.map(() => '?').join(', ');
  return db.prepare(`
    SELECT t.*, u.username, u.email AS current_email
    FROM user_email_tokens t JOIN users u ON u.id = t.user_id
    WHERE t.token_hash = ? AND t.type IN (${marks}) AND t.consumed_at IS NULL
      AND datetime(t.expires_at) > datetime('now')
  `).get(tokenHash(token), ...types);
}

function invalidLink(what) {
  return httpError(400, `${what} link is invalid or expired`, 'LINK_INVALID');
}

/** Whether a confirmation link is still good, without spending it (the GET page). */
function isConfirmationLinkLive(token) {
  return Boolean(token) && Boolean(findLiveToken(token, [SIGNUP, EMAIL_CHANGE]));
}

/** Whether a reset link is still good, without spending it. */
function isPasswordResetLinkLive(token) {
  return Boolean(token) && Boolean(findLiveToken(token, [PASSWORD_RESET]));
}

/**
 * Spends a confirmation link and applies it: marks the address verified
 * (sign-up) or replaces the account's address (email change). Returns
 * { type, email, previousEmail, user }; `type` is 'email_changed' only when
 * the address really changed. Throws LINK_INVALID (400) or EMAIL_TAKEN (409,
 * the link stays unspent).
 */
function consumeConfirmationToken(token) {
  return db.transaction(() => {
    const row = token ? findLiveToken(token, [SIGNUP, EMAIL_CHANGE]) : null;
    if (!row) throw invalidLink('Confirmation');
    const claimed = db.prepare(`
      UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE id = ? AND consumed_at IS NULL
    `).run(row.id);
    if (claimed.changes !== 1) throw invalidLink('Confirmation');
    const sameAddress = String(row.current_email || '').toLowerCase() === row.email.toLowerCase();
    if (row.type === SIGNUP && !sameAddress) throw invalidLink('Confirmation');
    if (row.type === EMAIL_CHANGE && !sameAddress) {
      const taken = db.prepare('SELECT 1 FROM users WHERE lower(email) = lower(?) AND id != ?').get(row.email, row.user_id);
      if (taken) throw httpError(409, 'Email is already in use', 'EMAIL_TAKEN');
    }
    try {
      db.prepare(`
        UPDATE users SET email = ?, email_verified_at = datetime('now') WHERE id = ?
      `).run(row.email, row.user_id);
    } catch (err) {
      // Lost a race for the address; the unique index decides.
      if (String(err?.code || '').startsWith('SQLITE_CONSTRAINT')) throw httpError(409, 'Email is already in use', 'EMAIL_TAKEN');
      throw err;
    }
    // Whatever else was waiting on the old address is moot now.
    spendTokens(row.user_id, [SIGNUP, EMAIL_CHANGE]);
    if (row.type === EMAIL_CHANGE && !sameAddress) spendTokens(row.user_id, [PASSWORD_RESET]);
    return {
      type: row.type === EMAIL_CHANGE && !sameAddress ? 'email_changed' : 'email_verified',
      email: row.email,
      previousEmail: row.current_email || null,
      user: { id: row.user_id, username: row.username },
    };
  })();
}

/**
 * Sets a new password from a reset link. The password is checked against the
 * account's own username and email first (a bad one leaves the link usable),
 * then the link is spent in the same write that sets the password, so two
 * submissions racing past the lookup can't both win. Everything that could
 * still sign in with the old password ends: every session, device token and
 * WebDAV app password, and the account's failed-sign-in counters are cleared
 * so the owner is not locked out by whoever was guessing. Owning the inbox
 * proves the address, so it is marked verified.
 */
async function consumePasswordResetToken(token, password) {
  const row = token ? findLiveToken(token, [PASSWORD_RESET]) : null;
  if (!row) throw invalidLink('Password reset');
  if (typeof password !== 'string' || !password) throw httpError(400, 'A new password is required.', 'MISSING_FIELDS');
  validateNewPassword(password, { username: row.username, email: row.current_email });
  const passwordHash = await hashPassword(password);
  const applied = db.transaction(() => {
    const claimed = db.prepare(`
      UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE id = ? AND consumed_at IS NULL
    `).run(row.id);
    if (claimed.changes !== 1) return false;
    // The token went to an address the account may have left since.
    const user = db.prepare('SELECT email FROM users WHERE id = ?').get(row.user_id);
    if (!user || String(user.email || '').toLowerCase() !== row.email.toLowerCase()) return false;
    db.prepare(`
      UPDATE users SET password = ?, email_verified_at = COALESCE(email_verified_at, datetime('now')) WHERE id = ?
    `).run(passwordHash, row.user_id);
    spendTokens(row.user_id, [PASSWORD_RESET, SIGNUP]);
    return true;
  })();
  if (!applied) throw invalidLink('Password reset');
  revokeAllSessionsForUser(row.user_id);
  revokeAllDeviceTokens(row.user_id);
  throttle.clearFailures(throttle.accountKey({ id: row.user_id }));
  throttle.clearFailures(throttle.confirmKey(row.user_id));
  return { user: { id: row.user_id, username: row.username, email: row.current_email } };
}

/** An admin vouches for the address (or the mail could not be delivered). */
function markEmailVerified(userId) {
  db.transaction(() => {
    db.prepare("UPDATE users SET email_verified_at = COALESCE(email_verified_at, datetime('now')) WHERE id = ?").run(userId);
    spendTokens(userId, [SIGNUP]);
  })();
}

module.exports = {
  EMAIL_CHANGE,
  PASSWORD_RESET,
  SIGNUP,
  confirmationUrl,
  consumeConfirmationToken,
  consumePasswordResetToken,
  createEmailToken,
  isConfirmationLinkLive,
  isPasswordResetLinkLive,
  markEmailVerified,
  passwordResetUrl,
  pendingEmailFor,
  recentTokenCount,
  spendTokens,
};
