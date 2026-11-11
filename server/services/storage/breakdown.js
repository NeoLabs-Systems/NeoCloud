'use strict';

// Where an account's storage goes: totals, live files by kind and by top
// folder, the largest files, and files stored more than once.

const db = require('../../db/database');
const { fileGroup } = require('../../utils/file_types');
const { LIVE_NODES_CTE, NODE_COLUMNS, getRoot, liveSubtree, serializeNode, withFolderPaths } = require('../files/tree');
const { getUsage } = require('./quota');

const LARGEST_COUNT = 20;
const MAX_DUPLICATE_GROUPS = 200;

/**
 * `total` is what counts against the quota (every stored blob once); `files`
 * the size of the live files (copies counted each time); `trash` and
 * `versions` as in the account usage. `byType` and `byFolder` cover live
 * files; vault files are their own type, "vault". `byFolder` lists the root's
 * folders, with the root's own loose files as `{ id: <root id>, name: "" }`.
 */
function storageBreakdown(userId) {
  const usage = getUsage(userId);
  const root = getRoot(userId);
  const byType = new Map();
  const byFolder = new Map();
  const files = [];
  let fileBytes = 0;
  for (const row of liveSubtree(userId, root.id)) {
    if (row.depth === 1 && row.type === 'folder') byFolder.set(row.relative_path, { id: row.id, name: row.name, bytes: 0 });
    if (row.type !== 'file') continue;
    fileBytes += row.size;
    files.push(row);
    const type = row.vault_id ? 'vault' : fileGroup(row.mime, row.name);
    const entry = byType.get(type) || { type, bytes: 0, count: 0 };
    entry.bytes += row.size;
    entry.count += 1;
    byType.set(type, entry);
    if (row.depth === 1) {
      if (!byFolder.has('')) byFolder.set('', { id: root.id, name: '', bytes: 0 });
      byFolder.get('').bytes += row.size;
    } else {
      byFolder.get(row.relative_path.split('/')[0]).bytes += row.size;
    }
  }
  const largestIds = files
    .filter((row) => !row.vault_id)
    .sort((a, b) => b.size - a.size)
    .slice(0, LARGEST_COUNT)
    .map((row) => row.id);
  const largestRows = db.prepare(`
    SELECT ${NODE_COLUMNS} FROM nodes WHERE id IN (SELECT value FROM json_each(?))
  `).all(JSON.stringify(largestIds)).sort((a, b) => b.size - a.size || a.name.localeCompare(b.name));
  return {
    total: usage.usedBytes,
    files: fileBytes,
    trash: usage.trashBytes,
    versions: usage.versionBytes,
    byType: [...byType.values()].sort((a, b) => b.bytes - a.bytes),
    byFolder: [...byFolder.values()].sort((a, b) => b.bytes - a.bytes),
    largest: withFolderPaths(userId, largestRows.map(serializeNode)),
  };
}

/**
 * Live files with the same content (same SHA-256), biggest waste first.
 * `reclaimable` counts what deleting all but one of each group would free:
 * copies made inside NeoCloud already share one stored blob and free nothing.
 * Empty files and vault files are left out.
 */
function findDuplicates(userId) {
  const rows = db.prepare(`
    ${LIVE_NODES_CTE},
    live AS (
      SELECT nodes.id, nodes.blob_id, b.sha256
      FROM nodes JOIN blobs b ON b.id = nodes.blob_id
      WHERE nodes.user_id = @userId AND nodes.type = 'file' AND nodes.size > 0 AND nodes.vault_id IS NULL
        AND nodes.id NOT IN (SELECT id FROM hidden)
    ),
    repeated AS (SELECT sha256 FROM live GROUP BY sha256 HAVING COUNT(*) > 1)
    SELECT ${NODE_COLUMNS}, live.sha256 AS sha256
    FROM live JOIN nodes ON nodes.id = live.id
    WHERE live.sha256 IN (SELECT sha256 FROM repeated)
    ORDER BY nodes.modified_at DESC
  `).all({ userId });
  const groups = new Map();
  for (const row of rows) {
    if (!groups.has(row.sha256)) groups.set(row.sha256, { sha256: row.sha256, size: row.size, blobs: new Set(), rows: [] });
    const group = groups.get(row.sha256);
    group.blobs.add(row.blob_id);
    group.rows.push(row);
  }
  const ranked = [...groups.values()]
    .map((group) => ({ ...group, waste: (group.blobs.size - 1) * group.size }))
    .sort((a, b) => b.waste - a.waste || b.size * b.rows.length - a.size * a.rows.length);
  const reclaimable = ranked.reduce((sum, group) => sum + group.waste, 0);
  const shown = ranked.slice(0, MAX_DUPLICATE_GROUPS);
  const withPaths = withFolderPaths(userId, shown.flatMap((group) => group.rows.map(serializeNode)));
  const byId = new Map(withPaths.map((node) => [node.id, node]));
  return {
    groups: shown.map((group) => ({
      sha256: group.sha256,
      size: group.size,
      reclaimable: group.waste,
      nodes: group.rows.map((row) => byId.get(row.id)),
    })),
    reclaimable,
  };
}

module.exports = { findDuplicates, storageBreakdown };
