'use strict';

// Sending the document's frozen PDF and the images people entered. Blobs are
// decrypted on the fly; responses carry the same sandbox policy as file
// downloads, so nothing stored can run as the app.

const { pipeline } = require('stream/promises');
const { parseRange } = require('../../utils/http_range');
const { contentDisposition } = require('../../utils/file_names');
const { openBlobChecked } = require('../storage/blob_store');

const SANDBOX_CSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'";

// A stored PDF blob of the document's owner, with Range support.
async function sendPdfBlob(req, res, { ownerId, blobId, size, sha256, disposition, fileName }) {
  const range = parseRange(req.get('range'), size);
  const start = range && !range.unsatisfiable ? range.start : 0;
  const end = range && !range.unsatisfiable ? range.end : size - 1;
  const content = range?.unsatisfiable || req.method === 'HEAD'
    ? null
    : await openBlobChecked(ownerId, blobId, size, { start, end });
  res.set({
    'Content-Type': 'application/pdf',
    'Content-Disposition': contentDisposition(disposition, fileName),
    'Accept-Ranges': 'bytes',
    ETag: `"${sha256}"`,
    'Cache-Control': 'private, no-store',
    'X-Content-Type-Options': 'nosniff',
    'Content-Security-Policy': SANDBOX_CSP,
  });
  if (range?.unsatisfiable) {
    res.status(416).set('Content-Range', `bytes */${size}`).end();
    return;
  }
  if (range) res.status(206).set('Content-Range', `bytes ${start}-${end}/${size}`);
  res.set('Content-Length', String(end - start + 1));
  if (req.method === 'HEAD') {
    res.end();
    return;
  }
  await pipeline(content, res);
}

/** The frozen PDF, for the owner or a signer. */
function sendSnapshotPdf(req, res, document) {
  return sendPdfBlob(req, res, {
    ownerId: document.owner_id,
    blobId: document.snapshot_blob_id,
    size: document.snapshot_size,
    sha256: document.snapshot_sha256,
    disposition: 'inline',
    fileName: `${document.title}.pdf`,
  });
}

/** The cached signed file without certificate pages (see clean.js) as a download named `fileName`. */
function sendCleanPdf(req, res, document, fileName) {
  return sendPdfBlob(req, res, {
    ownerId: document.owner_id,
    blobId: document.clean_blob_id,
    size: document.clean_size,
    sha256: document.clean_sha256,
    disposition: 'attachment',
    fileName,
  });
}

/** An uploaded image value (re-encoded PNG). `row` is its sign_values row. */
async function sendValueImage(res, ownerId, row, size) {
  const content = await openBlobChecked(ownerId, row.blob_ref, size, { start: 0, end: size - 1 });
  res.set({
    'Content-Type': 'image/png',
    'Content-Length': String(size),
    'Cache-Control': 'private, no-store',
    'X-Content-Type-Options': 'nosniff',
    'Content-Security-Policy': SANDBOX_CSP,
  });
  await pipeline(content, res);
}

module.exports = { sendCleanPdf, sendSnapshotPdf, sendValueImage };
