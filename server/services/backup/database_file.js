'use strict';

// The database as a file: consistent copies of the live one, checks on a
// copy, decrypting a snapshot, and swapping a restored copy into place.
// Shared by remote backups, local snapshots, restore and verify.

const fs = require('fs');
const path = require('path');
const Database = require('better-sqlite3');
const { pipeline } = require('stream/promises');
const db = require('../../db/database');
const { DATABASE_FILE, PID_FILE } = require('../../../runtime/paths');
const { DEFAULT_PORT } = require('../../../lib/setup/contract');
const { latestSchemaVersion } = require('../../../lib/schema_migrations');
const { HEADER_LENGTH, SEGMENT_SIZE, TAG_LENGTH, createDecryptStream } = require('../crypto/stream_cipher');
const { syncDirectory, syncFile } = require('../../utils/durable_fs');
const { httpError } = require('../../utils/http_error');

function snapshotName(date = new Date()) {
  return date.toISOString().replace(/\.\d+Z$/, 'Z').replace(/:/g, '-');
}

/** A consistent copy of the live database while the server keeps writing. */
async function copyLiveDatabase(destination) {
  await db.backup(destination);
}

/**
 * Opens a database file read-only and reports its schema version and the
 * result of quick_check (or integrity_check with `full`): 'ok' or the first
 * problem found. An empty file, or one without a schema version or a users
 * table, is not a NeoCloud database: SQLite would happily open it, and
 * restoring it would hand admin to whoever signs up first.
 */
function inspectDatabase(file, { full = false } = {}) {
  let copy;
  try {
    if (fs.statSync(file).size === 0) return { check: 'the database file is empty', schemaVersion: null };
    copy = new Database(file, { readonly: true, fileMustExist: true });
    const rows = copy.pragma(full ? 'integrity_check' : 'quick_check', { simple: false });
    const problems = rows.map((row) => Object.values(row)[0]).filter((value) => value !== 'ok');
    const schemaVersion = copy.pragma('user_version', { simple: true });
    if (problems.length > 0) return { check: String(problems[0]), schemaVersion };
    const hasUsers = Boolean(copy.prepare("SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'users'").get());
    if (schemaVersion < 1 || !hasUsers) return { check: 'not a NeoCloud database (no schema version or users table)', schemaVersion };
    return { check: 'ok', schemaVersion };
  } catch (err) {
    return { check: err.message, schemaVersion: null };
  } finally {
    copy?.close();
  }
}

/** Reads the blob list and schema version out of a database copy. */
function readSnapshotContents(file) {
  const copy = new Database(file, { readonly: true, fileMustExist: true });
  try {
    return {
      blobs: copy.prepare('SELECT id, user_id FROM blobs ORDER BY id').all(),
      schemaVersion: copy.pragma('user_version', { simple: true }),
    };
  } finally {
    copy.close();
  }
}

// Plaintext size of an encrypted file from its length, for snapshots that
// don't record it. Every file uses SEGMENT_SIZE; the cipher refuses others.
function plaintextSizeOf(encryptedPath) {
  const length = fs.statSync(encryptedPath).size;
  const segments = Math.ceil((length - HEADER_LENGTH) / (SEGMENT_SIZE + TAG_LENGTH));
  return length - HEADER_LENGTH - segments * TAG_LENGTH;
}

/** Decrypts an encrypted database snapshot into `destination`. */
async function decryptDatabase(encryptedPath, key, id, destination, size = null) {
  try {
    const plainSize = size ?? plaintextSizeOf(encryptedPath);
    await pipeline(createDecryptStream(encryptedPath, key, id, plainSize), fs.createWriteStream(destination, { mode: 0o600 }));
  } catch {
    throw new Error('The database snapshot could not be decrypted. Is NEOCLOUD_MASTER_KEY the one this backup was made with?');
  }
}

/** Refuses a snapshot this code can't run, or one that fails quick_check. */
function assertRestorable(stagedFile) {
  const info = inspectDatabase(stagedFile);
  if (info.check !== 'ok') throw new Error(`The snapshot's database is damaged (quick_check: ${info.check}). Nothing was changed.`);
  const latest = latestSchemaVersion();
  if (info.schemaVersion > latest) {
    throw new Error(`The snapshot has schema version ${info.schemaVersion}; this NeoCloud only knows up to ${latest}. Update NeoCloud first, then restore.`);
  }
  return info;
}

function processAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return err.code === 'EPERM';
  }
}

async function healthAnswers() {
  const port = Number(process.env.PORT) || DEFAULT_PORT;
  try {
    await fetch(`http://127.0.0.1:${port}/api/health`, { signal: AbortSignal.timeout(1500) });
    return true;
  } catch {
    return false;
  }
}

/** A restore replaces the database under the server's feet: only with it stopped. */
async function assertServerStopped() {
  let pid = null;
  try {
    pid = Number(fs.readFileSync(PID_FILE, 'utf8').trim());
  } catch {
    // No PID file: not started by `neocloud start`, or stopped cleanly.
  }
  const running = (Number.isInteger(pid) && pid > 0 && pid !== process.pid && processAlive(pid)) || await healthAnswers();
  if (running) {
    throw httpError(409, 'NeoCloud is still running. Stop it first (`neocloud stop`, or stop the container); a restore replaces the database.', 'SERVER_RUNNING');
  }
}

/**
 * Puts a checked, staged database file in place of the live one. The live
 * database is kept beside it as neocloud.db.before-restore-<time>. Closes the
 * process's database handle: the caller exits afterwards.
 */
async function swapInDatabase(stagedFile) {
  const keep = `${DATABASE_FILE}.before-restore-${snapshotName()}`;
  if (fs.existsSync(DATABASE_FILE)) {
    try {
      // Includes whatever still sits in the WAL.
      await db.backup(keep);
    } catch {
      fs.copyFileSync(DATABASE_FILE, keep);
      if (fs.existsSync(`${DATABASE_FILE}-wal`)) fs.copyFileSync(`${DATABASE_FILE}-wal`, `${keep}-wal`);
    }
  }
  db.close();
  await syncFile(stagedFile);
  for (const suffix of ['-wal', '-shm']) fs.rmSync(`${DATABASE_FILE}${suffix}`, { force: true });
  fs.renameSync(stagedFile, DATABASE_FILE);
  await syncDirectory(path.dirname(DATABASE_FILE));
  return { previous: fs.existsSync(keep) ? keep : null };
}

module.exports = {
  assertRestorable,
  assertServerStopped,
  copyLiveDatabase,
  decryptDatabase,
  inspectDatabase,
  readSnapshotContents,
  snapshotName,
  swapInDatabase,
};
