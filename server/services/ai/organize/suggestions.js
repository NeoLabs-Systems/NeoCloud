'use strict';

// The review queue's store: suggestions are created here, listed, counted
// (with the live `ai:suggestions` event) and turned stale. A pending
// suggestion turns `stale` once its file changed in a way that matters: new
// content (any kind), another name (renames), another folder (moves), or the
// file went to the trash. A move into a new folder also turns stale once the
// person switched "new folders" off, and is never high-tier: it is never
// auto-applied or taken by "accept all high-confidence"; the person accepts
// it on its own. Deciding on them is review.js.

const crypto = require('crypto');
const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { emitToUser } = require('../../events');
const { serializeNode, withFolderPaths, NODE_COLUMNS } = require('../../files/tree');
const { proposalCount } = require('./rules');
const { aiFeatures } = require('./features');

const KINDS = ['rename', 'move', 'tags'];
const TIERS = ['high', 'medium', 'low'];
const STATUSES = ['pending', 'accepted', 'rejected', 'applied', 'undone', 'stale'];
const HIGH = 0.85;
const MEDIUM = 0.6;
const DEFAULT_PAGE = 50;
const MAX_PAGE = 200;

function tierFor(confidence) {
  if (confidence >= HIGH) return 'high';
  if (confidence >= MEDIUM) return 'medium';
  return 'low';
}

function parseJson(value, fallback) {
  try {
    return JSON.parse(value);
  } catch {
    return fallback;
  }
}

// --- Counts and the live event ----------------------------------------------------------

function isStale(row, node) {
  if (!node || node.trashed_at) return true;
  if (node.etag !== row.basis_etag) return true;
  if (row.kind === 'rename' && node.name !== row.basis_name) return true;
  if (row.kind === 'move' && node.parent_id !== row.basis_parent_id) return true;
  return false;
}

/** Marks the account's pending suggestions whose file changed as stale; returns how many. */
function refreshStale(userId) {
  const rows = db.prepare(`
    SELECT s.id, s.kind, s.basis_name, s.basis_parent_id, s.basis_etag,
           n.id AS node_id, n.name, n.parent_id, n.etag, n.trashed_at
    FROM ai_suggestions s LEFT JOIN nodes n ON n.id = s.node_id
    WHERE s.user_id = ? AND s.status = 'pending'
  `).all(userId);
  const stale = rows.filter((row) => isStale(row, row.node_id ? row : null)).map((row) => row.id);
  if (!aiFeatures(userId).newFolders) {
    const newFolderMoves = db.prepare(`
      SELECT id FROM ai_suggestions
      WHERE user_id = ? AND status = 'pending' AND kind = 'move' AND json_extract(to_json, '$.id') IS NULL
    `).all(userId).map((row) => row.id);
    stale.push(...newFolderMoves.filter((id) => !stale.includes(id)));
  }
  if (stale.length > 0) {
    db.prepare(`
      UPDATE ai_suggestions SET status = 'stale', decided_at = datetime('now')
      WHERE id IN (SELECT value FROM json_each(?))
    `).run(JSON.stringify(stale));
  }
  return stale.length;
}

/**
 * Pending suggestions per kind (high and medium) and low ones apart, the
 * high ones in all and per kind, plus open rule proposals.
 */
function suggestionCounts(userId) {
  refreshStale(userId);
  const rows = db.prepare(`
    SELECT kind, tier, COUNT(*) AS count FROM ai_suggestions WHERE user_id = ? AND status = 'pending' GROUP BY kind, tier
  `).all(userId);
  const pending = { rename: 0, move: 0, tags: 0, total: 0 };
  const highByKind = { rename: 0, move: 0, tags: 0 };
  let hiddenLow = 0;
  let high = 0;
  for (const row of rows) {
    if (row.tier === 'low') {
      hiddenLow += row.count;
      continue;
    }
    pending[row.kind] += row.count;
    pending.total += row.count;
    if (row.tier === 'high') {
      high += row.count;
      highByKind[row.kind] += row.count;
    }
  }
  return { pending, high, highByKind, hiddenLow, proposals: proposalCount(userId) };
}

/** Tells the account's open apps to refresh their review counts. */
function announceSuggestions(userId) {
  emitToUser(userId, 'ai:suggestions', suggestionCounts(userId));
}

// --- Creating ---------------------------------------------------------------------------

// A rule's suggestion is high. A move into a folder that doesn't exist yet
// is at most medium, so nothing applies it in bulk or by itself.
function tierOf(suggestion, confidence) {
  if (suggestion.source === 'rule') return 'high';
  const tier = tierFor(confidence);
  const newFolder = suggestion.kind === 'move' && suggestion.to && suggestion.to.id === null;
  return newFolder && tier === 'high' ? 'medium' : tier;
}

/**
 * Stores a pending suggestion for the owner's file `node` (a node row):
 * { kind, from, to, reason, confidence, model, source, ruleId, signal }.
 * An identical pending one is kept instead; an older pending one of the same
 * kind for the file is replaced. Returns the row, or null when unchanged.
 */
