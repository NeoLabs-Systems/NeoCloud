'use strict';

// Tells the owner's open web sessions that a document changed, so the list
// and the progress page refresh. Phones poll instead.

const { emitToUser } = require('../events');

function notifyOwner(ownerId, documentId, status) {
  emitToUser(ownerId, 'signing:updated', { documentId, status });
}

module.exports = { notifyOwner };
