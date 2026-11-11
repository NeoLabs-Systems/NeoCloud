'use strict';

// Runs import jobs: one at a time per account, two downloads at a time per
// job. A job first lists the remote tree folder by folder into import_items,
// then creates the folders, then streams each file into the blob store.
// Progress lives in import_items, so a job stopped anywhere (pause, restart,
// crash) carries on where it was: finished files are never fetched again.

const db = require('../../db/database');
const { decryptValue } = require('../crypto/keys');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');
const { assertRoomFor, createFolders, finishFolders, storeFile, targetFolder } = require('./placement');
const { clientFor } = require('./source');
const { importNetworkFor } = require('./network_guard');
const store = require('./store');

const logger = createServiceLogger('Imports');
const PARALLEL_DOWNLOADS = 2;
const PROGRESS_EVENT_MS = 1000;
// The remote server decides how big a tree it reports; a scan stops (and the
// job fails) past these, before import_items grows without bound.
const MAX_SCAN_ITEMS = 250000;
const MAX_SCAN_DEPTH = 64;
const MAX_SCAN_PATH_LENGTH = 4096;

// jobId -> { userId, controller, stopReason, done, lastEventAt }
const active = new Map();
let accepting = true;

function progress(handle, jobId) {
  if (Date.now() - handle.lastEventAt < PROGRESS_EVENT_MS) return;
  handle.lastEventAt = Date.now();
  store.announce(handle.userId, jobId);
}

async function scan(job, client, handle) {
  const upsert = db.prepare(`
    INSERT INTO import_items (job_id, path, type, size, mtime, favorite) VALUES (?, ?, ?, ?, ?, ?)
    ON CONFLICT (job_id, path) DO UPDATE SET
      state = CASE
        WHEN import_items.type != excluded.type OR import_items.size != excluded.size
          OR import_items.mtime IS NOT excluded.mtime THEN 'pending'
        ELSE import_items.state END,
      type = excluded.type, size = excluded.size, mtime = excluded.mtime, favorite = excluded.favorite
  `);
  const { signal } = handle.controller;
  const folders = [''];
  let items = 0;
  while (folders.length > 0) {
    const folder = folders.shift();
    signal.throwIfAborted();
    let listing;
    try {
      listing = await client.list(folder, signal);
    } catch (err) {
      if (folder === '' || signal.aborted) throw err;
      store.addError(job.id, `/${folder}`, store.describe(err));
      continue;
    }
    items += listing.children.length;
    if (items > MAX_SCAN_ITEMS) {
      throw httpError(400, `The source has more than ${MAX_SCAN_ITEMS} files and folders; import a smaller folder.`, 'IMPORT_TOO_LARGE');
    }
    db.transaction(() => {
      for (const child of listing.children) {
        const path = folder ? `${folder}/${child.name}` : child.name;
        if (path.length > MAX_SCAN_PATH_LENGTH || path.split('/').length > MAX_SCAN_DEPTH) {
          throw httpError(400, `The source has paths longer than ${MAX_SCAN_PATH_LENGTH} characters or deeper than ${MAX_SCAN_DEPTH} folders.`, 'IMPORT_TOO_DEEP');
        }
        upsert.run(job.id, path, child.type, child.type === 'file' ? child.size : 0, child.mtime, child.favorite ? 1 : 0);
        if (child.type === 'folder') folders.push(path);
      }
    })();
    progress(handle, job.id);
  }
  db.prepare('UPDATE import_jobs SET scanned = 1 WHERE id = ?').run(job.id);
}

