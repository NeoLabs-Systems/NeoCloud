'use strict';

// Turning a fully signed document into its final file: the frozen PDF with
// every value drawn in, the audit pages appended and the seal applied, saved
// as an ordinary file next to the source. Runs after the last party
// completed; safe to repeat. A failure (engine error, no storage left) is
// recorded on the document and retried with growing pauses (see worker.js)
// until it works.

const { Readable } = require('stream');
const db = require('../../db/database');
const { splitExtension } = require('../../utils/file_names');
const { createServiceLogger } = require('../../utils/logger');
const { commitUploadedBlob } = require('../files/commit');
const { getLiveNode } = require('../files/tree');
const { writeTemporaryBlob } = require('../storage/blob_store');
const { readBlobBuffer } = require('./blobs');
const { DOCUMENT_STATUS, EVENT } = require('./constants');
const { appendEvent, listEvents } = require('./events');
const { notifyOwner } = require('./notify');
const engine = require('./pdf_engine');
const { getSealSigner } = require('./seal');
const { documentView, fieldView } = require('./serialize');
const {
  getDocumentById, listFields, listParties, listValues, nowIso, parseJson, updateDocument,
} = require('./store');

const logger = createServiceLogger('Signing');

const FIRST_RETRY_MS = 30 * 1000;
const MAX_RETRY_MS = 60 * 60 * 1000;
const FINAL_SUFFIX = { en: 'signed', de: 'signiert' };
const MAX_BASE_NAME = 200;

// Documents being finalized right now in this process.
const running = new Set();
let queue = Promise.resolve();

// "<base> (signed).pdf", or "<base> (signed, no certificate).pdf" for the variant without audit pages.
function finalFileName(document, { certificate = true } = {}) {
  const base = splitExtension(document.source_name)[0] || document.title;
  const german = document.locale === 'de';
  const word = FINAL_SUFFIX[german ? 'de' : 'en'];
  const extra = certificate ? '' : `, ${german ? 'ohne Protokoll' : 'no certificate'}`;
  return `${base.slice(0, MAX_BASE_NAME)} (${word}${extra}).pdf`;
}

function usableFolder(userId, folderId) {
  if (!folderId) return null;
  try {
    const folder = getLiveNode(userId, folderId, { type: 'folder' });
    const isVault = folder.vault_id || db.prepare('SELECT 1 FROM vaults WHERE folder_id = ?').get(folder.id);
    return isVault ? null : folder;
  } catch {
    return null;
  }
}

// The folder of the source file; if that is gone, the folder it was in when
// the document was made; else the root.
function targetFolderId(document) {
  const source = document.source_node_id
    ? db.prepare('SELECT parent_id FROM nodes WHERE id = ?').get(document.source_node_id)
    : null;
  const folder = usableFolder(document.owner_id, source?.parent_id) || usableFolder(document.owner_id, document.source_parent_id);
  return folder ? folder.id : getLiveNode(document.owner_id, 'root').id;
}

// What buildFinalPdf draws: values as the engine reads them (bytes for images and attachments).
async function engineValues(document, fields) {
  const kinds = new Map(fields.map((field) => [field.id, field.type]));
  const values = {};
  for (const row of listValues(document.id)) {
    const value = parseJson(row.value_json, null);
    if (!row.blob_ref) {
      values[row.field_id] = { value };
      continue;
    }
    const blob = db.prepare('SELECT size FROM blobs WHERE id = ?').get(row.blob_ref);
    const bytes = await readBlobBuffer(document.owner_id, row.blob_ref, blob.size);
    values[row.field_id] = kinds.get(row.field_id) === 'file'
      ? { bytes, filename: value.name, mime: value.mime }
      : { bytes, method: value.method, value: value.text ?? null };
  }
  return values;
}

function engineParties(rows) {
  return rows.map((row) => ({
    id: row.id,
    position: row.position,
    name: row.name,
    email: row.email,
    color: row.color,
    signerName: row.signer_name,
    ip: row.ip,
    userAgent: row.user_agent,
    timezone: row.timezone,
    tzOffsetMinutes: row.tz_offset_minutes,
    openedAt: row.opened_at,
    consentedAt: row.consented_at,
    completedAt: row.completed_at,
    declinedAt: row.declined_at,
    declineReason: row.decline_reason,
  }));
}

