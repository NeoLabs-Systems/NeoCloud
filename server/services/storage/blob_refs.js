'use strict';

// Blob rows and their reference counts. Call these inside the transaction
// that changes the nodes or versions pointing at the blob. A blob whose last
// reference goes away is removed from the database at once; its file is
// returned so the caller can delete it after the transaction commits.

const db = require('../../db/database');

function recordBlob(userId, { blobId, size, sha256 }, encVersion = 1) {
  db.prepare(`
    INSERT INTO blobs (id, user_id, size, sha256, enc_version, ref_count) VALUES (?, ?, ?, ?, ?, 0)
  `).run(blobId, userId, size, sha256, encVersion);
  db.prepare('UPDATE users SET used_bytes = used_bytes + ? WHERE id = ?').run(size, userId);
}

function retainBlob(blobId) {
  db.prepare('UPDATE blobs SET ref_count = ref_count + 1 WHERE id = ?').run(blobId);
}

/** Returns the ids of blobs that are now unreferenced and must be deleted from disk. */
function releaseBlobs(blobIds) {
  const orphaned = [];
  for (const blobId of blobIds) {
    const blob = db.prepare('SELECT user_id, size, ref_count FROM blobs WHERE id = ?').get(blobId);
    if (!blob) continue;
    if (blob.ref_count > 1) {
      db.prepare('UPDATE blobs SET ref_count = ref_count - 1 WHERE id = ?').run(blobId);
      continue;
    }
    db.prepare('DELETE FROM blobs WHERE id = ?').run(blobId);
    db.prepare('UPDATE users SET used_bytes = MAX(0, used_bytes - ?) WHERE id = ?').run(blob.size, blob.user_id);
    orphaned.push(blobId);
  }
  return orphaned;
}

module.exports = { recordBlob, releaseBlobs, retainBlob };
