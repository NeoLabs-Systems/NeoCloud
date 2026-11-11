'use strict';

// WebDAV locks (class 2), kept in memory per account and path. Finder,
// Windows and Office take them around writes; they only ever stop other
// WebDAV requests that don't present the token. The REST API and the sync
// clients never look at them, and a restart forgets them all.

const crypto = require('crypto');
const { httpError } = require('../../utils/http_error');

const DEFAULT_TIMEOUT_SECONDS = 3600;
const MAX_TIMEOUT_SECONDS = 3600;
const MAX_LOCKS_PER_ACCOUNT = 1000;

// userId -> Map(token -> lock)
const locksByUser = new Map();

function liveLocks(userId) {
  const locks = locksByUser.get(userId);
  if (!locks) return [];
  const now = Date.now();
  for (const [token, lock] of locks) {
    if (lock.expiresAt <= now) locks.delete(token);
  }
  if (locks.size === 0) locksByUser.delete(userId);
  return [...locks.values()];
}

function isBelow(key, ancestorKey) {
  return ancestorKey === '' ? key !== '' : key.startsWith(`${ancestorKey}/`);
}

// A lock on `lock.key` that applies to `key` itself.
function covers(lock, key) {
  return lock.key === key || (lock.depth === 'infinity' && isBelow(key, lock.key));
}

/** "Second-600", "Infinite" or a list of them -> seconds, capped. */
function parseTimeout(header) {
  const first = String(header || '').split(',')[0].trim();
  const match = /^Second-(\d+)$/i.exec(first);
  const seconds = match ? Number(match[1]) : DEFAULT_TIMEOUT_SECONDS;
  return Math.min(Math.max(seconds, 1), MAX_TIMEOUT_SECONDS);
}

/** Lock tokens a request submits in its If header. */
function submittedTokens(ifHeader) {
  return [...String(ifHeader || '').matchAll(/<(opaquelocktoken:[^>]+)>/g)].map((match) => match[1]);
}

/** Locks that apply to `key` (for lockdiscovery). */
function locksOn(userId, key) {
  return liveLocks(userId).filter((lock) => covers(lock, key));
}

function lockedError() {
  return httpError(423, 'The resource is locked.', 'LOCKED');
}

/**
 * Throws 423 when a lock the request doesn't hold applies to `key`, or, with
 * `deep`, to anything below it (DELETE and MOVE of a folder).
 */
function assertUnlocked(userId, key, tokens, { deep = false } = {}) {
  const held = new Set(tokens);
  const blocking = liveLocks(userId).find((lock) => (covers(lock, key) || (deep && isBelow(lock.key, key)))
    && !held.has(lock.token));
  if (blocking) throw lockedError();
}

function createLock(userId, { key, href, depth, scope, owner, timeout }) {
  const existing = liveLocks(userId).filter((lock) => covers(lock, key)
    || (depth === 'infinity' && isBelow(lock.key, key)));
  if (existing.some((lock) => lock.scope === 'exclusive') || (scope === 'exclusive' && existing.length > 0)) {
    throw lockedError();
  }
  if (liveLocks(userId).length >= MAX_LOCKS_PER_ACCOUNT) {
    throw httpError(503, 'Too many locks are held. Try again later.', 'TOO_MANY_LOCKS');
  }
  const lock = {
    token: `opaquelocktoken:${crypto.randomUUID()}`,
    key,
    href,
    depth,
    scope,
    owner,
    timeout,
    expiresAt: Date.now() + timeout * 1000,
  };
  if (!locksByUser.has(userId)) locksByUser.set(userId, new Map());
  locksByUser.get(userId).set(lock.token, lock);
  return lock;
}

/** Extends one of the submitted locks that applies to `key`; null if none. */
function refreshLock(userId, key, tokens, timeout) {
  const lock = locksOn(userId, key).find((candidate) => tokens.includes(candidate.token));
  if (!lock) return null;
  lock.timeout = timeout;
  lock.expiresAt = Date.now() + timeout * 1000;
  return lock;
}

/** Removes the lock `token` if it applies to `key`. */
function removeLock(userId, key, token) {
  const lock = locksOn(userId, key).find((candidate) => candidate.token === token);
  if (!lock) return false;
  locksByUser.get(userId).delete(token);
  return true;
}

/** Drops every lock on `key` and below, after the resource moved or went away. */
function releaseLocks(userId, key) {
  const locks = locksByUser.get(userId);
  if (!locks) return;
  for (const lock of liveLocks(userId)) {
    if (lock.key === key || isBelow(lock.key, key)) locks.delete(lock.token);
  }
}

module.exports = {
  assertUnlocked,
  createLock,
  locksOn,
  parseTimeout,
  refreshLock,
  releaseLocks,
  removeLock,
  submittedTokens,
};
