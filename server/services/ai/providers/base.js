'use strict';

// What every provider offers the AI facade (services/ai/client.js):
//   listModels(signal)            → [{ id, name, kind?, supports_vision?, … }]
//   chat(messages, options)       → { content, model, usage }
//   embed(texts, options)         → { vectors, model, usage }
// Messages use the OpenAI chat shape; a user message's content may be a list
// of { type: 'text' } and { type: 'image_url' } parts (data: URLs only).
// options: { model, maxTokens, temperature, json, signal }.
class BaseProvider {
  constructor(config = {}) {
    this.config = config;
    this.name = 'base';
  }

  requireModel(options = {}) {
    const model = String(options.model || '').trim();
    if (model) return model;
    const error = new Error(`Provider '${this.name}' requires a model selected from its live catalog.`);
    error.code = 'MODEL_SELECTION_REQUIRED';
    throw error;
  }

  async chat() {
    throw new Error(`Provider '${this.name}' does not implement chat.`);
  }

  async embed() {
    const error = new Error(`Provider '${this.name}' does not serve embeddings.`);
    error.code = 'EMBEDDINGS_UNSUPPORTED';
    throw error;
  }
}

// "data:image/png;base64,AAAA" → { mimeType: 'image/png', data: 'AAAA' }.
function parseDataUrl(url) {
  const match = /^data:([^;,]+);base64,(.*)$/s.exec(String(url || ''));
  return match ? { mimeType: match[1], data: match[2] } : null;
}

// A message's content as a list of parts, whatever form it came in.
function contentParts(content) {
  if (Array.isArray(content)) return content;
  return [{ type: 'text', text: String(content ?? '') }];
}

function textOf(content) {
  return contentParts(content)
    .filter((part) => part?.type === 'text')
    .map((part) => part.text || '')
    .join('\n');
}

// Model ids that name embedding or speech-to-text models; everything else an
// OpenAI-style /models list returns is taken for a chat model.
function kindFromModelId(id) {
  if (/embed/i.test(id)) return 'embedding';
  if (/whisper|transcribe/i.test(id)) return 'transcription';
  return 'chat';
}

module.exports = { BaseProvider, contentParts, kindFromModelId, parseDataUrl, textOf };
