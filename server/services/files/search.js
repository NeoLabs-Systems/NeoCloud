'use strict';

// Finding files: name search with filters and ranking, and the recent list
// for Quick Access. Vault nodes never show up here: their names are
// ciphertext only the client can read.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { SEARCH_TYPES, typeCondition } = require('../../utils/file_types');
const { LIVE_NODES_CTE, NODE_COLUMNS, getLiveNode, serializeNode, withFolderPaths } = require('./tree');
const { contentSearchEnabled, findContentMatches } = require('../search_index/content_search');
const { parseQuery } = require('../search_index/query_text');

const DEFAULT_SEARCH_LIMIT = 100;
const MAX_SEARCH_LIMIT = 500;
const DEFAULT_RECENT_LIMIT = 50;
const MAX_RECENT_LIMIT = 200;
const MAX_TERMS = 8;
// Characters after which a word starts inside a file name.
const WORD_SEPARATORS = [' ', '-', '_', '.', '('];

function likeEscape(text) {
  return text.replace(/[\\%_]/g, (ch) => `\\${ch}`);
}

function clampLimit(value, fallback, max) {
  const limit = Number.parseInt(value, 10);
  return Math.min(Number.isInteger(limit) && limit > 0 ? limit : fallback, max);
}

// An ISO date as SQLite's UTC text, the format of modified_at.
function parseDate(value, field) {
  if (value === undefined || value === null || value === '') return null;
  const time = Date.parse(String(value));
  if (!Number.isFinite(time)) throw httpError(400, `${field} must be a date.`, 'INVALID_DATE');
  return new Date(time).toISOString().slice(0, 19).replace('T', ' ');
}

function parseSize(value, field) {
  if (value === undefined || value === null || value === '') return null;
  const size = Number(value);
  if (!Number.isSafeInteger(size) || size < 0) throw httpError(400, `${field} must be a byte count.`, 'INVALID_SIZE');
  return size;
}

function parseTypes(value) {
  if (value === undefined || value === null || value === '') return [];
  const types = [...new Set(String(value).split(',').map((type) => type.trim().toLowerCase()).filter(Boolean))];
  for (const type of types) {
    if (!SEARCH_TYPES.includes(type)) {
      throw httpError(400, `type must be one of ${SEARCH_TYPES.join(', ')}.`, 'INVALID_TYPE');
    }
  }
  return types;
}

function parseFlag(value) {
  return value === '1' || value === 'true';
}

/**
 * Searches the account's live nodes by name. Every word of `q` must appear
 * in the name, or in the node's AI tags and extracted words. Results rank exact names first, then names starting with `q`,
 * then names with a word starting with `q`, then the rest; ties go to the
 * most recently modified. `q` may be empty when a filter is set.
 *
 * Filters: `type` (one or more of SEARCH_TYPES, comma-separated), `after` /
 * `before` (modified, ISO dates), `minSize` / `maxSize` (bytes), `parentId`
 * (only below that folder).
 *
 * `content=1` adds files whose text matches (when the admin turned full-text
 * search on and the account didn't opt out): name matches first as above,
 * then content matches by BM25. Every item then carries `match` ("name",
 * "content" or "both") and content matches a `snippet`. In this mode quoted
 * words are a phrase and quote marks don't count toward names.
 */
