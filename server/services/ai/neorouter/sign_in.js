'use strict';

const crypto = require('crypto');
const os = require('os');
const { fetchResponseText } = require('../http');
const { createServiceLogger } = require('../../../utils/logger');
const { sanitizeError } = require('../../../utils/security');
const { httpError } = require('../../../utils/http_error');
const { persistEnv } = require('../../admin/env_config');
const { setNeoRouterRoles, setProviderConfig, setProviderSecret } = require('../settings');
const { ENV, PROVIDER_ID, ROLES, apiBaseFor, normalizeRoles } = require('./connection');

// "Connect NeoRouter": NeoRouter's device sign-in (OAuth 2.0 device flow,
// RFC 8628). The person approves NeoCloud in NeoRouter, choosing what it may
// use, while this server polls for the key. No callback reaches NeoCloud, so
// it works the same for a NeoRouter on the home network as for a public one.
// scope 'server' stores the connection for everyone (admins only); 'user'
// stores it as the person's own key.
const CLIENT_ID = 'neocloud';
const FLOW_TTL_MS = 10 * 60 * 1000;
const SCOPES = 'chat embedding transcription image';

const logger = createServiceLogger('NeoRouterSignIn');
// id -> { scope, userId, apiBase, deviceCode, intervalMs, expiresAt, status, error }
const flows = new Map();

function pruneExpired(now = Date.now()) {
  for (const [id, flow] of flows) {
    if (flow.expiresAt <= now && flow.status === 'pending') flows.delete(id);
  }
}

async function postForm(url, fields, { trustedNetwork }) {
  const { response, text } = await fetchResponseText(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
    body: new URLSearchParams(fields).toString(),
    maxResponseBytes: 64 * 1024,
    serviceName: 'NeoRouter sign-in',
    timeoutMs: 15000,
    trustedNetwork,
  });
  let data = null;
  try {
    data = JSON.parse(text || 'null');
  } catch {
    data = null;
  }
  return { ok: response.ok, status: response.status, data };
}

function store(flow, apiKey) {
  if (flow.scope === 'server') {
    persistEnv(ENV.baseUrl, flow.apiBase);
    persistEnv(ENV.apiKey, apiKey);
    persistEnv(ENV.roles, ROLES.join(','));
    return;
  }
  setProviderConfig(flow.userId, PROVIDER_ID, { baseUrl: flow.apiBase });
  setProviderSecret(flow.userId, PROVIDER_ID, apiKey);
  setNeoRouterRoles(flow.userId, [...ROLES]);
}

function schedule(id, delayMs) {
  setTimeout(() => poll(id).catch((error) => logger.warn(`Polling failed: ${sanitizeError(error)}`)), delayMs).unref?.();
}

async function poll(id) {
  const flow = flows.get(id);
  if (!flow || flow.status !== 'pending') return;
  if (Date.now() >= flow.expiresAt) {
    flow.status = 'failed';
    flow.error = 'The sign-in expired. Start again.';
    return;
  }
  const { ok, data } = await postForm(`${new URL(flow.apiBase).origin}/oauth/token`, {
    grant_type: 'urn:ietf:params:oauth:grant-type:device_code',
    device_code: flow.deviceCode,
    client_id: CLIENT_ID,
  }, { trustedNetwork: flow.scope === 'server' }).catch((error) => ({ ok: false, data: { error: 'network', error_description: error.message } }));
  if (ok && data?.access_token) {
    try {
      store(flow, String(data.access_token));
      flow.status = 'completed';
    } catch (error) {
      flow.status = 'failed';
      flow.error = sanitizeError(error);
    }
    return;
  }
  const code = data?.error;
  if (code === 'slow_down') flow.intervalMs += 5000;
  if (code === 'authorization_pending' || code === 'slow_down' || code === 'network') {
    schedule(id, flow.intervalMs);
    return;
  }
  flow.status = 'failed';
  flow.error = code === 'access_denied' ? 'The connection was declined in NeoRouter.' : (data?.error_description || 'NeoRouter did not hand out a key.');
}

/**
 * Starts connecting. Returns the link that opens NeoRouter's approval page
 * and the code it shows, for the person to compare.
 */
async function begin({ scope, userId, address }) {
  pruneExpired();
  const apiBase = apiBaseFor(address);
  if (!apiBase) throw httpError(400, 'Enter the address of your NeoRouter, for example https://router.example.com.', 'INVALID_ADDRESS');
  const origin = new URL(apiBase).origin;
  let started;
  try {
    started = await postForm(`${origin}/oauth/device/code`, {
      client_id: CLIENT_ID,
      client_name: `NeoCloud (${os.hostname()})`,
      scope: SCOPES,
    }, { trustedNetwork: scope === 'server' });
  } catch {
    throw httpError(400, `Could not reach NeoRouter at ${origin}.`, 'NEOROUTER_UNREACHABLE');
  }
  if (!started.ok || !started.data?.device_code) {
    throw httpError(400, `${origin} did not answer like a NeoRouter. Check the address.`, 'NEOROUTER_UNREACHABLE');
  }
  const id = crypto.randomBytes(18).toString('hex');
  const intervalMs = Math.max(1, Number(started.data.interval) || 5) * 1000;
  flows.set(id, {
    scope,
    userId,
    apiBase,
    deviceCode: String(started.data.device_code),
    intervalMs,
    expiresAt: Date.now() + Math.min(FLOW_TTL_MS, (Number(started.data.expires_in) || 600) * 1000),
    status: 'pending',
    error: null,
  });
  schedule(id, intervalMs);
  return {
    id,
    url: String(started.data.verification_uri_complete || started.data.verification_uri),
    userCode: String(started.data.user_code || ''),
  };
}

// Polled by the app. Only the person who started it may read it; a finished
// result is handed out once.
function status(id, userId) {
  pruneExpired();
  const flow = flows.get(String(id || ''));
  if (!flow || flow.userId !== userId) return null;
  if (flow.status !== 'pending') flows.delete(id);
  return { status: flow.status, error: flow.error };
}

function disconnect({ scope, userId }) {
  if (scope === 'server') {
    for (const key of Object.values(ENV)) persistEnv(key, '');
    return;
  }
  setProviderSecret(userId, PROVIDER_ID, '');
  setProviderConfig(userId, PROVIDER_ID, { baseUrl: '' });
}

function updateRoles({ scope, userId, roles }) {
  const list = normalizeRoles(roles);
  // An empty list is stored as "none": an unset variable means every role.
  if (scope === 'server') persistEnv(ENV.roles, list.join(',') || 'none');
  else setNeoRouterRoles(userId, list);
  return list;
}

module.exports = { begin, disconnect, status, updateRoles };
