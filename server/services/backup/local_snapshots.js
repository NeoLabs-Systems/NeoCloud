'use strict';

// Encrypted database-only snapshots on this machine, in LOCAL_BACKUP_DIR:
//   daily-<time>.db.enc        taken daily while no backup target is scheduled (keep 14)
//   pre-update-<time>.db.enc   taken by `neocloud update` before it pulls (keep 5)
// They hold no file contents -- a lost disk still needs a real backup target --
// but they undo a bad upgrade or a damaged database. Encrypted with the backup
// key like remote snapshots.

const fs = require('fs');
const path = require('path');
const { pipeline } = require('stream/promises');
const { LOCAL_BACKUP_DIR, ensurePrivateDirectory } = require('../../../runtime/paths');
const { getBackupKey } = require('../crypto/keys');
const { createEncryptStream } = require('../crypto/stream_cipher');
const { removeQuietly } = require('../storage/blob_store');
const { syncDirectory, syncFile } = require('../../utils/durable_fs');
const {
  assertRestorable,
  assertServerStopped,
  copyLiveDatabase,
  decryptDatabase,
  readSnapshotContents,
  snapshotName,
  swapInDatabase,
} = require('./database_file');

const KINDS = { daily: 14, 'pre-update': 5 };
const SUFFIX = '.db.enc';

function snapshotId(file) {
  return `local:${path.basename(file, SUFFIX)}`;
}

function assertKind(kind) {
  if (!KINDS[kind]) throw new Error(`Unknown local snapshot kind: ${kind}`);
}

/** Local snapshots of one kind, newest first. */
function listLocalSnapshots(kind) {
  assertKind(kind);
  let names;
  try {
    names = fs.readdirSync(LOCAL_BACKUP_DIR);
  } catch {
    return [];
  }
  return names
    .filter((name) => name.startsWith(`${kind}-`) && name.endsWith(SUFFIX))
    .sort()
    .reverse()
    .map((name) => {
      const file = path.join(LOCAL_BACKUP_DIR, name);
      const stats = fs.statSync(file);
      return { name, file, size: stats.size, createdAt: stats.mtime.toISOString() };
    });
}

async function createLocalSnapshot(kind) {
  assertKind(kind);
  ensurePrivateDirectory(LOCAL_BACKUP_DIR);
  const name = `${kind}-${snapshotName()}`;
  const file = path.join(LOCAL_BACKUP_DIR, `${name}${SUFFIX}`);
  const plain = path.join(LOCAL_BACKUP_DIR, `.${name}.db`);
  const partial = `${file}.part`;
  try {
    await copyLiveDatabase(plain);
    const { schemaVersion } = readSnapshotContents(plain);
    await pipeline(
      fs.createReadStream(plain),
      createEncryptStream(getBackupKey(), snapshotId(file)),
      fs.createWriteStream(partial, { mode: 0o600 }),
    );
    await syncFile(partial);
    fs.renameSync(partial, file);
    await syncDirectory(LOCAL_BACKUP_DIR);
    for (const stale of listLocalSnapshots(kind).slice(KINDS[kind])) await removeQuietly(stale.file);
    return { name: path.basename(file), file, schemaVersion };
  } finally {
    await removeQuietly(plain);
    await removeQuietly(partial);
  }
}

/** Puts a local snapshot back in place of the database. Server must be stopped. */
async function restoreLocalSnapshot(file) {
  await assertServerStopped();
  const staged = path.join(LOCAL_BACKUP_DIR, `.restore-${Date.now()}.db`);
  try {
    await decryptDatabase(file, getBackupKey(), snapshotId(file), staged);
    assertRestorable(staged);
    return await swapInDatabase(staged);
  } finally {
    for (const leftover of [staged, `${staged}-wal`, `${staged}-shm`]) await removeQuietly(leftover);
  }
}

module.exports = { createLocalSnapshot, listLocalSnapshots, restoreLocalSnapshot };
