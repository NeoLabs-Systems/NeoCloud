'use strict';

const OpenAI = require('openai');
const { OpenAICompatibleProvider } = require('./openai_compatible');
const { fetchResponseText } = require('../http');

const OPENROUTER_BASE_URL = 'https://openrouter.ai/api/v1';
const ATTRIBUTION_HEADERS = Object.freeze({
  'HTTP-Referer': 'https://github.com/NeoLabs-Systems/NeoCloud',
  'X-Title': 'NeoCloud',
});

class OpenRouterProvider extends OpenAICompatibleProvider {
  constructor(config = {}) {
    super(config);
    this.name = 'openrouter';
    this.baseURL = config.baseUrl || OPENROUTER_BASE_URL;
    this.client = new OpenAI({
      apiKey: config.apiKey,
      baseURL: this.baseURL,
      timeout: 90_000, // free models can queue
      maxRetries: 0, // withProviderRetry handles retries
      defaultHeaders: ATTRIBUTION_HEADERS,
    });
  }

  // Chat models only; OpenRouter's catalog carries prices and modalities.
  async listModels(signal = null) {
    const { response, text } = await fetchResponseText(`${this.baseURL}/models`, {
      headers: { Authorization: `Bearer ${this.client.apiKey}` },
      maxResponseBytes: 5 * 1024 * 1024,
      serviceName: 'OpenRouter model catalog',
      signal,
    });
    if (!response.ok) {
      const error = new Error(`OpenRouter /models returned HTTP ${response.status}`);
      error.status = response.status;
      throw error;
    }
    let payload;
    try {
      payload = JSON.parse(text || '{}');
    } catch {
      throw new Error('OpenRouter /models returned invalid JSON.');
    }
    return (payload.data || []).filter((m) => (m.architecture?.output_modalities || ['text']).includes('text'));
  }

  buildParams(model, messages, options) {
    return { model, messages, temperature: options.temperature ?? 0.7, max_tokens: options.maxTokens || 4096 };
  }
}

module.exports = { OpenRouterProvider };
