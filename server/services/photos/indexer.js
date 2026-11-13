'use strict';

// Keeps photo_items in step with the files: every live photo and video of
// an account, outside vaults and the trash, with the time it was taken.
//
// Two steps, both cheap enough to run before a timeline request:
//   1. follow the change journal from the saved cursor and drop rows that
//      must go: trashed files and folders (with everything below them),
//      files moved into a vault or renamed to another type ('delete' needs
//      nothing: the row goes with the node);
//   2. add the account's photos and videos that have no row for their
//      current content, dated provisionally (date_state 2) from what is
//      known without reading the file: the client's dates, the file name,
//      else the upload time.
// The background loop then reads each new row's file once (probe.js) for
// the real date and size, settles the date (exif, client, filename, mtime,
// or upload: date_state 0, "date unknown"), and makes its grid thumbnail
// ahead of time.

const db = require('../../db/database');
const { createServiceLogger } = require('../../utils/logger');
const { LIVE_NODES_CTE, ancestry } = require('../files/tree');
const { prepareGridThumbnail } = require('../preview/previews');
const { probeMedia, resolveTakenAt, takenFromName, uploadedAt } = require('./probe');

const logger = createServiceLogger('Photos');

const JOURNAL_PAGE = 500;
const ADD_BATCH = 2000;
const FILES_PER_PASS = 10;
const FIRST_PASS_MS = 10 * 1000;
const IDLE_MS = 5 * 1000;
const BUSY_GAP_MS = 50;
const NOT_PHOTO_MIMES = ['image/svg+xml', 'image/x-icon', 'image/vnd.microsoft.icon'];

const MEDIA_WHERE = `
  ((n.mime LIKE 'image/%' AND n.mime NOT IN (SELECT value FROM json_each(@notPhotos)))
    OR n.mime LIKE 'video/%')
`;

function kindOf(mime) {
  return String(mime || '').startsWith('video/') ? 'video' : 'photo';
}

function isMedia(mime) {
  const value = String(mime || '');
  if (value.startsWith('video/')) return true;
  return value.startsWith('image/') && !NOT_PHOTO_MIMES.includes(value);
}

function readState(key) {
  return db.prepare('SELECT value FROM photo_index_state WHERE key = ?').get(key)?.value ?? null;
}

function writeState(key, value) {
  db.prepare(`
    INSERT INTO photo_index_state (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE SET value = excluded.value
  `).run(key, String(value));
}

// --- Step 1: the change journal ---------------------------------------------------

function removeSubtree(userId, nodeId) {
  db.prepare(`
    WITH RECURSIVE below(id) AS (
      SELECT id FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL SELECT n.id FROM nodes n JOIN below ON n.parent_id = below.id
    )
    DELETE FROM photo_items WHERE node_id IN (SELECT id FROM below)
  `).run(nodeId, userId);
}

function applyChange({ user_id: userId, node_id: nodeId, op }) {
  if (op !== 'trash' && op !== 'rename' && op !== 'move') return;
  const chain = ancestry(userId, nodeId);
  if (chain.length === 0) return;
  const node = db.prepare('SELECT type, mime, vault_id FROM nodes WHERE id = ?').get(nodeId);
  if (chain.some((entry) => entry.trashed_at) || node.vault_id) {
    removeSubtree(userId, nodeId);
  } else if (node.type === 'file' && !isMedia(node.mime)) {
    db.prepare('DELETE FROM photo_items WHERE node_id = ?').run(nodeId);
  }
}

function latestSeq() {
  return db.prepare('SELECT COALESCE(MAX(seq), 0) AS seq FROM changes').get().seq;
}

function followJournal() {
  const saved = readState('cursor');
  if (saved === null) {
    // Nothing indexed before this point; step 2 covers every file.
    writeState('cursor', latestSeq());
    return;
  }
  let cursor = Number(saved);
  for (;;) {
    const rows = db.prepare('SELECT seq, user_id, node_id, op FROM changes WHERE seq > ? ORDER BY seq LIMIT ?')
      .all(cursor, JOURNAL_PAGE);
    if (rows.length === 0) return;
    db.transaction(() => {
      for (const row of rows) applyChange(row);
      writeState('cursor', rows[rows.length - 1].seq);
    })();
    cursor = rows[rows.length - 1].seq;
    if (rows.length < JOURNAL_PAGE) return;
  }
}

// --- Rows from before taken_at_source --------------------------------------------------

// Works out where an older row's date came from, without reading files:
// a read row whose date is still the upload time had no date of its own.
function backfillSources() {
  const rows = db.prepare(`
    SELECT p.node_id, p.taken_at, p.probed, n.name, n.modified_at
    FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.taken_at_source IS NULL LIMIT 1000
  `).all();
  if (rows.length === 0) return false;
  const update = db.prepare('UPDATE photo_items SET taken_at_source = ?, date_state = ? WHERE node_id = ?');
  db.transaction(() => {
    for (const row of rows) {
      let source;
      if (takenFromName(row.name) === row.taken_at) source = 'filename';
      else if (uploadedAt(row) === row.taken_at) source = 'upload';
      else source = row.probed === 1 ? 'exif' : 'upload';
      const state = row.probed === 0 ? 2 : (source === 'upload' ? 0 : 1);
      update.run(source, state, row.node_id);
    }
  })();
  return rows.length === 1000;
}

let backfilled = false;

function backfillOnce() {
  if (backfilled) return;
  while (backfillSources());
  backfilled = true;
}

// --- Step 2: photos and videos without a row ----------------------------------------

