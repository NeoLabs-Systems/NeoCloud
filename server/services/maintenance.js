'use strict';

// Housekeeping that runs while the server is up. Each pass is independent and
// safe to repeat.
//   hourly  expired trash, abandoned uploads, a WAL checkpoint when the WAL
//           has grown, the next slice of the blob scrub
//   daily   PRAGMA optimize, and a quick_check on a worker thread whose
//           result admin health shows (a request never runs one itself)

const fs = require('fs');
const { Worker } = require('worker_threads');
const db = require('../db/database');
const { DATABASE_FILE } = require('../../runtime/paths');
const { getStorageConfig } = require('../config/storage');
const { purgeExpiredTrash } = require('./files/trash');
const { purgeExpiredUploads } = require('./transfer/uploads');
const { scrubSlice } = require('./storage/blob_scrub');
const { createServiceLogger } = require('../utils/logger');

const logger = createServiceLogger('Maintenance');
const INTERVAL_MS = 60 * 60 * 1000;
const DAILY_MS = 24 * 60 * 60 * 1000;
const FIRST_PASS_MS = 30 * 1000;
const WAL_CHECKPOINT_BYTES = 32 * 1024 * 1024;

const databaseCheck = { status: 'pending', checkedAt: null, durationMs: null };
let lastDailyAt = 0;

// The worker opens its own read-only connection, so a long check on a big
// database never blocks requests.
const QUICK_CHECK_WORKER = `
const { parentPort, workerData } = require('worker_threads');
const Database = require(workerData.driver);
const db = new Database(workerData.file, { readonly: true, fileMustExist: true });
try {
  const rows = db.pragma('quick_check');
  parentPort.postMessage(rows.map((row) => Object.values(row)[0]));
} finally {
  db.close();
}
`;

function quickCheckOffThread() {
  return new Promise((resolve, reject) => {
    const worker = new Worker(QUICK_CHECK_WORKER, {
      eval: true,
      workerData: { file: DATABASE_FILE, driver: require.resolve('better-sqlite3') },
    });
    worker.once('message', resolve);
    worker.once('error', reject);
    worker.once('exit', (code) => {
      if (code !== 0) reject(new Error(`quick_check worker exited with ${code}`));
    });
  });
}

async function checkDatabase() {
  const startedAt = Date.now();
  try {
    const results = await quickCheckOffThread();
    const problems = results.filter((value) => value !== 'ok');
    databaseCheck.status = problems.length === 0 ? 'ok' : 'damaged';
    if (problems.length > 0) logger.error(`Database quick_check found problems: ${problems.slice(0, 5).join('; ')}`);
  } catch (err) {
    databaseCheck.status = 'damaged';
    logger.error('Database quick_check failed:', err.message);
  }
  databaseCheck.checkedAt = new Date().toISOString();
  databaseCheck.durationMs = Date.now() - startedAt;
}

/** The last quick_check result: status 'pending' until the first one ran. */
function getDatabaseCheck() {
  return { ...databaseCheck };
}

function checkpointLargeWal() {
  let size = 0;
  try {
    size = fs.statSync(`${DATABASE_FILE}-wal`).size;
  } catch {
    return;
  }
  if (size < WAL_CHECKPOINT_BYTES) return;
  const [result] = db.pragma('wal_checkpoint(TRUNCATE)');
  logger.info(`WAL was ${Math.round(size / 1024 / 1024)} MB; checkpointed${result?.busy ? ' (partly: readers were active)' : ''}.`);
}

async function step(label, action) {
  try {
    await action();
  } catch (err) {
    logger.error(`${label} failed:`, err.message);
  }
}

async function runMaintenance() {
  await step('Cleanup', async () => {
    const trashed = await purgeExpiredTrash(getStorageConfig().trashRetentionDays);
    const uploads = await purgeExpiredUploads();
    if (trashed || uploads) logger.info(`Removed ${trashed} expired trash item(s) and ${uploads} abandoned upload(s).`);
  });
  await step('WAL checkpoint', checkpointLargeWal);
  await step('Blob scrub', scrubSlice);
  if (Date.now() - lastDailyAt >= DAILY_MS) {
    lastDailyAt = Date.now();
    await step('PRAGMA optimize', () => db.pragma('optimize'));
    await checkDatabase();
  }
}

function startMaintenance() {
  const first = setTimeout(runMaintenance, FIRST_PASS_MS);
  const timer = setInterval(runMaintenance, INTERVAL_MS);
  first.unref();
  timer.unref();
  return () => {
    clearTimeout(first);
    clearInterval(timer);
  };
}

module.exports = { checkDatabase, getDatabaseCheck, runMaintenance, startMaintenance };
