'use strict';

// The owner's access to what signers produced: the filled values for the
// progress preview, the final file, and a retry of a stuck finalization.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { getLiveNode } = require('../files/tree');
const { ensureClean } = require('./clean');
const { DOCUMENT_STATUS, EVENT } = require('./constants');
const { appendEvent, clientOf } = require('./events');
const { describeDocument } = require('./documents');
const { finalFileName, scheduleFinalize } = require('./finalize');
const { liveFinalNode } = require('./public');
const { UUID_PATTERN, getOwnedDocument, nowIso, updateDocument } = require('./store');
const { valueMap, valueOf } = require('./values');

/** { values } of every filled field, by field id. Uploaded images are fetched from .../values/:fieldId/image. */
function previewValues(userId, documentId) {
  const document = getOwnedDocument(userId, documentId);
  return { values: valueMap(document.id) };
}

/** The row of an uploaded image of the owner's document, with its size. */
function ownerImage(userId, documentId, fieldId) {
  const document = getOwnedDocument(userId, documentId);
  const row = UUID_PATTERN.test(String(fieldId || ''))
    ? db.prepare(`
      SELECT v.*, b.size AS size FROM sign_values v JOIN blobs b ON b.id = v.blob_ref
      WHERE v.field_id = ? AND v.document_id = ?
    `).get(fieldId, document.id)
    : null;
  if (!row || valueOf(row)?.kind !== 'image') throw httpError(404, 'Not found', 'NOT_FOUND');
  return { row, size: row.size, ownerId: document.owner_id };
}

// The final file while it is still the one that was made.
function intactFinalNode(document) {
  const node = liveFinalNode(document);
  if (!node) throw httpError(404, 'The signed file is not available.', 'FINAL_UNAVAILABLE');
  return node;
}

// One entry per person, variant and minute: browsers fetch a file in ranges,
// and a link must not be able to fill the trail by reloading.
const DOWNLOAD_EVENT_GAP_MS = 60 * 1000;

function recordDownload(document, partyId, by, req, certificate = true) {
  if (req.method === 'HEAD') return;
  const last = db.prepare(`
    SELECT at FROM sign_events
    WHERE document_id = ? AND type = ? AND party_id IS ? AND COALESCE(json_extract(data_json, '$.certificate'), 1) = ?
    ORDER BY id DESC LIMIT 1
  `).get(document.id, EVENT.FINAL_DOWNLOADED, partyId, certificate ? 1 : 0);
  if (last && Date.now() - Date.parse(last.at) < DOWNLOAD_EVENT_GAP_MS) return;
  appendEvent(document.id, {
    type: EVENT.FINAL_DOWNLOADED, partyId, ...clientOf(req), data: certificate ? { by } : { by, certificate: false },
  });
}

/** `?certificate=0|1` of the final-file routes; 1 (with the certificate pages) when absent. */
function wantsCertificate(query) {
  const value = query?.certificate;
  if (value === undefined || value === '1') return true;
  if (value === '0') return false;
  throw httpError(400, 'certificate must be 0 or 1.', 'INVALID_REQUEST');
}

/** The final file of the owner's document; records the download. */
function finalForOwner(userId, documentId, req) {
  const document = getOwnedDocument(userId, documentId);
  const node = intactFinalNode(document);
  recordDownload(document, null, 'owner', req);
  return { ownerId: userId, nodeId: node.id };
}

/** The final file for a signer of the document; records the download. */
function finalForSigner({ document, party }, req) {
  const node = intactFinalNode(document);
  recordDownload(document, party.id, 'signer', req);
  return { ownerId: document.owner_id, nodeId: node.id };
}

// The cached clean variant of a document whose final file is intact, with its download name.
async function cleanOf(document, partyId, by, req) {
  intactFinalNode(document);
  const built = await ensureClean(document);
  recordDownload(built, partyId, by, req, false);
  return { document: built, fileName: finalFileName(built, { certificate: false }) };
}

/** The signed file without certificate pages, for the owner of the document. */
function cleanForOwner(userId, documentId, req) {
  return cleanOf(getOwnedDocument(userId, documentId), null, 'owner', req);
}

/** The same for a signer of a completed document. */
function cleanForSigner({ document, party }, req) {
  return cleanOf(document, party.id, 'signer', req);
}

/** The same through the signed file's node in the library (owner only, the file must still be the one that was made). */
function cleanForNode(userId, nodeId, req) {
  const node = getLiveNode(userId, nodeId, { type: 'file' });
  const document = db.prepare('SELECT * FROM sign_documents WHERE final_node_id = ? AND owner_id = ?').get(node.id, userId);
  if (!document) throw httpError(404, 'File or folder not found', 'NOT_FOUND');
  return cleanOf(document, null, 'owner', req);
}

/** Tries the final file again right away (it is retried on its own with pauses). */
function retryFinalization(userId, documentId) {
  const document = getOwnedDocument(userId, documentId);
  if (document.status !== DOCUMENT_STATUS.FINALIZING) {
    throw httpError(409, 'This document is not waiting for its signed file.', 'INVALID_STATE');
  }
  updateDocument(document.id, { finalize_next_at: nowIso() });
  scheduleFinalize(document.id);
  return describeDocument(userId, document.id);
}

module.exports = {
  cleanForNode, cleanForOwner, cleanForSigner, finalForOwner, finalForSigner, ownerImage, previewValues, retryFinalization, wantsCertificate,
};
