'use strict';

// Failed sign-ins counted per account, not per client address, so a client
// that changes its address (or spoofs X-Forwarded-For) gains nothing. From
// the 5th failure in 15 minutes each next try waits twice as long (2 s,
// 4 s, ...); the 10th locks the account's sign-in until the oldest of those
// failures leaves the window. Locks are short on purpose: anyone who knows a
// username can trigger one, so it must never shut the owner out for long.
// A pending second-factor sign-in also gets at most 5 codes, then needs the
// password again.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

const FAILURE_WINDOW_MS = 15 * 60 * 1000;
const FAILURES_BEFORE_DELAY = 5;
const FAILURES_BEFORE_LOCK = 10;
const BASE_DELAY_MS = 2000;
const CODES_PER_CHALLENGE = 5;

/**
 * The key failures are counted under: the account when it exists, else the
 * name as typed, so an unknown name is throttled the same way and the
 * answer doesn't reveal which names are registered.
 */
function accountKey(user, typedName) {
  return user ? `user:${user.id}` : `name:${String(typedName || '').trim().toLowerCase().slice(0, 254)}`;
}

function signInLocked(retryAfterMs) {
  const retryAfter = Math.max(1, Math.ceil(retryAfterMs / 1000));
  const wait = retryAfter >= 90 ? `${Math.ceil(retryAfter / 60)} minutes` : `${retryAfter} seconds`;
  const error = httpError(429, `Too many failed sign-in attempts for this account. Try again in ${wait}.`, 'LOGIN_LOCKED');
  error.retryAfter = retryAfter;
  return error;
}

/** Refuses a try while the account is throttled: 429 LOGIN_LOCKED with `retryAfter` seconds. */
function assertSignInAllowed(key, now = Date.now()) {
  const failures = db.prepare(`
    SELECT failed_at_ms FROM login_failures WHERE account_key = ? AND failed_at_ms > ? ORDER BY failed_at_ms
  `).all(key, now - FAILURE_WINDOW_MS).map((row) => row.failed_at_ms);
  if (failures.length >= FAILURES_BEFORE_LOCK) {
    throw signInLocked(failures[failures.length - FAILURES_BEFORE_LOCK] + FAILURE_WINDOW_MS - now);
  }
  if (failures.length >= FAILURES_BEFORE_DELAY) {
    const next = failures[failures.length - 1] + BASE_DELAY_MS * 2 ** (failures.length - FAILURES_BEFORE_DELAY);
    if (now < next) throw signInLocked(next - now);
  }
}

/**
 * Counts a try as failed before its password or code is checked, so
 * parallel guesses count too; a success takes it back (clearFailures).
 */
function recordFailure(key, { kind, challengeId = null }, now = Date.now()) {
  db.prepare('DELETE FROM login_failures WHERE failed_at_ms <= ?').run(now - FAILURE_WINDOW_MS);
  db.prepare('INSERT INTO login_failures (account_key, kind, challenge_id, failed_at_ms) VALUES (?, ?, ?, ?)')
    .run(key, kind, challengeId, now);
}

/**
 * After a right password only its own failures go: wrong second-factor codes
 * keep counting until a full sign-in succeeds (`kind` null clears all).
 */
function clearFailures(key, kind = null) {
  db.prepare('DELETE FROM login_failures WHERE account_key = ? AND (? IS NULL OR kind = ?)').run(key, kind, kind);
}

/** True once a pending second-factor sign-in has used up its codes. */
function challengeExhausted(challengeId) {
  const { count } = db.prepare('SELECT COUNT(*) AS count FROM login_failures WHERE challenge_id = ?').get(challengeId);
  return count >= CODES_PER_CHALLENGE;
}

module.exports = {
  accountKey,
  assertSignInAllowed,
  challengeExhausted,
  clearFailures,
  recordFailure,
};
