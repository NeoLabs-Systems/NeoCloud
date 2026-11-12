'use strict';

// Read access to an account's file tree. Every lookup is scoped to the
// account, so a node id belonging to someone else behaves exactly like one
// that does not exist (404), never like a forbidden one.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

const ROOT_ALIAS = 'root';
const SORT_COLUMNS = { name: 'lower(name)', size: 'size', modified: 'modified_at', type: "COALESCE(mime, '')" };
const DEFAULT_PAGE_SIZE = 500;
const MAX_PAGE_SIZE = 2000;

// Every column of a node plus whether its owner marked it as a favorite and
// shared it with other accounts.
// AI tags and metadata (ai_node_meta): tags the owner accepted, and what the
// extraction found in the current content (gone once the content changes).
const NODE_COLUMNS = `nodes.*, EXISTS (
  SELECT 1 FROM favorites f WHERE f.user_id = nodes.user_id AND f.node_id = nodes.id
) AS favorite, EXISTS (
  SELECT 1 FROM user_shares s WHERE s.node_id = nodes.id
) AS shared, (
  SELECT m.tags_json FROM ai_node_meta m WHERE m.node_id = nodes.id
) AS ai_tags_json, (
  SELECT m.fields_json || char(31) || COALESCE(m.model, '') FROM ai_node_meta m
  WHERE m.node_id = nodes.id AND m.etag = nodes.etag AND m.fields_json <> '{}'
) AS ai_meta_json`;

// The aiMeta a client sees: the extracted fields with their confidences and
// the model, without the internal flags.
function aiMetaOf(raw) {
  if (!raw) return null;
  const [json, model] = raw.split('\u001f');
  try {
    const { camera: _camera, ...fields } = JSON.parse(json);
    return { ...fields, model: model || null };
  } catch {
    return null;
  }
}

function aiTagsOf(raw) {
  try {
    const tags = JSON.parse(raw || '[]');
    return Array.isArray(tags) ? tags : [];
  } catch {
    return [];
  }
}

function serializeNode(row) {
  return {
    id: row.id,
    parentId: row.parent_id,
    type: row.type,
    name: row.name,
    size: row.size,
    mime: row.mime,
    etag: row.etag,
    version: row.version,
    createdAt: row.created_at,
    modifiedAt: row.modified_at,
    vaultId: row.vault_id ?? null,
    // Set by the owner on a folder; the sync feeds don't carry it.
    ...(row.ai_excluded === undefined ? {} : { aiExcluded: row.ai_excluded === 1 }),
    // Known for rows read with NODE_COLUMNS; the sync feeds don't carry it.
    ...(row.favorite === undefined ? {} : { favorite: row.favorite === 1 }),
    ...(row.shared === undefined ? {} : { shared: row.shared === 1 }),
    ...(row.ai_tags_json === undefined ? {} : { aiTags: aiTagsOf(row.ai_tags_json), aiMeta: aiMetaOf(row.ai_meta_json) }),
    ...(row.trashed_at ? { trashedAt: row.trashed_at } : {}),
  };
}

/** A node row by id, with its favorite flag. */
function nodeById(id) {
  return db.prepare(`SELECT ${NODE_COLUMNS} FROM nodes WHERE id = ?`).get(id);
}

function notFound() {
  return httpError(404, 'File or folder not found', 'NOT_FOUND');
}

function getRoot(userId) {
  const existing = db.prepare('SELECT * FROM nodes WHERE user_id = ? AND parent_id IS NULL').get(userId);
  if (existing) return existing;
  db.prepare(`
    INSERT OR IGNORE INTO nodes (id, user_id, parent_id, type, name, etag) VALUES (?, ?, NULL, 'folder', '', ?)
  `).run(crypto.randomUUID(), userId, crypto.randomUUID());
  return db.prepare('SELECT * FROM nodes WHERE user_id = ? AND parent_id IS NULL').get(userId);
}

