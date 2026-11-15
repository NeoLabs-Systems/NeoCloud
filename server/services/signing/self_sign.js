'use strict';

// The owner signs one of the parties themselves. The party is marked as
// theirs (no invitation or reminder is ever mailed for it), bound to their
// account, and opened through a grant (grants.js) rather than a link.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { DOCUMENT_STATUS, SIGN_PAGE_PATH } = require('./constants');
const { createGrant } = require('./grants');
const { cancelQueued } = require('./mail/outbox');
const { getOwnedDocument, getParty, listParties } = require('./store');

const FINISHED = ['completed', 'declined'];

/** `{ url }`: a path of the web app that opens the owner's own party. */
function openAsOwner(userId, documentId, partyId) {
  const document = getOwnedDocument(userId, documentId);
  const party = getParty(document.id, partyId);
  if (document.status === DOCUMENT_STATUS.DRAFT) throw httpError(409, 'Send the document before you sign it.', 'INVALID_STATE');
  if (!party.token_hash && !FINISHED.includes(party.status)) throw httpError(409, 'This link was turned off.', 'LINK_REVOKED');
  if (party.user_id && party.user_id !== userId) throw httpError(409, 'This party was taken by another account.', 'ALREADY_CLAIMED');
  if (listParties(document.id).some((other) => other.is_self === 1 && other.id !== party.id)) {
    throw httpError(409, 'Another party is already you.', 'SELF_PARTY_EXISTS');
  }
  db.transaction(() => {
    db.prepare('UPDATE sign_parties SET is_self = 1, user_id = ? WHERE id = ?').run(userId, party.id);
    cancelQueued({ documentId: document.id, partyId: party.id });
  })();
  return { url: `${SIGN_PAGE_PATH}${createGrant(userId, party.id)}` };
}

module.exports = { openAsOwner };
