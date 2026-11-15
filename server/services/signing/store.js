'use strict';

// Row access shared by the signing modules: lookups scoped to the owner,
// and the small updates every lifecycle step makes.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

const nowIso = () => new Date().toISOString();

function documentNotFound() {
  return httpError(404, 'Document not found', 'NOT_FOUND');
}

/** The owner's document row. Another account's id, a malformed one and a missing one all answer 404. */
function getOwnedDocument(userId, documentId) {
  const id = String(documentId || '');
  const row = UUID_PATTERN.test(id)
    ? db.prepare('SELECT * FROM sign_documents WHERE id = ? AND owner_id = ?').get(id, userId)
    : null;
  if (!row) throw documentNotFound();
  return row;
}

const getDocumentById = (documentId) => db.prepare('SELECT * FROM sign_documents WHERE id = ?').get(documentId);

const listParties = (documentId) => db.prepare('SELECT * FROM sign_parties WHERE document_id = ? ORDER BY position').all(documentId);

const listFields = (documentId) => db.prepare('SELECT * FROM sign_fields WHERE document_id = ? ORDER BY position').all(documentId);

const listValues = (documentId) => db.prepare('SELECT * FROM sign_values WHERE document_id = ?').all(documentId);

/** A party of the document (by id), or 404 as for a missing one. */
function getParty(documentId, partyId) {
  const row = UUID_PATTERN.test(String(partyId || ''))
    ? db.prepare('SELECT * FROM sign_parties WHERE id = ? AND document_id = ?').get(partyId, documentId)
    : null;
  if (!row) throw httpError(404, 'Party not found', 'NOT_FOUND');
  return row;
}

/** Sets columns of a document (names from code, never from input) and bumps updated_at. */
function updateDocument(documentId, columns, at = nowIso()) {
  const assignments = Object.keys(columns).map((name) => `${name} = @${name}, `).join('');
  db.prepare(`UPDATE sign_documents SET ${assignments}updated_at = @updatedAt WHERE id = @id`)
    .run({ ...columns, updatedAt: at, id: documentId });
}

function parseJson(text, fallback) {
  try {
    return JSON.parse(text);
  } catch {
    return fallback;
  }
}

module.exports = {
  UUID_PATTERN,
  getDocumentById,
  getOwnedDocument,
  getParty,
  listFields,
  listParties,
  listValues,
  nowIso,
  parseJson,
  updateDocument,
};
