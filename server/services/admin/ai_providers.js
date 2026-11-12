'use strict';

const { cleanLine, persistEnv, maskSecret } = require('./env_config');
const { httpError } = require('../../utils/http_error');

// Server-wide AI provider credentials, stored as env keys. NeoRouter is not
// here: it is connected with its sign-in (routes/neorouter.js).
const PROVIDERS = [
  { key: 'NEOCLOUD_OPENAI_API_KEY', label: 'OpenAI', type: 'key' },
  { key: 'NEOCLOUD_OPENAI_BASE_URL', label: 'OpenAI base URL override', type: 'url' },
  { key: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY', label: 'Custom OpenAI-compatible token', type: 'key' },
  { key: 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL', label: 'Custom OpenAI-compatible base URL', type: 'url' },
  { key: 'NEOCLOUD_ANTHROPIC_API_KEY', label: 'Anthropic (Claude)', type: 'key' },
  { key: 'NEOCLOUD_ANTHROPIC_BASE_URL', label: 'Anthropic base URL override', type: 'url' },
  { key: 'NEOCLOUD_GOOGLE_AI_KEY', label: 'Google (Gemini)', type: 'key' },
  { key: 'NEOCLOUD_OPENROUTER_API_KEY', label: 'OpenRouter', type: 'key' },
  { key: 'NEOCLOUD_OLLAMA_URL', label: 'Ollama (Local)', type: 'url' },
];

const PROVIDER_BY_KEY = new Map(PROVIDERS.map((provider) => [provider.key, provider]));

function listProviders() {
  return {
    providers: PROVIDERS.map(({ key, label, type }) => {
      const value = process.env[key] || '';
      return { key, label, type, configured: Boolean(value), hint: type === 'url' ? value : maskSecret(value) };
    }),
  };
}

// An admin's address may be on the LAN (Ollama, a local endpoint); it only
// has to be a plain http(s) URL.
function assertProviderUrl(value) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw httpError(400, 'Provider URL must be a valid HTTP or HTTPS URL', 'INVALID_URL');
  }
  if (!['http:', 'https:'].includes(url.protocol) || !url.hostname) {
    throw httpError(400, 'Provider URL must use HTTP or HTTPS', 'INVALID_URL');
  }
  if (url.username || url.password) {
    throw httpError(400, 'Provider URL must not contain embedded credentials', 'INVALID_URL');
  }
}

/** Sets or (with an empty value) clears one provider credential. */
function updateProvider(key, value) {
  const provider = PROVIDER_BY_KEY.get(key);
  if (!provider) throw httpError(400, 'Unknown provider key', 'UNKNOWN_PROVIDER');
  const cleaned = cleanLine(value || '');
  if (provider.type === 'url' && cleaned) assertProviderUrl(cleaned);
  persistEnv(key, cleaned);
  return { ok: true };
}

module.exports = { listProviders, updateProvider };
