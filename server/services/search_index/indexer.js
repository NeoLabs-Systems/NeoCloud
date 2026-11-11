'use strict';

// The background indexer: one loop, one file at a time.
//
// Each pass
//   1. drops the entries of accounts that opted out,
//   2. follows the change journal from its saved cursor and drops entries
//      that must go: trashed files and folders (with everything below them),
//      files renamed to a type the index doesn't read ('delete' needs nothing:
//      the row goes with the node),
//   3. indexes files that have no entry for their current content yet: new,
//      changed, restored, or never seen (first start, rebuild, opt-in again).
// Step 3 works from the tables, not from memory, so a restart simply picks up
// where the last run stopped. Vault files and trashed files are never read.
// While the admin setting is off the loop idles and the index stays as it is.

const db = require('../../db/database');
const { readEnvBool } = require('../../../runtime/env');
const { createServiceLogger } = require('../../utils/logger');
const { LIVE_NODES_CTE, ancestry } = require('../files/tree');
const { ExtractError, extractNodeText, hasPdftotext, stopExtractor } = require('./extract');
const { indexableExtensions, indexableKind } = require('./extractors');
const store = require('./store');

const logger = createServiceLogger('SearchIndex');

const ENABLED_ENV = 'NEOCLOUD_FULL_TEXT_SEARCH';
const FILES_PER_PASS = 25;
const JOURNAL_PAGE = 500;
const FIRST_PASS_MS = 15 * 1000;
const IDLE_MS = 5 * 1000;
// A breather between busy passes, so requests always get their turn.
const BUSY_GAP_MS = 50;

function isIndexEnabled() {
  return readEnvBool(ENABLED_ENV, false);
}

// --- Step 2: the change journal -------------------------------------------------

function applyChange({ user_id: userId, node_id: nodeId, op }) {
  // New content ('update') needs nothing here: a stale entry is never shown
  // and step 3 reads the file again.
  if (op !== 'trash' && op !== 'rename' && op !== 'move') return;
  const chain = ancestry(userId, nodeId);
  if (chain.length === 0) {
    store.removeNode(nodeId);
    return;
  }
  const node = db.prepare('SELECT type, name, mime, vault_id FROM nodes WHERE id = ?').get(nodeId);
  if (chain.some((entry) => entry.trashed_at) || node.vault_id) {
    store.removeSubtree(userId, nodeId);
  } else if (node.type === 'file' && !indexableKind(node.name, node.mime)) {
    store.removeNode(nodeId);
  }
}

function latestSeq() {
  return db.prepare('SELECT COALESCE(MAX(seq), 0) AS seq FROM changes').get().seq;
}

function followJournal() {
  const saved = store.readState('cursor');
  if (saved === null) {
    // Nothing indexed before this point; step 3 covers every file.
    store.writeState('cursor', latestSeq());
    return;
  }
  let cursor = Number(saved);
  for (;;) {
    const rows = db.prepare('SELECT seq, user_id, node_id, op FROM changes WHERE seq > ? ORDER BY seq LIMIT ?')
      .all(cursor, JOURNAL_PAGE);
    if (rows.length === 0) return;
    db.transaction(() => {
      for (const row of rows) applyChange(row);
      store.writeState('cursor', rows[rows.length - 1].seq);
    })();
    cursor = rows[rows.length - 1].seq;
    if (rows.length < JOURNAL_PAGE) return;
  }
}

// --- Step 3: files without a current entry ------------------------------------------

const PENDING_WHERE = `
  n.user_id = @userId AND n.type = 'file' AND n.vault_id IS NULL AND n.trashed_at IS NULL
  AND n.blob_id IS NOT NULL
  AND n.id NOT IN (SELECT id FROM hidden)
  AND (n.mime LIKE 'text/%' OR n.mime = 'application/pdf'
    OR EXISTS (SELECT 1 FROM json_each(@extensions) e WHERE lower(n.name) LIKE '%' || e.value))
  AND NOT EXISTS (SELECT 1 FROM search_docs d WHERE d.node_id = n.id AND d.blob_id = n.blob_id)
`;

function pendingFiles(userId, limit) {
  return db.prepare(`
    ${LIVE_NODES_CTE}
    SELECT n.id, n.name, n.mime, n.size, n.blob_id FROM nodes n
    WHERE ${PENDING_WHERE}
    ORDER BY n.modified_at DESC
    LIMIT @limit
  `).all({ userId, limit, extensions: JSON.stringify(indexableExtensions()) });
}

