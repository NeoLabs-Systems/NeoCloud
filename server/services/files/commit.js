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
 * `keepIdentical` (the client declared the content's SHA-256): with rename,
 * a file of that name with the very same content is the upload itself,
 * sent again after its answer was lost. It is returned as it is, so a
 * retry never leaves "name (1)" beside it.
 */
async function commitUploadedBlob(userId, { parentId, name, conflict, baseEtag = null, uploadId = null, keepIdentical = false }, blob) {
  const cleanName = validateName(name);
  let finalPath = null;
  let result;
  try {
    finalPath = await moveIntoPlace(userId, blob);
    result = db.transaction(() => {
      const parent = getLiveNode(userId, parentId, { type: 'folder' });
      const existing = findLiveChildByName(parent.id, cleanName);
      if (keepIdentical && conflict === 'rename' && existing && existing.type === 'file'
        && existing.etag === blob.sha256 && existing.size === blob.size) {
        if (uploadId && db.prepare('DELETE FROM upload_sessions WHERE id = ?').run(uploadId).changes !== 1) {
          throw httpError(404, 'Upload not found or expired', 'UPLOAD_NOT_FOUND');
        }
        return { orphaned: [], node: existing, unchanged: true };
      }
      if (blob.size > remainingBytes(userId, { excludeUploadId: uploadId })) {
        throw httpError(413, 'Not enough storage left for this file.', 'QUOTA_EXCEEDED');
      }
      recordBlob(userId, blob);
      // The session ends with its file; one cancelled or completed meanwhile
      // must not give a second file.
      if (uploadId && db.prepare('DELETE FROM upload_sessions WHERE id = ?').run(uploadId).changes !== 1) {
        throw httpError(404, 'Upload not found or expired', 'UPLOAD_NOT_FOUND');
      }

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
  } catch (err) {
    await removeQuietly(finalPath || blob.tmpPath);
    throw err;
  }
  if (result.unchanged) {
    // Nothing references the blob just written.
    await removeQuietly(finalPath);
    return serializeNode(result.node);
  }
  // Committed: from here on the blob file belongs to the new row, so nothing
  // that fails below may remove it.
  await deleteBlobFiles(userId, result.orphaned);
  announceChanges(userId, [result.node.parent_id]);
  return serializeNode(result.node);
}

module.exports = { commitUploadedBlob };
