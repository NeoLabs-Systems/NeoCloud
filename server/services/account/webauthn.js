'use strict';

// Security keys and passkeys (WebAuthn), ported from NeoAgent. A signed-in
// account registers keys; anyone can then sign in with one, with or without
// typing a username first (discoverable credentials). The challenge for each
// ceremony lives in the session and is used at most once.

const {
  generateAuthenticationOptions,
  generateRegistrationOptions,
  verifyAuthenticationResponse,
  verifyRegistrationResponse,
} = require('@simplewebauthn/server');
const db = require('../../db/database');
const { isAllowedOrigin } = require('../../config/origins');
const { httpError } = require('../../utils/http_error');
const { findUserForSignIn } = require('./credentials');
const throttle = require('./login_throttle');

const RP_NAME = 'NeoCloud';
const CHALLENGE_TTL_MS = 5 * 60 * 1000;
const CEREMONY_TIMEOUT_MS = 2 * 60 * 1000;
const MAX_CREDENTIALS_PER_USER = 20;
const MAX_LABEL_LENGTH = 48;

function toBase64Url(bytes) {
  return Buffer.from(bytes).toString('base64url');
}

function fromBase64Url(value) {
  return new Uint8Array(Buffer.from(String(value), 'base64url'));
}

function parseTransports(row) {
  try {
    const parsed = JSON.parse(row.transports_json || '[]');
    return Array.isArray(parsed) ? parsed.filter((entry) => typeof entry === 'string') : [];
  } catch {
    return [];
  }
}

function normalizeLabel(value, fallback) {
  const label = String(value || '').trim().replace(/\s+/g, ' ');
  if (!label) return fallback;
  if (label.length > MAX_LABEL_LENGTH) {
    throw httpError(400, `Security key name must be ${MAX_LABEL_LENGTH} characters or fewer.`, 'SECURITY_KEY_NAME_TOO_LONG');
  }
  return label;
}

/**
 * WebAuthn ties a credential to the exact hostname it was created on. With
 * PUBLIC_URL set that is the relying party; otherwise it is the origin the
 * ceremony runs on (this server, or an origin in ALLOWED_ORIGINS), as in
 * NeoAgent.
 */
function resolveRelyingParty(req) {
  const publicUrl = String(req.app?.locals?.httpRuntimeConfig?.publicUrl || '');
  let origin;
  if (publicUrl) {
    origin = new URL(publicUrl).origin;
  } else {
    const selfOrigin = `${req.protocol}://${req.get('host')}`;
    origin = String(req.get('origin') || '').trim() || selfOrigin;
    if (origin !== selfOrigin && !isAllowedOrigin(origin, { allowMissingOrigin: false })) {
      throw httpError(403, 'Security key sign-in is not allowed from this origin.', 'ORIGIN_NOT_ALLOWED');
    }
  }
  let hostname = '';
  try {
    hostname = new URL(origin).hostname;
  } catch {
    hostname = '';
  }
  if (!hostname) {
    throw httpError(400, 'Security key sign-in requires a valid request origin.', 'INVALID_ORIGIN');
  }
  return { origin, rpId: hostname };
}

function toCredentialPayload(row) {
  return {
    id: row.id,
    label: row.label,
    createdAt: row.created_at || null,
    lastUsedAt: row.last_used_at || null,
    transports: parseTransports(row),
    backedUp: row.backed_up === 1,
    rpId: row.rp_id,
  };
}

function listCredentials(userId) {
  return db.prepare(`
    SELECT id, label, rp_id, transports_json, backed_up, created_at, last_used_at
    FROM user_webauthn_credentials
    WHERE user_id = ?
    ORDER BY created_at DESC, id DESC
  `).all(userId).map(toCredentialPayload);
}

function getCredentialsForRelyingParty(userId, rpId) {
  return db.prepare(`
    SELECT credential_id, transports_json FROM user_webauthn_credentials WHERE user_id = ? AND rp_id = ?
  `).all(userId, rpId);
}

function newChallenge(options, rpId) {
  return { challenge: options.challenge, rpId, expiresAt: Date.now() + CHALLENGE_TTL_MS };
}

