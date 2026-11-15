'use strict';

// Documents as the owner manages them: create from a library file, list,
// read, edit settings, duplicate, cancel, delete. Sending and links are in
// links.js; what signers do is in public.js.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { splitExtension } = require('../../utils/file_names');
const { getLiveNode } = require('../files/tree');
const { releaseBlobs, retainBlob } = require('../storage/blob_refs');
const { deleteBlobFiles } = require('../storage/blob_store');
const { adoptBlob, discard } = require('./blobs');
const {
  DEFAULT_LOCALE, DEFAULT_VALIDITY_DAYS, DELETABLE_STATUSES, DOCUMENT_STATUS, EVENT, LIMITS,
  LIVE_STATUSES, LOCALES, MAX_VALIDITY_DAYS, SIGNING_ORDERS,
} = require('./constants');
const { appendEvent, clientOf } = require('./events');
const { replaceFields, importFields } = require('./fields');
const { notifyOwner } = require('./notify');
const { singleLine } = require('./plain_text');
const { applyInitialStatuses, colorFor, defaultPartyName, insertParty, replaceParties } = require('./parties');
const { prepareSnapshot } = require('./snapshot');
const { readBlobBuffer } = require('./blobs');
const { documentView, fieldView, partySummary, partyView } = require('./serialize');
const {
  getOwnedDocument, listFields, listParties, nowIso, parseJson, updateDocument,
} = require('./store');

const DAY_MS = 24 * 60 * 60 * 1000;
const MAX_PAGE_SIZE = 100;
const DEFAULT_PAGE_SIZE = 50;

function lockedError() {
  return httpError(409, 'This document can no longer be changed.', 'DOCUMENT_LOCKED');
}

function invalidState(message) {
  return httpError(409, message, 'INVALID_STATE');
}

function cleanTitle(value) {
  const title = typeof value === 'string' ? singleLine(value) : '';
  if (!title || title.length > LIMITS.maxTitleLength) {
    throw httpError(400, `A title has 1 to ${LIMITS.maxTitleLength} characters.`, 'INVALID_REQUEST');
  }
  return title;
}

function cleanLocale(value) {
  if (!LOCALES.includes(value)) throw httpError(400, `The language must be one of: ${LOCALES.join(', ')}.`, 'INVALID_REQUEST');
  return value;
}

function cleanExpiry(value) {
  if (value === null) return null;
  const time = typeof value === 'string' ? Date.parse(value) : Number.NaN;
  if (!Number.isFinite(time)) throw httpError(400, 'expiresAt must be a date or null.', 'INVALID_REQUEST');
  if (time <= Date.now()) throw httpError(400, 'The expiry must lie in the future.', 'EXPIRY_IN_PAST');
  if (time > Date.now() + MAX_VALIDITY_DAYS * DAY_MS) {
    throw httpError(400, `A link can stay valid for ${MAX_VALIDITY_DAYS} days at most.`, 'INVALID_REQUEST');
  }
  return new Date(time).toISOString();
}

/** Fields and parties can be edited in a draft, and after sending while nobody has opened a link. */
function isEditable(document, parties) {
  if (document.status === DOCUMENT_STATUS.DRAFT) return true;
  return document.status === DOCUMENT_STATUS.SENT && parties.every((party) => !party.opened_at);
}

function assertEditable(document) {
  if (!isEditable(document, listParties(document.id))) throw lockedError();
}

function describe(document) {
  return {
    document: documentView(document),
    parties: listParties(document.id).map(partyView),
    fields: listFields(document.id).map(fieldView),
  };
}

function describeDocument(userId, documentId) {
  return describe(getOwnedDocument(userId, documentId));
}

/**
 * Makes a draft from a file of the library. The file's bytes are frozen now
 * (see snapshot.js); a default party is added so the editor has one.
 */
async function createDocument(userId, { nodeId, title, locale }, req) {
  if (typeof nodeId !== 'string' || !nodeId) throw httpError(400, 'nodeId is required.', 'INVALID_REQUEST');
  const node = getLiveNode(userId, nodeId, { type: 'file' });
  const documentLocale = locale === undefined ? DEFAULT_LOCALE : cleanLocale(locale);
  const documentTitle = title === undefined ? cleanTitle(splitExtension(node.name)[0] || node.name) : cleanTitle(title);
  const snapshot = await prepareSnapshot(userId, node);
  const id = crypto.randomUUID();
  const at = nowIso();
  const blobId = snapshot.reuseBlobId || snapshot.stored.blob.blobId;
  try {
    db.transaction(() => {
      if (snapshot.stored) adoptBlob(userId, snapshot.stored.blob);
      else retainBlob(blobId);
      db.prepare(`
        INSERT INTO sign_documents (id, owner_id, source_node_id, source_name, source_parent_id, title, locale, expires_at,
          snapshot_blob_id, snapshot_sha256, snapshot_size, page_count, pages_json, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      `).run(
        id, userId, node.id, node.name, node.parent_id, documentTitle, documentLocale,
        new Date(Date.now() + DEFAULT_VALIDITY_DAYS * DAY_MS).toISOString(),
        blobId, snapshot.sha256, snapshot.pdf.length, snapshot.probe.pageCount, JSON.stringify(snapshot.probe.pages), at, at,
      );
      insertParty(id, { name: defaultPartyName(documentLocale, 1), color: colorFor(0), position: 0 }, at);
      appendEvent(id, { type: EVENT.CREATED, at, ...clientOf(req), data: { sourceNodeId: node.id, sha256: snapshot.sha256 } });
    })();
  } catch (err) {
    if (snapshot.stored) await discard(snapshot.stored.path);
    throw err;
  }
  notifyOwner(userId, id, DOCUMENT_STATUS.DRAFT);
  // The final file is built from the frozen pages: existing form fields are
  // flattened and an existing digital signature in the source is lost.
  return { ...describeDocument(userId, id), warnings: snapshot.probe.hasAcroForm ? ['FORM_FIELDS_FLATTENED'] : [] };
}

