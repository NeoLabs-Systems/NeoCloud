'use strict';

const { fetchResponseText } = require('./http');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('AiModels');

// Facts most provider catalogs leave out -- when a model was released, what it
// costs, whether it reads images -- read from OpenRouter's public catalog,
// which lists the models of every major vendor. The request carries no key
// and nothing about the server's users. NEOCLOUD_AI_REFERENCE_URL=off skips
// it (models then rank without prices).
const DEFAULT_REFERENCE_URL = 'https://openrouter.ai/api/v1/models';
const REFERENCE_REFRESH_MS = 6 * 60 * 60 * 1000;
const REFERENCE_RETRY_MS = 5 * 60 * 1000;
const REFERENCE_TIMEOUT_MS = 10_000;
const REFERENCE_MAX_BYTES = 5 * 1024 * 1024;

const factsById = new Map();
const factsByName = new Map();
let expiresAt = 0;
let loadedFrom = null;
let refresh = null;
let firstLoad = null;

function referenceUrl() {
  const configured = String(process.env.NEOCLOUD_AI_REFERENCE_URL || '').trim();
  if (configured.toLowerCase() === 'off') return null;
  return configured || DEFAULT_REFERENCE_URL;
}

function readVision(model) {
  if (typeof model?.supports_vision === 'boolean') return model.supports_vision;
  if (typeof model?.neorouter?.vision === 'boolean') return model.neorouter.vision;
  const inputs = model?.architecture?.input_modalities;
  return Array.isArray(inputs) ? inputs.includes('image') : null;
}

// Release time in milliseconds, input price per million tokens, tool and image
// support, from any OpenAI-style catalog entry. Anything a catalog does not
// state stays null rather than being guessed.
function readCatalogFacts(model) {
  const seconds = Number(model?.created);
  const releasedAt = Date.parse(model?.created_at || '');
  const inputCostPerM = Number.parseFloat(model?.pricing?.prompt) * 1_000_000;
  let supportsTools = null;
  if (Array.isArray(model?.supported_parameters)) supportsTools = model.supported_parameters.includes('tools');
  else if (typeof model?.supports_tools === 'boolean') supportsTools = model.supports_tools;
  return {
    createdAt: seconds > 0 ? seconds * 1000 : (Number.isFinite(releasedAt) ? releasedAt : null),
    inputCostPerM: Number.isFinite(inputCostPerM) && inputCostPerM >= 0 ? inputCostPerM : null,
    supportsTools,
    supportsVision: readVision(model),
  };
}

// One model is "gpt-5.5" at OpenAI, "gpt-5.5-2026-04-23" as a snapshot and
// "openai/gpt-5.5" in the reference. Names are compared without the vendor
// prefix, a dated snapshot suffix, or the difference between dots and dashes.
function nameKey(id) {
  return String(id || '').trim().toLowerCase()
    .replace(/^.*\//, '')
    .replace(/-(?:\d{8}|\d{4}-\d{2}-\d{2})$/, '')
    .replace(/[._]/g, '-');
}

function recordReferenceModels(rawModels) {
  if (!Array.isArray(rawModels)) return;
  for (const model of rawModels) {
    const id = typeof model?.id === 'string' ? model.id.trim() : '';
    if (!id) continue;
    const facts = readCatalogFacts(model);
    factsById.set(id, facts);
    // A ":free" variant is priced apart from the model itself.
    if (!id.includes(':')) factsByName.set(nameKey(id), facts);
  }
}

function lookupModelFacts(modelId) {
  const id = String(modelId || '').trim();
  return factsById.get(id) || factsByName.get(nameKey(id)) || null;
}

async function loadReferenceCatalog(url) {
  try {
    const { response, text } = await fetchResponseText(url, {
      maxResponseBytes: REFERENCE_MAX_BYTES,
      serviceName: 'Model reference catalog',
      timeoutMs: REFERENCE_TIMEOUT_MS,
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    recordReferenceModels(JSON.parse(text || '{}').data);
    expiresAt = Date.now() + REFERENCE_REFRESH_MS;
  } catch (error) {
    logger.warn(`Could not refresh the model reference catalog: ${error.message}`);
    expiresAt = Date.now() + REFERENCE_RETRY_MS;
  }
}

// Only the first load is waited for; afterwards the catalog already held keeps
// serving while a fresh one loads.
async function refreshReferenceCatalog() {
  const url = referenceUrl();
  if (!url) return;
  if (url !== loadedFrom) {
    loadedFrom = url;
    expiresAt = 0;
    firstLoad = null;
  }
  if (expiresAt > Date.now()) return;
  if (!refresh) refresh = loadReferenceCatalog(url).finally(() => { refresh = null; });
  if (!firstLoad) firstLoad = refresh;
  await firstLoad;
}

function priceTierForCost(costPerM) {
  if (costPerM == null) return null;
  if (costPerM === 0) return 'free';
  if (costPerM < 0.5) return 'cheap';
  if (costPerM < 5) return 'medium';
  return 'expensive';
}

module.exports = {
  lookupModelFacts,
  priceTierForCost,
  readCatalogFacts,
  recordReferenceModels,
  refreshReferenceCatalog,
};
