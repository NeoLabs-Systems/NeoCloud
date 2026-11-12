'use strict';

// The AI job worker: one loop, at most two files at a time.
//
// It runs the jobs people start (jobs.js) and queues its own from the change
// journal, followed from a saved cursor like the search indexer: a file
// created for an account with "on upload" switched on gets a job (while AI
// is off nothing is queued, the cursor moves on).
// A job runs only while AI can run for its account: an account whose admin
// switch, opt-in or monthly budget stops it is paused, its jobs wait. A
// failed provider call is retried with backoff (1, 4, 16, 64 minutes), then
// the job fails. Vault, excluded, trashed and other accounts' files are never
// processed; shared-with-me files are never queued (the owner's own nodes
// only).

const db = require('../../../db/database');
const { createServiceLogger } = require('../../../utils/logger');
const { assertAiAllowedForNode } = require('../exclusions');
const { JOB_KINDS, aiFeatures, pauseReason } = require('./features');
const { suggestForFile } = require('./pipeline');
const { announceSuggestions } = require('./suggestions');
const { enqueue, sqlTime } = require('./queue');

const logger = createServiceLogger('AiJobs');

const CONCURRENCY = 2;
const JOBS_PER_PASS = 10;
const JOURNAL_PAGE = 500;
const MAX_ATTEMPTS = 5;
const RETRY_BASE_MS = 60 * 1000;
const INDEX_WAIT_RETRY_MS = 15 * 1000;
const KEEP_FINISHED_DAYS = 7;
const IDLE_MS = 5 * 1000;
const FIRST_PASS_MS = 20 * 1000;
const BUSY_GAP_MS = 50;

const SKIP_CODES = new Set(['NOT_FOUND', 'AI_VAULT', 'AI_EXCLUDED', 'NOT_A_FILE', 'AI_FEATURE_OFF']);
const PAUSE_CODES = new Set(['AI_DISABLED', 'AI_NOT_OPTED_IN', 'AI_BUDGET_EXCEEDED']);

// --- Following the journal ------------------------------------------------------------------

function readState(key) {
  return db.prepare('SELECT value FROM ai_state WHERE key = ?').get(key)?.value ?? null;
}

function writeState(key, value) {
  db.prepare('INSERT INTO ai_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value')
    .run(key, String(value));
}

function latestSeq() {
  return db.prepare('SELECT COALESCE(MAX(seq), 0) AS seq FROM changes').get().seq;
}

// The kinds an upload gets for this account, or null when it gets nothing.
function uploadKinds(userId, cache) {
  if (!cache.has(userId)) {
    const features = aiFeatures(userId);
    const reason = pauseReason(userId);
    const usable = reason === null || reason === 'AI_BUDGET_EXCEEDED';
    const kinds = JOB_KINDS.filter((kind) => features[kind]);
    cache.set(userId, usable && features.onUpload && kinds.length > 0 ? kinds : null);
  }
  return cache.get(userId);
}

function uploadAllowed(userId, nodeId) {
  try {
    assertAiAllowedForNode(userId, nodeId);
    return true;
  } catch {
    return false;
  }
}

function followJournal() {
  const saved = readState('cursor');
  if (saved === null) {
    writeState('cursor', latestSeq());
    return;
  }
  let cursor = Number(saved);
  const upto = latestSeq();
  const cache = new Map();
  while (cursor < upto) {
    const rows = db.prepare(`
      SELECT c.seq, c.user_id, c.node_id FROM changes c JOIN nodes n ON n.id = c.node_id
      WHERE c.seq > ? AND c.seq <= ? AND c.op = 'create' AND n.type = 'file'
      ORDER BY c.seq LIMIT ?
    `).all(cursor, upto, JOURNAL_PAGE);
    const head = rows.length < JOURNAL_PAGE ? upto : rows[rows.length - 1].seq;
    db.transaction(() => {
      for (const row of rows) {
        const kinds = uploadKinds(row.user_id, cache);
        if (kinds && uploadAllowed(row.user_id, row.node_id)) {
          enqueue(row.user_id, [row.node_id], { kinds, source: 'upload' });
        }
      }
      writeState('cursor', head);
    })();
    cursor = head;
  }
}

// --- Running jobs -----------------------------------------------------------------------------

function finish(job, status, code = null) {
  db.prepare("UPDATE ai_jobs SET status = ?, error_code = ?, updated_at = datetime('now') WHERE id = ?").run(status, code, job.id);
}

