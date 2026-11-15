'use strict';

// Freezing the document to sign. When a document is created, its bytes are
// turned into one PDF and kept as a blob of their own (an extra reference on
// the source's blob when it already was a PDF). Later edits, moves or the
// deletion of the source file change nothing about the document, and the
// positions of the fields keep matching the pages.

const crypto = require('crypto');
const { httpError } = require('../../utils/http_error');
const { remainingBytes } = require('../storage/quota');
const { LIMITS } = require('./constants');
const { readBlobBuffer, storeBlob } = require('./blobs');
const engine = require('./pdf_engine');

const OFFICE_MIME = /officedocument|msword|ms-excel|ms-powerpoint|opendocument|rtf$/i;
const IMAGE_MIME = /^image\/(png|jpeg|webp|gif|bmp|tiff|avif|heic|heif)$/i;

function looksLikePdf(bytes) {
  return bytes.subarray(0, 1024).includes('%PDF-');
}

/** Which of 'pdf' | 'image' | 'office' a file is, or null. A PDF is recognised by its bytes, the rest by type. */
function sourceKind(node, bytes) {
  if (looksLikePdf(bytes)) return 'pdf';
  if (IMAGE_MIME.test(node.mime || '')) return 'image';
  if (OFFICE_MIME.test(node.mime || '')) return 'office';
  return null;
}

/** What the server can turn into a signable PDF. */
function signingCapabilities() {
  return {
    formats: { pdf: true, image: true, office: engine.canConvertDocuments() },
    maxSourceBytes: LIMITS.maxSourceBytes,
    maxPages: LIMITS.maxPages,
    maxParties: LIMITS.maxParties,
    maxFields: LIMITS.maxFields,
  };
}

async function toPdf(kind, bytes, node) {
  if (kind === 'pdf') return bytes;
  if (kind === 'image') return engine.imageToPdf(bytes);
  return engine.docToPdf(bytes, node.name);
}

/**
 * Reads the source file and prepares its snapshot. Resolves with
 * `{ pdf, sha256, probe, reuseBlobId, stored }`: either `reuseBlobId` (the
 * file already was the PDF: take another reference on its blob) or `stored`
 * (`{ blob, path }` of the converted PDF, to adopt or discard).
 * Throws 4xx for files that cannot be signed.
 */
async function prepareSnapshot(userId, node) {
  if (node.vault_id) throw httpError(400, 'Files in a vault can’t be prepared for signing.', 'VAULT_FILE');
  if (node.size > LIMITS.maxSourceBytes) {
    throw httpError(413, 'This file is too large to sign (50 MB at most).', 'DOCUMENT_TOO_LARGE');
  }
  const bytes = await readBlobBuffer(userId, node.blob_id, node.size);
  const kind = sourceKind(node, bytes);
  if (!kind) throw httpError(415, 'Only PDF files, images and office documents can be signed.', 'UNSUPPORTED_TYPE');

  const pdf = await toPdf(kind, bytes, node);
  const probe = await engine.probePdf(pdf);
  if (probe.encrypted) throw httpError(400, 'This PDF is password-protected. Remove the protection first.', 'PDF_ENCRYPTED');
  if (!(probe.pageCount >= 1)) throw httpError(400, 'This PDF has no pages.', 'INVALID_PDF');
  if (probe.pageCount > LIMITS.maxPages) {
    throw httpError(400, `This PDF has more than ${LIMITS.maxPages} pages.`, 'TOO_MANY_PAGES');
  }
  const sha256 = crypto.createHash('sha256').update(pdf).digest('hex');
  if (kind === 'pdf') return { pdf, sha256, probe, reuseBlobId: node.blob_id, stored: null };

  if (pdf.length > remainingBytes(userId)) throw httpError(413, 'Not enough storage left for this file.', 'QUOTA_EXCEEDED');
  return { pdf, sha256, probe, reuseBlobId: null, stored: await storeBlob(userId, pdf) };
}

module.exports = { prepareSnapshot, signingCapabilities };
