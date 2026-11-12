'use strict';

// Restoring a snapshot from a backup target into this install. Only with the
// server stopped and the original NEOCLOUD_MASTER_KEY in place. The snapshot
// is downloaded, decrypted and checked (quick_check, schema version) before
// anything local changes; the current database is kept beside the restored
// one as neocloud.db.before-restore-<time>.

const fs = require('fs');
const path = require('path');
const { pipeline } = require('stream/promises');
const Database = require('better-sqlite3');
const { DATA_DIR } = require('../../../runtime/paths');
const { latestSchemaVersion } = require('../../../lib/schema_migrations');
const { getBackupKey } = require('../crypto/keys');
const { encryptedSize } = require('../crypto/stream_cipher');
const { removeQuietly } = require('../storage/blob_store');
const { syncDirectory, syncFile } = require('../../utils/durable_fs');
const { openTarget, openTargetSpec } = require('./targets');
const { DATABASE_OBJECT, localBlobPath, readManifest, snapshotIdFor } = require('./runner');
const { assertRestorable, assertServerStopped, decryptDatabase, swapInDatabase } = require('./database_file');

// Plaintext size of every blob the restored database records, by
// "<user id>/<blob id>".
function blobSizesOf(stagedFile) {
  const copy = new Database(stagedFile, { readonly: true, fileMustExist: true });
  try {
    return new Map(copy.prepare('SELECT id, user_id, size FROM blobs').all().map((row) => [`${row.user_id}/${row.id}`, row.size]));
  } finally {
    copy.close();
  }
}

// A local blob is kept only when it has the length its recorded size
// implies; a truncated or overgrown one (a damaged disk, an interrupted
// copy) is fetched again from the backup.
function localBlobIsWhole(local, size) {
  try {
    return size === undefined || fs.statSync(local).size === encryptedSize(size);
  } catch {
    return false;
  }
}

async function downloadMissingBlobs(adapter, blobs, sizes) {
  let fetched = 0;
  const directories = new Set();
  for (const blob of blobs) {
    const local = localBlobPath(blob);
    if (localBlobIsWhole(local, sizes.get(blob))) continue;
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

/**
 * `source` is a target id from this install's database, or `{ from }`: a
 * backup folder or target JSON file (see openTargetSpec) for an install
 * whose database doesn't know the target, such as a new machine.
 */
function openSource(source) {
  return typeof source === 'object' && source !== null ? openTargetSpec(source.from) : openTarget(source);
}

async function restoreSnapshot(source, name, { log = () => {} } = {}) {
  await assertServerStopped();
  const { adapter } = await openSource(source);
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

    log(`Downloaded ${await downloadMissingBlobs(adapter, manifest.blobs, blobSizesOf(staged))} blob(s).`);
    const { previous } = await swapInDatabase(staged);
    log(previous ? `Database restored. The previous one is at ${previous}.` : 'Database restored.');
  } finally {
    for (const file of [encrypted, staged, `${staged}-wal`, `${staged}-shm`]) await removeQuietly(file);
    await adapter.close();
  }
}

module.exports = { openSource, restoreSnapshot };
