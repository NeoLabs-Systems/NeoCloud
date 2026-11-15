'use strict';

// How much one send and one account's open sends may hold. Staged bytes count
// against the account's storage quota while they wait (they are ordinary
// encrypted blobs) and are released the moment the send ends.

const GIB = 1024 ** 3;

const LIMITS = Object.freeze({
  maxFiles: 50,
  maxTotalBytes: 2 * GIB,
  maxOpenOffers: 10,
  maxOpenBytes: 4 * GIB,
  // A send must be fully uploaded within this long, then it waits for its receiver.
  uploadWindowMinutes: 60,
  ttlHours: 24,
  // Finished sends stay listed for the sender this long.
  keepFinishedDays: 3,
});

module.exports = { LIMITS };
