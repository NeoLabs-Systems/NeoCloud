'use strict';

// Moves a target's existing objects to the storage class it is configured
// with (S3 only): each object is copied onto its own key with the new class
// (CopyObject, MetadataDirective COPY; a multipart copy above 5 GB). Only
// NeoCloud's own keys below the target's prefix (blobs/, snapshots/) are
// touched, and nothing is ever deleted. Objects already in the class are
// skipped, and so are objects in GLACIER or DEEP_ARCHIVE when the target
// class differs: those can't be copied without a restore. That makes it
// resumable: a second run lists again and copies only what is left.
//
// Progress lives in the state store (`backup-storage-class`, by target) and is
// shown with the target like a backup run's.

const { sanitizeError } = require('../../utils/security');
const { createServiceLogger } = require('../../utils/logger');
const { httpError } = require('../../utils/http_error');
const { eachLimit } = require('../../utils/each_limit');
const { readState, writeState } = require('../state_store');
const { getTargetRow, openTarget, readStorageMigration } = require('./targets');
const { isArchiveClass } = require('./adapters/s3');

const logger = createServiceLogger('BackupStorageClass');
const STATE = 'backup-storage-class';
const AREAS = ['blobs/', 'snapshots/'];
const COPY_CONCURRENCY = 4;
const HEARTBEAT_MS = 30 * 1000;
const running = new Set();

function saveProgress(targetId, progress) {
  const all = readState(STATE, {});
  all[targetId] = { ...progress, heartbeatAt: new Date().toISOString() };
  writeState(STATE, all);
}

function assertMigratable(targetId) {
  const row = getTargetRow(targetId);
  if (row.type !== 's3') throw httpError(400, 'Only S3 targets have a storage class.', 'BACKUP_NOT_S3');
  if (running.has(targetId) || readStorageMigration(targetId)?.status === 'running') {
    throw httpError(409, 'Existing backups on this target are already being moved.', 'MIGRATION_RUNNING');
  }
}

async function copyAll(adapter, targetId, progress) {
  const todo = [];
  for (const area of AREAS) {
    for (const object of await adapter.listObjects(area)) {
      if (object.storageClass === adapter.storageClass) progress.objectsSkipped += 1;
      else if (isArchiveClass(object.storageClass)) progress.objectsArchived += 1;
      else todo.push(object);
    }
  }
  progress.objectsTotal = todo.length;
  progress.bytesTotal = todo.reduce((sum, object) => sum + object.size, 0);
  saveProgress(targetId, progress);

  let lastSaved = Date.now();
  await eachLimit(todo, COPY_CONCURRENCY, async (object) => {
    try {
      await adapter.changeStorageClass(object.key, object.size);
    } catch (err) {
      // Pruned by a backup run since the listing: nothing left to move.
      if (err?.name !== 'NoSuchKey') throw err;
    }
    progress.objectsDone += 1;
    progress.bytesDone += object.size;
    if (Date.now() - lastSaved > 1000) {
      lastSaved = Date.now();
      saveProgress(targetId, progress);
    }
  });
}

/** Moves every existing object; resolves with the final progress, never throws for a failed copy. */
async function migrateStorageClass(targetId, { onProgress } = {}) {
  assertMigratable(targetId);
  running.add(targetId);
  const progress = {
    status: 'running',
    storageClass: null,
    startedAt: new Date().toISOString(),
    finishedAt: null,
    objectsTotal: null,
    objectsDone: 0,
    bytesTotal: null,
    bytesDone: 0,
    objectsSkipped: 0,
    objectsArchived: 0,
    error: null,
  };
  saveProgress(targetId, progress);
  const beat = setInterval(() => {
    saveProgress(targetId, progress);
    onProgress?.(progress);
  }, onProgress ? 2000 : HEARTBEAT_MS);
  beat.unref();
  let adapter;
  try {
    adapter = (await openTarget(targetId)).adapter;
    progress.storageClass = adapter.storageClass;
    await copyAll(adapter, targetId, progress);
    progress.status = 'ok';
    logger.info(`Target ${targetId}: ${progress.objectsDone} object(s) moved to ${progress.storageClass}, `
      + `${progress.objectsSkipped} already there, ${progress.objectsArchived} archived and left as they are.`);
  } catch (err) {
    progress.status = 'failed';
    progress.error = sanitizeError(err);
    logger.error(`Target ${targetId}: moving existing backups failed: ${progress.error}`);
  } finally {
    clearInterval(beat);
    running.delete(targetId);
    progress.finishedAt = new Date().toISOString();
    saveProgress(targetId, progress);
    await adapter?.close();
  }
  return progress;
}

/** Starts the migration in the background; the admin page polls the target list. */
function startStorageClassMigration(targetId) {
  assertMigratable(targetId);
  migrateStorageClass(targetId).catch((err) => logger.error('Storage class migration crashed:', err.message));
  return { started: true };
}

module.exports = { migrateStorageClass, startStorageClassMigration };
