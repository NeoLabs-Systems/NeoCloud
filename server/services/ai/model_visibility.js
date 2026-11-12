'use strict';

// Which discovered models the admin has switched off for everyone, and the
// policy for models a provider adds later: with "auto-enable new models" off,
// a new model joins the disabled list so a curating admin isn't silently
// opted into everything a provider adds. Defaults to on.

const { ENV_FILE, upsertEnvValue } = require('../../../runtime/paths');
const { persistEnv } = require('../admin/env_config');

const DISABLED_MODELS_ENV_KEY = 'NEOCLOUD_DISABLED_MODELS';
const AUTO_ENABLE_NEW_ENV_KEY = 'NEOCLOUD_AUTO_ENABLE_NEW_MODELS';
const KNOWN_MODELS_ENV_KEY = 'NEOCLOUD_KNOWN_MODELS';

function parseIdList(raw) {
  return raw ? raw.split(',').map((id) => id.trim()).filter(Boolean) : [];
}

function persistIdList(envKey, ids) {
  // Kept even when empty (persistEnv would unset it), so "known: none"
  // differs from "never set".
  const value = ids.join(',');
  upsertEnvValue(ENV_FILE, envKey, value);
  process.env[envKey] = value;
}

function getDisabledModelIds() {
  return parseIdList(process.env[DISABLED_MODELS_ENV_KEY]);
}

function setDisabledModelIds(disabledIds) {
  const unique = Array.from(new Set(disabledIds.map((id) => String(id).trim()).filter(Boolean)));
  persistIdList(DISABLED_MODELS_ENV_KEY, unique);
  return unique;
}

function getAutoEnableNewModels() {
  return process.env[AUTO_ENABLE_NEW_ENV_KEY] !== 'false';
}

function setAutoEnableNewModels(enabled) {
  persistEnv(AUTO_ENABLE_NEW_ENV_KEY, String(Boolean(enabled)));
  return Boolean(enabled);
}

// null means "never bootstrapped" -- distinct from an empty list.
function getKnownModelIds() {
  return process.env[KNOWN_MODELS_ENV_KEY] === undefined
    ? null
    : parseIdList(process.env[KNOWN_MODELS_ENV_KEY]);
}

/** Records newly discovered models and returns the disabled list after that. */
function reconcileModelVisibility(currentModelIds) {
  const currentSet = new Set((currentModelIds || []).map((id) => String(id).trim()).filter(Boolean));
  const disabledIds = getDisabledModelIds();
  // An empty catalog usually means a transient provider failure, not that
  // every model vanished -- skip rather than prune.
  if (currentSet.size === 0) return disabledIds;

  const knownIds = getKnownModelIds();
  if (knownIds === null) {
    // First run: adopt the current catalog as the baseline without touching
    // what is enabled.
    persistIdList(KNOWN_MODELS_ENV_KEY, Array.from(currentSet));
    return disabledIds;
  }

  const knownSet = new Set(knownIds);
  const newIds = Array.from(currentSet).filter((id) => !knownSet.has(id));
  // Models missing from this fetch stay in both lists: a provider hiccup must
  // not erase the admin's choices.
  if (!newIds.length) return disabledIds;

  const disabledSet = new Set(disabledIds);
  const autoDisableNewModels = !getAutoEnableNewModels();
  for (const id of newIds) {
    knownSet.add(id);
    if (autoDisableNewModels) disabledSet.add(id);
  }
  persistIdList(KNOWN_MODELS_ENV_KEY, Array.from(knownSet));
  if (autoDisableNewModels) persistIdList(DISABLED_MODELS_ENV_KEY, Array.from(disabledSet));
  return Array.from(disabledSet);
}

module.exports = {
  getAutoEnableNewModels,
  getDisabledModelIds,
  reconcileModelVisibility,
  setAutoEnableNewModels,
  setDisabledModelIds,
};