function encodeCursor(row) {
  return Buffer.from(JSON.stringify([row.updated_at, row.id])).toString('base64url');
}

function decodeCursor(cursor) {
  if (!cursor) return null;
  const parsed = parseJson(Buffer.from(String(cursor), 'base64url').toString('utf8'), null);
  if (!Array.isArray(parsed) || parsed.length !== 2 || parsed.some((part) => typeof part !== 'string')) {
    throw httpError(400, 'The cursor is not valid.', 'INVALID_CURSOR');
  }
  return parsed;
}

/** The owner's documents, newest activity first, `limit` at a time. */
function listDocuments(userId, { status, limit, cursor } = {}) {
  if (status !== undefined && !Object.values(DOCUMENT_STATUS).includes(status)) {
    throw httpError(400, 'Unknown status.', 'INVALID_REQUEST');
  }
  const pageSize = Math.min(Math.max(Math.trunc(Number(limit)) || DEFAULT_PAGE_SIZE, 1), MAX_PAGE_SIZE);
  const after = decodeCursor(cursor);
  const rows = db.prepare(`
    SELECT * FROM sign_documents
    WHERE owner_id = @userId
      AND (@status IS NULL OR status = @status)
      AND (@afterAt IS NULL OR updated_at < @afterAt OR (updated_at = @afterAt AND id < @afterId))
    ORDER BY updated_at DESC, id DESC
    LIMIT @limit
  `).all({
    userId, status: status ?? null, afterAt: after?.[0] ?? null, afterId: after?.[1] ?? null, limit: pageSize + 1,
  });
  const page = rows.slice(0, pageSize);
  const partiesByDocument = new Map(page.map((row) => [row.id, []]));
  const ids = page.map((row) => row.id);
  if (ids.length > 0) {
    const parties = db.prepare(`
      SELECT * FROM sign_parties WHERE document_id IN (SELECT value FROM json_each(?)) ORDER BY position
    `).all(JSON.stringify(ids));
    for (const party of parties) partiesByDocument.get(party.document_id).push(partySummary(party));
  }
  return {
    items: page.map((row) => ({
      id: row.id,
      title: row.title,
      status: row.status,
      parties: partiesByDocument.get(row.id),
      pageCount: row.page_count,
      expiresAt: row.expires_at,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      completedAt: row.completed_at,
      finalNodeId: row.final_node_id,
      sourceNodeId: row.source_node_id,
    })),
    nextCursor: rows.length > pageSize ? encodeCursor(page[page.length - 1]) : null,
  };
}

/** Title, order, expiry and language. Order, fields and parties are only for editable documents. */
function updateSettings(userId, documentId, body, req) {
  const document = getOwnedDocument(userId, documentId);
  assertEditable(document);
  const columns = {};
  if (body?.title !== undefined) columns.title = cleanTitle(body.title);
  if (body?.locale !== undefined) columns.locale = cleanLocale(body.locale);
  if (body?.expiresAt !== undefined) columns.expires_at = cleanExpiry(body.expiresAt);
  if (body?.signingOrder !== undefined) {
    if (!SIGNING_ORDERS.includes(body.signingOrder)) throw httpError(400, 'signingOrder is preserved or random.', 'INVALID_REQUEST');
    columns.signing_order = body.signingOrder;
  }
  db.transaction(() => {
    updateDocument(document.id, columns);
    if (columns.signing_order && document.status === DOCUMENT_STATUS.SENT) applyInitialStatuses(document.id, columns.signing_order);
    if (document.status === DOCUMENT_STATUS.SENT) appendEvent(document.id, { type: EVENT.UPDATED, ...clientOf(req), data: { changed: Object.keys(columns) } });
  })();
  notifyOwner(userId, document.id, document.status);
  return describeDocument(userId, document.id);
}

function saveParties(userId, documentId, parties) {
  const document = getOwnedDocument(userId, documentId);
  if (document.status !== DOCUMENT_STATUS.DRAFT) throw lockedError();
  replaceParties(document.id, parties);
  updateDocument(document.id, {});
  notifyOwner(userId, document.id, document.status);
  return describeDocument(userId, document.id);
}

