'use strict';

// The signed file without the certificate pages. Built on the first request
// from the same inputs as the full file (same seal, same finalization time),
// cached as a blob of the document's owner and released with the document.
// The blob counts against the owner's quota like any blob (used_bytes only
// changes in blob_refs). One build per document at a time: concurrent first
// requests share it.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { adoptBlob, discard, storeBlob } = require('./blobs');
const { buildFinal, enqueue } = require('./finalize');
const { getDocumentById, nowIso } = require('./store');

const building = new Map();

const hasClean = (row) => Boolean(row?.clean_blob_id && row.clean_sha256);

// Records the built blob unless another build got there first or the document is gone.
async function saveClean(document, { bytes, sha256 }) {
  const stored = await storeBlob(document.owner_id, bytes);
  const kept = db.transaction(() => {
    const current = getDocumentById(document.id);
    if (!current || hasClean(current)) return false;
    adoptBlob(document.owner_id, stored.blob);
    db.prepare('UPDATE sign_documents SET clean_blob_id = ?, clean_sha256 = ?, clean_size = ?, updated_at = ? WHERE id = ?')
      .run(stored.blob.blobId, sha256, bytes.length, nowIso(), document.id);
    return true;
  })();
  if (!kept) await discard(stored.path);
}

async function build(document) {
  const result = await enqueue(async () => {
    // Another request may have finished while this one waited its turn.
    if (hasClean(getDocumentById(document.id))) return null;
    return buildFinal(document, { certificate: false });
  });
  if (result) await saveClean(document, result);
}

/** The document row with its clean variant built; builds it first when it is missing. */
async function ensureClean(document) {
  const current = getDocumentById(document.id);
  if (hasClean(current)) return current;
  if (!building.has(document.id)) {
    const task = build(current).finally(() => building.delete(document.id));
    building.set(document.id, task);
  }
  await building.get(document.id);
  const built = getDocumentById(document.id);
  if (!hasClean(built)) {
    throw httpError(404, 'The signed file is not available.', 'FINAL_UNAVAILABLE');
  }
  return built;
}

module.exports = { ensureClean };
