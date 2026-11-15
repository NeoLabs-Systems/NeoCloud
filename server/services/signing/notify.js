'use strict';

// Tells the owner's open web sessions that a document changed, so the list
// and the progress page refresh, and the sessions of the accounts that sign it
// that their inbox changed. Phones poll instead.

const { emitToUser } = require('../events');
const { announceDocument } = require('./inbox');

function notifyOwner(ownerId, documentId, status) {
  emitToUser(ownerId, 'signing:updated', { documentId, status });
  announceDocument(documentId, status);
}

module.exports = { notifyOwner };
