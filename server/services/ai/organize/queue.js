'use strict';

// Putting work on the AI job queue (table ai_jobs). A file has at most one
// pending job: asking again widens it instead of adding another.

const db = require('../../../db/database');

function sqlTime(ms) {
  return new Date(ms).toISOString().slice(0, 19).replace('T', ' ');
}

/** Queues (or widens the pending job of) each of the owner's file ids. */
function enqueue(userId, nodeIds, { kinds, source, explicit = false, batchId = null }) {
  const pending = db.prepare(`
    SELECT id, kinds, explicit FROM ai_jobs WHERE user_id = ? AND node_id = ? AND status = 'pending'
  `);
  const widen = db.prepare('UPDATE ai_jobs SET kinds = ?, explicit = ?, batch_id = COALESCE(?, batch_id), updated_at = datetime(\'now\') WHERE id = ?');
  const insert = db.prepare(`
    INSERT INTO ai_jobs (user_id, node_id, kinds, source, explicit, batch_id) VALUES (?, ?, ?, ?, ?, ?)
  `);
  let queued = 0;
  db.transaction(() => {
    for (const nodeId of nodeIds) {
      const existing = pending.get(userId, nodeId);
      if (existing) {
        const merged = [...new Set([...JSON.parse(existing.kinds), ...kinds])];
        widen.run(JSON.stringify(merged), existing.explicit || (explicit ? 1 : 0), batchId, existing.id);
      } else {
        insert.run(userId, nodeId, JSON.stringify(kinds), source, explicit ? 1 : 0, batchId);
      }
      queued += 1;
    }
  })();
  return queued;
}

module.exports = { enqueue, sqlTime };
