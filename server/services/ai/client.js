'use strict';

// The one way NeoCloud's features talk to a model:
//   chat({ userId, feature, role, messages, json })  → { content, data?, … }
//   vision({ userId, feature, image, prompt, json }) → same, on the vision role
//   embed({ userId, feature, texts })                → { vectors, … }
// Each call checks that AI is on and the person opted in, checks their
// monthly budget, resolves the role's model (model_router.js), retries
// transient provider failures and records the usage -- metadata only.
// Callers check the files they read with exclusions.assertAiAllowedForNode
// before their content reaches a prompt.

const { readSettings } = require('../settings/store');
const { getSupportedModels, createProviderInstance } = require('./models');
const { resolveRoleModel } = require('./model_router');
const { withProviderRetry } = require('./provider_retry');
const { assertWithinBudget, recordModelUsage } = require('./usage');
const { assertAiUsable } = require('./policy');
const { publicErrorCode, httpError } = require('../../utils/http_error');
const { isAbortError } = require('../../utils/abort');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Ai');

const JSON_INSTRUCTION = 'Reply with a single JSON object and nothing else.';
const MAX_IMAGE_BYTES = 8 * 1024 * 1024;

async function prepare(userId, role, signal) {
  assertAiUsable(userId);
  assertWithinBudget(userId);
  const models = await getSupportedModels(userId, { signal });
  const { model } = resolveRoleModel(models, readSettings(userId), role);
  return { model, provider: createProviderInstance(model.provider, userId) };
}

// Runs one provider call with retries and records what it cost.
async function run({ userId, feature, model, signal }, call) {
  const startedAt = Date.now();
  const record = (usage, ok, errorCode = null) => recordModelUsage({
    userId,
    feature,
    provider: model.provider,
    model: model.modelId,
    usage,
    latencyMs: Date.now() - startedAt,
    ok,
    errorCode,
  });
  let result;
  try {
    result = await withProviderRetry(call, { label: `${feature} ${model.provider}`, signal });
  } catch (error) {
    if (isAbortError(error, signal)) throw error;
    const code = publicErrorCode(error) || (Number(error?.status) ? `HTTP_${error.status}` : 'PROVIDER_ERROR');
    record(null, false, code);
    logger.warn(`${feature}: ${model.id} failed (${code})`);
    throw httpError(502, `The AI provider could not answer: ${error.message}`, 'AI_PROVIDER_ERROR');
  }
  return { result, record };
}

// A model's JSON answer, with the ```json fences some models add anyway.
function parseJsonAnswer(text) {
  const raw = String(text || '').trim().replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/, '');
  const start = raw.indexOf('{');
  const end = raw.lastIndexOf('}');
  if (start === -1 || end < start) return null;
  try {
    return JSON.parse(raw.slice(start, end + 1));
  } catch {
    return null;
  }
}

async function complete({ userId, feature, role, messages, json, maxTokens, temperature, signal }) {
  const { model, provider } = await prepare(userId, role, signal);
  const prompt = json ? [{ role: 'system', content: JSON_INSTRUCTION }, ...messages] : messages;
  const { result, record } = await run({ userId, feature, model, signal }, () => provider.chat(prompt, {
    model: model.modelId,
    json,
    maxTokens,
    temperature,
    signal,
  }));
  const answer = {
    content: result.content,
    provider: model.provider,
    model: model.id,
    servedModel: result.model || model.modelId,
    usage: result.usage,
  };
  if (!json) {
    record(result.usage, true);
    return answer;
  }
  const data = parseJsonAnswer(result.content);
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    record(result.usage, false, 'AI_BAD_JSON');
    throw httpError(502, 'The AI model did not answer with valid JSON.', 'AI_BAD_JSON');
  }
  record(result.usage, true);
  return { ...answer, data };
}

/** A text answer from the role's model ('text' or 'smart'); `json` parses it into `data`. */
function chat({ userId, feature, role = 'text', messages, json = false, maxTokens, temperature, signal }) {
  if (role !== 'text' && role !== 'smart') throw new Error(`chat() takes the text or smart role, not ${role}`);
  return complete({ userId, feature, role, messages, json, maxTokens, temperature, signal });
}

/** An answer about one image (`image.data`: a Buffer, `image.mimeType`). */
function vision({ userId, feature, image, prompt, system, json = false, maxTokens, signal }) {
  if (!Buffer.isBuffer(image?.data) || !/^image\//.test(String(image?.mimeType || ''))) {
    throw httpError(400, 'An image is required.', 'INVALID_IMAGE');
  }
  if (image.data.length > MAX_IMAGE_BYTES) throw httpError(413, 'The image is too large for AI analysis.', 'IMAGE_TOO_LARGE');
  const messages = [
    ...(system ? [{ role: 'system', content: system }] : []),
    {
      role: 'user',
      content: [
        { type: 'text', text: String(prompt || 'Describe this image.') },
        { type: 'image_url', image_url: { url: `data:${image.mimeType};base64,${image.data.toString('base64')}` } },
      ],
    },
  ];
  return complete({ userId, feature, role: 'vision', messages, json, maxTokens, signal });
}

/** One vector per text, from the embedding role's model. */
async function embed({ userId, feature, texts, signal }) {
  const list = Array.isArray(texts) ? texts.map((text) => String(text ?? '')) : [];
  if (list.length === 0) throw httpError(400, 'Nothing to embed.', 'INVALID_INPUT');
  const { model, provider } = await prepare(userId, 'embedding', signal);
  const { result, record } = await run({ userId, feature, model, signal }, () => provider.embed(list, {
    model: model.modelId,
    signal,
  }));
  record(result.usage, true);
  return { vectors: result.vectors, provider: model.provider, model: model.id, usage: result.usage };
}

module.exports = { chat, embed, vision };
