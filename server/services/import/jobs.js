'use strict';

// Import use cases behind /api/imports: create, test, list, pause, resume,
// cancel, delete. The work itself happens in runner.js.

const crypto = require('crypto');
const db = require('../../db/database');
const { encryptValue } = require('../crypto/keys');
const { httpError, publicErrorCode } = require('../../utils/http_error');
const { ensureFolderPath } = require('../files/operations');
const { ROOT_ALIAS, getLiveNode } = require('../files/tree');
const { clientFor, parseConnection } = require('./source');
const { importNetworkFor } = require('./network_guard');
const runner = require('./runner');
const store = require('./store');

const DEFAULT_FOLDER = { nextcloud: 'Nextcloud', webdav: 'WebDAV import' };
const ENDED = ['done', 'failed', 'cancelled'];

// The folder given, or a reused "Nextcloud" folder in the root, so running
// the same import again lands on the same files.
function resolveTarget(userId, source, targetFolderId) {
  const folder = targetFolderId
    ? getLiveNode(userId, targetFolderId, { type: 'folder' })
    : getLiveNode(userId, ensureFolderPath(userId, { parentId: ROOT_ALIAS, path: DEFAULT_FOLDER[source] }).id);
  if (folder.vault_id) throw httpError(409, 'Files can’t be imported into a vault.', 'VAULT_BOUNDARY');
  return folder;
}

function createImport(userId, body = {}) {
  const connection = parseConnection(body);
  if (body.includeFavorites !== undefined && typeof body.includeFavorites !== 'boolean') {
    throw httpError(400, 'includeFavorites must be true or false.', 'INVALID_PARAM');
  }
  const target = resolveTarget(userId, connection.source, body.targetFolderId);
  const id = crypto.randomUUID();
  db.prepare(`
    INSERT INTO import_jobs (id, user_id, source, url, username, password_enc, source_path, target_folder_id, include_favorites)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).run(
    id,
    userId,
    connection.source,
    connection.url,
    connection.username,
    encryptValue(connection.password),
    connection.sourcePath,
    target.id,
    body.includeFavorites === false ? 0 : 1,
  );
  runner.wake();
  return store.serializeJob(store.jobRowById(id));
}

/**
 * Signs in and lists the source folder once, writing nothing. `files` counts
 * the entries directly in the folder; `bytes` is the folder's total size as
 * the server reports it (Nextcloud does), else the sum of those entries.
 */
async function testImport(userId, body = {}) {
  const connection = parseConnection(body);
  const client = clientFor(connection, connection.password, { retries: 1, ...importNetworkFor(userId) });
  try {
    await client.connect();
    const { self, children } = await client.list('');
    const listed = children.reduce((sum, child) => sum + child.size, 0);
    return { ok: true, files: children.length, bytes: Math.max(self.size || 0, listed) };
  } catch (err) {
    const code = publicErrorCode(err);
    return { ok: false, files: 0, bytes: 0, error: code ? err.message : 'The server could not be read.', ...(code ? { code } : {}) };
  }
}

function listImports(userId) {
  const rows = db.prepare('SELECT * FROM import_jobs WHERE user_id = ? ORDER BY created_at DESC, rowid DESC').all(userId);
  return { items: rows.map(store.serializeJob) };
}

function getImport(userId, jobId) {
  return store.serializeJob(store.getJobRow(userId, jobId));
}

function invalidState(message) {
  return httpError(409, message, 'IMPORT_STATE');
}

function pauseImport(userId, jobId) {
  const job = store.getJobRow(userId, jobId);
  if (!store.setStatus(job.id, 'paused', ['queued', 'scanning', 'running'])) {
    throw invalidState('Only a queued or running import can be paused.');
  }
  runner.interrupt(job.id, 'paused');
  return getImport(userId, job.id);
}

function cancelImport(userId, jobId) {
  const job = store.getJobRow(userId, jobId);
  if (!store.setStatus(job.id, 'cancelled', ['queued', 'scanning', 'running', 'paused'])) {
    throw invalidState('This import has already ended.');
  }
  runner.interrupt(job.id, 'cancelled');
  return getImport(userId, job.id);
}

/**
 * Continues a paused, failed or cancelled import. An ended one no longer
 * holds the password, so it must come again in the body. Failed files are
 * tried once more; finished ones are not fetched again.
 */
function resumeImport(userId, jobId, body = {}) {
  const job = store.getJobRow(userId, jobId);
  if (!['paused', 'failed', 'cancelled'].includes(job.status)) {
    throw invalidState('Only a paused, failed or cancelled import can be resumed.');
  }
  const password = typeof body.password === 'string' && body.password ? body.password : null;
  if (!password && !job.password_enc) {
    throw httpError(400, 'Enter the app password again to continue.', 'PASSWORD_REQUIRED');
  }
  db.transaction(() => {
    if (password) db.prepare('UPDATE import_jobs SET password_enc = ? WHERE id = ?').run(encryptValue(password), job.id);
    db.prepare("UPDATE import_items SET state = 'pending' WHERE job_id = ? AND state = 'failed'").run(job.id);
    db.prepare('DELETE FROM import_errors WHERE job_id = ?').run(job.id);
    store.setStatus(job.id, 'queued', [job.status]);
  })();
  runner.wake();
  return getImport(userId, job.id);
}

function deleteImport(userId, jobId) {
  const job = store.getJobRow(userId, jobId);
  if (!ENDED.includes(job.status)) throw invalidState('Cancel the import before deleting it.');
  db.prepare('DELETE FROM import_jobs WHERE id = ?').run(job.id);
  return { success: true };
}

module.exports = {
  cancelImport,
  createImport,
  deleteImport,
  getImport,
  listImports,
  pauseImport,
  resumeImport,
  testImport,
};