function createSuggestion(userId, node, suggestion) {
  const toJson = JSON.stringify(suggestion.to);
  const same = db.prepare(`
    SELECT id FROM ai_suggestions WHERE user_id = ? AND node_id = ? AND kind = ? AND status = 'pending' AND to_json = ?
  `).get(userId, node.id, suggestion.kind, toJson);
  if (same) return null;
  const confidence = Math.min(Math.max(Number(suggestion.confidence) || 0, 0), 1);
  return db.transaction(() => {
    db.prepare(`
      UPDATE ai_suggestions SET status = 'stale', decided_at = datetime('now')
      WHERE user_id = ? AND node_id = ? AND kind = ? AND status = 'pending'
    `).run(userId, node.id, suggestion.kind);
    return db.prepare(`
      INSERT INTO ai_suggestions (id, user_id, node_id, kind, from_json, to_json, reason, confidence, tier, source, rule_id,
        model, signal_json, basis_name, basis_parent_id, basis_etag)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *
    `).get(
      crypto.randomUUID(),
      userId,
      node.id,
      suggestion.kind,
      JSON.stringify(suggestion.from),
      toJson,
      String(suggestion.reason || '').slice(0, 300),
      confidence,
      tierOf(suggestion, confidence),
      suggestion.source === 'rule' ? 'rule' : 'ai',
      suggestion.ruleId ?? null,
      suggestion.model ?? null,
      JSON.stringify(suggestion.signal || {}),
      node.name,
      node.parent_id,
      node.etag,
    );
  })();
}

// --- Reading ------------------------------------------------------------------------------

function serializeSuggestion(row, node) {
  return {
    id: row.id,
    nodeId: row.node_id,
    kind: row.kind,
    from: parseJson(row.from_json, null),
    to: parseJson(row.to_json, null),
    reason: row.reason,
    confidence: Math.round(row.confidence * 100) / 100,
    tier: row.tier,
    status: row.status,
    source: row.source,
    ruleId: row.rule_id,
    model: row.model,
    createdAt: row.created_at,
    decidedAt: row.decided_at,
    node: node ?? null,
  };
}

function nodesFor(userId, rows) {
  const ids = [...new Set(rows.map((row) => row.node_id))];
  const nodes = db.prepare(`
    SELECT ${NODE_COLUMNS} FROM nodes WHERE user_id = ? AND id IN (SELECT value FROM json_each(?))
  `).all(userId, JSON.stringify(ids)).map(serializeNode);
  return new Map(withFolderPaths(userId, nodes).map((node) => [node.id, node]));
}

function listParam(value, allowed, field) {
  if (value === undefined || value === null || value === '') return null;
  const list = [...new Set(String(value).split(',').map((item) => item.trim()).filter(Boolean))];
  for (const item of list) {
    if (!allowed.includes(item)) throw httpError(400, `${field} must be one of ${allowed.join(', ')}.`, 'INVALID_PARAM');
  }
  return list;
}

/**
 * One page of suggestions, newest first. `kind`, `tier` and `status` take one
 * or more comma-separated values; by default pending ones of tier high and
 * medium (`tier=low` or `tier=all` shows the hidden ones).
 */
function listSuggestions(userId, { kind, tier, status, nodeId, limit, offset } = {}) {
  refreshStale(userId);
  const kinds = listParam(kind, KINDS, 'kind');
  const tiers = tier === 'all' ? TIERS : (listParam(tier, TIERS, 'tier') || ['high', 'medium']);
  const statuses = listParam(status, STATUSES, 'status') || ['pending'];
  const pageSize = Math.min(Math.max(Number.parseInt(limit, 10) || DEFAULT_PAGE, 1), MAX_PAGE);
  const start = Math.max(Number.parseInt(offset, 10) || 0, 0);
  const params = {
    userId,
    kinds: JSON.stringify(kinds || KINDS),
    tiers: JSON.stringify(tiers),
    statuses: JSON.stringify(statuses),
    nodeId: nodeId ? String(nodeId) : null,
  };
  const where = `
    user_id = @userId AND kind IN (SELECT value FROM json_each(@kinds)) AND tier IN (SELECT value FROM json_each(@tiers))
    AND status IN (SELECT value FROM json_each(@statuses)) AND (@nodeId IS NULL OR node_id = @nodeId)
  `;
  const rows = db.prepare(`
    SELECT * FROM ai_suggestions WHERE ${where}
    ORDER BY created_at DESC, CASE tier WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END, id
    LIMIT @limit OFFSET @offset
  `).all({ ...params, limit: pageSize + 1, offset: start });
  const { total } = db.prepare(`SELECT COUNT(*) AS total FROM ai_suggestions WHERE ${where}`).get(params);
  const page = rows.slice(0, pageSize);
  const nodes = nodesFor(userId, page);
  return {
    items: page.map((row) => serializeSuggestion(row, nodes.get(row.node_id))),
    total,
    nextOffset: rows.length > pageSize ? start + pageSize : null,
    counts: suggestionCounts(userId),
  };
}

function suggestionRow(userId, id) {
  const row = db.prepare('SELECT * FROM ai_suggestions WHERE id = ? AND user_id = ?').get(String(id || ''), userId);
  if (!row) throw httpError(404, 'Suggestion not found', 'NOT_FOUND');
  return row;
}

function withNode(userId, row) {
  const [node] = withFolderPaths(userId, db.prepare(`SELECT ${NODE_COLUMNS} FROM nodes WHERE id = ? AND user_id = ?`)
    .all(row.node_id, userId).map(serializeNode));
  return serializeSuggestion(row, node);
}

module.exports = {
  KINDS,
  announceSuggestions,
  createSuggestion,
  isStale,
  listParam,
  listSuggestions,
  parseJson,
  refreshStale,
  suggestionCounts,
  suggestionRow,
  withNode,
};
