'use strict';

// Proves the newest snapshot on a target can be restored, without touching
// this install: downloads and decrypts its database, runs quick_check on it,
// and checks that every blob its manifest lists is on the target. The result
// is kept per target and shown with it.
//
// On an archive storage class (S3 Glacier Flexible Retrieval, Deep Archive)
// nothing can be read without a restore, so verify checks metadata instead
// (mode 'metadata'): the manifest from this server's local copy, every blob
// it lists present in the listing, and the database object there with the
// exact size its encryption implies. Nothing is downloaded.

const fs = require('fs');
const path = require('path');
const { pipeline } = require('stream/promises');
const { UPLOAD_TMP_DIR, ensurePrivateDirectory } = require('../../../runtime/paths');
const { getBackupKey } = require('../crypto/keys');
const { removeQuietly } = require('../storage/blob_store');
const { sanitizeError } = require('../../utils/security');
const { createServiceLogger } = require('../../utils/logger');
const { readState, writeState } = require('../state_store');
const { getTargetRow, openTarget } = require('./targets');
const { encryptedSize } = require('../crypto/stream_cipher');
const { DATABASE_OBJECT, cachedManifest, listSnapshots, readManifest, snapshotIdFor } = require('./runner');
const { decryptDatabase, inspectDatabase } = require('./database_file');

const logger = createServiceLogger('BackupVerify');
const STATE = 'backup-verify';

function recordVerification(targetId, result) {
  const all = readState(STATE, {});
  all[targetId] = result;
  writeState(STATE, all);
}

function countMissingBlobs(manifest, remoteKeys, result) {
  const remote = new Set(remoteKeys);
  result.blobsChecked = manifest.blobs.length;
  result.missingBlobs = manifest.blobs.filter((blob) => !remote.has(`blobs/${blob}`)).length;
}

async function checkArchivedSnapshot(adapter, targetId, newest, result) {
  result.mode = 'metadata';
  const manifest = cachedManifest(targetId, newest);
  if (!manifest) {
    throw new Error(`Snapshot ${newest} is archived and this server has no local copy of its manifest; restore it to check it.`);
  }
  const stat = await adapter.stat(`snapshots/${newest}/${DATABASE_OBJECT}`);
  if (!stat) {
    result.database = 'missing';
  } else if (Number.isSafeInteger(manifest.databaseSize) && stat.size !== encryptedSize(manifest.databaseSize)) {
    result.database = `wrong size (${stat.size} bytes)`;
  } else {
    result.database = 'archived';
  }
  countMissingBlobs(manifest, await adapter.list('blobs/'), result);
}

async function checkNewestSnapshot(adapter, targetId, result) {
  const [newest] = await listSnapshots(adapter);
  if (!newest) throw new Error('There is no finished snapshot on this target yet.');
  result.snapshot = newest;
  if (adapter.archived) {
    await checkArchivedSnapshot(adapter, targetId, newest, result);
    return;
  }
  const manifest = await readManifest(adapter, newest);

  ensurePrivateDirectory(UPLOAD_TMP_DIR);
  const encrypted = path.join(UPLOAD_TMP_DIR, `verify-${targetId}-${Date.now()}.enc`);
  const staged = `${encrypted}.db`;
  try {
    await pipeline(await adapter.get(`snapshots/${newest}/${DATABASE_OBJECT}`), fs.createWriteStream(encrypted, { mode: 0o600 }));
    await decryptDatabase(encrypted, getBackupKey(), snapshotIdFor(newest), staged, manifest.databaseSize);
    result.database = inspectDatabase(staged).check;
  } finally {
    for (const file of [encrypted, staged, `${staged}-wal`, `${staged}-shm`]) await removeQuietly(file);
  }

  countMissingBlobs(manifest, await adapter.list('blobs/'), result);
}

/** Verifies the newest snapshot; never throws for a failed check, only reports it. */
async function verifyTarget(targetId) {
  getTargetRow(targetId);
  const result = {
    verifiedAt: new Date().toISOString(),
    ok: false,
    mode: 'full',
    snapshot: null,
    database: null,
    blobsChecked: 0,
    missingBlobs: 0,
    error: null,
  };
  let adapter;
  try {
    adapter = (await openTarget(targetId)).adapter;
    await checkNewestSnapshot(adapter, targetId, result);
    const databaseFine = result.database === (result.mode === 'metadata' ? 'archived' : 'ok');
    result.ok = databaseFine && result.missingBlobs === 0;
    if (!result.ok) {
      result.error = databaseFine
        ? `${result.missingBlobs} blob(s) listed in the snapshot are missing on the target.`
        : `The snapshot's database failed its check: ${result.database}`;
    }
  } catch (err) {
    result.error = sanitizeError(err);
  } finally {
    await adapter?.close();
  }
  recordVerification(targetId, result);
  if (result.ok && result.mode === 'metadata') logger.info(`Target ${targetId}: snapshot ${result.snapshot} checked without reading (archived).`);
  else if (result.ok) logger.info(`Target ${targetId}: snapshot ${result.snapshot} verified.`);
  else logger.warn(`Target ${targetId}: verification failed: ${result.error}`);
  return result;
}

module.exports = { verifyTarget };
