'use strict';

const crypto = require('crypto');
const { modelList } = require('./client');

// What a NeoRouter can do for each role, from its model lists: how many
// models it has and how many the key may not use. Shown next to the role
// switches.
const ROLE_TYPES = Object.freeze({
  chat: 'chat',
  embeddings: 'embedding',
  transcription: 'transcription',
  images: 'image',
});
const CACHE_MS = 5 * 60 * 1000;
const cache = new Map();

function cacheKey(connection) {
  return crypto.createHash('sha256').update(`${connection.apiBase}\n${connection.apiKey}`).digest('hex');
}

async function load(connection, signal) {
  const roles = {};
  const notAllowed = {};
  const results = await Promise.allSettled(Object.entries(ROLE_TYPES).map(async ([role, type]) => {
    const list = await modelList(connection, type, { signal });
    roles[role] = list.models.filter((model) => !String(model.id).startsWith('auto')).length;
    const hidden = list.notAllowed;
    notAllowed[role] = (hidden.free || 0) + (hidden.plan || 0) + (hidden.paid || 0);
  }));
  const failure = results.find((result) => result.status === 'rejected');
  if (failure && Object.keys(roles).length === 0) throw failure.reason;
  return { roles, notAllowed };
}

/** { roles: { chat: 12, embeddings: 2, … }, notAllowed: { chat: 9, … } } */
async function describeOffer(connection, { signal, fresh = false } = {}) {
  const key = cacheKey(connection);
  const cached = cache.get(key);
  if (!fresh && cached && cached.expiresAt > Date.now()) return cached.offer;
  const offer = await load(connection, signal);
  cache.set(key, { offer, expiresAt: Date.now() + CACHE_MS });
  return offer;
}

module.exports = { describeOffer };
