'use strict';

// Bring your own key: a person's own provider credentials. Everything here is
// scoped to the authenticated account; a key is never echoed back once saved.

const { AI_PROVIDER_DEFINITIONS } = require('./provider_definitions');
const { setProviderConfig, setProviderSecret } = require('./settings');
const { PROVIDER_FACTORIES, getProviderHealthCatalog, getProviderRuntimeConfig } = require('./models');
const { validateCloudUrlWithDns } = require('../../utils/cloud_security');
const { httpError } = require('../../utils/http_error');

const TEST_TIMEOUT_MS = 8000;

// NeoRouter is connected with its own sign-in, not a pasted key.
function isByokEligible(definition) {
  return Boolean(definition)
    && definition.authentication !== 'oauth'
    && (definition.supportsApiKey || definition.supportsBaseUrl);
}

function definitionFor(providerId) {
  const definition = AI_PROVIDER_DEFINITIONS[String(providerId || '')];
  if (!isByokEligible(definition)) throw httpError(404, 'Unknown or unsupported provider.', 'UNKNOWN_PROVIDER');
  return definition;
}

function isValidHttpUrl(value) {
  try {
    const url = new URL(value);
    return (url.protocol === 'http:' || url.protocol === 'https:') && Boolean(url.hostname) && !url.username && !url.password;
  } catch {
    return false;
  }
}

// A BYOK base URL is user input the server will call, so it may not resolve
// into the server's own loopback/LAN network -- unless it is the address the
// admin configured for that provider.
async function assertSafeBaseUrl(definition, baseUrl, signal) {
  if (!isValidHttpUrl(baseUrl)) throw httpError(400, 'Base URL must be a valid http(s) URL.', 'INVALID_URL');
  const operatorUrl = definition.baseUrlEnvKey ? String(process.env[definition.baseUrlEnvKey] || '').trim() : '';
  if (operatorUrl && baseUrl === operatorUrl) return;
  const result = await validateCloudUrlWithDns(baseUrl, { signal });
  if (!result.allowed) throw httpError(400, result.reason, 'URL_NOT_ALLOWED');
}

function describe(provider) {
  return {
    id: provider.id,
    label: provider.label,
    description: provider.description,
    supportsApiKey: provider.supportsApiKey,
    supportsBaseUrl: provider.supportsBaseUrl,
    requiresBaseUrl: provider.requiresBaseUrl,
    defaultBaseUrl: provider.defaultBaseUrl,
    isCustomEndpoint: provider.id === 'openai-compatible',
    configured: provider.isByok,
    baseUrl: provider.isByok ? provider.baseUrl : '',
    customLabel: provider.isByok ? (provider.customLabel || '') : '',
  };
}

async function listByok(userId, { signal } = {}) {
  const providers = await getProviderHealthCatalog(userId, { signal });
  return { providers: providers.filter(isByokEligible).map(describe) };
}

async function getByok(userId, providerId, { signal } = {}) {
  const definition = definitionFor(providerId);
  const providers = await getProviderHealthCatalog(userId, { signal });
  return { provider: describe(providers.find((provider) => provider.id === definition.id)) };
}

function readBody(body) {
  return {
    apiKey: typeof body?.apiKey === 'string' ? body.apiKey.trim() : '',
    baseUrl: typeof body?.baseUrl === 'string' ? body.baseUrl.trim() : undefined,
    label: typeof body?.label === 'string' ? body.label.trim() : undefined,
  };
}

// A provider with no API key concept (Ollama) is only ever "yours" by
// pointing it at your own address, so that address is mandatory.
function baseUrlRequired(definition) {
  return definition.requiresBaseUrl || !definition.supportsApiKey;
}

async function saveByok(userId, providerId, body, { signal } = {}) {
  const definition = definitionFor(providerId);
  const { apiKey, baseUrl, label } = readBody(body);
  if (definition.supportsApiKey && !apiKey) throw httpError(400, 'An API key is required.', 'API_KEY_REQUIRED');
  if (baseUrlRequired(definition) && !baseUrl) {
    throw httpError(400, 'A base URL is required for this provider.', 'BASE_URL_REQUIRED');
  }
  if (baseUrl) await assertSafeBaseUrl(definition, baseUrl, signal);
  if (definition.supportsApiKey) setProviderSecret(userId, definition.id, apiKey);
  if (definition.supportsBaseUrl && (baseUrl !== undefined || label !== undefined)) {
    setProviderConfig(userId, definition.id, { baseUrl, label });
  }
  return { success: true, configured: true };
}

function clearByok(userId, providerId) {
  const definition = definitionFor(providerId);
  if (definition.supportsApiKey) setProviderSecret(userId, definition.id, '');
  if (definition.supportsBaseUrl) {
    setProviderConfig(userId, definition.id, { baseUrl: '', ...(definition.supportsApiKey ? {} : { label: '' }) });
  }
  return { success: true, configured: false };
}

/**
 * Tests a key/endpoint the person is about to save (or the saved one, when
 * the body names none) by listing the provider's models. Nothing is stored.
 */
async function testByok(userId, providerId, body, { signal } = {}) {
  const definition = definitionFor(providerId);
  const factory = PROVIDER_FACTORIES[definition.id];
  const supplied = readBody(body);
  if (supplied.baseUrl && !supplied.apiKey && definition.supportsApiKey) {
    throw httpError(400, 'Testing a custom base URL requires the API key for that endpoint.', 'API_KEY_REQUIRED');
  }
  // Without a body the saved setup is tested; a key alone goes to the
  // address the account would use.
  const runtime = getProviderRuntimeConfig(userId, definition.id);
  const apiKey = supplied.apiKey || (supplied.baseUrl ? '' : runtime.apiKey);
  const baseUrl = supplied.baseUrl || runtime.baseUrl;
  if (definition.supportsApiKey && !apiKey) throw httpError(400, 'No API key to test.', 'API_KEY_REQUIRED');
  if (baseUrlRequired(definition) && !baseUrl) throw httpError(400, 'A base URL is required.', 'BASE_URL_REQUIRED');
  if (baseUrl) await assertSafeBaseUrl(definition, baseUrl, signal);

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), TEST_TIMEOUT_MS);
  try {
    const config = {};
    if (factory.apiKey) config.apiKey = apiKey;
    if (factory.baseUrl) config.baseUrl = baseUrl;
    const models = await new factory.Provider(config).listModels(controller.signal);
    return {
      success: true,
      ok: true,
      message: Array.isArray(models) && models.length
        ? `Connected — found ${models.length} model(s).`
        : 'Connected, but no models were returned.',
    };
  } catch (error) {
    return { success: true, ok: false, error: error?.message || 'Connection failed.' };
  } finally {
    clearTimeout(timeout);
  }
}

module.exports = { clearByok, getByok, listByok, saveByok, testByok };
