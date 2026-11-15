'use strict';

// Bytes in and out of the owner's encrypted blob storage, for what the
// signing feature keeps as blobs: converted source PDFs, signature images and
// attachments. The file is put in place first; the caller records it in the
// database and, if that fails, discards the file (see CLAUDE.md invariants).

const { Readable } = require('stream');
const { moveIntoPlace, openBlobChecked, removeQuietly, writeTemporaryBlob } = require('../storage/blob_store');
const { recordBlob, retainBlob } = require('../storage/blob_refs');

async function readBlobBuffer(userId, blobId, size) {
  if (size === 0) return Buffer.alloc(0);
  const chunks = [];
  const stream = await openBlobChecked(userId, blobId, size, { start: 0, end: size - 1 });
  for await (const chunk of stream) chunks.push(chunk);
  return Buffer.concat(chunks);
}

/**
 * Encrypts `source` (a Buffer or a readable) into the owner's blob store.
 * Returns `{ blob, path }`: pass `blob` to adoptBlob inside the transaction,
 * and call discard(path) when that does not happen.
 */
async function storeBlob(userId, source, options = {}) {
  const stream = Buffer.isBuffer(source) ? Readable.from([source]) : source;
  const blob = await writeTemporaryBlob(userId, stream, options);
  const path = await moveIntoPlace(userId, blob);
  return { blob, path };
}

/** Records a stored blob with one reference. Call inside the transaction. */
function adoptBlob(userId, blob) {
  recordBlob(userId, blob);
  retainBlob(blob.blobId);
}

const discard = (path) => removeQuietly(path);

module.exports = { adoptBlob, discard, readBlobBuffer, storeBlob };