function pendingCount(userId) {
  return db.prepare(`
    ${LIVE_NODES_CTE}
    SELECT COUNT(*) AS count FROM nodes n WHERE ${PENDING_WHERE}
  `).get({ userId, extensions: JSON.stringify(indexableExtensions()) }).count;
}

// Accounts whose files go into the index.
function indexedUsers() {
  return db.prepare(`
    SELECT id FROM users WHERE id NOT IN (
      SELECT user_id FROM user_settings WHERE key = 'fullTextSearch' AND value = 'false'
    ) ORDER BY id
  `).all().map((row) => row.id);
}

async function indexFile(userId, node) {
  let text;
  try {
    text = await extractNodeText(userId, node);
  } catch (err) {
    if (!(err instanceof ExtractError)) logger.warn(`Could not read a file for the index: ${err.message}`);
    store.saveFailed(userId, node);
    return;
  }
  store.saveIndexed(userId, node, text);
}

// What step 3 depends on: the journal position and who opted out. When it
// hasn't moved since a pass found nothing to do, the scan is skipped, so an
// idle server with many files costs two tiny queries per tick.
let idleAt = null;

function workMark() {
  const optedOut = db.prepare(`
    SELECT group_concat(user_id) AS ids FROM user_settings WHERE key = 'fullTextSearch' AND value = 'false'
  `).get().ids;
  return `${latestSeq()}|${optedOut ?? ''}`;
}

async function indexPass(maxFiles) {
  const mark = workMark();
  if (mark === idleAt) return 0;
  store.removeOptedOut();
  followJournal();
  let done = 0;
  for (const userId of indexedUsers()) {
    for (const node of pendingFiles(userId, maxFiles - done)) {
      await indexFile(userId, node);
      done += 1;
    }
    if (done >= maxFiles) break;
  }
  if (done === 0) idleAt = mark;
  return done;
}

// One pass at a time, whoever asks (the loop, a test, a rebuild).
let current = Promise.resolve(0);

function runPass(maxFiles = FILES_PER_PASS) {
  current = current.catch(() => 0).then(() => (isIndexEnabled() ? indexPass(maxFiles) : 0));
  return current;
}

/** Runs passes until nothing is left to index; returns the number of files read. */
async function runIndexerUntilIdle() {
  let total = 0;
  for (;;) {
    const done = await runPass();
    total += done;
    if (done === 0) return total;
  }
}

// --- The loop ---------------------------------------------------------------------

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
    done = await runPass();
  } catch (err) {
    logger.error('Indexing pass failed:', err.message);
  }
  if (!stopped) schedule(done > 0 ? BUSY_GAP_MS : IDLE_MS);
}

/** Starts the loop; returns a function that stops it. */
function startSearchIndexer() {
  stopped = false;
  schedule(FIRST_PASS_MS);
  return async () => {
    stopped = true;
    clearTimeout(timer);
    await current.catch(() => 0);
    await stopExtractor();
  };
}

/** Starts the next pass now instead of after the idle wait. */
function wakeIndexer() {
  if (!stopped) schedule(0);
}

// --- Admin ----------------------------------------------------------------------

async function getIndexStatus() {
  const totals = store.indexTotals();
  const pending = indexedUsers().reduce((sum, userId) => sum + pendingCount(userId), 0);
  return {
    enabled: isIndexEnabled(),
    indexed: totals.indexed,
    pending,
    failed: totals.failed,
    bytes: totals.bytes,
    // Office and OpenDocument text comes from the bundled zip reader.
    available: { pdftotext: await hasPdftotext(), office: true },
  };
}

/** Throws the index away; the loop reads every file again. */
async function rebuildIndex() {
  // Queued behind a running pass, so its results can't land after the wipe.
  current = current.catch(() => 0).then(() => {
    store.clearIndex();
    store.writeState('cursor', latestSeq());
    idleAt = null;
    return 0;
  });
  await current;
  logger.info('Search index cleared for a rebuild.');
  wakeIndexer();
  return getIndexStatus();
}

module.exports = {
  ENABLED_ENV,
  getIndexStatus,
  isIndexEnabled,
  rebuildIndex,
  runIndexerUntilIdle,
  startSearchIndexer,
  wakeIndexer,
};
