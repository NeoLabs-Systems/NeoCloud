'use strict';

// A person's own provider setup (BYOK): base URL / label per provider in
// `ai_provider_configs`, API keys encrypted as one JSON object under
// `ai_provider_api_keys`, and the roles their own NeoRouter handles under
// `ai_neorouter_roles`. These rows sit in user_settings beside the
// preferences, but the settings API never reads or writes them.

const db = require('../../db/database');
const { decryptValue, encryptValue } = require('../crypto/keys');
const { AI_PROVIDER_DEFINITIONS } = require('./provider_definitions');

const CONFIGS_KEY = 'ai_provider_configs';
const SECRETS_KEY = 'ai_provider_api_keys';
const NEOROUTER_ROLES_KEY = 'ai_neorouter_roles';

function readRow(userId, key) {
  return db.prepare('SELECT value FROM user_settings WHERE user_id = ? AND key = ?').get(userId, key)?.value ?? null;
}

function writeRow(userId, key, value) {
  db.prepare(`
    INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, datetime('now'))
    ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
  `).run(userId, key, value);
}

function parseJsonObject(value) {
  try {
    const parsed = JSON.parse(value || '{}');
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

function normalizeProviderConfigs(rawConfigs) {
  const parsed = rawConfigs && typeof rawConfigs === 'object' && !Array.isArray(rawConfigs) ? rawConfigs : {};
  const normalized = {};
  for (const definition of Object.values(AI_PROVIDER_DEFINITIONS)) {
    const raw = parsed[definition.id];
    const entry = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw : {};
    const baseUrl = typeof entry.baseUrl === 'string' ? entry.baseUrl.trim() : '';
    normalized[definition.id] = {
      baseUrl: definition.supportsBaseUrl ? baseUrl : '',
      label: typeof entry.label === 'string' ? entry.label.trim().slice(0, 80) : '',
    };
  }
  return normalized;
}

function getProviderConfigs(userId) {
  if (!userId) return normalizeProviderConfigs({});
  return normalizeProviderConfigs(parseJsonObject(readRow(userId, CONFIGS_KEY)));
}

function getProviderSecrets(userId) {
  if (!userId) return {};
  const value = readRow(userId, SECRETS_KEY);
  if (!value) return {};
  try {
    return parseJsonObject(decryptValue(value));
  } catch {
    return {};
  }
}

// Stores (or clears, when apiKey is empty) a person's own key for a provider.
// Callers pass the authenticated account's id, never one from the request.
function setProviderSecret(userId, providerId, apiKey) {
  if (!AI_PROVIDER_DEFINITIONS[providerId]) throw new Error(`Unknown provider: ${providerId}`);
  const trimmed = String(apiKey || '').trim();
  const secrets = getProviderSecrets(userId);
  if (trimmed) secrets[providerId] = trimmed;
  else delete secrets[providerId];
  writeRow(userId, SECRETS_KEY, encryptValue(JSON.stringify(secrets)));
  return { configured: Boolean(trimmed) };
}

// The non-secret side of a provider's BYOK config (base URL, display label).
function setProviderConfig(userId, providerId, updates = {}) {
  const definition = AI_PROVIDER_DEFINITIONS[providerId];
  if (!definition) throw new Error(`Unknown provider: ${providerId}`);
  const next = getProviderConfigs(userId);
  if (updates.baseUrl !== undefined) {
    next[providerId].baseUrl = definition.supportsBaseUrl ? String(updates.baseUrl || '').trim() : '';
  }
  if (updates.label !== undefined) {
    next[providerId].label = String(updates.label || '').trim().slice(0, 80);
  }
  const normalized = normalizeProviderConfigs(next);
  writeRow(userId, CONFIGS_KEY, JSON.stringify(normalized));
  return normalized[providerId];
}

/** The roles the person's own NeoRouter handles; null when never set. */
function getNeoRouterRoles(userId) {
  const value = userId ? readRow(userId, NEOROUTER_ROLES_KEY) : null;
  if (value == null) return null;
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

function setNeoRouterRoles(userId, roles) {
  writeRow(userId, NEOROUTER_ROLES_KEY, JSON.stringify(roles));
}

module.exports = {
  getNeoRouterRoles,
  getProviderConfigs,
  getProviderSecrets,
  setNeoRouterRoles,
  setProviderConfig,
  setProviderSecret,
};
