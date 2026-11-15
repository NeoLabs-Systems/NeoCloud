'use strict';

// What parties entered. Typed values are JSON in sign_values; uploads are
// blobs of the owner (see uploads.js) described by a small JSON object. Every
// write re-checks inside its transaction that the party may still write, so a
// value can never change after the party completed or the document closed.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { releaseBlobs } = require('../storage/blob_refs');
const { deleteBlobFiles } = require('../storage/blob_store');
const { adoptBlob } = require('./blobs');
const { LIMITS, LIVE_STATUSES, PARTY_STATUS } = require('./constants');
const { normalizeValue, typeOf } = require('./field_types');
const { getDocumentById, listValues, nowIso, parseJson } = require('./store');

function conflict(message, code) {
  return httpError(409, message, code);
}

/**
 * Throws unless the party may write now: the document is out for signing,
 * it is this party's turn (not yet completed) and consent was given (declining
 * needs no consent).
 */
function assertWritable(documentId, partyId, { needConsent = true } = {}) {
  const document = getDocumentById(documentId);
  const party = db.prepare('SELECT status, consented_at FROM sign_parties WHERE id = ? AND document_id = ?').get(partyId, documentId);
  if (!document || !party || !LIVE_STATUSES.includes(document.status)) {
    throw conflict('This document is closed.', 'DOCUMENT_CLOSED');
  }
  if (party.status === PARTY_STATUS.COMPLETED) throw conflict('You have already signed this document.', 'ALREADY_COMPLETED');
  if (party.status === PARTY_STATUS.WAITING) throw conflict('It is not your turn yet.', 'NOT_YOUR_TURN');
  if (needConsent && !party.consented_at) throw conflict('Agree to sign electronically first.', 'CONSENT_REQUIRED');
}

const isEmptyValue = (value) => value === null || value === '' || value === false || (Array.isArray(value) && value.length === 0);

function removeRow(row) {
  db.prepare('DELETE FROM sign_values WHERE id = ?').run(row.id);
  return row.blob_ref ? releaseBlobs([row.blob_ref]) : [];
}

const rowFor = (fieldId) => db.prepare('SELECT * FROM sign_values WHERE field_id = ?').get(fieldId);

/** Saves a typed value (null or an empty one clears the field). Returns the value as stored. */
function saveTypedValue(document, party, field, rawValue) {
  if (typeOf(field.type).storage !== 'inline') {
    throw httpError(400, 'This field takes an upload, not a typed value.', 'INVALID_VALUE');
  }
  const value = normalizeValue(field, rawValue);
  db.transaction(() => {
    assertWritable(document.id, party.id);
    const existing = rowFor(field.id);
    if (isEmptyValue(value)) {
      if (existing) removeRow(existing);
      return;
    }
    db.prepare(`
      INSERT INTO sign_values (id, document_id, field_id, party_id, value_json, updated_at) VALUES (?, ?, ?, ?, ?, ?)
      ON CONFLICT(field_id) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
    `).run(crypto.randomUUID(), document.id, field.id, party.id, JSON.stringify(value), nowIso());
  })();
  return isEmptyValue(value) ? null : value;
}

// Bytes the document's uploads occupy, leaving out the field being replaced.
function uploadedBytes(documentId, exceptFieldId) {
  return db.prepare(`
    SELECT COALESCE(SUM(b.size), 0) AS bytes FROM sign_values v JOIN blobs b ON b.id = v.blob_ref
    WHERE v.document_id = ? AND v.field_id != ?
  `).get(documentId, exceptFieldId).bytes;
}

function assertRoomForUpload(documentId, fieldId, size) {
  if (uploadedBytes(documentId, fieldId) + size > LIMITS.maxUploadBytesPerDocument) {
    throw httpError(413, 'This document has reached its limit for uploads.', 'UPLOAD_LIMIT');
  }
}

/**
 * Records an uploaded, already stored blob as the field's value (replacing an
 * earlier upload). `meta` describes it for clients. On any failure the caller
 * discards the blob file; the replaced blob's file is removed here, after commit.
 */
async function saveBlobValue(document, party, field, { blob, meta }) {
  const orphaned = db.transaction(() => {
    assertWritable(document.id, party.id);
    assertRoomForUpload(document.id, field.id, blob.size);
    const existing = rowFor(field.id);
    adoptBlob(document.owner_id, blob);
    let released = [];
    if (existing) released = removeRow(existing);
    db.prepare(`
      INSERT INTO sign_values (id, document_id, field_id, party_id, value_json, blob_ref, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
    `).run(crypto.randomUUID(), document.id, field.id, party.id, JSON.stringify(meta), blob.blobId, nowIso());
    return released;
  })();
  await deleteBlobFiles(document.owner_id, orphaned);
  return meta;
}

/** Clears an uploaded value. */
async function clearBlobValue(document, party, field) {
  const orphaned = db.transaction(() => {
    assertWritable(document.id, party.id);
    const existing = rowFor(field.id);
    return existing ? removeRow(existing) : [];
  })();
  await deleteBlobFiles(document.owner_id, orphaned);
}

/** The value of a stored row as clients see it: the JSON value, or the upload's description. */
const valueOf = (row) => parseJson(row.value_json, null);

/** fieldId -> value for the rows of the given parties (all parties when none are named). */
function valueMap(documentId, partyIds = null) {
  const map = {};
  for (const row of listValues(documentId)) {
    if (!partyIds || partyIds.has(row.party_id)) map[row.field_id] = valueOf(row);
  }
  return map;
}

module.exports = {
  assertWritable,
  clearBlobValue,
  rowFor,
  saveBlobValue,
  saveTypedValue,
  valueMap,
  valueOf,
};
