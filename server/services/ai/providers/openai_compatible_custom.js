'use strict';

const OpenAI = require('openai');
const { OpenAICompatibleProvider } = require('./openai_compatible');

// A custom OpenAI-compatible endpoint (vLLM, LM Studio, LiteLLM, …).
class CustomOpenAIProvider extends OpenAICompatibleProvider {
  constructor(config = {}) {
    super(config);
    this.name = 'openai-compatible';
    this.baseURL = config.baseUrl || '';
    this.client = new OpenAI({ apiKey: config.apiKey || 'none', baseURL: this.baseURL, maxRetries: 0 });
  }
}

module.exports = { CustomOpenAIProvider };
