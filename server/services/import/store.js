'use strict';

// Import job and item rows: reading, status changes, errors. Shared by the
// use cases (jobs.js), the runner and placement.js.

const db = require('../../db/database');
const { emitToUser } = require('../events');
const { breadcrumbs } = require('../files/tree');
const { httpError, publicErrorCode } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Imports');
const MAX_ERRORS = 50;

function getJobRow(userId, jobId) {
  const job = db.prepare('SELECT * FROM import_jobs WHERE id = ? AND user_id = ?').get(String(jobId || ''), userId);
  if (!job) throw httpError(404, 'Import not found', 'NOT_FOUND');
  return job;
}

function jobRowById(jobId) {
  return db.prepare('SELECT * FROM import_jobs WHERE id = ?').get(jobId) || null;
}

function targetPath(userId, folderId) {
  const names = breadcrumbs(userId, folderId).slice(1).map((crumb) => crumb.name);
  return `/${names.join('/')}`;
}

function serializeJob(row) {
  const counts = db.prepare(`
    SELECT
      COUNT(*) AS total_files,
      COALESCE(SUM(size), 0) AS total_bytes,
      COALESCE(SUM(state IN ('done', 'skipped')), 0) AS done_files,
      COALESCE(SUM(CASE WHEN state IN ('done', 'skipped') THEN size ELSE 0 END), 0) AS done_bytes,
      COALESCE(SUM(state = 'skipped'), 0) AS skipped_files,
      COALESCE(SUM(state = 'failed'), 0) AS failed_files
    FROM import_items WHERE job_id = ? AND type = 'file'
  `).get(row.id);
  const errors = db.prepare('SELECT path, message FROM import_errors WHERE job_id = ? ORDER BY id DESC LIMIT ?')
    .all(row.id, MAX_ERRORS)
    .reverse();
  return {
    id: row.id,
    source: row.source,
    url: row.url,
    username: row.username,
    sourcePath: row.source_path,
    targetFolderId: row.target_folder_id,
    targetPath: targetPath(row.user_id, row.target_folder_id),
    includeFavorites: row.include_favorites === 1,
    status: row.status,
    totalFiles: counts.total_files,
    totalBytes: counts.total_bytes,
    doneFiles: counts.done_files,
    doneBytes: counts.done_bytes,
    skippedFiles: counts.skipped_files,
    failedFiles: counts.failed_files,
    errors,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    createdAt: row.created_at,
  };
}

function announce(userId, jobId) {
  emitToUser(userId, 'imports:changed', { id: jobId });
}

/** Records a problem with one path ("/" for the job itself), keeping the newest MAX_ERRORS. */
function addError(jobId, path, message) {
  db.prepare('INSERT INTO import_errors (job_id, path, message) VALUES (?, ?, ?)').run(jobId, path, message);
  db.prepare(`
    DELETE FROM import_errors WHERE job_id = ? AND id NOT IN (
      SELECT id FROM import_errors WHERE job_id = ? ORDER BY id DESC LIMIT ?
    )
  `).run(jobId, jobId, MAX_ERRORS);
}

// What a person reads about a failure; technical causes stay in the log.
function describe(err) {
  if (publicErrorCode(err)) return err.message;
  logger.warn(`Import step failed: ${err?.message}`);
  return 'Something went wrong on this server.';
}

function setItemState(jobId, path, state) {
  db.prepare('UPDATE import_items SET state = ? WHERE job_id = ? AND path = ?').run(state, jobId, path);
}

function failItem(jobId, path, err) {
  setItemState(jobId, path, 'failed');
  addError(jobId, `/${path}`, describe(err));
}

/**
 * Moves a job to `status` if it is currently in one of `from`. Ended jobs
 * lose their stored password. Returns whether the row changed.
 */
function setStatus(jobId, status, from) {
  const ended = ['done', 'failed', 'cancelled'].includes(status);
  const result = db.prepare(`
    UPDATE import_jobs SET
      status = @status,
      password_enc = CASE WHEN @ended THEN NULL ELSE password_enc END,
      started_at = CASE WHEN @status = 'scanning' OR @status = 'running' THEN COALESCE(started_at, datetime('now')) ELSE started_at END,
      finished_at = CASE WHEN @ended THEN datetime('now') ELSE NULL END
    WHERE id = @jobId AND status IN (SELECT value FROM json_each(@from))
  `).run({ jobId, status, ended: ended ? 1 : 0, from: JSON.stringify(from) });
  if (result.changes === 1) {
    const row = jobRowById(jobId);
    if (row) announce(row.user_id, jobId);
  }
  return result.changes === 1;
}

module.exports = {
  addError,
  announce,
  describe,
  failItem,
  getJobRow,
  jobRowById,
  serializeJob,
  setItemState,
  setStatus,
};
