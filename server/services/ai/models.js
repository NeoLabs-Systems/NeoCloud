'use strict';

const { AnthropicProvider } = require('./providers/anthropic');
const { GoogleProvider } = require('./providers/google');
const { OllamaProvider } = require('./providers/ollama');
const { OpenAIProvider } = require('./providers/openai');
const { CustomOpenAIProvider } = require('./providers/openai_compatible_custom');
const { OpenRouterProvider } = require('./providers/openrouter');
const { NeoRouterProvider } = require('./providers/neorouter');
const { AI_PROVIDER_DEFINITIONS } = require('./provider_definitions');
const { getProviderConfigs, getProviderSecrets } = require('./settings');
const { forRole, PROVIDER_ID: NEOROUTER_ID } = require('./neorouter/connection');
const { toSelectableModel } = require('./model_identity');
const { refreshProviderModelList } = require('./model_discovery');
const { lookupModelFacts, priceTierForCost, refreshReferenceCatalog } = require('./model_reference');
const { getDisabledModelIds } = require('./model_visibility');
const { getGrantedModelIds, modelAccess } = require('./model_grants');
const { fetchResponseText } = require('./http');
const { createAbortError, isAbortError, throwIfAborted } = require('../../utils/abort');
const { isPrivateHost } = require('../../utils/cloud_security');

// Maps a provider id to its class and which runtime fields its constructor takes.
const PROVIDER_FACTORIES = Object.freeze({
  openai: { Provider: OpenAIProvider, apiKey: true, baseUrl: true },
  'openai-compatible': { Provider: CustomOpenAIProvider, apiKey: true, baseUrl: true },
  anthropic: { Provider: AnthropicProvider, apiKey: true, baseUrl: true },
  google: { Provider: GoogleProvider, apiKey: true, baseUrl: false },
  openrouter: { Provider: OpenRouterProvider, apiKey: true, baseUrl: true },
  neorouter: { Provider: NeoRouterProvider, apiKey: true, baseUrl: true },
  ollama: { Provider: OllamaProvider, apiKey: false, baseUrl: true },
});

// The NeoRouter role each catalog kind goes through.
const NEOROUTER_ROLE_BY_KIND = Object.freeze({ chat: 'chat', embedding: 'embeddings', transcription: 'transcription' });

function isValidHttpUrl(value) {
  try {
    const url = new URL(value);
    return (url.protocol === 'http:' || url.protocol === 'https:')
      && Boolean(url.hostname)
      && !url.username
      && !url.password;
  } catch {
    return false;
  }
}

// Probing an offline Ollama must not add a full timeout to every catalog
// read, so the outcome is reused briefly.
const OLLAMA_PROBE_TTL_MS = 30 * 1000;
const ollamaProbeCache = new Map();

async function probeOllama(baseUrl, signal) {
  const cached = ollamaProbeCache.get(baseUrl);
  if (cached && cached.expiresAt > Date.now()) return cached.probe;
  let probe;
  try {
    const { response, text } = await fetchResponseText(`${baseUrl}/api/tags`, {
      maxResponseBytes: 2 * 1024 * 1024,
      serviceName: 'Ollama health check',
      signal,
      timeoutMs: 1500,
    });
    if (response.ok) {
      let data = {};
      try { data = JSON.parse(text || '{}'); } catch { data = {}; }
      const modelCount = Array.isArray(data?.models) ? data.models.length : 0;
      probe = {
        healthy: true,
        reason: modelCount > 0
          ? `Connected to Ollama with ${modelCount} local model(s).`
          : 'Connected to Ollama, but no local models were reported.',
      };
    } else {
      probe = { healthy: false, reason: `Ollama returned HTTP ${response.status}.` };
    }
  } catch (err) {
    if (isAbortError(err, signal)) throw createAbortError(signal);
    probe = { healthy: false, reason: 'Could not reach Ollama at the configured address.' };
  }
  ollamaProbeCache.set(baseUrl, { probe, expiresAt: Date.now() + OLLAMA_PROBE_TTL_MS });
  return probe;
}

// A person's own base URL (BYOK custom endpoint or Ollama address) must never
// reach the server's loopback/LAN network -- that's an SSRF vector into the
// host's internal services. The operator's own configured address is not
// attacker-supplied, and is exempt (people sign in to the server's NeoRouter
// on the home network).
function isUnsafeUserBaseUrl(url, definition) {
  if (!url || url === definition.defaultBaseUrl) return false;
  const operatorUrl = definition.baseUrlEnvKey ? String(process.env[definition.baseUrlEnvKey] || '').trim() : '';
  if (operatorUrl && url === operatorUrl) return false;
  try {
    return isPrivateHost(new URL(url).hostname);
  } catch {
    return true;
  }
}