function readChallenge(pending, rpId) {
  if (!pending?.challenge || Date.now() > Number(pending.expiresAt || 0)) {
    throw httpError(400, 'Security key challenge expired, start again.', 'SECURITY_KEY_CHALLENGE_EXPIRED');
  }
  if (pending.rpId !== rpId) {
    throw httpError(400, 'Security key challenge was started on a different address.', 'SECURITY_KEY_CHALLENGE_EXPIRED');
  }
  return pending;
}

async function beginRegistration({ req, userId }) {
  const { rpId } = resolveRelyingParty(req);
  const user = db.prepare('SELECT id, username, display_name, email FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  if (listCredentials(userId).length >= MAX_CREDENTIALS_PER_USER) {
    throw httpError(400, `You can register at most ${MAX_CREDENTIALS_PER_USER} security keys.`, 'SECURITY_KEY_LIMIT');
  }

  const options = await generateRegistrationOptions({
    rpName: RP_NAME,
    rpID: rpId,
    userID: Buffer.from(String(user.id), 'utf8'),
    userName: user.email || user.username,
    userDisplayName: user.display_name || user.username,
    attestationType: 'none',
    timeout: CEREMONY_TIMEOUT_MS,
    excludeCredentials: getCredentialsForRelyingParty(userId, rpId).map((row) => ({
      id: row.credential_id,
      transports: parseTransports(row),
    })),
    authenticatorSelection: { residentKey: 'preferred', userVerification: 'preferred' },
  });
  return { options, challenge: newChallenge(options, rpId) };
}

