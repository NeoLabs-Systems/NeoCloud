'use strict';

const { GoogleGenAI } = require('@google/genai');
const { BaseProvider, contentParts, parseDataUrl, textOf } = require('./base');
const { fetchResponseText } = require('../http');
const { wrapProviderError } = require('./provider_error');

const DROP = /tts|lyria|robotics|deep-research|antigravity|computer-use|-image(?!.*it)/i;

function normalizeUsage(usage) {
  if (!usage) return null;
  const inputTokens = usage.promptTokenCount || 0;
  const outputTokens = usage.candidatesTokenCount || 0;
  return { inputTokens, outputTokens, totalTokens: usage.totalTokenCount || inputTokens + outputTokens };
}

function convertPart(part) {
  if (part?.type === 'image_url') {
    const image = parseDataUrl(part.image_url?.url);
    return image ? { inlineData: { mimeType: image.mimeType, data: image.data } } : null;
  }
  return { text: String(part?.text ?? '') };
}

class GoogleProvider extends BaseProvider {
  constructor(config = {}) {
    super(config);
    this.name = 'google';
    this.apiKey = config.apiKey;
    this.genAI = new GoogleGenAI({ apiKey: this.apiKey });
  }

  async listModels(signal = null) {
    const models = [];
    const seenPageTokens = new Set();
    let pageToken = '';
    do {
      const query = new URLSearchParams({ pageSize: '1000' });
      if (pageToken) query.set('pageToken', pageToken);
      const { response, text } = await fetchResponseText(
        `https://generativelanguage.googleapis.com/v1beta/models?${query}`,
        {
          headers: { 'x-goog-api-key': this.apiKey },
          maxResponseBytes: 5 * 1024 * 1024,
          serviceName: 'Google model catalog',
          signal,
        },
      );
      if (!response.ok) {
        const error = new Error(`Google models API returned ${response.status}`);
        error.status = response.status;
        throw error;
      }
      const payload = JSON.parse(text || '{}');
      if (Array.isArray(payload.models)) models.push(...payload.models);
      const nextPageToken = String(payload.nextPageToken || '').trim();
      if (!nextPageToken || seenPageTokens.has(nextPageToken)) break;
      seenPageTokens.add(nextPageToken);
      pageToken = nextPageToken;
    } while (pageToken);

    const listed = [];
    for (const m of models) {
      const id = m.name.replace('models/', '');
      const methods = m.supportedGenerationMethods || [];
      if (DROP.test(id)) continue;
      if (methods.includes('generateContent')) {
        // Gemini models are multimodal; Gemma models served here are not.
        listed.push({ id, name: m.displayName || id, kind: 'chat', supports_vision: /^gemini-/.test(id) });
      } else if (methods.includes('embedContent')) {
        listed.push({ id, name: m.displayName || id, kind: 'embedding' });
      }
    }
    return listed;
  }

  convertMessages(messages) {
    let systemInstruction = '';
    const history = [];
    for (const message of messages) {
      if (message.role === 'system') {
        systemInstruction += (systemInstruction ? '\n\n' : '') + textOf(message.content);
        continue;
      }
      const role = message.role === 'assistant' ? 'model' : 'user';
      const parts = contentParts(message.content).map(convertPart).filter(Boolean);
      const last = history[history.length - 1];
      // Gemini wants turns to alternate; consecutive turns of one role merge.
      if (last?.role === role) last.parts.push(...parts);
      else history.push({ role, parts });
    }
    // A request made only of system instructions still needs a user turn.
    if (history.length === 0 || history[history.length - 1].role === 'model') {
      history.push({ role: 'user', parts: [{ text: 'Provide the requested response using the system instructions.' }] });
    }
    return { systemInstruction, history };
  }

  async chat(messages, options = {}) {
    const model = this.requireModel(options);
    const { systemInstruction, history } = this.convertMessages(messages);
    const config = { maxOutputTokens: options.maxTokens || 4096 };
    if (systemInstruction) config.systemInstruction = systemInstruction;
    if (options.temperature != null) config.temperature = options.temperature;
    if (options.json) config.responseMimeType = 'application/json';
    if (options.signal) config.abortSignal = options.signal;
    let response;
    try {
      response = await this.genAI.models.generateContent({ model, contents: history, config });
    } catch (error) {
      throw wrapProviderError(error, 'Google request failed', { signal: options.signal });
    }
    let content = '';
    for (const candidate of response?.candidates || []) {
      for (const part of candidate.content?.parts || []) {
        if (part.text && part.thought !== true) content += part.text;
      }
    }
    return {
      content,
      model,
      truncated: response?.candidates?.[0]?.finishReason === 'MAX_TOKENS',
      usage: normalizeUsage(response.usageMetadata),
    };
  }

  async embed(texts, options = {}) {
    const model = this.requireModel(options);
    let response;
    try {
      response = await this.genAI.models.embedContent({
        model,
        contents: texts,
        config: options.signal ? { abortSignal: options.signal } : undefined,
      });
    } catch (error) {
      throw wrapProviderError(error, 'Google embedding request failed', { signal: options.signal });
    }
    return {
      vectors: (response?.embeddings || []).map((embedding) => embedding.values || []),
      model,
      usage: null,
    };
  }
}

module.exports = { GoogleProvider };
