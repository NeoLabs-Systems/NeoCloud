'use strict';

// Connected apps: NeoAgent's companion OAuth (authorization code + PKCE S256,
// public client). Modelled on NeoRecall's oauth_service. One client per
// NeoAgent install (its callback URL); codes and tokens are stored as SHA-256
// only. An access token lives one hour; a refresh token 90 days and is spent
// on use, replaced by a new one (rotation). What the user granted on the
// consent page is stored with the code and copied onto every token.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

const SCOPES = Object.freeze(['files:read', 'files:write', 'signing:read', 'signing:write']);
const COMPANION_KIND = 'companion:neoagent';
const CALLBACK_PATH = '/api/integrations/oauth/callback';
const ACCESS_TTL_SECONDS = 60 * 60;
const REFRESH_TTL_SECONDS = 90 * 24 * 60 * 60;
const CODE_TTL_SECONDS = 10 * 60;
const PREFIX = { client: 'ncoc_', code: 'ncc_', access: 'nca_', refresh: 'ncr_' };

const sha256 = (value) => crypto.createHash('sha256').update(String(value || '')).digest('hex');
const randomToken = (prefix) => `${prefix}${crypto.randomBytes(32).toString('base64url')}`;
const expiresAt = (seconds) => new Date(Date.now() + seconds * 1000).toISOString();
const nowIso = () => new Date().toISOString();

function invalidRequest(message) {
  return httpError(400, message, 'INVALID_REQUEST');
}

function invalidGrant(message) {
  return httpError(400, message, 'INVALID_GRANT');
}

function parseScopes(value) {
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed.filter((scope) => SCOPES.includes(scope)) : [];
  } catch {
    return [];
  }
}

/** The scopes a request asks for (space separated); all of them when none are named. */
function requestedScopes(value) {
  const names = [...new Set(String(value || '').trim().split(/\s+/).filter(Boolean))];
  if (names.length === 0) return SCOPES.slice();
  if (names.some((scope) => !SCOPES.includes(scope))) throw invalidRequest('Unknown scope.');
  return SCOPES.filter((scope) => names.includes(scope));
}

function isPrivateOrLoopbackHost(hostname) {
  const host = String(hostname || '').toLowerCase().replace(/^\[|\]$/g, '');
  return host === 'localhost' || host === '::1' || host.endsWith('.localhost') || host.startsWith('127.')
    || host.startsWith('10.') || host.startsWith('192.168.') || /^172\.(1[6-9]|2\d|3[01])\./.test(host)
    || /^f[cd][0-9a-f]{2}:/.test(host);
}

/** A NeoAgent callback: https, or http on a loopback or private address; path /api/integrations/oauth/callback. */
function canonicalRedirectUri(value) {
  let parsed;
  try {
    parsed = new URL(String(value || '').trim());
  } catch {
    throw invalidRequest('redirectUri must be a URL.');
  }
  const transportOk = parsed.protocol === 'https:' || (parsed.protocol === 'http:' && isPrivateOrLoopbackHost(parsed.hostname));
  if (!transportOk || parsed.hash || parsed.search || parsed.username || parsed.password
    || !parsed.pathname.endsWith(CALLBACK_PATH)) {
    throw invalidRequest(`redirectUri must be an https NeoAgent callback ending in ${CALLBACK_PATH} (http only on a local network).`);
  }
  return parsed.toString();
}

function activeClient(clientId) {
  const client = db.prepare('SELECT * FROM oauth_clients WHERE id = ? AND revoked_at IS NULL').get(String(clientId || ''));
  if (!client) throw httpError(400, 'Unknown client.', 'INVALID_CLIENT');
  return client;
}

