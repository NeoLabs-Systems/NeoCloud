'use strict';

// When the signing lifecycle sends mail. Every function here only decides and
// queues (mail/outbox.js sends); none of them throws into the lifecycle step
// that called it, and without a configured mail service every one of them
// does nothing, so signing stays link-only exactly as before.
//
// Parties get mail only when the owner chose that at sending
// (sign_documents.deliver_email); the owner's own notices follow the owner's
// profile and need a verified address.

const db = require('../../../db/database');
const { createServiceLogger } = require('../../../utils/logger');
const { isSigningMailEnabled } = require('../../account/service_email');
const { PARTY_STATUS } = require('../constants');
const { notifiesOnEachSignature } = require('../profile');
const { getDocumentById, listParties } = require('../store');
const { cancelQueued, enqueueMail, enqueueRequest } = require('./outbox');

const logger = createServiceLogger('SigningMail');

const REQUEST_KINDS = ['invitation', 'your_turn', 'reminder', 'expiry_warning'];

/** The owner's address when it is verified (the only kind notices go to), else null. */
function verifiedEmailOf(userId) {
  const user = db.prepare('SELECT email, email_verified_at, disabled_at FROM users WHERE id = ?').get(userId);
  return user?.email && user.email_verified_at && !user.disabled_at ? user.email : null;
}

const partiesMayBeMailed = (document) => isSigningMailEnabled() && document.deliver_email === 1;

// A party that can be mailed a request: it has an address and is not the owner signing themselves.
const canBeMailed = (party) => Boolean(party.email) && party.is_self === 0;

// Triggers run inside lifecycle steps (often inside their transactions): a mail problem must not undo those.
function guarded(label, task) {
  try {
    return task();
  } catch (err) {
    logger.error(`Queuing ${label} mail failed: ${err.message}`);
    return undefined;
  }
}

// A mail asking a party to sign, subject to the account's limits (limits.js).
function queueForParty(document, party, kind, payload = {}) {
  return enqueueRequest({ documentId: document.id, partyId: party.id, kind, toEmail: party.email, payload });
}

/**
 * After sending: queues the invitations that are due now (everyone for any
 * order; only the first party for sequential documents) and says what
 * happened to each party: [{ partyId, status }].
 */
function queueInvitations(document, parties) {
  return parties.map((party) => {
    if (!party.email) return { partyId: party.id, status: 'no_email' };
    const due = partiesMayBeMailed(document) && canBeMailed(party) && party.status !== PARTY_STATUS.WAITING;
    if (!due) return { partyId: party.id, status: 'skipped' };
    const result = queueForParty(document, party, 'invitation');
    return result.queued ? { partyId: party.id, status: 'queued' } : { partyId: party.id, status: 'skipped', code: result.code };
  });
}

/** A new link for a party that is already in play: its mail goes out again with the new link. */
function queueRenewedInvitation(document, party) {
  if (!partiesMayBeMailed(document) || !canBeMailed(party) || party.status === PARTY_STATUS.WAITING) return null;
  cancelQueued({ documentId: document.id, partyId: party.id, kinds: REQUEST_KINDS });
  const result = queueForParty(document, party, 'invitation');
  return result.queued ? { partyId: party.id, status: 'queued' } : { partyId: party.id, status: 'skipped', code: result.code };
}

function queueNotice(kind, document, party, extra = {}) {
  const to = verifiedEmailOf(document.owner_id);
  if (!to || !isSigningMailEnabled()) return;
  const done = listParties(document.id).filter((other) => other.status === PARTY_STATUS.COMPLETED).length;
  const total = listParties(document.id).length;
  const name = party ? (party.signer_name || party.name) : '';
  enqueueMail({ documentId: document.id, kind, toEmail: to, payload: { name, done, total, ...extra } });
}

/**
 * A party completed. Cancels its pending mail, tells the next party it is its
 * turn (`unlockedPartyId`), and tells the owner when their profile asks for it.
 * The last signature gets no notice of its own: the completion mail follows.
 */
function onPartyCompleted(documentId, partyId, { unlockedPartyId = null, finalizing = false } = {}) {
  guarded('completion step', () => {
    const document = getDocumentById(documentId);
    if (!document) return;
    cancelQueued({ documentId, partyId, kinds: REQUEST_KINDS });
    const parties = listParties(documentId);
    const unlocked = parties.find((party) => party.id === unlockedPartyId);
    if (unlocked && partiesMayBeMailed(document) && canBeMailed(unlocked)) queueForParty(document, unlocked, 'your_turn');
    const party = parties.find((candidate) => candidate.id === partyId);
    if (!finalizing && party && party.is_self === 0 && notifiesOnEachSignature(document.owner_id)) {
      queueNotice('creator_signed', document, party);
    }
  });
}

/** A party declined: the document is over. */
function onPartyDeclined(documentId, partyId, reason) {
  guarded('decline', () => {
    const document = getDocumentById(documentId);
    if (!document) return;
    cancelQueued({ documentId, kinds: REQUEST_KINDS });
    const party = listParties(documentId).find((candidate) => candidate.id === partyId);
    if (party && party.is_self === 0) queueNotice('creator_declined', document, party, { reason: reason || '' });
  });
}

/**
 * The document ended without everyone signing: 'expired' or 'canceled'. The
 * owner is told about an expiry, not about their own cancel (they just did it).
 */
function onDocumentEnded(documentId, how) {
  guarded(how, () => {
    const document = getDocumentById(documentId);
    if (!document) return;
    cancelQueued({ documentId, kinds: REQUEST_KINDS });
    if (how !== 'canceled') queueNotice(`creator_${how}`, document, null);
  });
}

/**
 * The signed file exists: every party with an address and the owner get the
 * completion mail. Call inside the transaction that records the completion.
 */
function queueCompletionMails(documentId) {
  guarded('completion', () => {
    const document = getDocumentById(documentId);
    if (!document || !isSigningMailEnabled()) return;
    if (partiesMayBeMailed(document)) {
      for (const party of listParties(documentId)) {
        if (canBeMailed(party)) queueForParty(document, party, 'completed');
      }
    }
    const to = verifiedEmailOf(document.owner_id);
    if (to) enqueueMail({ documentId, kind: 'completed', toEmail: to });
  });
}

module.exports = {
  onDocumentEnded,
  onPartyCompleted,
  onPartyDeclined,
  queueCompletionMails,
  queueInvitations,
  queueRenewedInvitation,
  queueForParty,
  verifiedEmailOf,
};