/** The final PDF's bytes and SHA-256; `certificate: false` leaves out the audit pages. */
async function buildFinal(document, { certificate = true } = {}) {
  const fields = listFields(document.id).map(fieldView);
  const events = listEvents(document.id);
  const allSigned = events.find((event) => event.type === EVENT.COMPLETED);
  return engine.buildFinalPdf({
    pdfBytes: await readBlobBuffer(document.owner_id, document.snapshot_blob_id, document.snapshot_size),
    parties: engineParties(listParties(document.id)),
    fields,
    values: await engineValues(document, fields),
    events,
    document: {
      ...documentView(document),
      originalSha256: document.snapshot_sha256,
      consentTextVersion: document.consent_text_version,
      // The moment everybody had signed: a rebuild gives the same bytes.
      finalizedAt: allSigned ? allSigned.at : nowIso(),
    },
    locale: document.locale,
    signer: getSealSigner() || undefined,
    certificate,
  });
}

async function saveFinalFile(document, bytes) {
  const blob = await writeTemporaryBlob(document.owner_id, Readable.from([bytes]));
  // keepIdentical: a rebuild after an interrupted run finds its own file.
  return commitUploadedBlob(
    document.owner_id,
    { parentId: targetFolderId(document), name: finalFileName(document), conflict: 'rename', keepIdentical: true },
    blob,
  );
}

function markCompleted(document, node, sha256) {
  const at = nowIso();
  db.transaction(() => {
    const changed = db.prepare(`
      UPDATE sign_documents SET status = 'completed', final_node_id = ?, final_sha256 = ?, completed_at = ?,
        finalize_error = NULL, finalize_next_at = NULL, updated_at = ? WHERE id = ? AND status = 'finalizing'
    `).run(node.id, sha256, at, at, document.id).changes;
    if (changed === 1) appendEvent(document.id, { type: EVENT.FINALIZED, at, data: { nodeId: node.id, sha256 } });
  })();
}

// What the owner may read about a failure: ours are plain sentences, the rest is generic.
function failureText(err) {
  if (err.status && err.status < 500 && err.code) return err.message;
  return 'The signed file could not be created yet. NeoCloud will try again.';
}

function markFailed(document, err) {
  const attempts = document.finalize_attempts + 1;
  const wait = Math.min(FIRST_RETRY_MS * 2 ** (attempts - 1), MAX_RETRY_MS);
  updateDocument(document.id, {
    finalize_attempts: attempts,
    finalize_error: failureText(err),
    finalize_next_at: new Date(Date.now() + wait).toISOString(),
  });
  logger.warn(`Finalizing document ${document.id} failed (attempt ${attempts}): ${err.message}`);
}

// One PDF build at a time: building and sealing a large PDF takes several times its size in memory.
function enqueue(task) {
  const turn = queue.then(task);
  queue = turn.catch(() => {});
  return turn;
}

/**
 * Builds and saves the final file of a document in `finalizing`. Another
 * status (already completed, deleted) is a no-op; so is a second call while
 * the first still runs. Never throws: failures are recorded for the retry.
 */
function finalizeDocument(documentId) {
  if (running.has(documentId)) return Promise.resolve();
  running.add(documentId);
  return enqueue(() => runFinalization(documentId)).finally(() => running.delete(documentId));
}

async function runFinalization(documentId) {
  const document = getDocumentById(documentId);
  if (!document || document.status !== DOCUMENT_STATUS.FINALIZING) return;
  try {
    const { bytes, sha256 } = await buildFinal(document);
    const node = await saveFinalFile(document, bytes);
    markCompleted(document, node, sha256);
    notifyOwner(document.owner_id, document.id, DOCUMENT_STATUS.COMPLETED);
  } catch (err) {
    if (getDocumentById(documentId)) markFailed(document, err);
    notifyOwner(document.owner_id, document.id, DOCUMENT_STATUS.FINALIZING);
  }
}

/** Starts finalizing in the background; the caller does not wait for it. */
function scheduleFinalize(documentId) {
  setImmediate(() => {
    finalizeDocument(documentId).catch((err) => logger.error(`Finalizing document ${documentId} stopped unexpectedly: ${err.message}`));
  });
}

/** Retries documents whose pause is over. Returns how many it tried. */
async function retryDueFinalizations() {
  const due = db.prepare(`
    SELECT id FROM sign_documents WHERE status = 'finalizing' AND (finalize_next_at IS NULL OR finalize_next_at <= ?)
  `).all(nowIso());
  for (const { id } of due) await finalizeDocument(id);
  return due.length;
}

module.exports = { buildFinal, enqueue, finalFileName, finalizeDocument, retryDueFinalizations, scheduleFinalize };
