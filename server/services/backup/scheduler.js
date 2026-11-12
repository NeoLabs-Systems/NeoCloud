'use strict';

// Starts scheduled backups. Every few minutes it looks for targets whose hour
// (and weekday, for weekly ones) has come and that haven't run in that slot.
// A failed run is retried within the slot after 10, 30 and 90 minutes rather
// than waiting a whole day or week. Times are the server's local time.
//
// While no target is scheduled, it keeps a daily encrypted database snapshot
// on this machine instead (see local_snapshots.js).

const db = require('../../db/database');
const { createServiceLogger } = require('../../utils/logger');
const { markInterruptedRuns, runBackup } = require('./runner');
const { createLocalSnapshot, listLocalSnapshots } = require('./local_snapshots');

const logger = createServiceLogger('BackupScheduler');
const CHECK_INTERVAL_MS = 5 * 60 * 1000;
const FIRST_CHECK_MS = 60 * 1000;
const RETRY_DELAYS_MINUTES = [10, 30, 90];
const DAILY_SNAPSHOT_AGE_MS = 23 * 60 * 60 * 1000;

function slotStart(target, now) {
  const start = new Date(now);
  start.setMinutes(0, 0, 0);
  start.setHours(target.hour);
  if (target.schedule === 'weekly') {
    start.setDate(start.getDate() - ((start.getDay() - target.weekday + 7) % 7));
  }
  if (start > now) start.setDate(start.getDate() - (target.schedule === 'weekly' ? 7 : 1));
  return start;
}

// backup_runs times are SQLite datetime('now'): UTC, "YYYY-MM-DD HH:MM:SS".
function toSqlTime(date) {
  return date.toISOString().slice(0, 19).replace('T', ' ');
}

function fromSqlTime(value) {
  return new Date(`${String(value).replace(' ', 'T')}Z`);
}

function isDue(target, now = new Date()) {
  if (target.enabled !== 1 || target.schedule === 'manual') return false;
  const runs = db.prepare('SELECT status, finished_at FROM backup_runs WHERE target_id = ? AND started_at >= ? ORDER BY id')
    .all(target.id, toSqlTime(slotStart(target, now)));
  if (runs.length === 0) return true;
  // One that succeeded or is still going settles the slot.
  if (runs.some((run) => run.status !== 'failed')) return false;
  if (runs.length > RETRY_DELAYS_MINUTES.length) return false;
  const lastFinished = fromSqlTime(runs[runs.length - 1].finished_at);
  return now - lastFinished >= RETRY_DELAYS_MINUTES[runs.length - 1] * 60 * 1000;
}

async function takeDailySnapshotIfDue(now) {
  const { scheduled } = db.prepare("SELECT COUNT(*) AS scheduled FROM backup_targets WHERE enabled = 1 AND schedule != 'manual'").get();
  if (scheduled > 0) return;
  const [newest] = listLocalSnapshots('daily');
  if (newest && now - new Date(newest.createdAt) < DAILY_SNAPSHOT_AGE_MS) return;
  const snapshot = await createLocalSnapshot('daily');
  logger.info(`No backup target is scheduled; kept a local database snapshot (${snapshot.name}).`);
}

async function checkSchedules(now = new Date()) {
  // A run whose process died stops beating; until it is marked failed it
  // would settle its slot and block the target.
  markInterruptedRuns();
  const targets = db.prepare('SELECT * FROM backup_targets').all();
  for (const target of targets) {
    if (!isDue(target, now)) continue;
    logger.info(`Starting scheduled backup to "${target.name}".`);
    await runBackup(target.id).catch((err) => logger.error('Scheduled backup crashed:', err.message));
  }
  await takeDailySnapshotIfDue(now).catch((err) => logger.error('Local database snapshot failed:', err.message));
}

function startBackupScheduler() {
  markInterruptedRuns();
  const tick = () => checkSchedules().catch((err) => logger.error('Backup schedule check failed:', err.message));
  const first = setTimeout(tick, FIRST_CHECK_MS);
  const timer = setInterval(tick, CHECK_INTERVAL_MS);
  first.unref();
  timer.unref();
  return () => {
    clearTimeout(first);
    clearInterval(timer);
  };
}

module.exports = { checkSchedules, isDue, slotStart, startBackupScheduler };
