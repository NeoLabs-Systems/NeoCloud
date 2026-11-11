'use strict';

// The change journal. Every tree mutation appends a row inside the same
// transaction; sync clients replay it from a cursor, and the web app gets a
// socket event so open folders refresh.

const db = require('../../db/database');
const { emitToUser } = require('../events');
const { serializeNode } = require('./tree');

const OPS = new Set(['create', 'update', 'rename', 'move', 'trash', 'restore', 'delete']);

function recordChange(userId, nodeId, op) {
  if (!OPS.has(op)) throw new Error(`Unknown change op: ${op}`);
  db.prepare('INSERT INTO changes (user_id, node_id, op) VALUES (?, ?, ?)').run(userId, nodeId, op);
}

// Called after the transaction commits: tells open web sessions to refresh.
function announceChanges(userId, parentIds) {
  const folders = [...new Set(parentIds.filter(Boolean))];
  if (folders.length > 0) emitToUser(userId, 'files:changed', { folderIds: folders });
}

function currentCursor(userId) {
  return db.prepare('SELECT COALESCE(MAX(seq), 0) AS seq FROM changes WHERE user_id = ?').get(userId).seq;
}

/**
 * Changes after `cursor`, oldest first, each with the node as it is now
 * (null once it has been deleted for good).
 */
function listChanges(userId, cursor, { limit = 1000 } = {}) {
  const pageSize = Math.min(Math.max(Number(limit) || 1000, 1), 5000);
  const rows = db.prepare(`
    SELECT c.seq, c.op, c.node_id, c.at, n.id AS present_id, n.parent_id, n.type, n.name, n.size,
           n.mime, n.etag, n.version, n.created_at, n.modified_at, n.trashed_at, n.vault_id
    FROM changes c LEFT JOIN nodes n ON n.id = c.node_id AND n.user_id = c.user_id
    WHERE c.user_id = ? AND c.seq > ?
    ORDER BY c.seq
    LIMIT ?
  `).all(userId, Math.max(Number(cursor) || 0, 0), pageSize + 1);
  const page = rows.slice(0, pageSize);
  return {
    changes: page.map((row) => ({
      seq: row.seq,
      op: row.op,
      nodeId: row.node_id,
      at: row.at,
      node: row.present_id ? serializeNode({ ...row, id: row.present_id }) : null,
    })),
    cursor: page.length > 0 ? page[page.length - 1].seq : Math.max(Number(cursor) || 0, 0),
    hasMore: rows.length > pageSize,
  };
}

module.exports = { announceChanges, currentCursor, listChanges, recordChange };