function getProviderRuntimeConfig(userId, providerId) {
  const definition = AI_PROVIDER_DEFINITIONS[providerId];
  if (!definition) throw new Error(`Unknown provider: ${providerId}`);

  const config = getProviderConfigs(userId)[providerId] || {};
  const secrets = getProviderSecrets(userId);
  const envApiKey = definition.envKey ? String(process.env[definition.envKey] || '').trim() : '';
  const envBaseUrl = definition.baseUrlEnvKey ? String(process.env[definition.baseUrlEnvKey] || '').trim() : '';
  const scopedApiKey = typeof secrets[providerId] === 'string' ? secrets[providerId].trim() : '';
  const rawConfigBaseUrl = typeof config.baseUrl === 'string' ? config.baseUrl.trim() : '';
  const configBaseUrl = isUnsafeUserBaseUrl(rawConfigBaseUrl, definition) ? '' : rawConfigBaseUrl;
  // A person's own credential always wins over the server's -- that is the
  // point of "bring your own key" -- and its base URL follows the same
  // precedence. A provider with no API key concept (Ollama) is BYOK'd by
  // pointing it at one's own address.
  const isByok = definition.supportsApiKey
    ? Boolean(scopedApiKey)
    : Boolean(definition.supportsBaseUrl && configBaseUrl);
  const apiKey = scopedApiKey || envApiKey;
  // A stored user URL belongs to that user's own key. The server key is sent
  // only to the operator-configured origin.
  let baseUrl = '';
  if (definition.supportsBaseUrl) {
    baseUrl = isByok
      ? (configBaseUrl || envBaseUrl || definition.defaultBaseUrl || '')
      : (envBaseUrl || definition.defaultBaseUrl || '');
  }

  return {
    ...definition,
    apiKey,
    credentialConfigured: Boolean(apiKey),
    baseUrl,
    baseUrlConfigured: Boolean(baseUrl),
    baseUrlValid: !baseUrl || isValidHttpUrl(baseUrl),
    isByok,
    label: typeof config.label === 'string' ? config.label : '',
  };
}

function providerStatus(definition, runtime) {
  if (definition.supportsApiKey && !runtime.apiKey) {
    return ['needs_setup', 'Setup Needed', 'Credentials for this provider are not available on this server yet.'];
  }
  if (definition.requiresBaseUrl && !runtime.baseUrlConfigured) {
    return ['needs_setup', 'Setup Needed', 'An address (base URL) is required for this provider.'];
  }
  if (definition.requiresBaseUrl && !runtime.baseUrlValid) {
    return ['needs_setup', 'Setup Needed', 'The configured base URL must be an HTTP or HTTPS URL without embedded credentials.'];
  }
  if (definition.id === 'ollama') return ['local', 'Local', 'This provider connects to an Ollama server.'];
  return ['configured', 'Configured', 'Credentials for this provider are available.'];
}

/** Every provider and whether this account (null: the server) can call it. */
function getProviderCatalog(userId) {
  return Object.values(AI_PROVIDER_DEFINITIONS).map((definition) => {
    const runtime = getProviderRuntimeConfig(userId, definition.id);
    const hasCredential = !definition.supportsApiKey || Boolean(runtime.apiKey);
    const hasRequiredBaseUrl = !definition.requiresBaseUrl || (runtime.baseUrlConfigured && runtime.baseUrlValid);
    const [status, statusLabel, availabilityReason] = providerStatus(definition, runtime);
    return {
      id: definition.id,
      label: definition.label,
      description: definition.description,
      authentication: definition.authentication,
      supportsApiKey: definition.supportsApiKey,
      supportsBaseUrl: definition.supportsBaseUrl,
      requiresBaseUrl: Boolean(definition.requiresBaseUrl),
      defaultBaseUrl: definition.defaultBaseUrl,
      available: hasCredential && hasRequiredBaseUrl,
      credentialConfigured: runtime.credentialConfigured,
      baseUrlConfigured: runtime.baseUrlConfigured,
      baseUrl: runtime.baseUrl,
      customLabel: runtime.label || '',
      isByok: runtime.isByok,
      status,
      statusLabel,
      availabilityReason,
    };
  });
}

/** getProviderCatalog, with a configured Ollama probed for reachability. */
async function getProviderHealthCatalog(userId, options = {}) {
  const enriched = [];
  for (const provider of getProviderCatalog(userId)) {
    throwIfAborted(options.signal);
    let { available, status, statusLabel, availabilityReason } = provider;
    if (provider.id === 'ollama' && provider.available) {
      const probe = await probeOllama(provider.baseUrl, options.signal);
      available = probe.healthy;
      status = probe.healthy ? 'healthy' : 'offline';
      statusLabel = probe.healthy ? 'Healthy' : 'Offline';
      availabilityReason = probe.reason;
    } else if (provider.available) {
      status = 'healthy';
      statusLabel = 'Healthy';
    }
    enriched.push({ ...provider, available, connected: available, healthy: available, status, statusLabel, availabilityReason });
  }
  return enriched;
}

