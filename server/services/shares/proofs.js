'use strict';

// Signed cookies a share link hands to visitors: "<expiry ms>.<HMAC>", each
// purpose under a key of its own derived from the session secret.
//   unlock    the right password was given; tied to the link and its
//             password hash, so a new password or another link voids it
//   continue  a counted download of one file; lets Range requests resume it

const crypto = require('crypto');
const { safeEqual } = require('../../utils/security');
const { getSessionSecret } = require('../account/session_secret');

const UNLOCK_LIFETIME_MS = 60 * 60 * 1000;
const CONTINUATION_LIFETIME_MS = 12 * 60 * 60 * 1000;

function linkSignature(purpose, payload) {
  const key = crypto.createHmac('sha256', getSessionSecret()).update(`neocloud-share-${purpose}`).digest();
  return crypto.createHmac('sha256', key).update(payload).digest('base64url');
}

function unlockPayload(share) {
  return `${share.id}.${share.token}.${share.password_hash}`;
}

function continuationPayload(share, nodeId) {
  return `${share.id}.${share.token}.${nodeId}`;
}

function issue(purpose, payload, lifetimeMs) {
  const expiresAtMs = Date.now() + lifetimeMs;
  return { proof: `${expiresAtMs}.${linkSignature(purpose, `${payload}.${expiresAtMs}`)}`, maxAgeMs: lifetimeMs };
}

function isValid(purpose, payload, proof) {
  const [expiresText, signature] = String(proof || '').split('.');
  const expiresAtMs = Number(expiresText);
  if (!Number.isSafeInteger(expiresAtMs) || expiresAtMs <= Date.now() || !signature) return false;
  return safeEqual(signature, linkSignature(purpose, `${payload}.${expiresAtMs}`));
}

/** { proof, maxAgeMs } for the unlock cookie. */
function unlockProof(share) {
  return issue('unlock', unlockPayload(share), UNLOCK_LIFETIME_MS);
}

function isValidUnlockProof(share, proof) {
  return isValid('unlock', unlockPayload(share), proof);
}

/** { proof, maxAgeMs } handed out with a counted download of `nodeId`. */
function continuationProof(share, nodeId) {
  return issue('continue', continuationPayload(share, nodeId), CONTINUATION_LIFETIME_MS);
}

function isContinuation(share, nodeId, proof) {
  return isValid('continue', continuationPayload(share, nodeId), proof);
}

module.exports = { continuationProof, isContinuation, isValidUnlockProof, unlockProof };
