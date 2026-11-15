'use strict';

// Sending a document and managing the links of its parties. A link's full URL
// exists only in the response that creates it: the database keeps the hash,
// and for a party with an email address an encrypted copy of the token so
// later mails (its turn, a reminder, the signed file) can link to the same
// page (see docs/signing.md).

const db = require('../../db/database');
const { isSigningMailEnabled } = require('../account/service_email');
const { encryptValue } = require('../crypto/keys');
const { httpError } = require('../../utils/http_error');
const {
  DOCUMENT_STATUS, EVENT, LIVE_STATUSES, PARTY_STATUS, SIGN_PAGE_PATH,
} = require('./constants');
const { CONSENT_VERSION } = require('./consent');
const { appendEvent, clientOf } = require('./events');
const { describeDocument } = require('./documents');
const { revokeForParty } = require('./grants');
const { typeOf } = require('./field_types');
const { assertMayMail } = require('./mail/limits');
const { cancelQueued } = require('./mail/outbox');
const { queueInvitations, queueRenewedInvitation } = require('./mail/triggers');
const { parseSendOptions } = require('./mail/options');
const { notifyOwner } = require('./notify');
const { partyView } = require('./serialize');
const {
  getOwnedDocument, getParty, listFields, listParties, nowIso, updateDocument,
} = require('./store');
const { generateToken, hashToken, tokenHint } = require('./tokens');
const { initialStatuses } = require('./turns');

function linkUrl(origin, token) {
  return `${origin}${SIGN_PAGE_PATH}${token}`;
}

function invalidState(message, code = 'INVALID_STATE') {
  return httpError(409, message, code);
}

function assertSendable(document, parties, fields) {
  if (document.status !== DOCUMENT_STATUS.DRAFT) {
    throw invalidState('This document was already sent. Create a new link for a party instead.', 'ALREADY_SENT');
  }
  if (document.expires_at && Date.parse(document.expires_at) <= Date.now()) {
    throw httpError(400, 'The expiry has passed. Set a new one before sending.', 'EXPIRY_IN_PAST');
  }
  // A party with nothing to fill in could never "sign": it would only click Finish.
  for (const party of parties) {
    const fillable = fields.some((field) => field.party_id === party.id && field.readonly === 0
      && typeOf(field.type).storage !== 'static');
    if (!fillable) throw httpError(400, `${party.name} has nothing to fill in or sign.`, 'PARTY_WITHOUT_FIELDS');
  }
}

// The link's token, encrypted for later mails, kept only for a party that has an address.
const tokenCopy = (party, token) => (party.email ? encryptValue(token) : null);

/**
 * Sends a draft: one link per party. Returns the document, the links (the
 * only time the URLs can be shown) and, per party, what happens to its mail
 * (`delivery`). `options` is the request body: { message, deliverEmail,
 * reminderDays }; without a configured mail service nothing is mailed.
 */
async function sendDocument(userId, documentId, req, origin, options = {}) {
  const document = getOwnedDocument(userId, documentId);
  const parties = listParties(document.id);
  assertSendable(document, parties, listFields(document.id));
  const choice = parseSendOptions(options);
  const statuses = initialStatuses(document.signing_order, parties);
  // Mail goes to other people: only from a verified address and within the daily limits (mail/limits.js).
  if (choice.deliverEmail && isSigningMailEnabled()) {
    assertMayMail(userId, parties
      .filter((party) => party.email && party.is_self === 0 && statuses.get(party.id) !== PARTY_STATUS.WAITING)
      .map((party) => ({ email: party.email, kind: 'invitation' })));
  }
  const tokens = new Map(parties.map((party) => [party.id, generateToken()]));
  const at = nowIso();
  db.transaction(() => {
    for (const party of parties) {
      const token = tokens.get(party.id);
      db.prepare('UPDATE sign_parties SET token_hash = ?, token_hint = ?, token_enc = ?, status = ? WHERE id = ?')
        .run(hashToken(token), tokenHint(token), tokenCopy(party, token), statuses.get(party.id), party.id);
    }
    updateDocument(document.id, {
      status: DOCUMENT_STATUS.SENT,
      sent_at: at,
      consent_text_version: CONSENT_VERSION,
      message: choice.message,
      reminder_days: choice.reminderDays,
      deliver_email: choice.deliverEmail && isSigningMailEnabled() ? 1 : 0,
    }, at);
    appendEvent(document.id, {
      type: EVENT.SENT, at, ...clientOf(req), data: { parties: parties.length, order: document.signing_order },
    });
  })();
  const links = parties.map((party) => ({ partyId: party.id, url: linkUrl(origin, tokens.get(party.id)) }));
  const delivery = queueInvitations(getOwnedDocument(userId, document.id), listParties(document.id));
  notifyOwner(userId, document.id, DOCUMENT_STATUS.SENT);
  return { ...describeDocument(userId, document.id), links, delivery };
}

// The party of a document that is out for signing and still has to act.
function livePartyOf(userId, documentId, partyId) {
  const document = getOwnedDocument(userId, documentId);
  if (!LIVE_STATUSES.includes(document.status)) throw invalidState('This document is not out for signing.');
  const party = getParty(document.id, partyId);
  if (party.status === PARTY_STATUS.COMPLETED || party.status === PARTY_STATUS.DECLINED) {
    throw invalidState('This party has already finished.', 'PARTY_FINISHED');
  }
  return { document, party };
}

/** Gives a party a new link; the old one stops working at once. */
function regenerateLink(userId, documentId, partyId, req, origin) {
  const { document, party } = livePartyOf(userId, documentId, partyId);
  const token = generateToken();
  db.transaction(() => {
    // The new link is another person's or at least another visit: whoever agreed
    // and typed a name through the old link must not stand in for it.
    db.prepare(`
      UPDATE sign_parties SET token_hash = ?, token_hint = ?, token_enc = ?, consented_at = NULL, signer_name = NULL, timezone = NULL,
        tz_offset_minutes = NULL, ip = NULL, user_agent = NULL, user_id = CASE WHEN is_self = 1 THEN user_id ELSE NULL END WHERE id = ?
    `).run(hashToken(token), tokenHint(token), tokenCopy(party, token), party.id);
    revokeForParty(party.id);
    updateDocument(document.id, {});
    appendEvent(document.id, { type: EVENT.LINK_REGENERATED, partyId: party.id, ...clientOf(req) });
  })();
  // The old link's mail (still waiting) is replaced by one with the new link.
  const delivery = queueRenewedInvitation(document, getParty(document.id, party.id));
  notifyOwner(userId, document.id, document.status);
  return { partyId: party.id, url: linkUrl(origin, token), ...(delivery ? { delivery } : {}) };
}

/** Turns a party's link off without replacing it. */
function revokeLink(userId, documentId, partyId, req) {
  const { document, party } = livePartyOf(userId, documentId, partyId);
  db.transaction(() => {
    db.prepare('UPDATE sign_parties SET token_hash = NULL, token_hint = NULL, token_enc = NULL WHERE id = ?').run(party.id);
    cancelQueued({ documentId: document.id, partyId: party.id });
    revokeForParty(party.id);
    updateDocument(document.id, {});
    appendEvent(document.id, { type: EVENT.REVOKED, partyId: party.id, ...clientOf(req) });
  })();
  notifyOwner(userId, document.id, document.status);
  return { party: partyView(getParty(document.id, party.id)) };
}

module.exports = { regenerateLink, revokeLink, sendDocument };
