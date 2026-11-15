'use strict';

// WOPI access tokens: what the document editor sends back on every call
// instead of the session cookie. Signed (HMAC-SHA-256 under a key derived
// from the session secret), valid for 10 hours, and bound to one account,
// one file and a write/read right. They carry no secrets and grant nothing
// on their own: every WOPI call checks the account's access again.

const crypto = require('crypto');
const { getSessionSecret } = require('../account/session_secret');

const TOKEN_TTL_MS = 10 * 60 * 60 * 1000;

function signingKey() {
  return crypto.createHmac('sha256', getSessionSecret()).update('neocloud-wopi-access-token-v1').digest();
}

function sign(payload) {
  return crypto.createHmac('sha256', signingKey()).update(payload).digest('base64url');
}

function issueAccessToken({ userId, nodeId, write }, now = Date.now()) {
  const expiresAt = now + TOKEN_TTL_MS;
  const payload = Buffer.from(JSON.stringify({ u: userId, n: nodeId, w: write ? 1 : 0, e: expiresAt })).toString('base64url');
  return { token: `${payload}.${sign(payload)}`, expiresAt };
}

/** { userId, nodeId, write, expiresAt }, or null for anything forged, malformed or expired. */
function verifyAccessToken(token, now = Date.now()) {
  const [payload, signature, extra] = String(token || '').split('.');
  if (!payload || !signature || extra !== undefined) return null;
  const expected = Buffer.from(sign(payload));
  const given = Buffer.from(signature);
  if (expected.length !== given.length || !crypto.timingSafeEqual(expected, given)) return null;
  let data;
  try {
    data = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'));
  } catch {
    return null;
  }
  if (!Number.isInteger(data?.u) || typeof data.n !== 'string' || !Number.isInteger(data.e) || data.e <= now) return null;
  return { userId: data.u, nodeId: data.n, write: data.w === 1, expiresAt: data.e };
}

module.exports = { TOKEN_TTL_MS, issueAccessToken, verifyAccessToken };
