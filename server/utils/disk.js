'use strict';

// Free space on the storage disk. Uploads are refused while it is below
// NEOCLOUD_MIN_FREE_DISK (default 1 GiB), so a full disk never leaves the
// database or a half-written blob behind.

const fs = require('fs');
const { STORAGE_DIR } = require('../../runtime/paths');
const { httpError } = require('./http_error');
const { parseByteSize } = require('./bytes');

const DEFAULT_MIN_FREE_BYTES = 1024 ** 3;

function minFreeDiskBytes(env = process.env) {
  return parseByteSize(env.NEOCLOUD_MIN_FREE_DISK) ?? DEFAULT_MIN_FREE_BYTES;
}

/** Free bytes for this process on the disk holding `directory`, or null if unknown. */
function freeDiskBytes(directory = STORAGE_DIR) {
  try {
    const stats = fs.statfsSync(directory);
    return stats.bavail * stats.bsize;
  } catch {
    return null;
  }
}

/** Throws 507 DISK_FULL when writing `bytes` more would dip below the floor. */
function assertDiskSpace(bytes = 0) {
  const free = freeDiskBytes();
  if (free === null) return;
  if (free - Math.max(0, Number(bytes) || 0) < minFreeDiskBytes()) {
    throw httpError(507, 'The server is running out of disk space and cannot take uploads right now. Ask the admin to free some space.', 'DISK_FULL');
  }
}

module.exports = { assertDiskSpace, freeDiskBytes, minFreeDiskBytes };
