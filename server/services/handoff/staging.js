'use strict';

// Staged files of a send. They are ordinary blobs under the account's key
// (encrypted at rest, counted in used_bytes) referenced by handoff_files, and
// removed through the normal blob release as soon as the send ends.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { assertDiskSpace } = require('../../utils/disk');
const { deleteBlobFiles, moveIntoPlace, removeQuietly, writeTemporaryBlob } = require('../storage/blob_store');
const { recordBlob, releaseBlobs, retainBlob } = require('../storage/blob_refs');
const { uploadLimit } = require('../storage/quota');

/**
 * Stores the body of the request as the staged content of `file` (a
 * handoff_files row). Exactly the declared size must arrive. Sending the file
 * again replaces what was staged.
 */
async function stageUpload(userId, file, body) {
  assertDiskSpace(file.size);
  const blob = await writeTemporaryBlob(userId, body, { ...uploadLimit(userId), expectedSize: file.size });
  let placed = null;
  let replaced = [];
  try {
    placed = await moveIntoPlace(userId, blob);
    replaced = db.transaction(() => {
      const live = db.prepare(`
        SELECT f.blob_id FROM handoff_files f JOIN handoff_offers o ON o.id = f.offer_id
        WHERE f.id = ? AND o.status = 'uploading'
      `).get(file.id);
      if (!live) throw httpError(409, 'This send is no longer waiting for files.', 'OFFER_CLOSED');
      recordBlob(userId, blob);
      retainBlob(blob.blobId);
      db.prepare('UPDATE handoff_files SET blob_id = ? WHERE id = ?').run(blob.blobId, file.id);
      return live.blob_id ? releaseBlobs([live.blob_id]) : [];
    })();
  } catch (err) {
    await removeQuietly(placed || blob.tmpPath);
    throw err;
  }
  await deleteBlobFiles(userId, replaced);
}

/** Points a staged file at a file already in NeoCloud: a copy that shares its blob. */
function stageExisting(fileId, node) {
  retainBlob(node.blob_id);
  db.prepare('UPDATE handoff_files SET blob_id = ? WHERE id = ?').run(node.blob_id, fileId);
}

/** Drops every staged file of an offer inside the caller's transaction; returns blobs to delete from disk. */
function releaseOfferFiles(offerId) {
  const blobIds = db.prepare('SELECT blob_id FROM handoff_files WHERE offer_id = ? AND blob_id IS NOT NULL')
    .all(offerId).map((row) => row.blob_id);
  db.prepare('UPDATE handoff_files SET blob_id = NULL WHERE offer_id = ?').run(offerId);
  return releaseBlobs(blobIds);
}

module.exports = { releaseOfferFiles, stageExisting, stageUpload };
