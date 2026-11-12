'use strict';

const Anthropic = require('@anthropic-ai/sdk');
const { BaseProvider, contentParts, parseDataUrl, textOf } = require('./base');
const { wrapProviderError } = require('./provider_error');
const { aiFetch } = require('../http');

// Anthropic's input_tokens leaves out what the prompt cache served. Every other
// provider counts it, so this makes the numbers mean the same thing.
function promptTokensOf(usage) {
  return Number(usage.input_tokens || 0)
    + Number(usage.cache_read_input_tokens || 0)
    + Number(usage.cache_creation_input_tokens || 0);
}

function convertPart(part) {
  if (part?.type === 'image_url') {
    const image = parseDataUrl(part.image_url?.url);
    if (!image) return null;
    return { type: 'image', source: { type: 'base64', media_type: image.mimeType, data: image.data } };
  }
  return { type: 'text', text: String(part?.text ?? '') };
}

class AnthropicProvider extends BaseProvider {
  constructor(config = {}) {
    super(config);
    this.name = 'anthropic';
    this.client = new Anthropic({ apiKey: config.apiKey, baseURL: config.baseUrl || undefined, maxRetries: 0, fetch: aiFetch });
  }

  // Every current Claude model reads images.
  async listModels(signal = null) {
    const res = await this.client.models.list({ limit: 100 }, { signal });
    return (res.data || []).map((m) => ({
      id: m.id,
      name: m.display_name || m.id,
      created_at: m.created_at,
      supports_vision: true,
    }));
  }

  convertMessages(messages) {
    const system = [];
    const converted = [];
    for (const message of messages) {
      if (message.role === 'system') {
        system.push({ type: 'text', text: textOf(message.content) });
        continue;
      }
      converted.push({
        role: message.role === 'assistant' ? 'assistant' : 'user',
        content: contentParts(message.content).map(convertPart).filter(Boolean),
      });
    }
    return { system, messages: converted };
  }

  async chat(messages, options = {}) {
    const model = this.requireModel(options);
    const { system, messages: converted } = this.convertMessages(messages);
    const params = { model, max_tokens: options.maxTokens || 4096, messages: converted };
    if (system.length > 0) params.system = system;
    if (options.temperature != null) params.temperature = options.temperature;
    let response;
    try {
      response = await this.client.messages.create(params, { signal: options.signal });
    } catch (error) {
      throw wrapProviderError(error, 'Anthropic request failed', { signal: options.signal });
    }
    const blocks = Array.isArray(response?.content) ? response.content : [];
    const usage = response.usage || {};
    const inputTokens = promptTokensOf(usage);
    const outputTokens = Number(usage.output_tokens || 0);
    return {
      content: blocks.filter((block) => block.type === 'text').map((block) => block.text).join(''),
      model: response.model || model,
      truncated: response.stop_reason === 'max_tokens',
      usage: { inputTokens, outputTokens, totalTokens: inputTokens + outputTokens },
    };
  }
}

module.exports = { AnthropicProvider };