// A NeoRouter offers each kind of model only when that kind of work goes
// through it (its roles); other providers always offer what they list.
function servedForKind(model, userId) {
  if (model.provider !== NEOROUTER_ID) return true;
  return Boolean(forRole(userId, NEOROUTER_ROLE_BY_KIND[model.kind]));
}

/**
 * Every model this account (null: the server) can see, as `provider::model`
 * ids with kind, abilities and price tier. `available` is false for a model
 * the admin turned off (unless granted to the account) or whose provider is
 * unreachable.
 */
async function getSupportedModels(userId, options = {}) {
  throwIfAborted(options.signal);
  const providerCatalog = options.providerCatalog || await getProviderHealthCatalog(userId, { signal: options.signal });
  const providerById = new Map(providerCatalog.map((provider) => [provider.id, provider]));

  const fetches = Object.keys(PROVIDER_FACTORIES)
    .filter((id) => providerById.get(id)?.available)
    .map((id) => {
      const runtime = getProviderRuntimeConfig(userId, id);
      return refreshProviderModelList({
        providerId: id,
        factory: PROVIDER_FACTORIES[id],
        apiKey: runtime.apiKey,
        baseUrl: runtime.baseUrl,
        signal: options.signal,
      });
    });
  const results = await Promise.allSettled(fetches);
  throwIfAborted(options.signal);
  const all = results
    .filter((result) => result.status === 'fulfilled')
    .flatMap((result) => result.value)
    .filter((model) => servedForKind(model, userId));

  if (all.some((model) => model.provider !== 'ollama')) {
    await refreshReferenceCatalog();
    throwIfAborted(options.signal);
  }

  const disabledIds = getDisabledModelIds();
  const disabledSet = disabledIds.length ? new Set(disabledIds) : null;
  // An admin can grant one account a model that is off for everyone else.
  const grantedIds = disabledSet ? getGrantedModelIds(userId) : [];
  const grantedSet = grantedIds.length ? new Set(grantedIds) : null;

  return all.map((model) => {
    const selectableModel = toSelectableModel(model);
    const provider = providerById.get(model.provider);
    // Local models cost nothing per token; every other model is looked up in
    // the reference catalog by name.
    const local = model.provider === 'ollama';
    const facts = local ? null : lookupModelFacts(model.id);
    const inputCostPerM = local ? 0 : (facts?.inputCostPerM ?? null);
    const priceTier = model.priceTier ?? priceTierForCost(inputCostPerM);
    const { blocked, adminGranted } = modelAccess(selectableModel, disabledSet, grantedSet);
    return {
      ...selectableModel,
      createdAt: facts?.createdAt ?? model.createdAt ?? null,
      supportsTools: model.supportsTools ?? facts?.supportsTools ?? null,
      supportsVision: model.supportsVision ?? facts?.supportsVision ?? null,
      purpose: priceTier === 'cheap' ? 'fast' : 'general',
      priceTier,
      inputCostPerM,
      available: provider?.available !== false && !blocked,
      providerStatus: provider?.status || 'unknown',
      providerStatusLabel: provider?.statusLabel || 'Unknown',
      isByok: Boolean(provider?.isByok),
      byokLabel: provider?.customLabel || '',
      adminGranted,
    };
  });
}

function createProviderInstance(providerId, userId) {
  const factory = PROVIDER_FACTORIES[providerId];
  if (!factory) throw new Error(`Unknown provider: ${providerId}`);
  const runtime = getProviderRuntimeConfig(userId, providerId);
  if (runtime.supportsApiKey && !runtime.apiKey) {
    throw new Error(`Provider '${providerId}' is not configured on this server.`);
  }
  if (runtime.requiresBaseUrl && (!runtime.baseUrl || !runtime.baseUrlValid)) {
    throw new Error(`Provider '${providerId}' requires a valid HTTP or HTTPS base URL.`);
  }
  const config = {};
  if (factory.apiKey) config.apiKey = runtime.apiKey;
  if (factory.baseUrl) config.baseUrl = runtime.baseUrl;
  return new factory.Provider(config);
}

module.exports = {
  PROVIDER_FACTORIES,
  createProviderInstance,
  getProviderHealthCatalog,
  getProviderRuntimeConfig,
  getSupportedModels,
};
