'use strict';

const { resolveModelSelection } = require('./model_identity');
const { httpError } = require('../../utils/http_error');

// The kinds of work NeoCloud asks a model for. Each has its own per-user
// setting ('auto' or a `provider::model` id) and resolves on its own:
//   the person's choice, if that model is available to them →
//   the NeoRouter auto profile for the role →
//   the best-ranked available model of the right kind →
//   AI_MODELS_UNAVAILABLE.
const MODEL_ROLES = Object.freeze({
  // Fast text: titles, tags, metadata extraction, sorting decisions.
  text: { setting: 'ai_model_text', kind: 'chat', profile: 'fast', neorouter: ['auto:fast', 'auto:default'] },
  // Smart text: summaries, questions about a file.
  smart: { setting: 'ai_model_smart', kind: 'chat', profile: 'quality', neorouter: ['auto:smart', 'auto:default'] },
  // Images, screenshots, scans: only models that read images.
  vision: { setting: 'ai_model_vision', kind: 'chat', vision: true, profile: 'balanced', neorouter: ['auto:default'] },
  embedding: { setting: 'ai_model_embedding', kind: 'embedding', profile: 'balanced', neorouter: ['auto:default'] },
  transcription: { setting: 'ai_model_transcription', kind: 'transcription', profile: 'balanced', neorouter: ['auto:default'] },
});

// The price class each role reaches for first. Providers sell their small
// and large models at very different rates, so price says which class a model
// is in and its release date says whether it is current. A model with no
// known price ranks with the mid-priced ones.
const TIER_PREFERENCES = Object.freeze({
  balanced: ['medium', 'cheap', 'expensive', 'free'],
  fast: ['cheap', 'medium', 'free', 'expensive'],
  quality: ['expensive', 'medium', 'cheap', 'free'],
});

function tierRank(model, preference) {
  const rank = preference.indexOf(model?.priceTier);
  return rank === -1 ? preference.indexOf('medium') : rank;
}

// The price class the role wants, then the most recent release. Nothing here
// reads model names.
function rankModels(models, profile) {
  const preference = TIER_PREFERENCES[profile] || TIER_PREFERENCES.balanced;
  return models
    .map((model, index) => ({ model, index, tierScore: tierRank(model, preference), createdAt: Number(model.createdAt) || 0 }))
    .sort((left, right) => (
      left.tierScore - right.tierScore
      || right.createdAt - left.createdAt
      || left.index - right.index
    ))
    .map((entry) => entry.model);
}

/** The models that can do a role's work for this account right now. */
function rolePool(models, role) {
  const spec = MODEL_ROLES[role];
  return models.filter((model) => model.available !== false
    && model.kind === spec.kind
    && (!spec.vision || model.supportsVision === true));
}

/**
 * { model, source } for a role: source is 'user', 'neorouter' or 'auto', and
 * `requestedUnavailable` says the person's choice could not be used. Throws
 * 503 AI_MODELS_UNAVAILABLE when no model can do the work.
 */
function resolveRoleModel(models, settings, role) {
  const spec = MODEL_ROLES[role];
  const pool = rolePool(models, role);
  const requested = String(settings[spec.setting] || 'auto').trim();
  if (requested !== 'auto') {
    const chosen = resolveModelSelection(pool, requested);
    if (chosen) return { model: chosen, source: 'user', requestedUnavailable: false };
  }
  const requestedUnavailable = requested !== 'auto';
  for (const profile of spec.neorouter) {
    const routed = pool.find((model) => model.provider === 'neorouter' && model.modelId === profile);
    if (routed) return { model: routed, source: 'neorouter', requestedUnavailable };
  }
  const [ranked] = rankModels(pool, spec.profile);
  if (ranked) return { model: ranked, source: 'auto', requestedUnavailable };
  throw httpError(503, `No AI model is available for ${role} work. Ask your admin to set up a provider, or add your own key in Settings › AI.`, 'AI_MODELS_UNAVAILABLE');
}

module.exports = { MODEL_ROLES, resolveRoleModel };