/** The node and its ancestors, nearest first, or [] when not the account's. */
function ancestry(userId, nodeId) {
  return db.prepare(`
    WITH RECURSIVE up(id, parent_id, name, trashed_at, depth) AS (
      SELECT id, parent_id, name, trashed_at, 0 FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL
      SELECT n.id, n.parent_id, n.name, n.trashed_at, up.depth + 1
      FROM nodes n JOIN up ON n.id = up.parent_id
    )
    SELECT id, parent_id, name, trashed_at FROM up ORDER BY depth
  `).all(nodeId, userId);
}

function isLiveChain(chain) {
  return chain.length > 0 && chain.every((node) => !node.trashed_at);
}

/**
 * A node the account can see in its tree: owned, and neither it nor any
 * folder above it is in the trash. `root` names the account's root folder.
 */
function getLiveNode(userId, nodeId, { type = null } = {}) {
  const id = nodeId === ROOT_ALIAS ? getRoot(userId).id : String(nodeId || '');
  if (!isLiveChain(ancestry(userId, id))) throw notFound();
  const node = db.prepare(`SELECT ${NODE_COLUMNS} FROM nodes WHERE id = ? AND user_id = ?`).get(id, userId);
  if (type && node.type !== type) {
    throw type === 'folder'
      ? httpError(400, 'That is not a folder.', 'NOT_A_FOLDER')
      : httpError(400, 'That is not a file.', 'NOT_A_FILE');
  }
  return node;
}

// Names of the folders from the root down to (and including) the node.
function breadcrumbs(userId, nodeId) {
  return ancestry(userId, nodeId)
    .reverse()
    .map((node) => ({ id: node.id, name: node.name }));
}

// A page cursor: the sort it was made for and the sort key of the last row.
function encodeCursor(sort, order, row) {
  return Buffer.from(JSON.stringify([sort, order, row.is_folder, row.sort_value, row.sort_name, row.id])).toString('base64url');
}

function decodeCursor(cursor, sort, order) {
  let parts;
  try {
    parts = JSON.parse(Buffer.from(String(cursor), 'base64url').toString('utf8'));
  } catch {
    parts = null;
  }
  const valid = Array.isArray(parts) && parts.length === 6 && parts[0] === sort && parts[1] === order
    && (parts[2] === 0 || parts[2] === 1)
    && ['string', 'number'].includes(typeof parts[3])
    && typeof parts[4] === 'string' && typeof parts[5] === 'string';
  if (!valid) throw httpError(400, 'cursor is not valid for this listing.', 'INVALID_CURSOR');
  return { isFolder: parts[2], value: parts[3], name: parts[4], id: parts[5] };
}

/**
 * One page of a folder: folders first, then by `sort` and `order`, ties by
 * name and id, so every sort is a total order. Pages continue from
 * `nextCursor` (keyset, stable while the folder changes); `offset` still
 * works for clients that page by position.
 */
function listChildren(userId, folderId, { sort = 'name', order = 'asc', limit, offset = 0, cursor = null } = {}) {
  const folder = getLiveNode(userId, folderId, { type: 'folder' });
  const sortKey = Object.hasOwn(SORT_COLUMNS, sort) ? sort : 'name';
  const column = SORT_COLUMNS[sortKey];
  const direction = order === 'desc' ? 'desc' : 'asc';
  const pageSize = Math.min(Math.max(Number.parseInt(limit, 10) || DEFAULT_PAGE_SIZE, 1), MAX_PAGE_SIZE);
  const params = { folderId: folder.id, userId, limit: pageSize + 1, offset: 0 };
  let after = '';
  if (cursor) {
    const last = decodeCursor(cursor, sortKey, direction);
    Object.assign(params, { cFolder: last.isFolder, cValue: last.value, cName: last.name, cId: last.id });
    const beyond = direction === 'desc' ? '<' : '>';
    after = `AND (is_folder < @cFolder OR (is_folder = @cFolder AND (sort_value ${beyond} @cValue
      OR (sort_value = @cValue AND (sort_name > @cName OR (sort_name = @cName AND id > @cId))))))`;
  } else {
    params.offset = Math.min(Math.max(Number.parseInt(offset, 10) || 0, 0), Number.MAX_SAFE_INTEGER);
  }
  // Column, direction and comparison come from the whitelists above, never from input.
  const rows = db.prepare(`
    SELECT * FROM (
      SELECT ${NODE_COLUMNS}, (type = 'folder') AS is_folder, ${column} AS sort_value, lower(name) AS sort_name
      FROM nodes
      WHERE parent_id = @folderId AND user_id = @userId AND trashed_at IS NULL
    )
    WHERE 1 = 1 ${after}
    ORDER BY is_folder DESC, sort_value ${direction.toUpperCase()}, sort_name ASC, id ASC
    LIMIT @limit OFFSET @offset
  `).all(params);
  const hasMore = rows.length > pageSize;
  const page = hasMore ? rows.slice(0, pageSize) : rows;
  const { total } = db.prepare(`
    SELECT COUNT(*) AS total FROM nodes WHERE parent_id = ? AND user_id = ? AND trashed_at IS NULL
  `).get(folder.id, userId);
  return {
    folder: serializeNode(folder),
    path: breadcrumbs(userId, folder.id),
    items: page.map(serializeNode),
    total,
    nextCursor: hasMore ? encodeCursor(sortKey, direction, page[page.length - 1]) : null,
  };
}

