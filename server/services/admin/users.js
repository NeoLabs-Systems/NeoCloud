'use strict';

const db = require('../../db/database');
const { recordAccessEvent } = require('../access/audit');
const { setAdmin } = require('../access/admin');
const { hashPassword, validateNewPassword } = require('../account/credentials');
const { revokeAllSessionsForUser } = require('../account/sessions');
const { revokeAllDeviceTokens } = require('../devices/tokens');
const { eraseAccount, isLastActiveAdmin } = require('../account/erasure');
const { UNLIMITED, effectiveQuota, recalculateUsage } = require('../storage/quota');
const { httpError } = require('../../utils/http_error');
const { parseByteSize } = require('../../utils/bytes');

function serializeUser(row) {
  const quota = effectiveQuota(row.quota_bytes);
  return {
    id: row.id,
    username: row.username,
    displayName: row.display_name,
    email: row.email,
    isAdmin: Number(row.is_admin) === 1,
    disabled: Boolean(row.disabled_at),
    disabledAt: row.disabled_at,
    usedBytes: row.used_bytes,
    quotaBytes: quota,
    quotaMode: row.quota_bytes === null ? 'default' : (row.quota_bytes === UNLIMITED ? 'unlimited' : 'custom'),
    fileCount: row.file_count ?? 0,
    // The account's own AI token budget: null follows the server default, 0 is no limit.
    aiMonthlyTokens: row.ai_monthly_tokens ?? null,
    createdAt: row.created_at,
    lastLogin: row.last_login,
  };
}

function listUsers(search) {
  const pattern = search ? `%${search}%` : null;
  const rows = db.prepare(`
    SELECT u.*, (SELECT COUNT(*) FROM nodes n WHERE n.user_id = u.id AND n.type = 'file') AS file_count
    FROM users u
    WHERE ? IS NULL OR u.username LIKE ? OR u.email LIKE ?
    ORDER BY u.created_at DESC
    LIMIT 500
  `).all(pattern, pattern, pattern);
  return { users: rows.map(serializeUser) };
}

function getUserRow(userId) {
  const row = db.prepare('SELECT * FROM users WHERE id = ?').get(userId);
  if (!row) throw httpError(404, 'User not found', 'NOT_FOUND');
  return row;
}

/**
 * mode "default" follows the server default, "unlimited" removes the limit,
 * "custom" takes `bytes` (a number or a size string like "50GB"). A quota
 * below current usage is allowed: the account just can't add more.
 */
function setUserQuota(userId, { mode, bytes }, { actorUserId }) {
  getUserRow(userId);
  let value;
  if (mode === 'default') {
    value = null;
  } else if (mode === 'unlimited') {
    value = UNLIMITED;
  } else if (mode === 'custom') {
    value = parseByteSize(bytes);
    if (value === null) throw httpError(400, 'Enter the quota as a size, e.g. 50GB.', 'INVALID_QUOTA');
  } else {
    throw httpError(400, 'mode must be default, unlimited or custom.', 'INVALID_QUOTA_MODE');
  }
  db.prepare('UPDATE users SET quota_bytes = ? WHERE id = ?').run(value, userId);
  recordAccessEvent('account.quota', { actorUserId, subjectUserId: userId, detail: { mode, bytes: value } });
  return serializeUser(getUserRow(userId));
}

function setUserAdmin(userId, isAdmin, { actorUserId }) {
  // Anything but a boolean (a missing field, "true", 1) must not demote.
  if (typeof isAdmin !== 'boolean') throw httpError(400, 'isAdmin must be true or false.', 'INVALID_VALUE');
  if (userId === actorUserId && !isAdmin) {
    throw httpError(400, 'You can’t remove your own admin rights.', 'SELF_DEMOTE');
  }
  setAdmin(userId, Boolean(isAdmin), { actorUserId, source: 'app' });
  return serializeUser(getUserRow(userId));
}

async function resetUserPassword(userId, password, { actorUserId }) {
  const user = getUserRow(userId);
  if (typeof password !== 'string' || !password) throw httpError(400, 'A new password is required.', 'MISSING_FIELDS');
  const value = password;
  validateNewPassword(value, { username: user.username, email: user.email });
  const hash = await hashPassword(value);
  db.transaction(() => {
    db.prepare('UPDATE users SET password = ? WHERE id = ?').run(hash, user.id);
    recordAccessEvent('account.password_reset', { actorUserId, subjectUserId: user.id });
  })();
  // Signs the account out everywhere: browser sessions, devices, app passwords.
  revokeAllSessionsForUser(user.id);
  revokeAllDeviceTokens(user.id);
  return { success: true };
}

/**
 * Deletes an account with all its files (services/account/erasure.js).
 * Admin accounts must lose admin first, so one click can't remove an
 * administrator.
 */
async function deleteUser(userId, { actorUserId }) {
  const user = getUserRow(userId);
  if (Number(user.is_admin) === 1) {
    throw httpError(403, 'Remove admin rights before deleting this account.', 'ADMIN_ACCOUNT');
  }
  return eraseAccount(user.id, { actorUserId });
}

/**
 * Disables or enables an account, as in NeoRouter. Disabling ends its
 * sessions and device tokens at once (they stay revoked when the account is
 * enabled again); while disabled it can't sign in by any route and its
 * public links don't serve. Nobody disables themselves or the last admin.
 */
function setUserDisabled(userId, disabled, { actorUserId }) {
  if (typeof disabled !== 'boolean') throw httpError(400, 'disabled must be true or false.', 'INVALID_VALUE');
  const user = getUserRow(userId);
  if (disabled && userId === actorUserId) {
    throw httpError(400, 'You can’t disable your own account.', 'CANNOT_DISABLE_SELF');
  }
  if (disabled && isLastActiveAdmin(user)) {
    throw httpError(409, 'The last admin can’t be disabled.', 'LAST_ADMIN');
  }
  if (Boolean(user.disabled_at) !== disabled) {
    db.transaction(() => {
      db.prepare("UPDATE users SET disabled_at = CASE WHEN ? THEN datetime('now') END WHERE id = ?").run(disabled ? 1 : 0, user.id);
      recordAccessEvent(disabled ? 'account.disable' : 'account.enable', { actorUserId, subjectUserId: user.id });
    })();
    if (disabled) signOutEverywhere(user.id);
  }
  return serializeUser(getUserRow(userId));
}

/** NeoAgent's "Sign out everywhere": every browser session and device token. */
function signOutEverywhere(userId, { keepSessionId = null, keepDeviceId = null } = {}) {
  const sessions = revokeAllSessionsForUser(userId, { exceptSessionId: keepSessionId }).revoked;
  const devices = revokeAllDeviceTokens(userId, { exceptDeviceId: keepDeviceId });
  return { success: true, sessions, devices };
}

function revokeUserSessions(userId, { actorUserId, sessionId, deviceId }) {
  getUserRow(userId);
  // An admin signing themselves out everywhere keeps the browser session or
  // phone they asked from.
  const self = userId === actorUserId;
  const result = signOutEverywhere(userId, {
    keepSessionId: self ? sessionId : null,
    keepDeviceId: self ? deviceId : null,
  });
  recordAccessEvent('account.sign_out_everywhere', { actorUserId, subjectUserId: userId });
  return result;
}

function recalculateUserUsage(userId) {
  getUserRow(userId);
  recalculateUsage(userId);
  return serializeUser(getUserRow(userId));
}

module.exports = {
  deleteUser,
  listUsers,
  recalculateUserUsage,
  resetUserPassword,
  revokeUserSessions,
  setUserAdmin,
  setUserDisabled,
  setUserQuota,
};
