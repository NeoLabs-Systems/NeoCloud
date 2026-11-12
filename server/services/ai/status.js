'use strict';

const { readSettings } = require('../settings/store');
const { getSupportedModels } = require('./models');
const { MODEL_ROLES, resolveRoleModel } = require('./model_router');
const { budgetOf } = require('./usage');
const { isAiEnabled } = require('./policy');

function notReady() {
  return { ready: false, model: null, source: null, requestedUnavailable: false };
}

/**
 * What the apps show: whether AI can run here, whether the person opted in,
 * which model each role resolves to right now, and this month's tokens.
 * While the admin switch is off nothing is discovered (no provider is asked).
 */
async function getAiStatus(userId, { signal } = {}) {
  const enabled = isAiEnabled();
  const settings = readSettings(userId);
  const roles = Object.fromEntries(Object.keys(MODEL_ROLES).map((role) => [role, notReady()]));
  if (enabled) {
    const models = await getSupportedModels(userId, { signal });
    for (const role of Object.keys(MODEL_ROLES)) {
      try {
        const { model, source, requestedUnavailable } = resolveRoleModel(models, settings, role);
        roles[role] = { ready: true, model: model.id, source, requestedUnavailable };
      } catch (error) {
        if (error.code !== 'AI_MODELS_UNAVAILABLE') throw error;
      }
    }
  }
  return {
    available: enabled && Object.values(roles).some((role) => role.ready),
    enabled,
    optedIn: settings.ai_opt_in === true,
    roles,
    budget: budgetOf(userId),
  };
}

module.exports = { getAiStatus };
