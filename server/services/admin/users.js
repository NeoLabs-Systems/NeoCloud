'use strict';

const db = require('../../db/database');
const { recordAccessEvent } = require('../access/audit');
const { setAdmin } = require('../access/admin');
const { hashPassword, validateNewPassword } = require('../account/credentials');
const { revokeAllSessionsForUser } = require('../account/sessions');
const { revokeAllDeviceTokens } = require('../devices/tokens');
const { deleteUserBlobDirectory } = require('../storage/blob_store');
const { UNLIMITED, effectiveQuota, recalculateUsage } = require('../storage/quota');
const { httpError } = require('../../utils/http_error');
const { parseByteSize } = require('../../utils/bytes');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Admin');

function serializeUser(row) {
  const quota = effectiveQuota(row.quota_bytes);
  return {
    id: row.id,
    username: row.username,
    displayName: row.display_name,
    email: row.email,
    isAdmin: Number(row.is_admin) === 1,
    usedBytes: row.used_bytes,
    quotaBytes: quota,
    quotaMode: row.quota_bytes === null ? 'default' : (row.quota_bytes === UNLIMITED ? 'unlimited' : 'custom'),
    fileCount: row.file_count ?? 0,
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
  if (userId === actorUserId && !isAdmin) {
    throw httpError(400, 'You can’t remove your own admin rights.', 'SELF_DEMOTE');
  }
  setAdmin(userId, Boolean(isAdmin), { actorUserId, source: 'app' });
  return serializeUser(getUserRow(userId));
}

async function resetUserPassword(userId, password, { actorUserId }) {
  const user = getUserRow(userId);
  const value = String(password || '');
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
 * Deletes an account with all its files. Admin accounts must lose admin
 * first, so one click can't remove an administrator.
 */
async function deleteUser(userId, { actorUserId }) {
  const user = getUserRow(userId);
  if (Number(user.is_admin) === 1) {
    throw httpError(403, 'Remove admin rights before deleting this account.', 'ADMIN_ACCOUNT');
  }
  revokeAllSessionsForUser(user.id);
  db.transaction(() => {
    // Nodes reference blobs, so they go first; the rest cascades with the user.
    db.prepare('DELETE FROM nodes WHERE user_id = ? AND parent_id IS NULL').run(user.id);
    db.prepare('DELETE FROM nodes WHERE user_id = ?').run(user.id);
    db.prepare('DELETE FROM blobs WHERE user_id = ?').run(user.id);
    db.prepare('DELETE FROM users WHERE id = ?').run(user.id);
    recordAccessEvent('account.delete', { actorUserId, detail: { userId: user.id } });
  })();
  await deleteUserBlobDirectory(user.id);
  logger.info(`Admin ${actorUserId} deleted account ${user.id}`);
  return { success: true };
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
  setUserAdmin,
  setUserQuota,
};