function saveFields(userId, documentId, fields, req) {
  const document = getOwnedDocument(userId, documentId);
  assertEditable(document);
  replaceFields(document, fields);
  db.transaction(() => {
    updateDocument(document.id, {});
    if (document.status === DOCUMENT_STATUS.SENT) appendEvent(document.id, { type: EVENT.UPDATED, ...clientOf(req), data: { changed: ['fields'] } });
  })();
  notifyOwner(userId, document.id, document.status);
  return { fields: listFields(document.id).map(fieldView) };
}

/** Adds the PDF's own form fields to the first party. Draft only. */
async function importAcroFields(userId, documentId) {
  const document = getOwnedDocument(userId, documentId);
  if (document.status !== DOCUMENT_STATUS.DRAFT) throw lockedError();
  const [firstParty] = listParties(document.id);
  const pdf = await readBlobBuffer(userId, document.snapshot_blob_id, document.snapshot_size);
  const imported = await importFields(document, pdf, firstParty.id);
  notifyOwner(userId, document.id, document.status);
  return { imported, fields: listFields(document.id).map(fieldView) };
}

/** A new draft with the same pages, parties and fields; no values, links or history. */
function duplicateDocument(userId, documentId, req) {
  const source = getOwnedDocument(userId, documentId);
  const id = crypto.randomUUID();
  const at = nowIso();
  const partyIds = new Map();
  db.transaction(() => {
    retainBlob(source.snapshot_blob_id);
    db.prepare(`
      INSERT INTO sign_documents (id, owner_id, source_node_id, source_name, source_parent_id, title, signing_order, locale,
        expires_at, snapshot_blob_id, snapshot_sha256, snapshot_size, page_count, pages_json, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      id, userId, source.source_node_id, source.source_name, source.source_parent_id,
      `${source.title} (copy)`.slice(0, LIMITS.maxTitleLength), source.signing_order, source.locale,
      new Date(Date.now() + DEFAULT_VALIDITY_DAYS * DAY_MS).toISOString(),
      source.snapshot_blob_id, source.snapshot_sha256, source.snapshot_size, source.page_count, source.pages_json, at, at,
    );
    for (const party of listParties(source.id)) {
      partyIds.set(party.id, insertParty(id, { name: party.name, email: party.email, color: party.color, position: party.position }, at));
    }
    for (const field of listFields(source.id)) {
      db.prepare(`
        INSERT INTO sign_fields (id, document_id, party_id, type, name, required, readonly, default_value, prefs_json, areas_json, position)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      `).run(crypto.randomUUID(), id, partyIds.get(field.party_id), field.type, field.name, field.required, field.readonly,
        field.default_value, field.prefs_json, field.areas_json, field.position);
    }
    appendEvent(id, { type: EVENT.CREATED, at, ...clientOf(req), data: { duplicatedFrom: source.id } });
  })();
  notifyOwner(userId, id, DOCUMENT_STATUS.DRAFT);
  return describeDocument(userId, id);
}

/** Stops a document that is out for signing; every link then shows "canceled". */
function cancelDocument(userId, documentId, req) {
  const document = getOwnedDocument(userId, documentId);
  if (!LIVE_STATUSES.includes(document.status)) throw invalidState('Only a document that is out for signing can be canceled.');
  const at = nowIso();
  db.transaction(() => {
    updateDocument(document.id, { status: DOCUMENT_STATUS.CANCELED, canceled_at: at }, at);
    appendEvent(document.id, { type: EVENT.CANCELED, at, ...clientOf(req) });
  })();
  notifyOwner(userId, document.id, DOCUMENT_STATUS.CANCELED);
  return describeDocument(userId, document.id);
}

/** Deletes a document with its values and snapshot. The signed file in the library stays. */
async function deleteDocument(userId, documentId) {
  const document = getOwnedDocument(userId, documentId);
  if (!DELETABLE_STATUSES.includes(document.status)) throw invalidState('Cancel the document before deleting it.');
  const orphaned = db.transaction(() => {
    const blobIds = db.prepare('SELECT blob_ref FROM sign_values WHERE document_id = ? AND blob_ref IS NOT NULL')
      .all(document.id).map((row) => row.blob_ref);
    // Read here, not above: a clean variant may have been cached since the document was loaded.
    const cached = db.prepare('SELECT clean_blob_id FROM sign_documents WHERE id = ? AND clean_blob_id IS NOT NULL')
      .all(document.id).map((row) => row.clean_blob_id);
    db.prepare('DELETE FROM sign_documents WHERE id = ?').run(document.id);
    return releaseBlobs([document.snapshot_blob_id, ...blobIds, ...cached]);
  })();
  await deleteBlobFiles(userId, orphaned);
  notifyOwner(userId, document.id, 'deleted');
  return { success: true };
}

module.exports = {
  assertEditable,
  cancelDocument,
  createDocument,
  deleteDocument,
  describeDocument,
  duplicateDocument,
  importAcroFields,
  isEditable,
  listDocuments,
  saveFields,
  saveParties,
  updateSettings,
};
