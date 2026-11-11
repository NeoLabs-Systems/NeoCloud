'use strict';

// Previous contents of a file. Replacing a file keeps the old blob as a
// version (up to NEOCLOUD_VERSION_RETENTION of them); restoring one makes it
// the current content again and keeps what was current as a version.

const db = require('../../db/database');
const { getStorageConfig } = require('../../config/storage');
const { httpError } = require('../../utils/http_error');
const { mimeForName } = require('../../utils/mime');
const { deleteBlobFiles } = require('../storage/blob_store');
const { releaseBlobs, retainBlob } = require('../storage/blob_refs');
const { announceChanges, recordChange } = require('./changes');
const { getLiveNode, nodeById, serializeNode } = require('./tree');

function serializeVersion(row) {
  return { version: row.version, size: row.size, etag: row.etag, createdAt: row.created_at };
}

function listVersions(userId, fileId) {
  const node = getLiveNode(userId, fileId, { type: 'file' });
  const rows = db.prepare('SELECT * FROM file_versions WHERE node_id = ? ORDER BY version DESC').all(node.id);
  return { current: serializeNode(node), versions: rows.map(serializeVersion) };
}

function getVersion(userId, fileId, version) {
  const node = getLiveNode(userId, fileId, { type: 'file' });
  const row = db.prepare('SELECT * FROM file_versions WHERE node_id = ? AND version = ?').get(node.id, Number(version));
  if (!row) throw httpError(404, 'Version not found', 'NOT_FOUND');
  return { node, version: row };
}

// Inside a transaction: drops the oldest versions beyond the retention limit.
function pruneVersions(nodeId) {
  const keep = getStorageConfig().versionRetention;
  const stale = db.prepare(`
    SELECT id, blob_id FROM file_versions WHERE node_id = ? ORDER BY version DESC LIMIT -1 OFFSET ?
  `).all(nodeId, keep);
  for (const row of stale) {
    db.prepare('DELETE FROM file_versions WHERE id = ?').run(row.id);
  }
  return releaseBlobs(stale.map((row) => row.blob_id));
}

/**
 * Inside a transaction: points `node` at new content, keeping the current
 * content as a version. The blob must already be recorded; this takes one
 * reference on it. Returns blob ids that became unreferenced.
 */
function replaceContent(userId, node, { blobId, size, etag }) {
  db.prepare(`
    INSERT INTO file_versions (node_id, blob_id, version, size, etag, created_at)
    VALUES (?, ?, ?, ?, ?, ?)
  `).run(node.id, node.blob_id, node.version, node.size, node.etag, node.modified_at);
  retainBlob(blobId);
  db.prepare(`
    UPDATE nodes SET blob_id = ?, size = ?, etag = ?, version = version + 1,
                     mime = ?, modified_at = datetime('now')
    WHERE id = ?
  `).run(blobId, size, etag, mimeForName(node.name), node.id);
  recordChange(userId, node.id, 'update');
  // The current blob moved to file_versions and keeps its reference there.
  return pruneVersions(node.id);
}

async function restoreVersion(userId, fileId, version) {
  const result = db.transaction(() => {
    const { node, version: row } = getVersion(userId, fileId, version);
    const orphaned = replaceContent(userId, node, { blobId: row.blob_id, size: row.size, etag: row.etag });
    return { orphaned, node: nodeById(node.id) };
  })();
  await deleteBlobFiles(userId, result.orphaned);
  announceChanges(userId, [result.node.parent_id]);
  return serializeNode(result.node);
}

module.exports = { getVersion, listVersions, pruneVersions, replaceContent, restoreVersion };
