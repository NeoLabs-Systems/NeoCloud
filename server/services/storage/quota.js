'use strict';

// How much an account may store. used_bytes counts every blob the account
// owns -- current files, kept versions and trash -- because all of it takes
// disk space. Copies share a blob and cost nothing extra.

const db = require('../../db/database');
const { getStorageConfig } = require('../../config/storage');
const { httpError } = require('../../utils/http_error');

const UNLIMITED = -1;
const DEFAULT_MAX_NODES = 1000000;
const DEFAULT_MAX_COPY_NODES = 50000;

function readPositiveInt(value, fallback) {
  const parsed = Number.parseInt(String(value ?? '').trim(), 10);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}

/**
 * Bytes are not the only cost: every file and folder is a database row, and
 * copies share their blobs, so node counts have limits of their own.
 * NEOCLOUD_MAX_NODES caps an account's nodes (trash included),
 * NEOCLOUD_MAX_COPY_NODES one copy. Read at use time like the storage policy.
 */
function nodeLimits(env = process.env) {
  return {
    maxNodes: readPositiveInt(env.NEOCLOUD_MAX_NODES, DEFAULT_MAX_NODES),
    maxCopyNodes: readPositiveInt(env.NEOCLOUD_MAX_COPY_NODES, DEFAULT_MAX_COPY_NODES),
  };
}

/** Refuses `adding` more nodes past the account's limit: 507 NODE_LIMIT. Call inside the inserting transaction. */
function assertNodeRoom(userId, adding = 1) {
  const user = db.prepare('SELECT node_count FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  const { maxNodes } = nodeLimits();
  if (user.node_count + adding > maxNodes) {
    throw httpError(507, `This account has reached its limit of ${maxNodes} files and folders (trash included).`, 'NODE_LIMIT');
  }
}

// The quota that applies: the account's own value, or the server default.
function effectiveQuota(quotaBytes) {
  return quotaBytes === null || quotaBytes === undefined
    ? getStorageConfig().defaultQuotaBytes
    : quotaBytes;
}

function reservedBytes(userId) {
  return db.prepare(`
    SELECT COALESCE(SUM(total_size), 0) AS bytes FROM upload_sessions
    WHERE user_id = ? AND datetime(expires_at) > datetime('now')
  `).get(userId).bytes;
}

function getUsage(userId) {
  const user = db.prepare('SELECT quota_bytes, used_bytes FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  const quota = effectiveQuota(user.quota_bytes);
  return {
    usedBytes: user.used_bytes,
    quotaBytes: quota,
    unlimited: quota === UNLIMITED,
    customQuota: user.quota_bytes !== null,
    trashBytes: db.prepare(`
      WITH RECURSIVE trashed(id) AS (
        SELECT id FROM nodes WHERE user_id = ? AND trashed_at IS NOT NULL
        UNION SELECT n.id FROM nodes n JOIN trashed t ON n.parent_id = t.id
      )
      SELECT COALESCE(SUM(n.size), 0) AS bytes FROM nodes n JOIN trashed t ON t.id = n.id
      WHERE n.type = 'file'
    `).get(userId).bytes,
    versionBytes: db.prepare(`
      SELECT COALESCE(SUM(v.size), 0) AS bytes FROM file_versions v
      JOIN nodes n ON n.id = v.node_id WHERE n.user_id = ?
    `).get(userId).bytes,
  };
}

/**
 * Bytes the account can still add, after its stored data and uploads that
 * are in progress. Infinity when unlimited. `excludeUploadId` leaves one
 * upload's own reservation out, for that upload's final check.
 */
function remainingBytes(userId, { excludeUploadId = null } = {}) {
  const user = db.prepare('SELECT quota_bytes, used_bytes FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  const quota = effectiveQuota(user.quota_bytes);
  if (quota === UNLIMITED) return Number.POSITIVE_INFINITY;
  let reserved = reservedBytes(userId);
  if (excludeUploadId) {
    const own = db.prepare('SELECT total_size FROM upload_sessions WHERE id = ?').get(excludeUploadId);
    reserved -= own?.total_size || 0;
  }
  return Math.max(0, quota - user.used_bytes - reserved);
}

function quotaExceeded() {
  return httpError(413, 'Not enough storage left for this file.', 'QUOTA_EXCEEDED');
}

function fileTooLarge() {
  return httpError(413, 'This file is larger than the server allows.', 'FILE_TOO_LARGE');
}

/**
 * The most bytes one upload may carry right now, and the error to raise when
 * it goes over: the per-file limit or the space left, whichever is lower.
 */
function uploadLimit(userId, options) {
  const { maxUploadBytes } = getStorageConfig();
  const remaining = remainingBytes(userId, options);
  if (maxUploadBytes > 0 && maxUploadBytes <= remaining) {
    return { limit: maxUploadBytes, limitError: fileTooLarge };
  }
  return { limit: remaining, limitError: quotaExceeded };
}

function assertUploadFits(userId, size, options) {
  const { limit, limitError } = uploadLimit(userId, options);
  if (size > limit) throw limitError();
}

// Brings used_bytes back in line with the blobs actually recorded.
function recalculateUsage(userId) {
  const { bytes } = db.prepare('SELECT COALESCE(SUM(size), 0) AS bytes FROM blobs WHERE user_id = ?').get(userId);
  db.prepare('UPDATE users SET used_bytes = ? WHERE id = ?').run(bytes, userId);
  return bytes;
}

module.exports = {
  UNLIMITED,
  assertNodeRoom,
  assertUploadFits,
  effectiveQuota,
  getUsage,
  nodeLimits,
  recalculateUsage,
  remainingBytes,
  uploadLimit,
};
