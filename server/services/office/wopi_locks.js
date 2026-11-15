'use strict';

// Lock, Unlock, RefreshLock, GetLock and UnlockAndRelock as the WOPI spec
// defines them. A refused request is a 409 that carries the lock the file
// really has (header X-WOPI-Lock), '' when it has none.

const { httpError } = require('../../utils/http_error');
const { MAX_LOCK_LENGTH, clearLock, currentLock, setLock } = require('./locks');

function lockConflict(held) {
  const error = httpError(409, 'The file is locked by someone else.', 'LOCK_MISMATCH');
  error.wopiLock = held;
  return error;
}

function requireLock(value) {
  const lock = String(value ?? '');
  if (!lock || lock.length > MAX_LOCK_LENGTH) throw httpError(400, 'A lock id is required.', 'LOCK_REQUIRED');
  return lock;
}

/** Throws 409 unless the file's lock is `lock`. */
function assertLockedWith(nodeId, lock) {
  const held = currentLock(nodeId);
  if (held !== lock) throw lockConflict(held);
}

function lock(nodeId, { lock: wanted, oldLock }) {
  const next = requireLock(wanted);
  const held = currentLock(nodeId);
  if (oldLock !== undefined) {
    // UnlockAndRelock
    if (held !== requireLock(oldLock)) throw lockConflict(held);
  } else if (held && held !== next) {
    throw lockConflict(held);
  }
  setLock(nodeId, next);
}

function refreshLock(nodeId, wanted) {
  assertLockedWith(nodeId, requireLock(wanted));
  setLock(nodeId, wanted);
}

function unlock(nodeId, wanted) {
  assertLockedWith(nodeId, requireLock(wanted));
  clearLock(nodeId);
}

module.exports = { assertLockedWith, getLock: currentLock, lock, refreshLock, unlock };
