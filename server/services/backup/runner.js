'use strict';

// Running and pruning backups.
//
// On the target:
//   blobs/<user>/<blob>               encrypted file contents, copied as they are
//   snapshots/<name>/neocloud.db.enc  the database, encrypted with the backup key
//   snapshots/<name>/manifest.json    written last; a snapshot without it is unfinished
//
// A run copies the database first and then uploads exactly the blobs that
// copy references, so a restored database never points at a blob the backup
// doesn't have. Blobs are content-addressed and immutable: a run only uploads
// blobs the target doesn't have yet. After a run the oldest snapshots beyond
// the retention are removed, then every blob no remaining snapshot needs.
//
// Nothing on the target is readable without NEOCLOUD_MASTER_KEY.

const fs = require('fs');
const path = require('path');
const { Readable } = require('stream');
const db = require('../../db/database');
const { BLOB_DIR, UPLOAD_TMP_DIR, ensurePrivateDirectory } = require('../../../runtime/paths');
const { getBackupKey } = require('../crypto/keys');
const { createEncryptStream } = require('../crypto/stream_cipher');
const { blobPath, removeQuietly } = require('../storage/blob_store');
const { sanitizeError } = require('../../utils/security');
const { createServiceLogger } = require('../../utils/logger');
const { httpError } = require('../../utils/http_error');
const { version } = require('../../../package.json');
const { openTarget, serializeRun } = require('./targets');
const { copyLiveDatabase, readSnapshotContents, snapshotName } = require('./database_file');

const logger = createServiceLogger('Backup');
const MANIFEST = 'manifest.json';
const DATABASE_OBJECT = 'neocloud.db.enc';
const MANIFEST_FORMAT = 2;
// The manifest is plain JSON on the target and not authenticated: whoever
// can write the target can change it. It is read with a size cap and every
// field is checked before anything uses it.
const MAX_MANIFEST_BYTES = 64 * 1024 * 1024;
const SNAPSHOT_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$/;
const USER_ID_PATTERN = /^[1-9][0-9]*$/;
const running = new Set();

function snapshotIdFor(name) {
  return `snapshot:${name}`;
}

function invalidManifest(detail) {
  return new Error(`The snapshot manifest is not valid (${detail}).`);
}

function assertSnapshotName(name) {
  if (typeof name !== 'string' || !SNAPSHOT_NAME_PATTERN.test(name)) throw new Error('That is not a snapshot name.');
}

/**
 * Where the blob a manifest entry "<user id>/<blob id>" names lives on this
 * server. Throws for anything else, so an entry can never lead outside
 * BLOB_DIR.
 */
function localBlobPath(entry) {
  const parts = typeof entry === 'string' ? entry.split('/') : [];
  const [userId, blobId] = parts;
  if (parts.length !== 2 || !USER_ID_PATTERN.test(userId) || !Number.isSafeInteger(Number(userId))) {
    throw invalidManifest('bad blob entry');
  }
  let local;
  try {
    local = path.resolve(blobPath(Number(userId), blobId));
  } catch {
    throw invalidManifest('bad blob entry');
  }
  if (!local.startsWith(path.resolve(BLOB_DIR) + path.sep)) throw invalidManifest('bad blob entry');
  return local;
}

