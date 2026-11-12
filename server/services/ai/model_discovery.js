'use strict';

const crypto = require('crypto');
const { readCatalogFacts, recordReferenceModels } = require('./model_reference');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('AiModels');

const REFRESH_INTERVAL_MS = 5 * 60 * 1000;
const PERMANENT_ERROR_BACKOFF_MS = 30 * 60 * 1000;
const DISCOVERY_TIMEOUT_MS = 10_000;
const KINDS = new Set(['chat', 'embedding', 'transcription']);

// cache key -> { models, expiresAt }; one shared refresh per key at a time.
const providerModelCache = new Map();
const providerRefreshes = new Map();

const PROVIDER_LABELS = Object.freeze({
  openai: (model) => `${model.id} (OpenAI)`,
  'openai-compatible': (model) => `${model.name || model.id} (Custom OpenAI-compatible)`,
  anthropic: (model) => `${model.name || model.id} (Anthropic)`,
  google: (model) => `${model.name || model.id} (Google)`,
  openrouter: (model) => `${model.name || model.id} (OpenRouter)`,
  ollama: (model) => `${model.id} (Ollama / Local)`,
});

function cacheKeyForProvider(providerId, apiKey, baseUrl) {
  return crypto
    .createHash('sha256')
    .update(String(providerId || ''))
    .update('\0')
    .update(String(apiKey || ''))
    .update('\0')
    .update(String(baseUrl || ''))
    .digest('hex');
}

function abortError(signal) {
  if (signal?.reason instanceof Error) return signal.reason;
  const error = new Error(String(signal?.reason || 'Model discovery aborted.'));
  error.name = 'AbortError';
  error.code = 'ABORT_ERR';
  return error;
}

function waitForSharedResult(promise, signal) {
  if (!signal) return promise;
  if (signal.aborted) return Promise.reject(abortError(signal));
  return new Promise((resolve, reject) => {
    const onAbort = () => reject(abortError(signal));
    signal.addEventListener('abort', onAbort, { once: true });
    promise.then(resolve, reject).finally(() => {
      signal.removeEventListener('abort', onAbort);
    });
  });
}

async function runDiscovery(factory, timeoutMs = DISCOVERY_TIMEOUT_MS) {
  const controller = new AbortController();
  let timer = null;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => {
      const error = new Error(`Model discovery timed out after ${timeoutMs}ms.`);
      error.code = 'MODEL_DISCOVERY_TIMEOUT';
      controller.abort(error);
      reject(error);
    }, timeoutMs);
  });
  try {
    return await Promise.race([
      Promise.resolve().then(() => factory(controller.signal)),
      timeout,
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function normalizeRawModels(rawModels, providerId) {
  const source = Array.isArray(rawModels) ? rawModels : [];
  const labelModel = PROVIDER_LABELS[providerId] || ((model) => model.name || model.id);
  const normalized = [];
  const seen = new Set();

  for (const raw of source) {
    const model = typeof raw === 'string' ? { id: raw, name: raw } : raw;
    const id = String(model?.id || '').trim();
    const kind = KINDS.has(model?.kind) ? model.kind : 'chat';
    // NeoRouter's "auto" is one id in several kinds.
    if (!id || seen.has(`${kind}\0${id}`)) continue;
    seen.add(`${kind}\0${id}`);
    const facts = readCatalogFacts(model);
    normalized.push({
      id,
      label: labelModel({ ...model, id }),
      provider: providerId,
      kind,
      createdAt: facts.createdAt,
      supportsTools: facts.supportsTools,
      supportsVision: facts.supportsVision,
      // A provider that knows its own prices (NeoRouter) keeps its tier.
      ...(model.priceTier ? { priceTier: model.priceTier } : {}),
    });
  }
  return normalized;
}

function isPermanentDiscoveryError(error) {
  return /401|403|unauthorized|forbidden|credits|spending/i.test(String(error?.message || ''));
}

async function loadProviderModels({ providerId, factory, apiKey, baseUrl, existing }) {
  try {
    const config = {};
    if (factory.apiKey) config.apiKey = apiKey;
    if (factory.baseUrl) config.baseUrl = baseUrl;
    const provider = new factory.Provider(config);
    const rawModels = await runDiscovery((signal) => provider.listModels(signal));
    if (providerId === 'openrouter') recordReferenceModels(rawModels);
    return { models: normalizeRawModels(rawModels, providerId), expiresAt: Date.now() + REFRESH_INTERVAL_MS };
  } catch (error) {
    logger.warn(`Failed to refresh ${providerId} models: ${error.message}`);
    const retryAfterMs = isPermanentDiscoveryError(error) ? PERMANENT_ERROR_BACKOFF_MS : REFRESH_INTERVAL_MS;
    return { models: existing?.models || [], expiresAt: Date.now() + retryAfterMs };
  }
}

/** A provider's models (every kind), cached per key and address for 5 minutes. */
async function refreshProviderModelList({ providerId, factory, apiKey, baseUrl, signal }) {
  const cacheKey = cacheKeyForProvider(providerId, apiKey, baseUrl);
  const existing = providerModelCache.get(cacheKey);
  if (existing && existing.expiresAt > Date.now()) return existing.models;

  let refresh = providerRefreshes.get(cacheKey);
  if (!refresh) {
    refresh = loadProviderModels({ providerId, factory, apiKey, baseUrl, existing })
      .then((entry) => {
        providerModelCache.set(cacheKey, entry);
        return entry.models;
      })
      .finally(() => providerRefreshes.delete(cacheKey));
    providerRefreshes.set(cacheKey, refresh);
  }
  return waitForSharedResult(refresh, signal);
}

module.exports = { refreshProviderModelList };
