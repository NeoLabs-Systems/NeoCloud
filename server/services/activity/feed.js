'use strict';

// What the viewer may read of the activity log: entries in their own tree,
// and entries in other accounts' trees that still resolve through a live
// grant (sharedAccess, the same check as the file routes). Vault nodes never
// show. A node that moved out of the share, or a share that ended, hides its
// entries again.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { nodeById } = require('../files/tree');
const { activeGrant, resolveAccess, sharedAccess } = require('../members/access');
const { parseDetail } = require('./record');

const DEFAULT_LIMIT = 30;
const MAX_LIMIT = 100;
const SCAN_BATCH = 200;
const MAX_SCANNED = 1000;
const SCOPES = new Set(['all', 'mine', 'shared']);

function notFound() {
  return httpError(404, 'File or folder not found', 'NOT_FOUND');
}

function isoTime(sqlTime) {
  return `${String(sqlTime).replace(' ', 'T')}Z`;
}

function ownersSharingWith(userId) {
  return db.prepare(`
    SELECT DISTINCT owner_id FROM user_shares WHERE grantee_id = ? AND ${activeGrant()}
  `).all(userId).map((row) => row.owner_id);
}

// The node a feed is limited to, as { ownerId, nodeId }. Another account's
// id, an unknown one and a vault node all answer 404.
function resolveFocus(userId, nodeId) {
  const access = resolveAccess(userId, nodeId);
  if (access.shared) return { ownerId: access.ownerId, nodeId: access.node.id };
  const node = nodeById(String(nodeId));
  if (!node || node.user_id !== userId || node.vault_id) throw notFound();
  return { ownerId: userId, nodeId: node.id };
}

function visibleToMember(viewerId, row, detail, cache) {
  // Who a node was shared with is for the owner and that member only.
  if (row.kind === 'share' && detail.granteeId !== viewerId) return false;
  const node = nodeById(row.node_id);
  // A live node counts where it is now; a trashed or removed one where it was.
  const checkId = node && !node.trashed_at ? node.id : row.parent_id;
  if (!checkId) return false;
  const key = `${row.owner_id}:${checkId}`;
  if (!cache.has(key)) cache.set(key, Boolean(sharedAccess(viewerId, row.owner_id, checkId)));
  return cache.get(key);
}

function names(ids) {
  const found = new Map();
  for (const row of db.prepare(`
    SELECT id, COALESCE(NULLIF(display_name, ''), username) AS name FROM users
    WHERE id IN (SELECT value FROM json_each(?))
  `).all(JSON.stringify([...ids]))) found.set(row.id, row.name);
  return found;
}

// The name of the folder an entry happened in, when the viewer can see that
// folder (the owner always can); the top of an account has no name.
function folderName(viewerId, row, cache) {
  if (!row.parent_id || row.kind === 'share') return null;
  if (row.owner_id !== viewerId) {
    const key = `${row.owner_id}:${row.parent_id}`;
    if (!cache.has(key)) cache.set(key, Boolean(sharedAccess(viewerId, row.owner_id, row.parent_id)));
    if (!cache.get(key)) return null;
  }
  return nodeById(row.parent_id)?.name || null;
}

function serialize(viewerId, rows, cache) {
  const people = names(new Set(rows.flatMap((row) => [row.actor_id, row.owner_id, parseDetail(row.detail).granteeId ?? row.actor_id])));
  return rows.map((row) => {
    const { names: sample, from, granteeId } = parseDetail(row.detail);
    const folder = folderName(viewerId, row, cache);
    return {
      id: row.id,
      kind: row.kind,
      at: isoTime(row.at),
      count: row.count,
      actor: { name: people.get(row.actor_id) ?? '', self: row.actor_id === viewerId },
      owner: { name: people.get(row.owner_id) ?? '', self: row.owner_id === viewerId },
      node: { id: row.node_id, name: row.name, type: row.node_type },
      ...(folder ? { folder } : {}),
      ...(sample ? { names: sample } : {}),
      ...(from ? { from } : {}),
      ...(granteeId ? { member: { name: people.get(granteeId) ?? '', self: granteeId === viewerId } } : {}),
    };
  });
}

// One batch of candidate rows below `before`, newest first.
function candidateRows(owners, focus, before, size) {
  return db.prepare(`
    SELECT a.* FROM activity a
    WHERE a.owner_id IN (SELECT value FROM json_each(@owners)) AND a.id < @before
      AND (@focus IS NULL OR a.node_id = @focus OR a.parent_id = @focus)
      AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = a.node_id AND n.vault_id IS NOT NULL)
    ORDER BY a.id DESC LIMIT @size
  `).all({ owners: JSON.stringify(owners), focus, before, size });
}

/**
 * The viewer's feed, newest first. `scope` is all, mine (their own tree) or
 * shared (what others' trees show them); `nodeId` limits it to one file or
 * folder and what is directly inside it. `cursor` is the `nextCursor` of the
 * page before.
 */
function listActivity(viewerId, { scope = 'all', nodeId = null, cursor = null, limit = DEFAULT_LIMIT } = {}) {
  if (!SCOPES.has(scope)) throw httpError(400, 'Unknown scope.', 'INVALID_SCOPE');
  const pageSize = Math.min(Math.max(Math.trunc(Number(limit)) || DEFAULT_LIMIT, 1), MAX_LIMIT);
  const focus = nodeId ? resolveFocus(viewerId, nodeId) : null;
  const wanted = focus
    ? [focus.ownerId]
    : [...(scope === 'shared' ? [] : [viewerId]), ...(scope === 'mine' ? [] : ownersSharingWith(viewerId))];
  const cache = new Map();
  const page = [];
  let before = Number(cursor) > 0 ? Math.trunc(Number(cursor)) : Number.MAX_SAFE_INTEGER;
  let scanned = 0;
  let exhausted = false;
  while (page.length <= pageSize && scanned < MAX_SCANNED) {
    const rows = candidateRows(wanted, focus?.nodeId ?? null, before, SCAN_BATCH);
    exhausted = rows.length < SCAN_BATCH;
    for (const row of rows) {
      scanned += 1;
      before = row.id;
      if (row.owner_id === viewerId || visibleToMember(viewerId, row, parseDetail(row.detail), cache)) page.push(row);
      if (page.length > pageSize) break;
    }
    if (exhausted || page.length > pageSize) break;
  }
  const more = page.length > pageSize;
  const shown = page.slice(0, pageSize);
  const resumeAfter = more ? shown[shown.length - 1].id : before;
  return {
    items: serialize(viewerId, shown, cache),
    nextCursor: more || !exhausted ? resumeAfter : null,
  };
}

module.exports = { listActivity };
