'use strict';

const { getNeoRouterRoles, getProviderConfigs, getProviderSecrets } = require('../settings');

// A NeoRouter that NeoCloud works through. There are two kinds: the server's,
// set up by an admin and kept in the environment, and a person's own, from
// their sign-in and stored like a BYOK key. A person's own connection wins.
// Each connection lists the kinds of work (roles) that go through it; the
// rest keep their usual providers.
const ROLES = Object.freeze(['chat', 'embeddings', 'transcription', 'images']);
const ENV = Object.freeze({ baseUrl: 'NEOROUTER_BASE_URL', apiKey: 'NEOROUTER_API_KEY', roles: 'NEOROUTER_ROLES' });
const PROVIDER_ID = 'neorouter';

// "router.local:3333", "https://router.example.com/app/" → the /v1 API base.
function apiBaseFor(address) {
  const raw = String(address || '').trim();
  if (!raw) return null;
  try {
    const url = new URL(/^https?:\/\//i.test(raw) ? raw : `https://${raw}`);
    if (url.username || url.password) return null;
    return `${url.origin}/v1`;
  } catch {
    return null;
  }
}

// Unset means every role; a stored list keeps only the known ones.
function normalizeRoles(value) {
  if (value == null) return [...ROLES];
  const list = Array.isArray(value) ? value : String(value).split(',');
  const wanted = new Set(list.map((role) => String(role).trim()));
  return ROLES.filter((role) => wanted.has(role));
}

function build(scope, apiBase, apiKey, roles) {
  if (!apiBase || !apiKey) return null;
  return { scope, apiBase, origin: new URL(apiBase).origin, apiKey, roles: new Set(roles) };
}

function serverConnection() {
  const env = (name) => String(process.env[name] || '').trim();
  return build('server', apiBaseFor(env(ENV.baseUrl)), env(ENV.apiKey), normalizeRoles(process.env[ENV.roles] || null));
}

function userConnection(userId) {
  if (!userId) return null;
  const apiKey = String(getProviderSecrets(userId)[PROVIDER_ID] || '').trim();
  if (!apiKey) return null;
  const apiBase = apiBaseFor(getProviderConfigs(userId)[PROVIDER_ID]?.baseUrl);
  return build('user', apiBase, apiKey, normalizeRoles(getNeoRouterRoles(userId)));
}

function connectionFor(userId) {
  return userConnection(userId) || serverConnection();
}

/** The connection that handles `role` for this account, or null. */
function forRole(userId, role) {
  const connection = connectionFor(userId);
  return connection?.roles.has(role) ? connection : null;
}

module.exports = {
  ENV,
  PROVIDER_ID,
  ROLES,
  apiBaseFor,
  forRole,
  normalizeRoles,
  serverConnection,
  userConnection,
};
