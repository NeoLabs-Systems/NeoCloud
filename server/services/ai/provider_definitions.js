'use strict';

// The AI providers NeoCloud can use. Server-wide credentials live in the
// environment (written by the admin API with persistEnv); a person's own keys
// (BYOK) are stored encrypted in user_settings.
const AI_PROVIDER_DEFINITIONS = Object.freeze({
  openai: {
    id: 'openai',
    label: 'OpenAI',
    description: 'GPT models for text and images, plus OpenAI embeddings.',
    envKey: 'NEOCLOUD_OPENAI_API_KEY',
    baseUrlEnvKey: 'NEOCLOUD_OPENAI_BASE_URL',
    authentication: 'api_key',
    supportsApiKey: true,
    supportsBaseUrl: true,
    defaultBaseUrl: '',
  },
  'openai-compatible': {
    id: 'openai-compatible',
    label: 'Custom OpenAI-compatible',
    description: 'Models exposed by a custom OpenAI-compatible endpoint (vLLM, LM Studio, LiteLLM, …).',
    envKey: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY',
    baseUrlEnvKey: 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL',
    authentication: 'api_key',
    supportsApiKey: true,
    supportsBaseUrl: true,
    requiresBaseUrl: true,
    defaultBaseUrl: '',
  },
  anthropic: {
    id: 'anthropic',
    label: 'Anthropic',
    description: 'Claude models for long documents and careful summaries.',
    envKey: 'NEOCLOUD_ANTHROPIC_API_KEY',
    baseUrlEnvKey: 'NEOCLOUD_ANTHROPIC_BASE_URL',
    authentication: 'api_key',
    supportsApiKey: true,
    supportsBaseUrl: true,
    defaultBaseUrl: '',
  },
  google: {
    id: 'google',
    label: 'Google',
    description: 'Gemini models with large context windows and image understanding.',
    envKey: 'NEOCLOUD_GOOGLE_AI_KEY',
    authentication: 'api_key',
    supportsApiKey: true,
    supportsBaseUrl: false,
    defaultBaseUrl: '',
  },
  openrouter: {
    id: 'openrouter',
    label: 'OpenRouter',
    description: 'Hundreds of models through one API, including free ones. Get a key at openrouter.ai.',
    envKey: 'NEOCLOUD_OPENROUTER_API_KEY',
    authentication: 'api_key',
    supportsApiKey: true,
    supportsBaseUrl: true,
    defaultBaseUrl: 'https://openrouter.ai/api/v1',
  },
  // Connected through "Connect NeoRouter" (services/ai/neorouter), not a
  // pasted key: one sign-in reaches every model it routes.
  neorouter: {
    id: 'neorouter',
    label: 'NeoRouter',
    description: 'Your NeoRouter: the models of all its providers through one connection.',
    envKey: 'NEOROUTER_API_KEY',
    baseUrlEnvKey: 'NEOROUTER_BASE_URL',
    authentication: 'oauth',
    supportsApiKey: true,
    supportsBaseUrl: true,
    requiresBaseUrl: true,
    defaultBaseUrl: '',
  },
  // Off until an address is set: a file server should not probe its own
  // localhost for a model server nobody configured.
  ollama: {
    id: 'ollama',
    label: 'Ollama',
    description: 'Local models running on an Ollama server.',
    envKey: '',
    baseUrlEnvKey: 'NEOCLOUD_OLLAMA_URL',
    authentication: 'local',
    supportsApiKey: false,
    supportsBaseUrl: true,
    requiresBaseUrl: true,
    defaultBaseUrl: '',
  },
});

module.exports = { AI_PROVIDER_DEFINITIONS };
