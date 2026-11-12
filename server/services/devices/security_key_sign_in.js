'use strict';

// Security key sign-in for native clients (the lite desktop app). The
// client is its own WebAuthn client: it asks for options, has the key sign
// a clientDataJSON whose origin is this server's public origin, and swaps
// the assertion for a device token instead of a session. The ceremony is
// the browser's (services/account/webauthn.js), with the pending challenge
// held here under a random id because the client has no session cookie.
//
// Pending ceremonies live in memory: they last minutes, and a restart only
// means touching the key again.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const webauthn = require('../account/webauthn');
const { getTwoFactorStatus, verifySignInCode } = require('../account/two_factor');
const throttle = require('../account/login_throttle');
const { cleanDeviceInfo, createDeviceToken } = require('./tokens');

const TWO_FACTOR_WINDOW_MS = 10 * 60 * 1000;
// Far above what the per-address limiter lets anyone open; a bound on memory.
const MAX_PENDING = 2000;

// challengeId → { stage: 'assertion', challenge } or
//               { stage: 'second_factor', userId, device, expiresAt }
const pending = new Map();

function expiresAt(entry) {
  return entry.stage === 'assertion' ? Number(entry.challenge.expiresAt) : entry.expiresAt;
}

function prune(now = Date.now()) {
  for (const [id, entry] of pending) {
    if (now > expiresAt(entry)) pending.delete(id);
  }
}

// Removes and returns the entry: every challenge is spent by its first use.
function take(challengeId, stage) {
  const id = typeof challengeId === 'string' ? challengeId : '';
  const entry = pending.get(id);
  pending.delete(id);
  if (!entry || entry.stage !== stage || Date.now() > expiresAt(entry)) return null;
  return entry;
}

function issueToken(userId, device) {
  const { device: created, token } = createDeviceToken(userId, device);
  const user = db.prepare('SELECT id, username FROM users WHERE id = ?').get(userId);
  return {
    token,
    device: { id: created.id, name: created.name, platform: created.platform },
    user: { id: user.id, username: user.username },
  };
}

/** Options for the key, for the relying party at `serverOrigin`. */
async function startSecurityKeySignIn(serverOrigin, username) {
  const relyingParty = webauthn.relyingPartyForOrigin(serverOrigin);
  const { options, challenge } = await webauthn.beginLogin({ relyingParty, username });
  prune();
  if (pending.size >= MAX_PENDING) {
    throw httpError(429, 'Too many security key sign-ins in progress, try again shortly.', 'RATE_LIMITED');
  }
  const challengeId = crypto.randomBytes(32).toString('base64url');
  pending.set(challengeId, { stage: 'assertion', challenge });
  return { challengeId, options, origin: relyingParty.origin };
}

/**
 * Verifies the assertion and issues a token. As with the browser sign-in, a
 * key that verified the user (PIN or biometric) stands in for the second
 * factor; one that only confirmed presence leaves an account with 2FA at
 * { requiresTwoFactor, challengeId }, finished by finishTwoFactor.
 */
async function finishSecurityKeySignIn(serverOrigin, { challengeId, response, name, platform }) {
  const device = cleanDeviceInfo({ name, platform });
  const entry = take(challengeId, 'assertion');
  const result = await webauthn.completeLogin({
    relyingParty: webauthn.relyingPartyForOrigin(serverOrigin),
    response,
    pending: entry?.challenge,
  });
  const user = db.prepare('SELECT id FROM users WHERE id = ?').get(result.userId);
  if (!user) throw httpError(401, 'User not found', 'UNAUTHORIZED');
  if (!result.userVerified && getTwoFactorStatus(user.id).enabled) {
    pending.set(challengeId, {
      stage: 'second_factor',
      userId: user.id,
      device,
      expiresAt: Date.now() + TWO_FACTOR_WINDOW_MS,
    });
    return { requiresTwoFactor: true, challengeId };
  }
  return issueToken(user.id, device);
}

/** The authenticator code after a key that did not verify the user. */
async function finishTwoFactor({ challengeId, code }) {
  if (!code) throw httpError(400, 'Two-factor code required', 'MISSING_FIELDS');
  const entry = take(challengeId, 'second_factor');
  if (!entry) throw httpError(401, 'Two-factor challenge expired', 'TWO_FACTOR_EXPIRED');
  try {
    await verifySignInCode({ userId: entry.userId, challengeId, code, restart: 'your security key' });
  } catch (err) {
    // A wrong code keeps the challenge for the next try, up to the limit.
    if (!throttle.challengeExhausted(challengeId)) pending.set(challengeId, entry);
    throw err;
  }
  return issueToken(entry.userId, entry.device);
}

module.exports = { finishSecurityKeySignIn, finishTwoFactor, startSecurityKeySignIn };