function later(job, ms, { countAttempt }) {
  db.prepare(`
    UPDATE ai_jobs SET status = 'pending', attempts = attempts + ?, run_after = ?, updated_at = datetime('now') WHERE id = ?
  `).run(countAttempt ? 1 : 0, sqlTime(Date.now() + ms), job.id);
}

function jobFailed(job, error) {
  const code = error.code || 'FAILED';
  if (SKIP_CODES.has(code)) {
    finish(job, 'skipped', code);
  } else if (PAUSE_CODES.has(code)) {
    // Paused: it runs once AI can run for the account again.
    later(job, 0, { countAttempt: false });
  } else if (job.attempts + 1 >= MAX_ATTEMPTS) {
    logger.warn(`An AI job failed for good (${code}).`);
    finish(job, 'failed', code);
  } else {
    db.prepare('UPDATE ai_jobs SET error_code = ? WHERE id = ?').run(code, job.id);
    later(job, RETRY_BASE_MS * 4 ** job.attempts, { countAttempt: true });
  }
}

async function runJob(job) {
  let result;
  try {
    result = await suggestForFile(job.user_id, job.node_id, {
      kinds: JSON.parse(job.kinds),
      explicit: job.explicit === 1,
      waitForIndex: job.source === 'upload',
    });
  } catch (error) {
    jobFailed(job, error);
    return;
  }
  if (result.wait) {
    later(job, INDEX_WAIT_RETRY_MS, { countAttempt: false });
  } else if (result.skipped) {
    finish(job, 'skipped', result.skipped);
  } else {
    finish(job, 'done');
    if (result.created.length > 0) announceSuggestions(job.user_id);
  }
}

// Due jobs of accounts that aren't paused, oldest first.
function claimJobs(limit) {
  const due = db.prepare(`
    SELECT * FROM ai_jobs WHERE status = 'pending' AND run_after <= datetime('now') ORDER BY id LIMIT 500
  `).all();
  const paused = new Map();
  const claimed = [];
  for (const job of due) {
    if (!paused.has(job.user_id)) paused.set(job.user_id, pauseReason(job.user_id) !== null);
    if (paused.get(job.user_id)) continue;
    claimed.push(job);
    if (claimed.length >= limit) break;
  }
  const mark = db.prepare("UPDATE ai_jobs SET status = 'running', updated_at = datetime('now') WHERE id = ?");
  db.transaction(() => claimed.forEach((job) => mark.run(job.id)))();
  return claimed;
}

function cleanUp() {
  db.prepare(`
    DELETE FROM ai_jobs WHERE status IN ('done', 'skipped', 'failed') AND updated_at < datetime('now', ?)
  `).run(`-${KEEP_FINISHED_DAYS} days`);
}

async function workPass(maxJobs) {
  followJournal();
  cleanUp();
  const jobs = claimJobs(maxJobs);
  let next = 0;
  const worker = async () => {
    while (next < jobs.length) {
      const job = jobs[next];
      next += 1;
      await runJob(job);
    }
  };
  await Promise.all(Array.from({ length: Math.min(CONCURRENCY, jobs.length) }, worker));
  return jobs.length;
}

// One pass at a time, whoever asks (the loop or a test).
let current = Promise.resolve(0);

function runAiPass(maxJobs = JOBS_PER_PASS) {
  current = current.catch(() => 0).then(() => workPass(maxJobs));
  return current;
}

/** Runs passes until no job is due; returns how many jobs ran. */
async function runAiJobsUntilIdle() {
  let total = 0;
  for (;;) {
    const done = await runAiPass();
    total += done;
    if (done === 0) return total;
  }
}

// --- The loop -------------------------------------------------------------------------------------

let timer = null;
let stopped = true;

function schedule(ms) {
  clearTimeout(timer);
  timer = setTimeout(tick, ms);
  timer.unref();
}

async function tick() {
  if (stopped) return;
  let done = 0;
  try {
    done = await runAiPass();
  } catch (error) {
    logger.error('AI job pass failed:', error.message);
  }
  if (!stopped) schedule(done > 0 ? BUSY_GAP_MS : IDLE_MS);
}

/** Starts the worker (jobs a stop left running wait again); returns a function that stops it. */
function startAiWorker() {
  db.prepare("UPDATE ai_jobs SET status = 'pending' WHERE status = 'running'").run();
  stopped = false;
  schedule(FIRST_PASS_MS);
  return async () => {
    stopped = true;
    clearTimeout(timer);
    await current.catch(() => 0);
  };
}

function wakeAiWorker() {
  if (!stopped) schedule(0);
}


module.exports = { runAiJobsUntilIdle, startAiWorker, wakeAiWorker };