function search(userId, { q, type, after, before, minSize, maxSize, parentId, limit, content } = {}) {
  const withContent = parseFlag(content);
  const rawText = String(q ?? '').trim().toLowerCase().slice(0, 200);
  const text = withContent ? rawText.replace(/"/g, ' ').replace(/\s+/g, ' ').trim() : rawText;
  const filters = {
    types: parseTypes(type),
    after: parseDate(after, 'after'),
    before: parseDate(before, 'before'),
    minSize: parseSize(minSize, 'minSize'),
    maxSize: parseSize(maxSize, 'maxSize'),
  };
  const scoped = parentId !== undefined && parentId !== null && parentId !== '';
  const hasFilter = scoped || filters.types.length > 0 || filters.after !== null || filters.before !== null
    || filters.minSize !== null || filters.maxSize !== null;
  if (!text && !hasFilter) return { items: [] };

  const params = { userId, limit: clampLimit(limit, DEFAULT_SEARCH_LIMIT, MAX_SEARCH_LIMIT) };
  const where = ['n.user_id = @userId', 'n.parent_id IS NOT NULL', 'n.vault_id IS NULL'];

  // Every fragment below is fixed text; values are bound parameters.
  if (filters.types.length > 0) {
    const conditions = filters.types.map((name, index) => {
      const condition = typeCondition(name, `type${index}_`);
      Object.assign(params, condition.params);
      // Unqualified type, mime and name resolve to the node row `n`.
      return condition.sql;
    });
    where.push(`(${conditions.join(' OR ')})`);
  }
  if (filters.after !== null) {
    params.after = filters.after;
    where.push('n.modified_at >= @after');
  }
  if (filters.before !== null) {
    params.before = filters.before;
    where.push('n.modified_at < @before');
  }
  if (filters.minSize !== null) {
    params.minSize = filters.minSize;
    where.push('n.size >= @minSize');
  }
  if (filters.maxSize !== null) {
    params.maxSize = filters.maxSize;
    where.push('n.size <= @maxSize');
  }

  let scope;
  if (scoped) {
    params.parentId = getLiveNode(userId, parentId, { type: 'folder' }).id;
    scope = `WITH RECURSIVE scope(id) AS (
      SELECT id FROM nodes WHERE parent_id = @parentId AND trashed_at IS NULL
      UNION ALL SELECT c.id FROM nodes c JOIN scope ON c.parent_id = scope.id WHERE c.trashed_at IS NULL
    )`;
    where.push('n.id IN (SELECT id FROM scope)');
  } else {
    scope = LIVE_NODES_CTE;
    where.push('n.id NOT IN (SELECT id FROM hidden)');
  }
  // The filters alone, for content matches; names add their own conditions.
  const filterWhere = [...where];
  const nameParams = {};
  const terms = text.split(/\s+/).filter(Boolean).slice(0, MAX_TERMS);
  terms.forEach((term, index) => {
    nameParams[`term${index}`] = `%${likeEscape(term)}%`;
    // AI tags and extracted words (issuer, subject, type, caption) match like the name.
    where.push(`(n.name LIKE @term${index} ESCAPE '\\' OR n.id IN (
      SELECT m.node_id FROM ai_node_meta m WHERE m.user_id = @userId AND m.search_text LIKE @term${index} ESCAPE '\\'
    ))`);
  });

  let rank = '';
  if (text) {
    const escaped = likeEscape(text);
    Object.assign(nameParams, {
      exact: text,
      exactStem: `${escaped}.%`,
      deeperStem: `${escaped}.%.%`,
      prefix: `${escaped}%`,
    });
    const wordPrefixes = WORD_SEPARATORS.map((separator, index) => {
      nameParams[`word${index}`] = `%${likeEscape(separator)}${escaped}%`;
      return `lower(n.name) LIKE @word${index} ESCAPE '\\'`;
    });
    rank = `CASE
      WHEN lower(n.name) = @exact
        OR (lower(n.name) LIKE @exactStem ESCAPE '\\' AND lower(n.name) NOT LIKE @deeperStem ESCAPE '\\') THEN 0
      WHEN lower(n.name) LIKE @prefix ESCAPE '\\' THEN 1
      WHEN ${wordPrefixes.join(' OR ')} THEN 2
      ELSE 3 END,`;
  }

  const rows = db.prepare(`
    ${scope}
    SELECT * FROM (SELECT ${NODE_COLUMNS} FROM nodes) n
    WHERE ${where.join(' AND ')}
    ORDER BY ${rank} n.modified_at DESC, lower(n.name)
    LIMIT @limit
  `).all({ ...params, ...nameParams });

  if (!withContent) return { items: withFolderPaths(userId, rows.map(serializeNode)) };
  const contentTerms = contentSearchEnabled(userId) ? parseQuery(rawText) : [];
  if (contentTerms.length === 0) {
    return { items: withFolderPaths(userId, rows.map((row) => ({ ...serializeNode(row), match: 'name' }))) };
  }
  const found = findContentMatches(userId, {
    terms: contentTerms,
    scope,
    where: filterWhere,
    params,
    nameIds: rows.map((row) => row.id),
    limit: params.limit,
  });
  const items = [
    ...rows.map((row) => (found.snippets.has(row.id)
      ? { ...serializeNode(row), match: 'both', snippet: found.snippets.get(row.id) }
      : { ...serializeNode(row), match: 'name' })),
    ...found.rows.map(({ row, snippet }) => ({ ...serializeNode(row), match: 'content', snippet })),
  ];
  return { items: withFolderPaths(userId, items) };
}

function recentlyModified(userId, limit, excludeIds = []) {
  return db.prepare(`
    ${LIVE_NODES_CTE}
    SELECT ${NODE_COLUMNS} FROM nodes
    WHERE user_id = @userId AND type = 'file' AND vault_id IS NULL
      AND id NOT IN (SELECT id FROM hidden)
      AND id NOT IN (SELECT value FROM json_each(@exclude))
    ORDER BY modified_at DESC
    LIMIT @limit
  `).all({ userId, limit, exclude: JSON.stringify(excludeIds) });
}

/**
 * Recent files. `by=opened` (default): the files the account opened most
 * recently, filled up with the most recently modified ones it never opened.
 * `by=modified`: by modification time only. Items carry `lastOpenedAt`
 * (null when never opened) and their folder `path`.
 */
function recentFiles(userId, { by = 'opened', limit } = {}) {
  const mode = by === undefined || by === null || by === '' ? 'opened' : String(by);
  if (mode !== 'opened' && mode !== 'modified') {
    throw httpError(400, 'by must be opened or modified.', 'INVALID_PARAM');
  }
  const pageSize = clampLimit(limit, DEFAULT_RECENT_LIMIT, MAX_RECENT_LIMIT);
  let rows;
  if (mode === 'opened') {
    const opened = db.prepare(`
      ${LIVE_NODES_CTE}
      SELECT ${NODE_COLUMNS} FROM node_opens o JOIN nodes ON nodes.id = o.node_id
      WHERE o.user_id = @userId AND nodes.type = 'file' AND nodes.vault_id IS NULL
        AND nodes.id NOT IN (SELECT id FROM hidden)
      ORDER BY o.last_opened_at DESC, o.open_count DESC
      LIMIT @limit
    `).all({ userId, limit: pageSize });
    rows = opened.length < pageSize
      ? [...opened, ...recentlyModified(userId, pageSize - opened.length, opened.map((row) => row.id))]
      : opened;
  } else {
    rows = recentlyModified(userId, pageSize);
  }
  const openedAt = new Map(db.prepare(`
    SELECT node_id, last_opened_at FROM node_opens
    WHERE user_id = ? AND node_id IN (SELECT value FROM json_each(?))
  `).all(userId, JSON.stringify(rows.map((row) => row.id))).map((row) => [row.node_id, row.last_opened_at]));
  const items = rows.map((row) => ({ ...serializeNode(row), lastOpenedAt: openedAt.get(row.id) ?? null }));
  return { items: withFolderPaths(userId, items) };
}

module.exports = { recentFiles, search };