/** The NeoAgent client for a callback URL; the same one comes back for the same URL. */
function bootstrapCompanion({ redirectUri, appName }) {
  if (appName !== undefined && appName !== null && String(appName).trim() !== 'NeoAgent') {
    throw invalidRequest('Only NeoAgent can connect this way.');
  }
  const uri = canonicalRedirectUri(redirectUri);
  return db.transaction(() => {
    const existing = db.prepare(`
      SELECT * FROM oauth_clients WHERE kind = ? AND redirect_uri = ? AND revoked_at IS NULL ORDER BY created_at LIMIT 1
    `).get(COMPANION_KIND, uri);
    if (existing) return { client: existing, created: false };
    const id = `${PREFIX.client}${crypto.randomBytes(18).toString('base64url')}`;
    db.prepare('INSERT INTO oauth_clients (id, kind, name, redirect_uri) VALUES (?, ?, ?, ?)').run(id, COMPANION_KIND, 'NeoAgent', uri);
    return { client: activeClient(id), created: true };
  })();
}

/** Checks an authorization request; returns { client, redirectUri, state, codeChallenge, scopes }. */
function validateAuthorizationRequest(params = {}) {
  if (String(params.response_type || '') !== 'code') throw invalidRequest('response_type must be code.');
  const client = activeClient(params.client_id);
  const redirectUri = String(params.redirect_uri || '');
  if (redirectUri !== client.redirect_uri) throw invalidRequest('redirect_uri does not match this client.');
  const state = String(params.state || '');
  const codeChallenge = String(params.code_challenge || '');
  if (state.length < 8 || state.length > 512 || !/^[A-Za-z0-9._~-]+$/.test(state)) throw invalidRequest('state is missing or malformed.');
  if (!/^[A-Za-z0-9_-]{43}$/.test(codeChallenge) || String(params.code_challenge_method || '') !== 'S256') {
    throw invalidRequest('PKCE with code_challenge_method=S256 is required.');
  }
  return { client, redirectUri, state, codeChallenge, scopes: requestedScopes(params.scope) };
}

/**
 * What the consent page grants out of `requested`: files read only or read &
 * write (`files`: 'read' | 'write'), and signatures (`signing`: true grants
 * signing:read + signing:write, false leaves signing:read when it was asked for).
 */
function grantedScopes(requested, { files, signing }) {
  const granted = new Set();
  const wantsFiles = requested.includes('files:read') || requested.includes('files:write');
  if (wantsFiles) granted.add('files:read');
  if (files === 'write' && requested.includes('files:write')) granted.add('files:write');
  if (requested.includes('signing:read')) granted.add('signing:read');
  if (signing && requested.includes('signing:write')) {
    granted.add('signing:read');
    granted.add('signing:write');
  }
  return SCOPES.filter((scope) => granted.has(scope));
}

function createAuthorizationCode({ clientId, userId, redirectUri, scopes, codeChallenge }) {
  if (scopes.length === 0) throw invalidRequest('Nothing was granted.');
  const code = randomToken(PREFIX.code);
  db.prepare(`
    INSERT INTO oauth_codes (client_id, user_id, code_hash, redirect_uri, scopes_json, code_challenge, expires_at)
    VALUES (?, ?, ?, ?, ?, ?, ?)
  `).run(clientId, userId, sha256(code), redirectUri, JSON.stringify(scopes), codeChallenge, expiresAt(CODE_TTL_SECONDS));
  return code;
}

function verifyPkce(verifier, challenge) {
  const value = String(verifier || '');
  if (value.length < 43 || value.length > 128) return false;
  const actual = Buffer.from(crypto.createHash('sha256').update(value).digest('base64url'));
  const expected = Buffer.from(String(challenge));
  return actual.length === expected.length && crypto.timingSafeEqual(actual, expected);
}

