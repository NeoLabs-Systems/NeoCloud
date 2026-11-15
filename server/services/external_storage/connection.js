'use strict';

// Opening a connection to a provider, and testing one before it is saved.
// Each use opens its own connection and closes it again; nothing is kept
// between requests.

const { httpError } = require('../../utils/http_error');
const { providerFor } = require('./config');
const { resolveAllowed } = require('./network');

const MAX_ACTIVE_PER_USER = 8;
const TEST_TIMEOUT_MS = 45 * 1000;
const active = new Map();

/** Counts the account's running provider operations; a few at a time is plenty. */
function enter(userId) {
  const count = active.get(userId) || 0;
  if (count >= MAX_ACTIVE_PER_USER) throw httpError(429, 'Too many transfers at once. Wait for one to finish.', 'RATE_LIMITED');
  active.set(userId, count + 1);
  let left = false;
  return () => {
    if (left) return;
    left = true;
    const remaining = (active.get(userId) || 1) - 1;
    if (remaining <= 0) active.delete(userId);
    else active.set(userId, remaining);
  };
}

/**
 * A connected adapter. `release()` closes it and frees the account's slot;
 * call it when the operation, or the stream it returned, is done.
 */
async function openAdapter(userId, type, config, options = {}) {
  const provider = providerFor(type);
  const leave = enter(userId);
  let adapter;
  const release = async () => {
    leave();
    if (adapter) await adapter.close().catch(() => {});
  };
  try {
    const host = provider.hostOf(config);
    if (host) await resolveAllowed(host, userId);
    adapter = provider.create(config, { userId, ...options });
    await adapter.open();
    return { adapter, release };
  } catch (err) {
    await release();
    throw err;
  }
}

/** Connects with a settings set, lists its root and closes again. Never throws for a refusing server. */
async function testConnection(userId, type, config, options) {
  let timer;
  try {
    const result = await Promise.race([
      (async () => {
        const { adapter, release } = await openAdapter(userId, type, config, options);
        try {
          const { entries } = await adapter.list('', { limit: 50 });
          return { ok: true, items: entries.length, hostKey: adapter.fingerprint || null };
        } finally {
          await release();
        }
      })(),
      new Promise((resolve) => {
        timer = setTimeout(() => resolve({ ok: false, error: 'The server took too long to answer.', code: 'EXTERNAL_UNREACHABLE' }), TEST_TIMEOUT_MS);
      }),
    ]);
    return result;
  } catch (err) {
    if (!err.status) throw err;
    return { ok: false, error: err.message, code: err.code || null };
  } finally {
    clearTimeout(timer);
  }
}

module.exports = { openAdapter, testConnection };
