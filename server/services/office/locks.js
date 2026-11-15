'use strict';

// WOPI locks, kept in memory: one opaque string per file, valid for 30
// minutes unless refreshed. A restart drops them; the editor then simply
// locks again.

const LOCK_TTL_MS = 30 * 60 * 1000;
const MAX_LOCK_LENGTH = 1024;

const locks = new Map();

/** The lock on a file, '' when there is none (or it ran out). */
function currentLock(nodeId, now = Date.now()) {
  const entry = locks.get(nodeId);
  if (!entry) return '';
  if (entry.expiresAt <= now) {
    locks.delete(nodeId);
    return '';
  }
  return entry.lock;
}

function setLock(nodeId, lock, now = Date.now()) {
  locks.set(nodeId, { lock, expiresAt: now + LOCK_TTL_MS });
}

function clearLock(nodeId) {
  locks.delete(nodeId);
}

module.exports = { LOCK_TTL_MS, MAX_LOCK_LENGTH, clearLock, currentLock, setLock };