// Every node below the account's trashed nodes, so listings can skip them.
const LIVE_NODES_CTE = `
  WITH RECURSIVE hidden(id) AS (
    SELECT id FROM nodes WHERE user_id = @userId AND trashed_at IS NOT NULL
    UNION SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id = h.id
  )
`;

/**
 * The folder path ("/Photos/2026", "/" for the root) of each node's parent,
 * for many nodes in one query. Returns the serialized nodes with `path` added.
 */
function withFolderPaths(userId, items) {
  const parentIds = [...new Set(items.map((item) => item.parentId).filter(Boolean))];
  if (parentIds.length === 0) return items.map((item) => ({ ...item, path: '/' }));
  const rows = db.prepare(`
    WITH RECURSIVE up(start, id, parent_id, name, depth) AS (
      SELECT n.id, n.id, n.parent_id, n.name, 0
      FROM json_each(?) j JOIN nodes n ON n.id = j.value AND n.user_id = ?
      UNION ALL
      SELECT up.start, p.id, p.parent_id, p.name, up.depth + 1
      FROM nodes p JOIN up ON p.id = up.parent_id
    )
    SELECT start, name, parent_id FROM up ORDER BY start, depth DESC
  `).all(JSON.stringify(parentIds), userId);
  const names = new Map();
  for (const row of rows) {
    if (!names.has(row.start)) names.set(row.start, []);
    // The root folder has no name of its own.
    if (row.parent_id !== null) names.get(row.start).push(row.name);
  }
  return items.map((item) => ({ ...item, path: `/${(names.get(item.parentId) || []).join('/')}` }));
}

/** The live subtree under a folder, parents before children, with relative paths. */
function liveSubtree(userId, folderId) {
  return db.prepare(`
    WITH RECURSIVE down(id, path, depth) AS (
      SELECT id, '', 0 FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL
      SELECT n.id,
             CASE WHEN down.depth = 0 THEN n.name ELSE down.path || '/' || n.name END,
             down.depth + 1
      FROM nodes n JOIN down ON n.parent_id = down.id
      WHERE n.trashed_at IS NULL
    )
    SELECT nodes.*, down.path AS relative_path, down.depth AS depth
    FROM down JOIN nodes ON nodes.id = down.id
    ORDER BY down.depth, lower(nodes.name)
  `).all(folderId, userId);
}

function findLiveChildByName(parentId, name) {
  return db.prepare(`
    SELECT * FROM nodes WHERE parent_id = ? AND lower(name) = lower(?) AND trashed_at IS NULL
  `).get(parentId, name) || null;
}

module.exports = {
  LIVE_NODES_CTE,
  NODE_COLUMNS,
  ROOT_ALIAS,
  ancestry,
  breadcrumbs,
  findLiveChildByName,
  getLiveNode,
  getRoot,
  liveSubtree,
  listChildren,
  nodeById,
  serializeNode,
  withFolderPaths,
};
