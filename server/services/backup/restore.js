'use strict';

// Restoring a snapshot from a backup target into this install. Only with the
// server stopped and the original NEOCLOUD_MASTER_KEY in place. The snapshot
// is downloaded, decrypted and checked (quick_check, schema version) before
// anything local changes; the current database is kept beside the restored
// one as neocloud.db.before-restore-<time>.

const fs = require('fs');
const path = require('path');
const { pipeline } = require('stream/promises');
const { DATA_DIR } = require('../../../runtime/paths');
const { latestSchemaVersion } = require('../../../lib/schema_migrations');
const { getBackupKey } = require('../crypto/keys');
const { removeQuietly } = require('../storage/blob_store');
const { syncDirectory, syncFile } = require('../../utils/durable_fs');
const { openTarget } = require('./targets');
const { DATABASE_OBJECT, localBlobPath, readManifest, snapshotIdFor } = require('./runner');
const { assertRestorable, assertServerStopped, decryptDatabase, swapInDatabase } = require('./database_file');

async function downloadMissingBlobs(adapter, blobs) {
  let fetched = 0;
  const directories = new Set();
  for (const blob of blobs) {
    const local = localBlobPath(blob);
    if (fs.existsSync(local)) continue;
    fs.mkdirSync(path.dirname(local), { recursive: true, mode: 0o700 });
    await pipeline(await adapter.get(`blobs/${blob}`), fs.createWriteStream(`${local}.part`, { mode: 0o600 }));
    await syncFile(`${local}.part`);
    fs.renameSync(`${local}.part`, local);
    directories.add(path.dirname(local));
    fetched += 1;
  }
  for (const directory of directories) await syncDirectory(directory);
  return fetched;
}

async function restoreSnapshot(targetId, name, { log = () => {} } = {}) {
  await assertServerStopped();
  const { adapter } = await openTarget(targetId);
  const stamp = Date.now();
  const encrypted = path.join(DATA_DIR, `restore-${stamp}.enc`);
  const staged = path.join(DATA_DIR, `restore-${stamp}.db`);
  try {
    const manifest = await readManifest(adapter, name);
    log(`Snapshot ${name}: ${manifest.blobs.length} blob(s), made by NeoCloud ${manifest.version}.`);
    if (manifest.schemaVersion > latestSchemaVersion()) {
      throw new Error(`Snapshot ${name} was made by a newer NeoCloud (schema ${manifest.schemaVersion}). Update NeoCloud first, then restore.`);
    }

    await pipeline(await adapter.get(`snapshots/${name}/${DATABASE_OBJECT}`), fs.createWriteStream(encrypted, { mode: 0o600 }));
    await decryptDatabase(encrypted, getBackupKey(), snapshotIdFor(name), staged, manifest.databaseSize);
    await removeQuietly(encrypted);
    assertRestorable(staged);
    log('Database snapshot decrypted and checked.');

    log(`Downloaded ${await downloadMissingBlobs(adapter, manifest.blobs)} blob(s).`);
    const { previous } = await swapInDatabase(staged);
    log(previous ? `Database restored. The previous one is at ${previous}.` : 'Database restored.');
  } finally {
    for (const file of [encrypted, staged, `${staged}-wal`, `${staged}-shm`]) await removeQuietly(file);
    await adapter.close();
  }
}

module.exports = { restoreSnapshot };