async function readCapped(adapter, key, limit) {
  const chunks = [];
  let size = 0;
  for await (const chunk of await adapter.get(key)) {
    size += chunk.length;
    if (size > limit) throw invalidManifest(`larger than ${limit} bytes`);
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

/** A snapshot's manifest, read with a size cap and checked field by field. */
async function readManifest(adapter, name) {
  assertSnapshotName(name);
  let manifest;
  try {
    manifest = JSON.parse(await readCapped(adapter, `snapshots/${name}/${MANIFEST}`, MAX_MANIFEST_BYTES));
  } catch (err) {
    if (err instanceof SyntaxError) throw invalidManifest('not JSON');
    throw err;
  }
  if (!manifest || typeof manifest !== 'object' || !Array.isArray(manifest.blobs)) throw invalidManifest('no blob list');
  const { databaseSize, schemaVersion } = manifest;
  if (databaseSize != null && !(Number.isSafeInteger(databaseSize) && databaseSize >= 0)) throw invalidManifest('bad databaseSize');
  if (schemaVersion != null && !Number.isSafeInteger(schemaVersion)) throw invalidManifest('bad schemaVersion');
  for (const entry of manifest.blobs) localBlobPath(entry);
  return manifest;
}

/** Finished snapshots on the target, newest first. */
async function listSnapshots(adapter) {
  const keys = await adapter.list('snapshots/');
  return keys
    .map((key) => key.split('/'))
    .filter((parts) => parts.length === 3 && parts[2] === MANIFEST && SNAPSHOT_NAME_PATTERN.test(parts[1]))
    .map((parts) => parts[1])
    .sort()
    .reverse();
}

/**
 * Uploads every blob the snapshot references that the target lacks. A blob
 * deleted for good after the snapshot was taken can be gone from disk; it is
 * counted, since restoring this snapshot would bring back a file without
 * content.
 */
async function uploadSnapshotBlobs(adapter, blobs, run) {
  const remote = new Set(await adapter.list('blobs/'));
  let missing = 0;
  for (const blob of blobs) {
    const key = `blobs/${blob.user_id}/${blob.id}`;
    if (remote.has(key)) continue;
    const local = blobPath(blob.user_id, blob.id);
    if (!fs.existsSync(local)) {
      logger.warn(`Blob file missing on disk, not in this backup: ${blob.id}`);
      missing += 1;
      continue;
    }
    await adapter.put(key, () => fs.createReadStream(local));
    run.blobs += 1;
    run.bytes += fs.statSync(local).size;
    db.prepare('UPDATE backup_runs SET blobs_uploaded = ?, bytes_uploaded = ? WHERE id = ?').run(run.blobs, run.bytes, run.id);
  }
  return missing;
}

async function prune(adapter, retention) {
  const snapshots = await listSnapshots(adapter);
  for (const stale of snapshots.slice(retention)) {
    await adapter.deletePrefix(`snapshots/${stale}`);
  }
  const needed = new Set();
  for (const name of snapshots.slice(0, retention)) {
    const manifest = await readManifest(adapter, name);
    for (const blob of manifest.blobs) needed.add(`blobs/${blob}`);
  }
  for (const key of await adapter.list('blobs/')) {
    if (!needed.has(key)) await adapter.delete(key);
  }
}

/**
 * One backup run to the target. Returns the finished run. Only one run per
 * target at a time.
 */
async function runBackup(targetId) {
  if (running.has(targetId)) throw httpError(409, 'A backup to this target is already running.', 'BACKUP_RUNNING');
  running.add(targetId);
  const name = snapshotName();
  const inserted = db.prepare("INSERT INTO backup_runs (target_id, status, snapshot) VALUES (?, 'running', ?)").run(targetId, name);
  const run = { id: Number(inserted.lastInsertRowid), blobs: 0, bytes: 0 };
  ensurePrivateDirectory(UPLOAD_TMP_DIR);
  const copy = path.join(UPLOAD_TMP_DIR, `backup-${targetId}-${name}.db`);
  let adapter;
  try {
    const opened = await openTarget(targetId);
    adapter = opened.adapter;
    await copyLiveDatabase(copy);
    const contents = readSnapshotContents(copy);
    const missingBlobs = await uploadSnapshotBlobs(adapter, contents.blobs, run);
    const databaseSize = fs.statSync(copy).size;
    await adapter.put(`snapshots/${name}/${DATABASE_OBJECT}`, () =>
      fs.createReadStream(copy).pipe(createEncryptStream(getBackupKey(), snapshotIdFor(name))));
    const manifest = {
      format: MANIFEST_FORMAT,
      app: 'NeoCloud',
      version,
      schemaVersion: contents.schemaVersion,
      createdAt: new Date().toISOString(),
      databaseSize,
      blobs: contents.blobs.map((blob) => `${blob.user_id}/${blob.id}`),
      missingBlobs,
    };
    await adapter.put(`snapshots/${name}/${MANIFEST}`, () => Readable.from([JSON.stringify(manifest, null, 2)]));
    await prune(adapter, opened.row.retention);
    db.prepare("UPDATE backup_runs SET status = 'ok', finished_at = datetime('now'), blobs_uploaded = ?, bytes_uploaded = ? WHERE id = ?")
      .run(run.blobs, run.bytes, run.id);
    logger.info(`Backup ${name} to target ${targetId} finished: ${run.blobs} new blob(s).`);
  } catch (err) {
    const message = sanitizeError(err);
    db.prepare("UPDATE backup_runs SET status = 'failed', finished_at = datetime('now'), error = ? WHERE id = ?").run(message, run.id);
    logger.error(`Backup to target ${targetId} failed: ${message}`);
  } finally {
    running.delete(targetId);
    await removeQuietly(copy);
    await adapter?.close();
  }
  return serializeRun(db.prepare('SELECT * FROM backup_runs WHERE id = ?').get(run.id));
}

/** Starts a run without waiting for it; the admin page polls the run list. */
function startBackup(targetId) {
  if (running.has(targetId)) throw httpError(409, 'A backup to this target is already running.', 'BACKUP_RUNNING');
  runBackup(targetId).catch((err) => logger.error('Backup run crashed:', err.message));
  return { started: true };
}

/**
 * Runs left in 'running' by a crash or a kill can never finish; called once
 * at startup, before the scheduler looks at them.
 */
function markInterruptedRuns() {
  const result = db.prepare(`
    UPDATE backup_runs SET status = 'failed', finished_at = datetime('now'), error = 'interrupted'
    WHERE status = 'running'
  `).run();
  if (result.changes > 0) logger.warn(`Marked ${result.changes} unfinished backup run(s) as interrupted.`);
  return result.changes;
}

async function testTarget(targetId) {
  const { adapter } = await openTarget(targetId).catch((err) => {
    throw httpError(400, `Could not connect: ${sanitizeError(err)}`, 'BACKUP_CONNECT_FAILED');
  });
  try {
    const key = `.neocloud-test-${Date.now()}`;
    await adapter.put(key, () => Readable.from(['NeoCloud backup test']));
    await adapter.delete(key);
    return { ok: true };
  } catch (err) {
    throw httpError(400, `Connected, but writing failed: ${sanitizeError(err)}`, 'BACKUP_WRITE_FAILED');
  } finally {
    await adapter.close();
  }
}

async function snapshotsOf(targetId) {
  const { adapter } = await openTarget(targetId);
  try {
    return { snapshots: await listSnapshots(adapter) };
  } finally {
    await adapter.close();
  }
}

module.exports = {
  DATABASE_OBJECT,
  listSnapshots,
  localBlobPath,
  markInterruptedRuns,
  readManifest,
  runBackup,
  snapshotIdFor,
  snapshotsOf,
  startBackup,
  testTarget,
};
