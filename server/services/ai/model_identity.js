'use strict';

// Model ids are provider-scoped: "openai::gpt-5.5", "neorouter::auto:fast".

const MODEL_SELECTION_SEPARATOR = '::';

function createModelSelectionId(provider, modelId) {
  return `${String(provider).trim().toLowerCase()}${MODEL_SELECTION_SEPARATOR}${String(modelId).trim()}`;
}

function parseModelSelectionId(value) {
  const raw = String(value || '').trim();
  const separator = raw.indexOf(MODEL_SELECTION_SEPARATOR);
  if (separator <= 0) return null;
  const provider = raw.slice(0, separator).trim().toLowerCase();
  const modelId = raw.slice(separator + MODEL_SELECTION_SEPARATOR.length).trim();
  if (!provider || !modelId || modelId === 'auto') return null;
  return { provider, modelId };
}

function getRawModelId(model) {
  return String(model?.modelId || model?.id || '').trim();
}

function toSelectableModel(model) {
  const provider = String(model?.provider || '').trim().toLowerCase();
  const modelId = getRawModelId(model);
  return {
    ...model,
    id: createModelSelectionId(provider, modelId),
    modelId,
    provider,
  };
}

function resolveModelSelection(models, value) {
  const requested = String(value || '').trim();
  return requested ? models.find((model) => model.id === requested) || null : null;
}

function modelMatchesConfiguredId(model, configuredIds) {
  if (!configuredIds) return false;
  return configuredIds.has(model.id) || configuredIds.has(getRawModelId(model));
}

module.exports = {
  modelMatchesConfiguredId,
  parseModelSelectionId,
  resolveModelSelection,
  toSelectableModel,
};
