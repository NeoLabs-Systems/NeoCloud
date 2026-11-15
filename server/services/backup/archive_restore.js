'use strict';

// Objects in an archive storage class (S3 Glacier Flexible Retrieval, Deep
// Archive) can't be read until Glacier makes a temporary copy available.
// Before a restore downloads anything, every object it needs is HEADed;
// archived ones without a readable copy get a restore request (or are left
// to the request already running), and the restore stops until they are all
// available. Running the same command again continues.

const { eachLimit } = require('../../utils/each_limit');
const { isArchiveClass } = require('./adapters/s3');

const TIERS = new Set(['Standard', 'Bulk']);
const HEAD_CONCURRENCY = 16;

/**
 * → { requested, pending, archived }: restores asked for now, restores
 * Glacier is still working on, and archived objects in all. Nothing to wait
 * for when requested + pending is 0. Targets that can't archive (anything
 * but S3) answer zeros without a request.
 */
async function wakeArchivedObjects(adapter, keys, { tier = 'Standard' } = {}) {
  const counts = { requested: 0, pending: 0, archived: 0 };
  if (typeof adapter.stat !== 'function') return counts;
  if (!TIERS.has(tier)) throw new Error('The restore tier must be Standard or Bulk.');
  await eachLimit(keys, HEAD_CONCURRENCY, async (key) => {
    const stat = await adapter.stat(key);
    if (!stat || !isArchiveClass(stat.storageClass)) return;
    counts.archived += 1;
    if (stat.restore === 'available') return;
    if (stat.restore === 'ongoing') {
      counts.pending += 1;
      return;
    }
    await adapter.requestRestore(key, tier);
    counts.requested += 1;
  });
  return counts;
}

module.exports = { wakeArchivedObjects };
