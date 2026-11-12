'use strict';

// Account identity and password rules shared by sign-up, sign-in and the
// admin password reset. Failures are httpErrors with a stable `code` so
// clients can point at the right field without parsing the English text.

const bcrypt = require('bcrypt');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { isReservedAdminUsername } = require('../access/admin');
const { revokeAllDeviceTokens } = require('../devices/tokens');
const { revokeAllSessionsForUser } = require('./sessions');
const {
  MIN_PASSWORD_LENGTH,
  evaluatePasswordStrength,
  passwordStrengthError,
} = require('./password_policy');

const MIN_USERNAME_LENGTH = 3;
const MAX_USERNAME_LENGTH = 64;
const USERNAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const PASSWORD_HASH_ROUNDS = 12;
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

// Sign-in takes the username or the account email, ignoring case.
function findUserForSignIn(identifier) {
  const value = String(identifier || '').trim();
  if (!value) return null;
  return db.prepare(`
    SELECT * FROM users
    WHERE lower(username) = lower(?) OR lower(email) = lower(?)
    ORDER BY lower(username) = lower(?) DESC, id
    LIMIT 1
  `).get(value, value, value) || null;
}

function normalizeEmail(value) {
  return String(value || '').trim().toLowerCase();
}

function isValidEmail(value) {
  return value.length <= 254 && EMAIL_PATTERN.test(value);
}

function validateNewUsername(username) {
  if (username.length < MIN_USERNAME_LENGTH) {
    throw httpError(400, `Username must be at least ${MIN_USERNAME_LENGTH} characters.`, 'USERNAME_TOO_SHORT');
  }
  if (username.length > MAX_USERNAME_LENGTH || !USERNAME_PATTERN.test(username)) {
    throw httpError(400, 'Username may use letters, digits, dots, dashes and underscores.', 'USERNAME_INVALID');
  }
  const taken = db.prepare('SELECT 1 FROM users WHERE lower(username) = lower(?)').get(username);
  if (taken || isReservedAdminUsername(username)) {
    throw httpError(409, 'Username is already taken', 'USERNAME_TAKEN');
  }
}

function assertEmailAvailable(email) {
  if (db.prepare('SELECT 1 FROM users WHERE lower(email) = lower(?)').get(email)) {
    throw httpError(409, 'Email is already in use', 'EMAIL_TAKEN');
  }
}

// `context` carries the account's username/email so the password may not contain them.
function validateNewPassword(password, context = {}) {
  const strength = evaluatePasswordStrength(password, context);
  if (!strength.hasMinimumLength) {
    throw httpError(400, `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`, 'PASSWORD_TOO_SHORT');
  }
  if (!strength.isAcceptable) {
    throw httpError(400, passwordStrengthError(strength), 'PASSWORD_WEAK');
  }
}

function hashPassword(password) {
  return bcrypt.hash(password, PASSWORD_HASH_ROUNDS);
}

async function verifyPassword(userId, password) {
  const user = db.prepare('SELECT password FROM users WHERE id = ?').get(userId);
  if (!user || !(await bcrypt.compare(String(password || ''), user.password))) {
    throw httpError(401, 'Current password is incorrect', 'WRONG_PASSWORD');
  }
}

/**
 * A self-service password change. Whoever else knew the old password may
 * have signed in with it, so every other browser session, device token and
 * WebDAV app password ends here. The caller stays signed in: its browser
 * session (`keepSessionId`, which should then get a fresh id) or its phone
 * app token (`keepDeviceId`).
 */
async function changePassword(userId, { currentPassword, newPassword }, { keepSessionId = null, keepDeviceId = null }) {
  const user = db.prepare('SELECT username, email FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  if (typeof newPassword !== 'string' || !newPassword) {
    throw httpError(400, 'A new password is required.', 'MISSING_FIELDS');
  }
  const nextPassword = newPassword;
  await verifyPassword(userId, currentPassword);
  validateNewPassword(nextPassword, { username: user.username, email: user.email });
  if (nextPassword === String(currentPassword || '')) {
    throw httpError(400, 'Choose a password different from the current one.', 'PASSWORD_UNCHANGED');
  }
  db.prepare('UPDATE users SET password = ? WHERE id = ?').run(await hashPassword(nextPassword), userId);
  revokeAllSessionsForUser(userId, { exceptSessionId: keepSessionId });
  revokeAllDeviceTokens(userId, { exceptDeviceId: keepDeviceId });
}

module.exports = {
  assertEmailAvailable,
  changePassword,
  findUserForSignIn,
  hashPassword,
  isValidEmail,
  normalizeEmail,
  validateNewPassword,
  validateNewUsername,
  verifyPassword,
};
