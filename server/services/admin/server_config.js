'use strict';

const fs = require('fs');
const db = require('../../db/database');
const { STORAGE_DIR, DATA_DIR } = require('../../../runtime/paths');
const { getStorageConfig } = require('../../config/storage');
const { getAllowSignup } = require('../../config/access');
const { recordAccessEvent } = require('../access/audit');
const { confirmIdentity } = require('../account/two_factor');
const { recalculateUsage } = require('../storage/quota');
const { persistEnv } = require('./env_config');
const { httpError } = require('../../utils/http_error');
const { parseByteSize } = require('../../utils/bytes');
const { getServerVersion } = require('../../utils/version');
const { getCapabilities } = require('../preview/previews');
const { getDatabaseCheck } = require('../maintenance');
const { getScrubStatus } = require('../storage/blob_scrub');
const { minFreeDiskBytes } = require('../../utils/disk');
const { ENABLED_ENV: FULL_TEXT_SEARCH_ENV, isIndexEnabled, wakeIndexer } = require('../search_index/indexer');

function getStorageSettings() {
  return getStorageConfig();
}

function nonNegativeInt(value, field) {
  const number = Number(value);
  if (!Number.isInteger(number) || number < 0) throw httpError(400, `${field} must be a whole number ≥ 0.`, 'INVALID_VALUE');
  return number;
}

// Each field is optional; sizes accept numbers or strings like "20GB".
function updateStorageSettings(body = {}) {
  const updates = {};
  if (body.defaultQuotaBytes !== undefined) {
    if (body.defaultQuotaBytes === -1 || body.defaultQuotaBytes === 'unlimited') {
      updates.NEOCLOUD_DEFAULT_QUOTA = 'unlimited';
    } else {
      const bytes = parseByteSize(body.defaultQuotaBytes);
      if (bytes === null) throw httpError(400, 'Default quota must be a size.', 'INVALID_VALUE');
      updates.NEOCLOUD_DEFAULT_QUOTA = String(bytes);
    }
  }
  if (body.maxUploadBytes !== undefined) {
    const bytes = parseByteSize(body.maxUploadBytes);
    if (bytes === null) throw httpError(400, 'Max upload size must be a size (0 for none).', 'INVALID_VALUE');
    updates.NEOCLOUD_MAX_UPLOAD_SIZE = String(bytes);
  }
  if (body.trashRetentionDays !== undefined) {
    updates.NEOCLOUD_TRASH_RETENTION_DAYS = String(nonNegativeInt(body.trashRetentionDays, 'trashRetentionDays'));
  }
  if (body.versionRetention !== undefined) {
    updates.NEOCLOUD_VERSION_RETENTION = String(nonNegativeInt(body.versionRetention, 'versionRetention'));
  }
  for (const [key, value] of Object.entries(updates)) persistEnv(key, value);
  return getStorageConfig();
}

function getAccessSettings() {
  return { allowSignup: getAllowSignup() };
}

function setAllowSignup(allow) {
  if (typeof allow !== 'boolean') throw httpError(400, 'allowSignup must be true or false.', 'INVALID_VALUE');
  persistEnv('NEOCLOUD_ALLOW_SIGNUP', allow ? 'true' : 'false');
  return getAccessSettings();
}

function diskSpace(directory) {
  try {
    const stats = fs.statfsSync(directory);
    return { totalBytes: stats.blocks * stats.bsize, freeBytes: stats.bavail * stats.bsize };
  } catch {
    return null;
  }
}

// Cheap by design: the database check and the blob scrub run on the
// maintenance timer; this only reports their last results.
async function getHealth() {
  const totals = db.prepare(`
    SELECT (SELECT COUNT(*) FROM users) AS users,
           (SELECT COUNT(*) FROM nodes WHERE type = 'file') AS files,
           (SELECT COALESCE(SUM(used_bytes), 0) FROM users) AS used_bytes,
           (SELECT COUNT(*) FROM upload_sessions) AS uploads
  `).get();
  const databaseCheck = getDatabaseCheck();
  const scrub = getScrubStatus();
  return {
    ...getServerVersion(),
    uptimeSeconds: Math.round(process.uptime()),
    // 'ok' | 'damaged' | 'pending' (no quick_check since the start yet).
    database: databaseCheck.status,
    databaseCheckedAt: databaseCheck.checkedAt,
    damagedBlobs: scrub.damaged,
    blobScrubCycleAt: scrub.lastCycleAt,
    minFreeDiskBytes: minFreeDiskBytes(),
    users: totals.users,
    files: totals.files,
    usedBytes: totals.used_bytes,
    activeUploads: totals.uploads,
    storageDisk: diskSpace(STORAGE_DIR),
    dataDisk: diskSpace(DATA_DIR),
    previews: await getCapabilities(),
  };
}

function recalculateAllUsage() {
  const users = db.prepare('SELECT id FROM users').all();
  for (const { id } of users) recalculateUsage(id);
  return { success: true, users: users.length };
}

/**
 * The master key, for an admin who confirmed their password and, with 2FA
 * on, a code. Without it a backup of the storage directory can never be
 * decrypted.
 */
async function exportMasterKey(userId, { currentPassword, code }) {
  await confirmIdentity(userId, { currentPassword, code });
  recordAccessEvent('keys.master_export', { actorUserId: userId });
  return { masterKey: process.env.NEOCLOUD_MASTER_KEY };
}

/** Server-wide switches: `fullTextSearch` (off by default). */
function getServerConfig() {
  return { fullTextSearch: isIndexEnabled() };
}

function updateServerConfig(body = {}) {
  if (body.fullTextSearch !== undefined) {
    if (typeof body.fullTextSearch !== 'boolean') {
      throw httpError(400, 'fullTextSearch must be true or false.', 'INVALID_VALUE');
    }
    persistEnv(FULL_TEXT_SEARCH_ENV, body.fullTextSearch ? 'true' : 'false');
    if (body.fullTextSearch) wakeIndexer();
  }
  return getServerConfig();
}

module.exports = {
  exportMasterKey,
  getAccessSettings,
  getHealth,
  getServerConfig,
  getStorageSettings,
  recalculateAllUsage,
  setAllowSignup,
  updateServerConfig,
  updateStorageSettings,
};