function issueTokens({ clientId, userId, scopes }) {
  const accessToken = randomToken(PREFIX.access);
  const refreshToken = randomToken(PREFIX.refresh);
  const access = db.prepare(`
    INSERT INTO oauth_access_tokens (client_id, user_id, token_hash, scopes_json, expires_at) VALUES (?, ?, ?, ?, ?)
  `).run(clientId, userId, sha256(accessToken), JSON.stringify(scopes), expiresAt(ACCESS_TTL_SECONDS));
  db.prepare(`
    INSERT INTO oauth_refresh_tokens (access_token_id, client_id, user_id, token_hash, scopes_json, expires_at)
    VALUES (?, ?, ?, ?, ?, ?)
  `).run(access.lastInsertRowid, clientId, userId, sha256(refreshToken), JSON.stringify(scopes), expiresAt(REFRESH_TTL_SECONDS));
  return { accessToken, refreshToken, expiresIn: ACCESS_TTL_SECONDS, scope: scopes.join(' ') };
}

function assertUserActive(userId) {
  if (!db.prepare('SELECT 1 FROM users WHERE id = ? AND disabled_at IS NULL').get(userId)) throw invalidGrant('This account cannot be used.');
}

function exchangeAuthorizationCode({ clientId, code, redirectUri, codeVerifier }) {
  return db.transaction(() => {
    const client = activeClient(clientId);
    const row = db.prepare(`
      SELECT * FROM oauth_codes WHERE code_hash = ? AND client_id = ? AND consumed_at IS NULL AND expires_at > ?
    `).get(sha256(code), client.id, nowIso());
    if (!row || row.redirect_uri !== String(redirectUri || '') || !verifyPkce(codeVerifier, row.code_challenge)) {
      throw invalidGrant('The authorization code is invalid or expired.');
    }
    db.prepare('UPDATE oauth_codes SET consumed_at = ? WHERE id = ?').run(nowIso(), row.id);
    assertUserActive(row.user_id);
    return issueTokens({ clientId: client.id, userId: row.user_id, scopes: parseScopes(row.scopes_json) });
  })();
}

function refreshTokens({ clientId, refreshToken }) {
  return db.transaction(() => {
    const client = activeClient(clientId);
    const row = db.prepare(`
      SELECT * FROM oauth_refresh_tokens WHERE token_hash = ? AND client_id = ? AND revoked_at IS NULL AND expires_at > ?
    `).get(sha256(refreshToken), client.id, nowIso());
    if (!row) throw invalidGrant('The refresh token is invalid or expired.');
    assertUserActive(row.user_id);
    const at = nowIso();
    db.prepare('UPDATE oauth_refresh_tokens SET revoked_at = ? WHERE id = ?').run(at, row.id);
    db.prepare('UPDATE oauth_access_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL').run(at, row.access_token_id);
    return issueTokens({ clientId: client.id, userId: row.user_id, scopes: parseScopes(row.scopes_json) });
  })();
}

/** Revokes an access or refresh token of the client (RFC 7009: unknown tokens are no error). */
function revokeToken({ clientId, token }) {
  const client = activeClient(clientId);
  const hash = sha256(token);
  const at = nowIso();
  db.transaction(() => {
    const refresh = db.prepare('SELECT access_token_id FROM oauth_refresh_tokens WHERE client_id = ? AND token_hash = ?').get(client.id, hash);
    db.prepare('UPDATE oauth_refresh_tokens SET revoked_at = ? WHERE client_id = ? AND token_hash = ? AND revoked_at IS NULL').run(at, client.id, hash);
    db.prepare('UPDATE oauth_access_tokens SET revoked_at = ? WHERE client_id = ? AND token_hash = ? AND revoked_at IS NULL').run(at, client.id, hash);
    if (refresh?.access_token_id) {
      db.prepare('UPDATE oauth_access_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL').run(at, refresh.access_token_id);
    }
  })();
}

