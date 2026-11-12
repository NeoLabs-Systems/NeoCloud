'use strict';

const { CustomOpenAIProvider } = require('./openai_compatible_custom');
const { modelList } = require('../neorouter/client');
const { priceTierForCost } = require('../model_reference');

// NeoRouter's auto models: plain auto (the key's own profile) and one per
// routing profile. "auto" alone is not a valid selection id, so plain auto
// is listed as "auto:default".
const isAuto = (id) => /^auto(-(free|plan|paid))?(:|$)/.test(String(id));
const selectableAutoId = (id) => (String(id).includes(':') ? String(id) : `${id}:default`);
// "Auto (Smartest, free models only)" → "NeoRouter Auto · Smartest · free models only".
function autoLabel(model) {
  const name = String(model.name || model.id);
  const inner = /^Auto \((.*)\)$/.exec(name);
  return inner ? `NeoRouter Auto · ${inner[1].split(', ').join(' · ')}` : `NeoRouter ${name}`;
}
// The catalog kinds NeoCloud uses, as NeoRouter's ?type= names them.
const LISTED_KINDS = Object.freeze({ chat: 'chat', embedding: 'embedding', transcription: 'transcription' });

function listedModel(model, kind) {
  if (isAuto(model.id)) {
    // NeoRouter sends images only to models that can see them.
    return { id: selectableAutoId(model.id), name: autoLabel(model), kind, supports_tools: true, supports_vision: kind === 'chat' };
  }
  const facts = model.neorouter || {};
  const perToken = Number(model.pricing?.prompt);
  const known = Number.isFinite(perToken) && model.pricing?.prompt !== null;
  const priceTier = facts.free || facts.plan ? 'free' : priceTierForCost(known ? perToken * 1e6 : null);
  return {
    id: model.id,
    name: model.name || model.id,
    kind,
    supports_tools: facts.tools ?? model.supports_tools,
    supports_vision: facts.vision ?? model.supports_vision,
    ...(priceTier ? { priceTier } : {}),
  };
}

// A NeoRouter speaks the OpenAI API for every kind of model and routes each
// request across all of its providers. Its own model list carries prices and
// abilities, so NeoCloud need not look them up elsewhere.
class NeoRouterProvider extends CustomOpenAIProvider {
  constructor(config = {}) {
    // An address may come with or without the /v1 the API lives under.
    const baseUrl = config.baseUrl ? `${String(config.baseUrl).replace(/\/+$/, '').replace(/\/v1$/, '')}/v1` : config.baseUrl;
    super({ ...config, baseUrl });
    this.name = 'neorouter';
  }

  get connection() {
    return { apiBase: this.baseURL.replace(/\/+$/, ''), apiKey: this.client.apiKey };
  }

  // Chat, embedding and speech-to-text models. A kind the router can't list
  // is left out; only when none can be listed is that an error.
  async listModels(signal = null) {
    const lists = await Promise.allSettled(Object.entries(LISTED_KINDS).map(async ([kind, type]) => {
      const { models } = await modelList(this.connection, type, { signal });
      return models.map((model) => listedModel(model, kind));
    }));
    const listed = lists.filter((result) => result.status === 'fulfilled');
    if (listed.length === 0) throw lists[0].reason;
    return listed.flatMap((result) => result.value);
  }

  // Embeddings take plain "auto", the profile the key was approved with.
  embeddingModelId(model) {
    return model === 'auto:default' ? 'auto' : model;
  }
}

module.exports = { NeoRouterProvider };
