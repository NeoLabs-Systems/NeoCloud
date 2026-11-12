'use strict';

// The Photos timeline: an account's photos and videos, newest first, in
// pages that continue from a cursor (keyset paging on taken_at and the node
// id, so page 500 costs what page 1 does), plus counts per day and month
// for the date scrubber and section headers.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { catchUp } = require('./indexer');

const DEFAULT_LIMIT = 200;
const MAX_LIMIT = 500;
const DAY_PATTERN = /^\d{4}-\d{2}-\d{2}$/;
const TAKEN_AT_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/;

function encodeCursor(row) {
  return Buffer.from(JSON.stringify([row.taken_at, row.node_id])).toString('base64url');
}

function decodeCursor(value) {
  try {
    const [takenAt, nodeId] = JSON.parse(Buffer.from(String(value), 'base64url').toString('utf8'));
    if (TAKEN_AT_PATTERN.test(takenAt) && typeof nodeId === 'string' && nodeId) return { takenAt, nodeId };
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

// Where the page starts: after the cursor, at the newest item of `day`, or
// at the newest item of all. The upper key is exclusive.
function startKey({ cursor, day }) {
  if (cursor) return decodeCursor(cursor);
  if (day) {
    if (!DAY_PATTERN.test(String(day))) throw httpError(400, 'day must be YYYY-MM-DD', 'INVALID_DAY');
    return { takenAt: `${day}T23:59:59`, nodeId: '￿' };
  }
  return { takenAt: '9999-12-31T23:59:59', nodeId: '￿' };
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
    SELECT p.node_id, p.taken_at, p.kind, p.width, p.height, p.duration_seconds,
      n.name, n.parent_id, n.mime, n.size, n.etag,
      EXISTS (SELECT 1 FROM favorites f WHERE f.user_id = p.user_id AND f.node_id = p.node_id) AS favorite
    FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.user_id = @userId AND (p.taken_at, p.node_id) < (@takenAt, @nodeId)
      AND n.trashed_at IS NULL AND n.vault_id IS NULL
    ORDER BY p.taken_at DESC, p.node_id DESC
    LIMIT @limit
  `).all({ userId, takenAt: start.takenAt, nodeId: start.nodeId, limit: pageSize });
  return {
    items: rows.map(serialize),
    nextCursor: rows.length === pageSize ? encodeCursor(rows[rows.length - 1]) : null,
  };
}

/**
 * { total, pending, days: [{ date, count }], months: [{ month, count }] },
 * newest first. `pending` counts items whose file hasn't been read yet:
 * their date is still a guess and may move.
 */
function photoSummary(userId) {
  catchUp(userId);
  const days = db.prepare(`
    SELECT substr(taken_at, 1, 10) AS date, COUNT(*) AS count FROM photo_items
    WHERE user_id = ? GROUP BY date ORDER BY date DESC
  `).all(userId);
  const months = [];
  for (const { date, count } of days) {
    const month = date.slice(0, 7);
    if (months.length > 0 && months[months.length - 1].month === month) months[months.length - 1].count += count;
    else months.push({ month, count });
  }
  const pending = db.prepare('SELECT COUNT(*) AS count FROM photo_items WHERE user_id = ? AND probed = 0').get(userId).count;
  return {
    total: days.reduce((sum, day) => sum + day.count, 0),
    pending,
    days,
    months,
  };
}

module.exports = { listPhotos, photoSummary };
