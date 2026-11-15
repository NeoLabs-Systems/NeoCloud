'use strict';

// A signed-in account's signing requests: the parties bound to it and the
// parties whose address is the account's VERIFIED email. An unverified
// address entitles to nothing, or anyone could read other people's requests
// by typing their address. Opening a request mints a grant token (grants.js);
// the party's own token is never shown.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { emitToUser } = require('../events');
const { DOCUMENT_STATUS, EVENT, LIVE_STATUSES, SIGN_PAGE_PATH } = require('./constants');
const { createGrant } = require('./grants');
const { liveFinalNode } = require('./final_node');
const { UUID_PATTERN, getDocumentById } = require('./store');
const { publicState } = require('./turns');

const MAX_ITEMS = 200;
const TODO_STATES = ['ready', 'waiting'];

function verifiedEmail(userId) {
  const user = db.prepare('SELECT email, email_verified_at FROM users WHERE id = ?').get(userId);
  return user?.email && user.email_verified_at ? user.email.toLowerCase() : null;
}

// A document whose time is up counts as expired even before the background pass records it.
function effectiveDocument(row, now) {
  const overdue = LIVE_STATUSES.includes(row.doc_status) && row.expires_at && Date.parse(row.expires_at) <= now;
  return { status: overdue ? DOCUMENT_STATUS.EXPIRED : row.doc_status };
}

function savedNodeIds(partyIds) {
  if (partyIds.length === 0) return new Map();
  const rows = db.prepare(`
    SELECT party_id, json_extract(data_json, '$.nodeId') AS node_id FROM sign_events
    WHERE type = ? AND party_id IN (SELECT value FROM json_each(?)) ORDER BY id
  `).all(EVENT.AUTOSAVED, JSON.stringify(partyIds));
  return new Map(rows.map((row) => [row.party_id, row.node_id]));
}

function entitledRows(userId) {
  return db.prepare(`
    SELECT p.id AS party_id, p.status AS party_status, p.completed_at AS party_completed_at, p.token_hash,
      d.id AS document_id, d.title, d.status AS doc_status, d.expires_at, d.sent_at, d.final_node_id, d.final_sha256,
      u.display_name, u.username
    FROM sign_parties p
    JOIN sign_documents d ON d.id = p.document_id
    JOIN users u ON u.id = d.owner_id
    WHERE d.status != 'draft' AND u.disabled_at IS NULL
      AND (p.user_id = @userId OR (p.user_id IS NULL AND @email IS NOT NULL AND lower(p.email) = @email))
    ORDER BY d.updated_at DESC, d.id DESC
    LIMIT ${MAX_ITEMS}
  `).all({ userId, email: verifiedEmail(userId) });
}

function itemOf(row, now, saved) {
  const status = publicState(effectiveDocument(row, now), { status: row.party_status });
  return {
    documentId: row.document_id,
    partyId: row.party_id,
    title: row.title,
    creatorName: row.display_name || row.username,
    status,
    expiresAt: row.expires_at,
    sentAt: row.sent_at,
    completedAt: row.party_completed_at,
    finalAvailable: row.party_status === 'completed' && liveFinalNode({
      status: row.doc_status, final_node_id: row.final_node_id, final_sha256: row.final_sha256,
    }) !== null,
    ...(saved.has(row.party_id) ? { savedNodeId: saved.get(row.party_id) } : {}),
  };
}

// A request whose link was turned off cannot be acted on; it only stays listed once it is finished.
const isListed = (row) => Boolean(row.token_hash) || row.party_status === 'completed' || row.party_status === 'declined';

/** `{ items, counts: { todo } }`; `status` 'todo' (ready or waiting) or 'done' narrows the list. */
function listInbox(userId, status, now = Date.now()) {
  if (status !== undefined && !['todo', 'done'].includes(status)) {
    throw httpError(400, 'status is todo or done.', 'INVALID_REQUEST');
  }
  const rows = entitledRows(userId).filter(isListed);
  const saved = savedNodeIds(rows.map((row) => row.party_id));
  const all = rows.map((row) => itemOf(row, now, saved));
  const items = all.filter((item) => status === undefined || (status === 'todo') === TODO_STATES.includes(item.status));
  return { items, counts: { todo: all.filter((item) => item.status === 'ready').length } };
}

/** How many requests wait for the account's signature (what the inbox badge shows). */
function todoCount(userId) {
  return listInbox(userId, 'todo').counts.todo;
}

// The party if the account is entitled to it (bound to it, or its address is the account's verified one), else the uniform 404. */
function entitledParty(userId, partyId) {
  const row = UUID_PATTERN.test(String(partyId || ''))
    ? entitledRows(userId).find((candidate) => candidate.party_id === partyId)
    : null;
  if (!row || !isListed(row)) throw httpError(404, 'Request not found', 'NOT_FOUND');
  const party = db.prepare('SELECT * FROM sign_parties WHERE id = ?').get(partyId);
  if (party.user_id && party.user_id !== userId) throw httpError(409, 'This request belongs to another account.', 'ALREADY_CLAIMED');
  return party;
}

/** A grant link for one of the account's requests: `{ url }`, a path of the web app. */
function openInboxParty(userId, partyId) {
  const party = entitledParty(userId, partyId);
  return { url: `${SIGN_PAGE_PATH}${createGrant(userId, party.id)}` };
}

/**
 * `{ document, party }` for the signed file of one of the account's finished
 * requests: the party must be the account's (bound, or by its verified
 * address) and its document completed. A grant dies with the party's
 * signature, so this is how an account gets the file afterwards.
 */
function finalContext(userId, partyId) {
  const party = entitledParty(userId, partyId);
  const document = getDocumentById(party.document_id);
  if (!document || document.status !== DOCUMENT_STATUS.COMPLETED || party.status !== 'completed') {
    throw httpError(404, 'The signed file is not available.', 'FINAL_UNAVAILABLE');
  }
  return { document, party };
}

// Accounts entitled to a party of the document, by binding or by verified address.
function accountsOfDocument(documentId) {
  return db.prepare(`
    SELECT DISTINCT u.id FROM sign_parties p
    JOIN users u ON u.id = p.user_id
      OR (p.user_id IS NULL AND p.email IS NOT NULL AND u.email_verified_at IS NOT NULL AND lower(u.email) = lower(p.email))
    WHERE p.document_id = ?
  `).all(documentId).map((row) => row.id);
}

/** Tells the open sessions of every account entitled to the document that its inbox changed. */
function announceDocument(documentId, status) {
  for (const userId of accountsOfDocument(documentId)) {
    emitToUser(userId, 'signing:inbox', { documentId, status, counts: { todo: todoCount(userId) } });
  }
}

module.exports = { announceDocument, finalContext, listInbox, openInboxParty, todoCount };