async function transferFiles(job, client, handle) {
  const items = db.prepare(`
    SELECT path, size, mtime, favorite FROM import_items
    WHERE job_id = ? AND type = 'file' AND state = 'pending' ORDER BY path
  `).all(job.id);
  const { controller } = handle;
  let next = 0;
  async function worker() {
    while (next < items.length) {
      controller.signal.throwIfAborted();
      const item = items[next];
      next += 1;
      try {
        store.setItemState(job.id, item.path, await storeFile(job, client, item, controller.signal));
      } catch (err) {
        if (controller.signal.aborted) throw controller.signal.reason;
        // Disk full stops the whole job (paused); everything else is this file's problem.
        if (err.code === 'DISK_FULL') {
          controller.abort(err);
          throw err;
        }
        store.failItem(job.id, item.path, err);
      }
      progress(handle, job.id);
    }
  }
  const results = await Promise.allSettled(Array.from({ length: PARALLEL_DOWNLOADS }, worker));
  const failure = results.find((result) => result.status === 'rejected');
  if (failure) throw failure.reason;
}

async function runJob(jobId, handle) {
  const { signal } = handle.controller;
  try {
    const job = store.jobRowById(jobId);
    if (!job || job.status !== 'queued') return;
    if (!job.password_enc) throw httpError(400, 'Enter the app password again to continue.', 'PASSWORD_REQUIRED');
    if (!store.setStatus(jobId, job.scanned ? 'running' : 'scanning', ['queued'])) return;
    targetFolder(job);
    const client = clientFor({
      source: job.source,
      url: job.url,
      username: job.username,
      sourcePath: job.source_path,
    }, decryptValue(job.password_enc), importNetworkFor(job.user_id));
    await client.connect(signal);
    if (!job.scanned) {
      await scan(job, client, handle);
      store.setStatus(jobId, 'running', ['scanning']);
    }
    assertRoomFor(job);
    createFolders(job);
    await transferFiles(job, client, handle);
    finishFolders(job);
    store.setStatus(jobId, 'done', ['running']);
  } catch (err) {
    if (handle.stopReason) return;
    try {
      store.addError(jobId, '/', store.describe(err));
      // A full disk is the server's problem, not the job's: wait for space.
      store.setStatus(jobId, err.code === 'DISK_FULL' ? 'paused' : 'failed', ['queued', 'scanning', 'running']);
    } catch (recordErr) {
      logger.error(`Could not record the end of import ${jobId}: ${recordErr.message}`);
    }
  }
}

function schedule() {
  if (!accepting) return;
  const busy = new Set([...active.values()].map((handle) => handle.userId));
  const queued = db.prepare("SELECT id, user_id FROM import_jobs WHERE status = 'queued' ORDER BY created_at, rowid").all();
  for (const job of queued) {
    if (busy.has(job.user_id)) continue;
    busy.add(job.user_id);
    const handle = { userId: job.user_id, controller: new AbortController(), stopReason: null, lastEventAt: 0 };
    active.set(job.id, handle);
    handle.done = runJob(job.id, handle).finally(() => {
      active.delete(job.id);
      wake();
    });
  }
}

/** Looks for queued jobs to start (after a create, resume or a job ending). */
function wake() {
  if (accepting) setImmediate(schedule);
}

/** Stops a running job's downloads; the caller has already set its new status. */
function interrupt(jobId, reason) {
  const handle = active.get(jobId);
  if (!handle) return;
  handle.stopReason = reason;
  handle.controller.abort(new Error(`Import ${reason}.`));
}

/**
 * At startup: jobs that were running when the server stopped go back to the
 * queue and continue from their last finished file.
 */
function startImports() {
  accepting = true;
  const { changes } = db.prepare("UPDATE import_jobs SET status = 'queued' WHERE status IN ('scanning', 'running')").run();
  if (changes > 0) logger.info(`Resuming ${changes} interrupted import(s).`);
  wake();
}

/** At shutdown: stops all downloads, leaving the jobs to resume on the next start. */
function stopImports() {
  accepting = false;
  const handles = [...active.values()];
  for (const handle of handles) {
    handle.stopReason = 'stopped';
    handle.controller.abort(new Error('NeoCloud is stopping.'));
  }
  return Promise.all(handles.map((handle) => handle.done));
}

module.exports = { interrupt, startImports, stopImports, wake };