/** The account and granted scopes of a live access token, or null. A disabled account's tokens resolve to nothing. */
function resolveAccessToken(token) {
  const value = String(token || '');
  if (!value.startsWith(PREFIX.access)) return null;
  const row = db.prepare(`
    SELECT t.id, t.user_id, t.client_id, t.scopes_json, t.last_used_at, u.username, u.email
    FROM oauth_access_tokens t
    JOIN users u ON u.id = t.user_id
    JOIN oauth_clients c ON c.id = t.client_id
    WHERE t.token_hash = ? AND t.revoked_at IS NULL AND t.expires_at > ? AND c.revoked_at IS NULL AND u.disabled_at IS NULL
  `).get(sha256(value), nowIso());
  if (!row) return null;
  if (!row.last_used_at || Date.parse(row.last_used_at) < Date.now() - 60 * 1000) {
    db.prepare('UPDATE oauth_access_tokens SET last_used_at = ? WHERE id = ?').run(nowIso(), row.id);
  }
  return {
    userId: row.user_id,
    clientId: row.client_id,
    scopes: parseScopes(row.scopes_json),
    username: row.username,
    email: row.email,
  };
}

/** The apps holding a live grant on the account, for settings. */
function listConnectedApps(userId) {
  const now = nowIso();
  return db.prepare(`
    SELECT c.id, c.name, c.redirect_uri,
      (SELECT MIN(r.created_at) FROM oauth_refresh_tokens r WHERE r.client_id = c.id AND r.user_id = @userId) AS connected_at,
      (SELECT MAX(t.last_used_at) FROM oauth_access_tokens t WHERE t.client_id = c.id AND t.user_id = @userId) AS last_used_at,
      (SELECT r.scopes_json FROM oauth_refresh_tokens r
        WHERE r.client_id = c.id AND r.user_id = @userId AND r.revoked_at IS NULL AND r.expires_at > @now
        ORDER BY r.id DESC LIMIT 1) AS scopes_json
    FROM oauth_clients c
    WHERE c.revoked_at IS NULL AND EXISTS (
      SELECT 1 FROM oauth_refresh_tokens r
      WHERE r.client_id = c.id AND r.user_id = @userId AND r.revoked_at IS NULL AND r.expires_at > @now
    )
    ORDER BY connected_at
  `).all({ userId, now }).map((row) => ({
    id: row.id,
    name: row.name,
    // Where the app runs, so two NeoAgent installs can be told apart.
    host: new URL(row.redirect_uri).host,
    scopes: parseScopes(row.scopes_json),
    connectedAt: row.connected_at,
    lastUsedAt: row.last_used_at,
  }));
}

function revokeGrants(userId, clientId = null) {
  const at = nowIso();
  return db.transaction(() => {
    const access = db.prepare(`
      UPDATE oauth_access_tokens SET revoked_at = ? WHERE user_id = ? AND (? IS NULL OR client_id = ?) AND revoked_at IS NULL
    `).run(at, userId, clientId, clientId).changes;
    const refresh = db.prepare(`
      UPDATE oauth_refresh_tokens SET revoked_at = ? WHERE user_id = ? AND (? IS NULL OR client_id = ?) AND revoked_at IS NULL
    `).run(at, userId, clientId, clientId).changes;
    db.prepare(`
      UPDATE oauth_codes SET consumed_at = ? WHERE user_id = ? AND (? IS NULL OR client_id = ?) AND consumed_at IS NULL
    `).run(at, userId, clientId, clientId);
    return access + refresh;
  })();
}

/** Disconnects one app from the account; another account's app id answers 404. */
function disconnectApp(userId, clientId) {
  const live = listConnectedApps(userId).some((app) => app.id === clientId);
  if (!live) throw httpError(404, 'Connected app not found', 'NOT_FOUND');
  revokeGrants(userId, clientId);
  return { success: true };
}

/** Every grant of the account (sign out everywhere, password change, disable). */
function revokeAllGrants(userId) {
  return revokeGrants(userId);
}

module.exports = {
  CALLBACK_PATH,
  SCOPES,
  bootstrapCompanion,
  disconnectApp,
  exchangeAuthorizationCode,
  grantedScopes,
  listConnectedApps,
  refreshTokens,
  resolveAccessToken,
  revokeAllGrants,
  revokeToken,
  validateAuthorizationRequest,
  createAuthorizationCode,
};
