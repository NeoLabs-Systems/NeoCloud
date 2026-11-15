'use strict';

// Whose turn it is. Pure functions over rows; the callers do the writing.
//
// signing_order 'preserved': parties sign one after another by position,
// the next one is `waiting` until the one before has completed.
// 'random': everybody may sign at once.

const { DOCUMENT_STATUS, PARTY_STATUS } = require('./constants');

/** Party statuses right after sending: id -> status. */
function initialStatuses(order, parties) {
  const statuses = new Map();
  parties.forEach((party, index) => {
    const first = order === 'random' || index === 0;
    statuses.set(party.id, first ? PARTY_STATUS.PENDING : PARTY_STATUS.WAITING);
  });
  return statuses;
}

/** The party that becomes ready once the one before it has completed, or null. */
function nextToUnlock(parties) {
  return parties.find((party) => party.status === PARTY_STATUS.WAITING) || null;
}

function allCompleted(parties) {
  return parties.every((party) => party.status === PARTY_STATUS.COMPLETED);
}

/**
 * What a signer's link shows: 'ready' (this party can act), 'waiting' (its
 * turn has not come), 'completed' (it has signed), or the document's end state
 * ('declined', 'expired', 'canceled'). Drafts have no links.
 */
function publicState(document, party) {
  const { status } = document;
  if (status === DOCUMENT_STATUS.CANCELED || status === DOCUMENT_STATUS.EXPIRED || status === DOCUMENT_STATUS.DECLINED) {
    return status;
  }
  if (party.status === PARTY_STATUS.COMPLETED) return 'completed';
  if (party.status === PARTY_STATUS.WAITING) return 'waiting';
  return 'ready';
}

/** How many parties ahead of a waiting party have not completed yet (a count only: signers never learn who the others are). */
function waitingCount(party, parties) {
  return parties.filter((other) => other.position < party.position && other.status !== PARTY_STATUS.COMPLETED).length;
}

module.exports = { allCompleted, initialStatuses, nextToUnlock, publicState, waitingCount };
