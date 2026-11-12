'use strict';

const OpenAI = require('openai');
const { OpenAICompatibleProvider } = require('./openai_compatible');
const { kindFromModelId } = require('./base');
const { wrapProviderError } = require('./provider_error');

// Models OpenAI lists that neither chat nor embed nor transcribe.
const DROP = /dall-e|tts|moderat|realtime|audio|search-api|-image-|babbage|davinci-002|^sora|-instruct|deep-research|computer-use|gpt-live/i;

class OpenAIProvider extends OpenAICompatibleProvider {
  constructor(config = {}) {
    super(config);
    this.name = 'openai';
    this.client = new OpenAI({ apiKey: config.apiKey, baseURL: config.baseUrl || undefined, maxRetries: 0 });
  }

  async listModels(signal = null) {
    try {
      const res = await this.client.models.list({ signal });
      return res.data
        .filter((m) => !DROP.test(m.id))
        .map((m) => ({ id: m.id, name: m.id, created: m.created, kind: kindFromModelId(m.id) }));
    } catch (err) {
      throw wrapProviderError(err, 'Failed to list OpenAI models', { signal });
    }
  }

  isReasoningModel(model) {
    const id = String(model || '').trim().toLowerCase();
    return /^o\d/.test(id)
      || /^gpt-(?:[5-9]|\d{2,})(?:[.\-]|$)/.test(id)
      || id.includes('reasoning');
  }

  buildParams(model, messages, options) {
    if (!this.isReasoningModel(model)) {
      return { model, messages, temperature: options.temperature ?? 0.7, max_tokens: options.maxTokens || 4096 };
    }
    // Reasoning models (GPT-5, o-series) take the developer role, a
    // completion-token limit and no temperature.
    return {
      model,
      messages: messages.map((m) => (m.role === 'system' ? { ...m, role: 'developer' } : m)),
      max_completion_tokens: options.maxTokens || 4096,
    };
  }
}

module.exports = { OpenAIProvider };
