'use strict';

const { BaseProvider, contentParts, parseDataUrl, textOf } = require('./base');
const { fetchResponseText } = require('../http');

const MAX_RESPONSE_BYTES = 16 * 1024 * 1024;
const CHAT_TIMEOUT_MS = 5 * 60 * 1000;

function ollamaError(message, status = null) {
  const error = new Error(status == null
    ? `Ollama request failed: ${message}`
    : `Ollama request failed (HTTP ${status}): ${message}`);
  if (status != null) error.status = status;
  return error;
}

// Ollama's own message shape: text in `content`, images as bare base64.
function convertMessage(message) {
  const images = contentParts(message.content)
    .filter((part) => part?.type === 'image_url')
    .map((part) => parseDataUrl(part.image_url?.url)?.data)
    .filter(Boolean);
  return { role: message.role, content: textOf(message.content), ...(images.length ? { images } : {}) };
}

class OllamaProvider extends BaseProvider {
  constructor(config = {}) {
    super(config);
    this.name = 'ollama';
    this.baseUrl = String(config.baseUrl || '').replace(/\/+$/, '');
  }

  async post(path, body, signal) {
    const { response, text } = await fetchResponseText(`${this.baseUrl}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      maxResponseBytes: MAX_RESPONSE_BYTES,
      serviceName: 'Ollama',
      signal,
      timeoutMs: CHAT_TIMEOUT_MS,
    });
    let data = null;
    try {
      data = JSON.parse(text || 'null');
    } catch {
      data = null;
    }
    if (!response.ok) throw ollamaError(data?.error || text || 'request failed', response.status);
    if (!data) throw ollamaError('the answer was not JSON');
    if (data.error) throw ollamaError(data.error);
    return data;
  }

  async listModels(signal = null) {
    const { response, text } = await fetchResponseText(`${this.baseUrl}/api/tags`, {
      maxResponseBytes: 2 * 1024 * 1024,
      serviceName: 'Ollama model catalog',
      signal,
      timeoutMs: 5000,
    });
    if (!response.ok) throw ollamaError('could not list models', response.status);
    return (JSON.parse(text || '{}').models || []).map((model) => {
      const capabilities = Array.isArray(model.capabilities) ? model.capabilities : null;
      const embedding = capabilities ? capabilities.includes('embedding') : /embed/i.test(model.name);
      return {
        id: model.name,
        name: model.name,
        kind: embedding ? 'embedding' : 'chat',
        ...(capabilities ? { supports_vision: capabilities.includes('vision') } : {}),
      };
    });
  }

  async chat(messages, options = {}) {
    const model = this.requireModel(options);
    const data = await this.post('/api/chat', {
      model,
      messages: messages.map(convertMessage),
      stream: false,
      ...(options.json ? { format: 'json' } : {}),
      options: {
        num_predict: options.maxTokens || 4096,
        ...(options.temperature != null ? { temperature: options.temperature } : {}),
      },
    }, options.signal);
    const inputTokens = data.prompt_eval_count || 0;
    const outputTokens = data.eval_count || 0;
    return {
      content: data.message?.content || '',
      model: data.model || model,
      truncated: data.done_reason === 'length',
      usage: { inputTokens, outputTokens, totalTokens: inputTokens + outputTokens },
    };
  }

  async embed(texts, options = {}) {
    const model = this.requireModel(options);
    const data = await this.post('/api/embed', { model, input: texts }, options.signal);
    const inputTokens = data.prompt_eval_count || 0;
    return {
      vectors: Array.isArray(data.embeddings) ? data.embeddings : [],
      model: data.model || model,
      usage: { inputTokens, outputTokens: 0, totalTokens: inputTokens },
    };
  }
}

module.exports = { OllamaProvider };
