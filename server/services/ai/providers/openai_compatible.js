'use strict';

const { BaseProvider, kindFromModelId } = require('./base');
const { wrapProviderError } = require('./provider_error');

// The shared body of every provider that speaks OpenAI's API (OpenAI itself,
// custom endpoints, OpenRouter, NeoRouter). Subclasses set `this.client` (an
// `openai` SDK client) and may adjust the request in buildParams().
class OpenAICompatibleProvider extends BaseProvider {
  normalizeUsage(usage) {
    if (!usage) return null;
    const inputTokens = usage.prompt_tokens ?? 0;
    const outputTokens = usage.completion_tokens ?? 0;
    return { inputTokens, outputTokens, totalTokens: usage.total_tokens ?? inputTokens + outputTokens };
  }

  buildParams(model, messages, options) {
    const params = { model, messages, max_tokens: options.maxTokens || 4096 };
    if (options.temperature != null) params.temperature = options.temperature;
    return params;
  }

  async listModels(signal = null) {
    try {
      const response = await this.client.models.list({ signal });
      const models = Array.isArray(response?.data) ? response.data : [];
      return models
        .filter((model) => String(model?.id || '').trim())
        .map((model) => ({ ...model, name: model.name || model.id, kind: kindFromModelId(model.id) }));
    } catch (error) {
      throw wrapProviderError(error, `Failed to list ${this.name} models`, { signal });
    }
  }

  async chat(messages, options = {}) {
    const model = this.requireModel(options);
    const params = this.buildParams(model, messages, options);
    if (options.json) params.response_format = { type: 'json_object' };
    let response;
    try {
      response = await this.client.chat.completions.create(params, { signal: options.signal });
    } catch (error) {
      throw wrapProviderError(error, `${this.name} request failed`, { signal: options.signal });
    }
    // Routers answer some failures with HTTP 200 and an error body.
    if (response?.error) {
      const error = new Error(`${this.name}: ${response.error.message || 'request failed'}`);
      error.status = Number(response.error.code) || undefined;
      throw error;
    }
    const choice = response?.choices?.[0];
    if (!choice) throw new Error(`Provider '${this.name}' returned no choices.`);
    return {
      content: typeof choice.message?.content === 'string' ? choice.message.content : '',
      // A router may answer with another model than the one asked for.
      model: response.model || model,
      truncated: choice.finish_reason === 'length',
      usage: this.normalizeUsage(response.usage),
    };
  }

  async embed(texts, options = {}) {
    const model = this.requireModel(options);
    let response;
    try {
      response = await this.client.embeddings.create(
        { model: this.embeddingModelId(model), input: texts, encoding_format: 'float' },
        { signal: options.signal },
      );
    } catch (error) {
      throw wrapProviderError(error, `${this.name} embedding request failed`, { signal: options.signal });
    }
    const rows = Array.isArray(response?.data) ? [...response.data] : [];
    rows.sort((left, right) => (left.index ?? 0) - (right.index ?? 0));
    return {
      vectors: rows.map((row) => row.embedding),
      model: response.model || model,
      usage: this.normalizeUsage(response.usage),
    };
  }

  embeddingModelId(model) {
    return model;
  }
}

module.exports = { OpenAICompatibleProvider };