function missingMedia(userId) {
  return db.prepare(`
    ${LIVE_NODES_CTE}
    SELECT n.id, n.name, n.mime, n.blob_id, n.modified_at,
      d.created_at AS client_created_at, d.modified_at AS client_modified_at
    FROM nodes n LEFT JOIN file_client_dates d ON d.node_id = n.id AND d.blob_id = n.blob_id
    WHERE n.user_id = @userId AND n.type = 'file' AND n.vault_id IS NULL AND n.trashed_at IS NULL
      AND n.blob_id IS NOT NULL AND ${MEDIA_WHERE}
      AND n.id NOT IN (SELECT id FROM hidden)
      AND NOT EXISTS (SELECT 1 FROM photo_items p WHERE p.node_id = n.id AND p.blob_id = n.blob_id)
    LIMIT @limit
  `).all({ userId, limit: ADD_BATCH, notPhotos: JSON.stringify(NOT_PHOTO_MIMES) });
}

function addMissing(userId) {
  const upsert = db.prepare(`
    INSERT INTO photo_items (node_id, user_id, blob_id, kind, taken_at, taken_at_source, date_state)
    VALUES (?, ?, ?, ?, ?, ?, 2)
    ON CONFLICT(node_id) DO UPDATE SET blob_id = excluded.blob_id, kind = excluded.kind,
      taken_at = excluded.taken_at, taken_at_source = excluded.taken_at_source, date_state = 2,
      width = NULL, height = NULL, duration_seconds = NULL, probed = 0
  `);
  for (;;) {
    const rows = missingMedia(userId);
    db.transaction(() => {
      for (const node of rows) {
        const { takenAt, source } = resolveTakenAt(node);
        upsert.run(node.id, userId, node.blob_id, kindOf(node.mime), takenAt, source);
      }
    })();
    if (rows.length < ADD_BATCH) return;
  }
}

// The journal position each account's rows were last checked at. Step 2
// runs again for an account only when its own part of the journal moved.
const checkedAt = new Map();

function accountSeq(userId) {
  return db.prepare('SELECT COALESCE(MAX(seq), 0) AS seq FROM changes WHERE user_id = ?').get(userId).seq;
}

/**
 * Brings the account's rows up to date with its files (steps 1 and 2).
 * Synchronous, and close to free when nothing changed: the timeline calls
 * it before answering, so uploads show and trashed files go at once.
 */
function catchUp(userId) {
  backfillOnce();
  followJournal();
  const seq = accountSeq(userId);
  if (checkedAt.get(userId) === seq) return;
  addMissing(userId);
  checkedAt.set(userId, seq);
}

// --- The background reads -------------------------------------------------------------

function unprobed(limit) {
  return db.prepare(`
    SELECT p.user_id, p.kind, n.id, n.name, n.mime, n.size, n.blob_id, n.modified_at,
      d.created_at AS client_created_at, d.modified_at AS client_modified_at
    FROM photo_items p JOIN nodes n ON n.id = p.node_id
      LEFT JOIN file_client_dates d ON d.node_id = n.id AND d.blob_id = n.blob_id
    WHERE p.probed = 0 AND n.blob_id = p.blob_id
    ORDER BY p.taken_at DESC
    LIMIT ?
  `).all(limit);
}

function saveProbe(row, found, probed) {
  const { takenAt, source } = resolveTakenAt(row, found.takenAt || null);
  db.prepare(`
    UPDATE photo_items SET
      taken_at = @takenAt, taken_at_source = @source, date_state = @state, width = @width, height = @height,
      duration_seconds = @durationSeconds, probed = @probed
    WHERE node_id = @nodeId AND blob_id = @blobId
  `).run({
    nodeId: row.id,
    blobId: row.blob_id,
    takenAt,
    source,
    state: source === 'upload' ? 0 : 1,
    width: found.width || null,
    height: found.height || null,
    durationSeconds: found.durationSeconds || null,
    probed,
  });
}

async function probeOne(row) {
  const node = { id: row.id, name: row.name, mime: row.mime, size: row.size, blob_id: row.blob_id };
  try {
    saveProbe(row, await probeMedia(row.user_id, node, row.kind), 1);
  } catch (err) {
    logger.warn(`Could not read a ${row.mime} file for Photos: ${err.message}`);
    saveProbe(row, {}, 2);
    return;
  }
  if (row.kind === 'photo') {
    await prepareGridThumbnail(row.user_id, node).catch((err) => {
      logger.warn(`Could not prepare a grid thumbnail: ${err.message}`);
    });
  }
}

function allUsers() {
  return db.prepare('SELECT id FROM users ORDER BY id').all().map((row) => row.id);
}

async function indexPass(maxFiles) {
  for (const userId of allUsers()) catchUp(userId);
  const rows = unprobed(maxFiles);
  for (const row of rows) await probeOne(row);
  return rows.length;
}

// One pass at a time, whoever asks (the loop or a test).
let current = Promise.resolve(0);

function runPass(maxFiles = FILES_PER_PASS) {
  current = current.catch(() => 0).then(() => indexPass(maxFiles));
  return current;
}

/** Runs passes until every photo and video was read; returns how many were. */
async function runPhotoIndexerUntilIdle() {
  let total = 0;
  for (;;) {
    const done = await runPass();
    total += done;
    if (done === 0) return total;
  }
}

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
    logger.error('Photos pass failed:', err.message);
  }
  if (!stopped) schedule(done > 0 ? BUSY_GAP_MS : IDLE_MS);
}

/** Starts the loop; returns a function that stops it. */
function startPhotoIndexer() {
  stopped = false;
  schedule(FIRST_PASS_MS);
  return async () => {
    stopped = true;
    clearTimeout(timer);
    await current.catch(() => 0);
  };
}

module.exports = { catchUp, runPhotoIndexerUntilIdle, startPhotoIndexer };
