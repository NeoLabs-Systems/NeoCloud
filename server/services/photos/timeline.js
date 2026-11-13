'use strict';

// The Photos timeline: an account's photos and videos, newest first, in
// pages that continue from a cursor (keyset paging on date_state, taken_at
// and the node id, so page 500 costs what page 1 does), plus counts per day
// and month for the date scrubber and section headers.
//
// A date that is only a guess never lands under a day: items whose file
// wasn't read yet (date_state 2, "sorting") come first, then the dated
// ones by day, then those whose date is unknown (date_state 0, only the
// upload time is known) at the very end.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { catchUp } = require('./indexer');

const DEFAULT_LIMIT = 200;
const MAX_LIMIT = 500;
const DAY_PATTERN = /^\d{4}-\d{2}-\d{2}$/;
const TAKEN_AT_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/;

const STATE_SORTING = 2;
const STATE_DATED = 1;
const STATE_UNKNOWN = 0;
const LAST_KEY = { takenAt: '9999-12-31T23:59:59', nodeId: '\uffff' };

function encodeCursor(row) {
  return Buffer.from(JSON.stringify([row.date_state, row.taken_at, row.node_id])).toString('base64url');
}

function decodeCursor(value) {
  try {
    const [state, takenAt, nodeId] = JSON.parse(Buffer.from(String(value), 'base64url').toString('utf8'));
    if ([STATE_SORTING, STATE_DATED, STATE_UNKNOWN].includes(state) && TAKEN_AT_PATTERN.test(takenAt)
      && typeof nodeId === 'string' && nodeId) return { state, takenAt, nodeId };
  } catch {
    // Falls through to the error below.
  }
  throw httpError(400, 'Invalid cursor', 'INVALID_CURSOR');
}

function parseLimit(value) {
  if (value === undefined || value === null || value === '') return DEFAULT_LIMIT;
  const limit = Number(value);
  if (!Number.isInteger(limit) || limit < 1) throw httpError(400, 'Invalid limit', 'INVALID_LIMIT');
  return Math.min(limit, MAX_LIMIT);
}

// Where the page starts: after the cursor, at the newest item of `day`
// (or the first of "unknown"), or at the very top. The key is exclusive.
function startKey({ cursor, day }) {
  if (cursor) return decodeCursor(cursor);
  if (day === 'unknown') return { state: STATE_UNKNOWN, ...LAST_KEY };
  if (day) {
    if (!DAY_PATTERN.test(String(day))) throw httpError(400, 'day must be YYYY-MM-DD or unknown', 'INVALID_DAY');
    return { state: STATE_DATED, takenAt: `${day}T23:59:59`, nodeId: '\uffff' };
  }
  return { state: STATE_SORTING, ...LAST_KEY };
}

function serialize(row) {
  return {
    id: row.node_id,
    takenAt: row.taken_at,
    kind: row.kind,
    width: row.width,
    height: row.height,
    durationSeconds: row.duration_seconds,
    name: row.name,
    parentId: row.parent_id,
    mime: row.mime,
    size: row.size,
    etag: row.etag,
    favorite: Boolean(row.favorite),
    // exif | client | filename | mtime | upload
    dateSource: row.taken_at_source || 'upload',
    // The file wasn't read yet: the date may still change.
    provisional: row.date_state === STATE_SORTING,
    // Nothing but the upload time is known.
    dateUnknown: row.date_state === STATE_UNKNOWN,
  };
}

/**
 * A page of the timeline: { items, nextCursor } with nextCursor null on the
 * last page. `day` ("YYYY-MM-DD") starts the page at that day instead, for
 * jumping there from the date scrubber.
 */
function listPhotos(userId, { cursor = null, day = null, limit } = {}) {
  const pageSize = parseLimit(limit);
  const start = startKey({ cursor, day });
  catchUp(userId);
  const rows = db.prepare(`
    SELECT p.node_id, p.taken_at, p.taken_at_source, p.date_state, p.kind, p.width, p.height, p.duration_seconds,
      n.name, n.parent_id, n.mime, n.size, n.etag,
      EXISTS (SELECT 1 FROM favorites f WHERE f.user_id = p.user_id AND f.node_id = p.node_id) AS favorite
    FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.user_id = @userId AND (p.date_state, p.taken_at, p.node_id) < (@state, @takenAt, @nodeId)
      AND n.trashed_at IS NULL AND n.vault_id IS NULL
    ORDER BY p.date_state DESC, p.taken_at DESC, p.node_id DESC
    LIMIT @limit
  `).all({ userId, state: start.state, takenAt: start.takenAt, nodeId: start.nodeId, limit: pageSize });
  return {
    items: rows.map(serialize),
    nextCursor: rows.length === pageSize ? encodeCursor(rows[rows.length - 1]) : null,
  };
}

/**
 * { total, pending, unknown, days: [{ date, count }], months: [{ month, count }] },
 * newest first. The timeline holds, in this order, `pending` items whose
 * file hasn't been read yet (their date is a guess and may move), the
 * days, and `unknown` items with no date of their own:
 * total = pending + sum(days) + unknown.
 */
function photoSummary(userId) {
  catchUp(userId);
  const days = db.prepare(`
    SELECT substr(taken_at, 1, 10) AS date, COUNT(*) AS count FROM photo_items
    WHERE user_id = ? AND date_state = 1 GROUP BY date ORDER BY date DESC
  `).all(userId);
  const months = [];
  for (const { date, count } of days) {
    const month = date.slice(0, 7);
    if (months.length > 0 && months[months.length - 1].month === month) months[months.length - 1].count += count;
    else months.push({ month, count });
  }
  const counts = db.prepare(`
    SELECT COALESCE(SUM(date_state = 2), 0) AS pending, COALESCE(SUM(date_state = 0), 0) AS unknown
    FROM photo_items WHERE user_id = ?
  `).get(userId);
  return {
    total: counts.pending + counts.unknown + days.reduce((sum, day) => sum + day.count, 0),
    pending: counts.pending,
    unknown: counts.unknown,
    days,
    months,
  };
}

module.exports = { listPhotos, photoSummary };
