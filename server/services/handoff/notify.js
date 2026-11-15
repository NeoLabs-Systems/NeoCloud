'use strict';

// Tells the two devices of a send what happened, through their live
// connections. Only the sender and the receiver are told; the offer's
// contents go to the receiver only once the send is ready.

const { serializeOffer } = require('./offers');
const { pushToDevice } = require('./presence');

/** A ready send reaches its receiver (also again when the receiver reconnects). */
function offerArrived(userId, row) {
  pushToDevice(userId, row.target_key, 'handoff:offer', serializeOffer(row, 'receiver'));
}

/** The send changed state: both devices refresh. */
function offerChanged(userId, row) {
  pushToDevice(userId, row.sender_key, 'handoff:updated', { offer: serializeOffer(row, 'sender') });
  if (row.status !== 'uploading') {
    pushToDevice(userId, row.target_key, 'handoff:updated', { offer: serializeOffer(row, 'receiver') });
  }
}

module.exports = { offerArrived, offerChanged };
