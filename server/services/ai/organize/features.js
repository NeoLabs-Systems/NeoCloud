'use strict';

// A person's AI feature switches (setting `ai_features`) with their
// defaults, and whether AI may run for them at all right now.

const { readSettings } = require('../../settings/store');
const { isAiEnabled } = require('../policy');
const { budgetOf } = require('../usage');
const { httpError } = require('../../../utils/http_error');

// On-demand features are on once the person opted in; anything that acts
// without being asked (on upload, auto-apply) is off until switched on.
const FEATURE_DEFAULTS = Object.freeze({
  names: true,
  folders: true,
  tags: true,
  summaries: true,
  onUpload: false,
  autoApplyHighConfidence: false,
  // Tags outside the person's existing ones, folders that don't exist yet.
  newTags: true,
  newFolders: false,
});

// What a job can suggest; each is also the name of its switch.
const JOB_KINDS = Object.freeze(['names', 'folders', 'tags']);

function aiFeatures(userId) {
  const stored = readSettings(userId).ai_features || {};
  const features = { ...FEATURE_DEFAULTS };
  for (const key of Object.keys(FEATURE_DEFAULTS)) {
    if (typeof stored[key] === 'boolean') features[key] = stored[key];
  }
  return features;
}

function assertFeature(userId, feature) {
  if (!aiFeatures(userId)[feature]) {
    throw httpError(403, 'This AI feature is turned off in Settings › AI.', 'AI_FEATURE_OFF');
  }
}

/**
 * Why AI can't run for the account now (AI_DISABLED, AI_NOT_OPTED_IN,
 * AI_BUDGET_EXCEEDED), or null when it can. Jobs of a paused account wait.
 */
function pauseReason(userId) {
  if (!isAiEnabled()) return 'AI_DISABLED';
  if (readSettings(userId).ai_opt_in !== true) return 'AI_NOT_OPTED_IN';
  const { used, limit } = budgetOf(userId);
  if (limit !== null && used >= limit) return 'AI_BUDGET_EXCEEDED';
  return null;
}

module.exports = { JOB_KINDS, aiFeatures, assertFeature, pauseReason };
