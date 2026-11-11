'use strict';

// Turns a freshly written blob into a file in the tree. Shared by the
// single-request upload and the resumable upload.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { validateName } = require('../../utils/file_names');
const { deleteBlobFiles, moveIntoPlace, removeQuietly } = require('../storage/blob_store');
const { recordBlob, retainBlob } = require('../storage/blob_refs');
const { remainingBytes } = require('../storage/quota');
const { announceChanges } = require('./changes');
const { freeName, insertNode, nameExists, touchFolder } = require('./operations');
const { findLiveChildByName, getLiveNode, nodeById, serializeNode } = require('./tree');
const { replaceContent } = require('./versions');

/**
 * `blob` comes from writeTemporaryBlob. Conflict modes for an existing name:
 *   fail    -> 409 NAME_EXISTS
 *   rename  -> stored as "name (1)"
 *   replace -> becomes the file's new version (same node id)
 * `baseEtag`, when given with replace, must match the file's current etag,
 * so a sync client never overwrites a change it has not seen (412).
 */
async function commitUploadedBlob(userId, { parentId, name, conflict, baseEtag = null, uploadId = null }, blob) {
  const cleanName = validateName(name);
  let finalPath = null;
  try {
    finalPath = await moveIntoPlace(userId, blob);
    const result = db.transaction(() => {
      const parent = getLiveNode(userId, parentId, { type: 'folder' });
      if (blob.size > remainingBytes(userId, { excludeUploadId: uploadId })) {
        throw httpError(413, 'Not enough storage left for this file.', 'QUOTA_EXCEEDED');
      }
      recordBlob(userId, blob);
      if (uploadId) db.prepare('DELETE FROM upload_sessions WHERE id = ?').run(uploadId);

      const existing = findLiveChildByName(parent.id, cleanName);
      if (existing && conflict === 'replace' && existing.type === 'file') {
        if (baseEtag && baseEtag !== existing.etag) {
          throw httpError(412, 'The file changed since you last saw it.', 'ETAG_MISMATCH');
        }
        const orphaned = replaceContent(userId, existing, { blobId: blob.blobId, size: blob.size, etag: blob.sha256 });
        touchFolder(parent.id);
        return { orphaned, node: nodeById(existing.id) };
      }
      if (existing && conflict !== 'rename') throw nameExists(existing);

      const node = insertNode(userId, {
        parentId: parent.id,
        type: 'file',
        name: existing ? freeName(parent.id, cleanName) : cleanName,
        size: blob.size,
        blobId: blob.blobId,
        etag: blob.sha256,
      });
      retainBlob(blob.blobId);
      touchFolder(parent.id);
      return { orphaned: [], node };
    })();
    await deleteBlobFiles(userId, result.orphaned);
    announceChanges(userId, [result.node.parent_id]);
    return serializeNode(result.node);
  } catch (err) {
    await removeQuietly(finalPath || blob.tmpPath);
    throw err;
  }
}

module.exports = { commitUploadedBlob };
