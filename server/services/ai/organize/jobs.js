'use strict';

// AI jobs the person starts, and how far they got. `POST /api/ai/jobs`
// queues chosen files ("suggest names for these": their names are suggested
// even when they already read well) or a backfill over a folder (meaningless
// names only). Only the account's own files: vault, excluded, trashed and
// shared-with-me files are refused or left out. The worker (worker.js) runs
// them.

const crypto = require('crypto');
const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { getLiveNode } = require('../../files/tree');
const { assertAiUsable } = require('../policy');
const { assertAiAllowedForNode } = require('../exclusions');
const { readSettings } = require('../../settings/store');
const { isSkippedType } = require('../file_types');
const { JOB_KINDS, aiFeatures, pauseReason } = require('./features');
const { enqueue } = require('./queue');
const { wakeAiWorker } = require('./worker');

const MAX_REQUEST_NODES = 1000;
const MAX_BACKFILL_FILES = 5000;

function parseKinds(userId, value) {
  const features = aiFeatures(userId);
  if (value === undefined || value === null) {
    const enabled = JOB_KINDS.filter((kind) => features[kind]);
    if (enabled.length === 0) throw httpError(403, 'Every AI suggestion feature is turned off.', 'AI_FEATURE_OFF');
    return enabled;
  }
  if (!Array.isArray(value) || value.length === 0 || value.some((kind) => !JOB_KINDS.includes(kind))) {
    throw httpError(400, `kinds must be a list of ${JOB_KINDS.join(', ')}.`, 'INVALID_KINDS');
  }
  const kinds = [...new Set(value)];
  if (kinds.some((kind) => !features[kind])) {
    throw httpError(403, 'This AI feature is turned off in Settings › AI.', 'AI_FEATURE_OFF');
  }
  return kinds;
}

// The files below a folder AI may read: no vault, nothing below an excluded
// folder, nothing in the trash, no file type the person left out of AI.
function backfillFiles(userId, folderId) {
  const rows = db.prepare(`
    WITH RECURSIVE down(id) AS (
      SELECT id FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL
      SELECT n.id FROM nodes n JOIN down ON n.parent_id = down.id
      WHERE n.trashed_at IS NULL AND n.vault_id IS NULL AND n.ai_excluded = 0
    )
    SELECT n.id, n.name FROM down JOIN nodes n ON n.id = down.id
    WHERE n.type = 'file' AND n.blob_id IS NOT NULL
  `).all(folderId, userId);
  const skip = readSettings(userId).ai_skip_types;
  return rows.filter((row) => !isSkippedType(row.name, skip)).slice(0, MAX_BACKFILL_FILES + 1).map((row) => row.id);
}

/**
 * Starts suggestions: `nodeIds` (the owner's files; their names are
 * suggested even when they already read well) or `folderId` (a backfill over
 * everything below it, renaming only meaningless names). `kinds` defaults to
 * every switched-on kind. Returns { batchId, queued }.
 */
function requestSuggestions(userId, { nodeIds, folderId, kinds } = {}) {
  assertAiUsable(userId);
  const wanted = parseKinds(userId, kinds);
  const byIds = nodeIds !== undefined && nodeIds !== null;
  if (byIds === (folderId !== undefined && folderId !== null)) {
    throw httpError(400, 'Send either nodeIds or folderId.', 'INVALID_REQUEST');
  }
  const batchId = crypto.randomUUID();
  let ids;
  if (byIds) {
    if (!Array.isArray(nodeIds) || nodeIds.length === 0 || nodeIds.length > MAX_REQUEST_NODES) {
      throw httpError(400, `nodeIds must list 1 to ${MAX_REQUEST_NODES} files.`, 'INVALID_REQUEST');
    }
    ids = [...new Set(nodeIds.map(String))];
    for (const id of ids) {
      getLiveNode(userId, id, { type: 'file' });
      assertAiAllowedForNode(userId, id);
    }
  } else {
    const folder = getLiveNode(userId, folderId, { type: 'folder' });
    assertAiAllowedForNode(userId, folder.id);
    ids = backfillFiles(userId, folder.id);
    if (ids.length > MAX_BACKFILL_FILES) {
      throw httpError(413, `A backfill covers at most ${MAX_BACKFILL_FILES} files; pick a smaller folder.`, 'TOO_MANY_FILES');
    }
  }
  const queued = enqueue(userId, ids, { kinds: wanted, source: byIds ? 'request' : 'backfill', explicit: byIds, batchId });
  wakeAiWorker();
  return { batchId, queued };
}

/**
 * The account's queue: counts by status, why it is paused (or null), and the
 * progress of its recent requests and backfills (one, with `batchId`).
 */
function jobStatus(userId, { batchId } = {}) {
  const counts = { pending: 0, running: 0, done: 0, skipped: 0, failed: 0 };
  for (const row of db.prepare('SELECT status, COUNT(*) AS count FROM ai_jobs WHERE user_id = ? GROUP BY status').all(userId)) {
    counts[row.status] = row.count;
  }
  const batches = db.prepare(`
    SELECT batch_id AS id, MIN(created_at) AS createdAt, COUNT(*) AS total,
           SUM(status IN ('pending', 'running')) AS pending, SUM(status = 'done') AS done,
           SUM(status = 'skipped') AS skipped, SUM(status = 'failed') AS failed
    FROM ai_jobs WHERE user_id = ? AND batch_id IS NOT NULL AND (? IS NULL OR batch_id = ?)
    GROUP BY batch_id ORDER BY MIN(id) DESC LIMIT 10
  `).all(userId, batchId ?? null, batchId ?? null);
  if (batchId && batches.length === 0) throw httpError(404, 'Batch not found', 'NOT_FOUND');
  return { ...counts, paused: counts.pending > 0 ? pauseReason(userId) : null, batches };
}

module.exports = { jobStatus, requestSuggestions };