async function completeRegistration({ req, userId, response, label, pending }) {
  const { origin, rpId } = resolveRelyingParty(req);
  const challenge = readChallenge(pending, rpId);

  let verification;
  try {
    verification = await verifyRegistrationResponse({
      response,
      expectedChallenge: challenge.challenge,
      expectedOrigin: origin,
      expectedRPID: rpId,
      requireUserVerification: false,
    });
  } catch {
    verification = null;
  }
  if (!verification?.verified || !verification.registrationInfo) {
    throw httpError(400, 'Security key could not be verified.', 'SECURITY_KEY_REJECTED');
  }

  const { credential, credentialDeviceType, credentialBackedUp, userVerified } = verification.registrationInfo;
  if (db.prepare('SELECT 1 FROM user_webauthn_credentials WHERE credential_id = ?').get(credential.id)) {
    throw httpError(409, 'That security key is already registered.', 'SECURITY_KEY_EXISTS');
  }
  const existingCount = listCredentials(userId).length;
  if (existingCount >= MAX_CREDENTIALS_PER_USER) {
    throw httpError(400, `You can register at most ${MAX_CREDENTIALS_PER_USER} security keys.`, 'SECURITY_KEY_LIMIT');
  }
  db.prepare(`
    INSERT INTO user_webauthn_credentials
      (user_id, credential_id, public_key, counter, rp_id, transports_json, device_type, backed_up, user_verified, label)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).run(
    userId,
    credential.id,
    toBase64Url(credential.publicKey),
    Number(credential.counter || 0),
    rpId,
    JSON.stringify(credential.transports || []),
    credentialDeviceType || null,
    credentialBackedUp ? 1 : 0,
    userVerified ? 1 : 0,
    normalizeLabel(label, `Security key ${existingCount + 1}`),
  );
  return { success: true, credentials: listCredentials(userId) };
}

async function beginLogin({ req, username }) {
  const { rpId } = resolveRelyingParty(req);
  const account = String(username || '').trim();

  // With nothing registered anywhere the browser would sit on its key prompt
  // until the ceremony times out. This count covers the whole relying party,
  // so refusing early says nothing about any particular account.
  const { count } = db.prepare('SELECT COUNT(*) AS count FROM user_webauthn_credentials WHERE rp_id = ?').get(rpId);
  if (count === 0) {
    throw httpError(
      409,
      'No security keys are registered for this server yet. Add one under Settings › Security first.',
      'NO_SECURITY_KEYS',
    );
  }

  // Without a username the browser picks a discoverable credential itself, so
  // the allow-list stays empty and no account existence is leaked.
  let allowCredentials = [];
  if (account) {
    const user = findUserForSignIn(account);
    if (user) {
      allowCredentials = getCredentialsForRelyingParty(user.id, rpId).map((row) => ({
        id: row.credential_id,
        transports: parseTransports(row),
      }));
    }
  }

  const options = await generateAuthenticationOptions({
    rpID: rpId,
    timeout: CEREMONY_TIMEOUT_MS,
    userVerification: 'preferred',
    allowCredentials,
  });
  return { options, challenge: newChallenge(options, rpId) };
}

function unknownKey() {
  return httpError(401, 'That security key is not registered.', 'SECURITY_KEY_UNKNOWN');
}

/**
 * Verifies a sign-in assertion. Counts against the account's sign-in
 * throttle like a password try, and stores the authenticator's new
 * signature counter; a counter that went backwards (a cloned key) fails.
 */
async function completeLogin({ req, response, pending }) {
  const { origin, rpId } = resolveRelyingParty(req);
  const challenge = readChallenge(pending, rpId);

  const credentialId = String(response?.id || '').trim();
  if (!credentialId) {
    throw httpError(400, 'Security key response is incomplete.', 'MISSING_FIELDS');
  }
  const stored = db.prepare(`
    SELECT id, user_id, credential_id, public_key, counter, transports_json
    FROM user_webauthn_credentials
    WHERE credential_id = ? AND rp_id = ?
  `).get(credentialId, rpId);
  if (!stored) throw unknownKey();

  const userHandle = response?.response?.userHandle;
  if (userHandle && Buffer.from(fromBase64Url(userHandle)).toString('utf8') !== String(stored.user_id)) {
    throw unknownKey();
  }

  const key = throttle.accountKey({ id: stored.user_id });
  throttle.assertSignInAllowed(key);
  throttle.recordFailure(key, { kind: 'password' });

  let verification;
  try {
    verification = await verifyAuthenticationResponse({
      response,
      expectedChallenge: challenge.challenge,
      expectedOrigin: origin,
      expectedRPID: rpId,
      requireUserVerification: false,
      credential: {
        id: stored.credential_id,
        publicKey: fromBase64Url(stored.public_key),
        counter: Number(stored.counter || 0),
        transports: parseTransports(stored),
      },
    });
  } catch {
    verification = null;
  }
  if (!verification?.verified) {
    throw httpError(401, 'Security key could not be verified.', 'SECURITY_KEY_REJECTED');
  }
  throttle.clearFailures(key, 'password');

  db.prepare(`
    UPDATE user_webauthn_credentials
    SET counter = ?, user_verified = ?, last_used_at = datetime('now')
    WHERE id = ?
  `).run(
    Number(verification.authenticationInfo.newCounter || 0),
    verification.authenticationInfo.userVerified ? 1 : 0,
    stored.id,
  );

  return {
    userId: stored.user_id,
    // A key that verified the user (PIN or biometric) is already two factors:
    // something you have plus something you know or are.
    userVerified: verification.authenticationInfo.userVerified === true,
  };
}

function renameCredential(userId, credentialRowId, label) {
  const nextLabel = normalizeLabel(label, '');
  if (!nextLabel) throw httpError(400, 'Security key name is required.', 'MISSING_FIELDS');
  const result = db.prepare('UPDATE user_webauthn_credentials SET label = ? WHERE id = ? AND user_id = ?')
    .run(nextLabel, credentialRowId, userId);
  if (result.changes === 0) throw httpError(404, 'Security key not found.', 'NOT_FOUND');
  return { success: true, credentials: listCredentials(userId) };
}

function deleteCredential(userId, credentialRowId) {
  const result = db.prepare('DELETE FROM user_webauthn_credentials WHERE id = ? AND user_id = ?')
    .run(credentialRowId, userId);
  if (result.changes === 0) throw httpError(404, 'Security key not found.', 'NOT_FOUND');
  return { success: true, credentials: listCredentials(userId) };
}

module.exports = {
  beginLogin,
  beginRegistration,
  completeLogin,
  completeRegistration,
  deleteCredential,
  listCredentials,
  renameCredential,
};
