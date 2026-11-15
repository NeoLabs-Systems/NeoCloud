'use strict';

// Writes the activity log: one row per thing that happened to a node in an
// account's tree. Called inside the transaction of the change itself, next
// to the change journal. Vault contents are never logged. Bursts (many
// uploads to one folder by one account within a few minutes) become one row
// with a count.

const db = require('../../db/database');
const { currentActor } = require('./actor');

const KINDS = new Set(['upload', 'folder', 'copy', 'version', 'rename', 'move', 'trash', 'restore', 'share', 'comment']);
const BURST_KINDS = new Set(['upload', 'folder', 'copy', 'trash', 'restore', 'move']);
const BURST_WINDOW = '-5 minutes';
const NAME_SAMPLE = 3;
const RETENTION_DAYS = 90;

function parseDetail(text) {
  try {
    return text ? JSON.parse(text) : {};
  } catch {
    return {};
  }
}

// Folds `node` into the actor's last matching row (same folder, same kind,
// inside the window): the row is replaced by a newer one so the feed order
// stays that of the events. Returns false when there was nothing to fold into.
function foldIntoBurst(ownerId, actorId, kind, node) {
  const last = db.prepare(`
    SELECT id, name, count, detail FROM activity
    WHERE owner_id = ? AND actor_id = ? AND kind = ? AND parent_id IS ? AND at >= datetime('now', ?)
    ORDER BY id DESC LIMIT 1
  `).get(ownerId, actorId, kind, node.parent_id, BURST_WINDOW);
  if (!last) return false;
  const names = parseDetail(last.detail).names ?? [last.name];
  const sample = [...new Set([...names, node.name])].slice(0, NAME_SAMPLE);
  db.prepare('DELETE FROM activity WHERE id = ?').run(last.id);
  db.prepare(`
    INSERT INTO activity (owner_id, actor_id, kind, node_id, parent_id, node_type, name, detail, count)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).run(ownerId, actorId, kind, node.id, node.parent_id, node.type, node.name, JSON.stringify({ names: sample }), last.count + 1);
  return true;
}

/**
 * Logs `kind` for `node` (a nodes row) in `ownerId`'s tree. The actor is the
 * member named by runAs, else the owner, unless `actorId` says otherwise.
 * `detail` is small JSON-able context (the old name of a rename, the member
 * of a share).
 */
function recordActivity(ownerId, kind, node, { actorId = null, detail = null } = {}) {
  if (!KINDS.has(kind)) throw new Error(`Unknown activity kind: ${kind}`);
  if (node.vault_id) return;
  const actor = actorId ?? currentActor() ?? ownerId;
  if (BURST_KINDS.has(kind) && foldIntoBurst(ownerId, actor, kind, node)) return;
  db.prepare(`
    INSERT INTO activity (owner_id, actor_id, kind, node_id, parent_id, node_type, name, detail)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  `).run(ownerId, actor, kind, node.id, node.parent_id, node.type, node.name, detail ? JSON.stringify(detail) : null);
}

/** Removes entries older than the retention window. Returns how many. */
function pruneActivity(days = RETENTION_DAYS) {
  return db.prepare("DELETE FROM activity WHERE at < datetime('now', ?)").run(`-${Math.floor(days)} days`).changes;
}

module.exports = { parseDetail, pruneActivity, recordActivity };
