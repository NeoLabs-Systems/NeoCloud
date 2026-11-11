'use strict';

// Rotates through every stored blob about once a week, a slice per hourly
// maintenance pass: the file exists, has the right size, and its first and
// last segments authenticate. Damaged blobs are remembered until a later
// check finds them fine or their row is gone; admin health shows the count.
// Progress survives restarts in the 'blob-scrub' state file.

const db = require('../../db/database');
const { createServiceLogger } = require('../../utils/logger');
const { readState, writeState } = require('../state_store');
const { checkBlob } = require('./blob_store');

const logger = createServiceLogger('BlobScrub');
const STATE = 'blob-scrub';
const CYCLE_PASSES = 7 * 24;
const MIN_SLICE = 100;

function emptyState() {
  return { cursor: '', damaged: {}, cycleStartedAt: null, lastCycleAt: null };
}

/** Checks the next slice of blobs. Returns how many it checked. */
async function scrubSlice() {
  const state = { ...emptyState(), ...readState(STATE, {}) };
  const { total } = db.prepare('SELECT COUNT(*) AS total FROM blobs').get();
  const limit = Math.max(MIN_SLICE, Math.ceil(total / CYCLE_PASSES));
  const rows = db.prepare('SELECT id, user_id, size FROM blobs WHERE id > ? ORDER BY id LIMIT ?').all(state.cursor, limit);
  if (!state.cursor) state.cycleStartedAt = new Date().toISOString();

  const stillExists = db.prepare('SELECT 1 FROM blobs WHERE id = ?');
  for (const row of rows) {
    const problem = await checkBlob(row.user_id, row.id, row.size);
    // Deleted for good while being checked: not damage.
    if (problem && stillExists.get(row.id)) {
      if (!state.damaged[row.id]) logger.warn(`Blob ${row.id} of account ${row.user_id}: ${problem}.`);
      state.damaged[row.id] = { userId: row.user_id, problem, at: new Date().toISOString() };
    } else {
      delete state.damaged[row.id];
    }
  }

  if (rows.length < limit) {
    for (const id of Object.keys(state.damaged)) {
      if (!stillExists.get(id)) delete state.damaged[id];
    }
    state.cursor = '';
    state.lastCycleAt = new Date().toISOString();
  } else {
    state.cursor = rows[rows.length - 1].id;
  }
  writeState(STATE, state);
  return rows.length;
}

function getScrubStatus() {
  const state = { ...emptyState(), ...readState(STATE, {}) };
  return { damaged: Object.keys(state.damaged).length, lastCycleAt: state.lastCycleAt };
}

module.exports = { getScrubStatus, scrubSlice };
