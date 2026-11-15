'use strict';

// Saving the signed file into the cloud of every signer who has an account
// (a party bound to one) and whose profile says so: a copy of the final PDF,
// certificate pages included, owned by the signer and counted against the
// signer's quota. The owner already has the file next to the original. A
// failure never touches the document: it is recorded in the trail
// (`autosave_failed`) and tried once more later (5 minutes on, by the
// background pass); success is `autosaved` with the new file's id.

const db = require('../../db/database');
const { createServiceLogger } = require('../../utils/logger');
const { commitUploadedBlob } = require('../files/commit');
const { createFolder } = require('../files/operations');
const { findLiveChildByName, getLiveNode } = require('../files/tree');
const { openBlobChecked, writeTemporaryBlob } = require('../storage/blob_store');
const { emitToUser } = require('../events');
const { DOCUMENT_STATUS, EVENT, PARTY_STATUS } = require('./constants');
const { appendEvent } = require('./events');
const { liveFinalNode } = require('./final_node');
const { usableFolder } = require('./folders');
const { announceDocument } = require('./inbox');
const { autoSaveSettings } = require('./profile');
const { getDocumentById } = require('./store');

const logger = createServiceLogger('Signing');

const AUTO_FOLDER_NAME = 'Signed documents';
const RETRY_AFTER_MS = 5 * 60 * 1000;
// Parties that completed longer ago than this are left alone: nobody expects a file to appear days later.
const WINDOW_MS = 24 * 60 * 60 * 1000;
const MAX_NAME = 200;

const running = new Set();

function ensureAutoFolder(userId) {
  const root = getLiveNode(userId, 'root');
  const existing = findLiveChildByName(root.id, AUTO_FOLDER_NAME);
  if (existing && existing.type === 'folder' && usableFolder(userId, existing.id)) return existing.id;
  return createFolder(userId, { parentId: root.id, name: AUTO_FOLDER_NAME, conflict: existing ? 'rename' : 'fail' }).id;
}

// The chosen folder while it is usable, else the automatic one (made on demand, never in a vault).
function targetFolder(userId, chosenId) {
  return usableFolder(userId, chosenId)?.id || ensureAutoFolder(userId);
}

function fileNameOf(document) {
  const title = document.title.replace(/[\u0000-\u001f\u007f]+/g, ' ').replace(/[/\\]+/g, '-').trim().slice(0, MAX_NAME) || 'document';
  return `${title} (signed).pdf`;
}

const eventsOf = (partyId, type) => db.prepare('SELECT at FROM sign_events WHERE party_id = ? AND type = ? ORDER BY id').all(partyId, type);

async function copyFinalFile(document, userId, parentId) {
  const node = liveFinalNode(document);
  if (!node) {
    const error = new Error('The signed file is not available.');
    error.code = 'FINAL_UNAVAILABLE';
    throw error;
  }
  const source = await openBlobChecked(document.owner_id, node.blob_id, node.size, { start: 0, end: node.size - 1 });
  const blob = await writeTemporaryBlob(userId, source);
  return commitUploadedBlob(userId, { parentId, name: fileNameOf(document), conflict: 'rename' }, blob);
}

function failureCode(err) {
  return typeof err?.code === 'string' && /^[A-Z][A-Z0-9_]{1,63}$/.test(err.code) ? err.code : 'FAILED';
}

/**
 * Saves the document's final file for one party, if the party qualifies.
 * Resolves with 'saved', 'failed' or 'skipped'; never throws.
 */
async function autosaveParty(documentId, partyId) {
  if (running.has(partyId)) return 'skipped';
  running.add(partyId);
  try {
    const document = getDocumentById(documentId);
    const party = db.prepare('SELECT * FROM sign_parties WHERE id = ? AND document_id = ?').get(partyId, documentId);
    const eligible = document && party && document.status === DOCUMENT_STATUS.COMPLETED && party.status === PARTY_STATUS.COMPLETED
      && party.user_id && party.user_id !== document.owner_id;
    if (!eligible || eventsOf(party.id, EVENT.AUTOSAVED).length > 0) return 'skipped';
    const settings = autoSaveSettings(party.user_id);
    if (!settings.enabled) return 'skipped';
    try {
      const node = await copyFinalFile(document, party.user_id, targetFolder(party.user_id, settings.folderId));
      appendEvent(document.id, { type: EVENT.AUTOSAVED, partyId: party.id, data: { nodeId: node.id } });
      emitToUser(party.user_id, 'signing:saved', { documentId: document.id, nodeId: node.id });
      announceDocument(document.id, document.status);
      return 'saved';
    } catch (err) {
      appendEvent(document.id, { type: EVENT.AUTOSAVE_FAILED, partyId: party.id, data: { code: failureCode(err) } });
      logger.warn(`Saving a signed file to an account failed (${failureCode(err)}).`);
      return 'failed';
    }
  } finally {
    running.delete(partyId);
  }
}

// The parties of a document that may need a save, as ids.
function candidatesOf(documentId) {
  return db.prepare(`
    SELECT id FROM sign_parties WHERE document_id = ? AND status = 'completed' AND user_id IS NOT NULL
  `).all(documentId).map((row) => row.id);
}

/** Saves the final file for every qualifying party of a document (the background does not wait). */
function scheduleAutosave(documentId) {
  setImmediate(async () => {
    for (const partyId of candidatesOf(documentId)) await autosaveParty(documentId, partyId);
  });
}

/**
 * The background pass: a party that completed but was never tried (the server
 * stopped, or it only claimed afterwards) is tried now; one that failed once
 * gets a second try after 5 minutes. A second failure is final.
 */
async function retryPendingAutosaves({ now = Date.now() } = {}) {
  const parties = db.prepare(`
    SELECT p.id, p.document_id FROM sign_parties p JOIN sign_documents d ON d.id = p.document_id
    WHERE d.status = 'completed' AND d.completed_at >= ? AND p.status = 'completed' AND p.user_id IS NOT NULL AND p.user_id != d.owner_id
  `).all(new Date(now - WINDOW_MS).toISOString());
  let tried = 0;
  for (const party of parties) {
    if (eventsOf(party.id, EVENT.AUTOSAVED).length > 0) continue;
    const failures = eventsOf(party.id, EVENT.AUTOSAVE_FAILED);
    const waited = failures.length === 0 || Date.parse(failures[failures.length - 1].at) <= now - RETRY_AFTER_MS;
    if (failures.length >= 2 || !waited) continue;
    if (await autosaveParty(party.document_id, party.id) !== 'skipped') tried += 1;
  }
  return tried;
}

module.exports = { autosaveParty, retryPendingAutosaves, scheduleAutosave };
