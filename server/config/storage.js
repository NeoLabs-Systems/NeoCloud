'use strict';

// Server-wide storage policy, read from the environment at use time so an
// admin change (written to .env and process.env) applies without a restart.

const { parseByteSize } = require('../utils/bytes');

const DEFAULTS = Object.freeze({
  defaultQuotaBytes: 10 * 1024 ** 3,
  maxUploadBytes: 0,
  trashRetentionDays: 30,
  versionRetention: 10,
});

// NEOCLOUD_DEFAULT_QUOTA: a size ("25GB") or "unlimited". Unlimited is -1.
function readDefaultQuota(env) {
  const raw = String(env.NEOCLOUD_DEFAULT_QUOTA ?? '').trim().toLowerCase();
  if (!raw) return DEFAULTS.defaultQuotaBytes;
  if (raw === 'unlimited' || raw === '-1') return -1;
  return parseByteSize(raw) ?? DEFAULTS.defaultQuotaBytes;
}

function readNonNegativeInt(value, fallback) {
  const parsed = Number.parseInt(String(value ?? '').trim(), 10);
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : fallback;
}

/**
 * maxUploadBytes 0 means no per-file limit beyond the account's quota.
 * trashRetentionDays 0 keeps trash until emptied by hand.
 * versionRetention is how many previous versions of a file are kept.
 */
function getStorageConfig(env = process.env) {
  return {
    defaultQuotaBytes: readDefaultQuota(env),
    maxUploadBytes: parseByteSize(String(env.NEOCLOUD_MAX_UPLOAD_SIZE ?? '').trim() || '0') ?? DEFAULTS.maxUploadBytes,
    trashRetentionDays: readNonNegativeInt(env.NEOCLOUD_TRASH_RETENTION_DAYS, DEFAULTS.trashRetentionDays),
    versionRetention: readNonNegativeInt(env.NEOCLOUD_VERSION_RETENTION, DEFAULTS.versionRetention),
  };
}

module.exports = { getStorageConfig };
