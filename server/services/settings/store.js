'use strict';

// Per-account preferences the apps share. Each key has a default and a
// validator; anything else is refused, so the table never fills with junk.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { parseModelSelectionId } = require('../ai/model_identity');

const oneOf = (...values) => (value) => values.includes(value);
const isBoolean = (value) => typeof value === 'boolean';
// 'auto' or a `provider::model` id from the catalog.
const isModelChoice = (value) => value === 'auto'
  || (typeof value === 'string' && value.length <= 300 && parseModelSelectionId(value) !== null);
// Per-feature switches: { autoTitle: true, … }, booleans only.
const isFeatureFlags = (value) => value !== null
  && typeof value === 'object'
  && !Array.isArray(value)
  && Object.keys(value).length <= 32
  && Object.entries(value).every(([key, flag]) => /^[a-zA-Z][a-zA-Z0-9]{0,39}$/.test(key) && isBoolean(flag));
// A file name pattern such as "{date} {issuer} – {subject}"; never a path.
const isNamingTemplate = (value) => typeof value === 'string'
  && value.trim().length > 0
  && value.length <= 200
  && !/[\\/\u0000-\u001f]/.test(value);

// Sort and view per folder: { <folder id or page>: { sort, dir, view } }.
// A PUT carries only the entries that changed (null removes one) and is
// merged into what is stored, so two devices never overwrite each other's
// folders. The oldest entries go once there are more than the cap.
const FOLDER_VIEWS_LIMIT = 500;
const FOLDER_VIEW_KEY = /^[A-Za-z0-9_:-]{1,64}$/;
const isFolderView = (value) => value !== null
  && typeof value === 'object'
  && !Array.isArray(value)
  && Object.keys(value).length === 3
  && oneOf('name', 'size', 'modified', 'type', 'relevance')(value.sort)
  && oneOf('asc', 'desc')(value.dir)
  && oneOf('list', 'grid')(value.view);
const isFolderViewsPatch = (value) => value !== null
  && typeof value === 'object'
  && !Array.isArray(value)
  && Object.keys(value).length <= FOLDER_VIEWS_LIMIT
  && Object.entries(value).every(([key, view]) => FOLDER_VIEW_KEY.test(key) && (view === null || isFolderView(view)));

function mergeFolderViews(stored, patch) {
  const merged = { ...stored };
  const at = Date.now();
  for (const [key, view] of Object.entries(patch)) {
    if (view === null) {
      delete merged[key];
    } else {
      merged[key] = { sort: view.sort, dir: view.dir, view: view.view, at };
    }
  }
  const keys = Object.keys(merged);
  if (keys.length <= FOLDER_VIEWS_LIMIT) return merged;
  // Entries of one PUT share a time; the order they came in breaks the tie.
  const order = new Map(keys.map((key, index) => [key, index]));
  const newest = keys
    .sort((a, b) => ((merged[b].at || 0) - (merged[a].at || 0)) || (order.get(b) - order.get(a)))
    .slice(0, FOLDER_VIEWS_LIMIT);
  return Object.fromEntries(newest.map((key) => [key, merged[key]]));
}

const SETTINGS = {
  language: { default: null, valid: (value) => value === null || oneOf('de', 'en')(value) },
  theme: { default: 'system', valid: oneOf('system', 'light', 'dark') },
  fileView: { default: 'list', valid: oneOf('list', 'grid') },
  sortField: { default: 'name', valid: oneOf('name', 'size', 'modified', 'type') },
  sortOrder: { default: 'asc', valid: oneOf('asc', 'desc') },
  showHiddenFiles: { default: false, valid: isBoolean },
  confirmDelete: { default: true, valid: isBoolean },
  uploadConflict: { default: 'ask', valid: oneOf('ask', 'replace', 'rename') },
  // Whether the account's files go into the full-text index (when the admin turned it on).
  fullTextSearch: { default: true, valid: isBoolean },
  // AI (docs/ai.md): off until the person opts in; each role picks a model.
  ai_opt_in: { default: false, valid: isBoolean },
  ai_model_text: { default: 'auto', valid: isModelChoice },
  ai_model_smart: { default: 'auto', valid: isModelChoice },
  ai_model_vision: { default: 'auto', valid: isModelChoice },
  ai_model_embedding: { default: 'auto', valid: isModelChoice },
  ai_model_transcription: { default: 'auto', valid: isModelChoice },
  ai_features: { default: {}, valid: isFeatureFlags },
  ai_naming_template: { default: '{date} {issuer} – {subject}', valid: isNamingTemplate },
  folder_views: { default: {}, valid: isFolderViewsPatch, merge: mergeFolderViews },
};

function parseStored(text, fallback) {
  if (text === undefined) return fallback;
  try {
    return JSON.parse(text);
  } catch {
    return fallback;
  }
}

function readSettings(userId) {
  const values = Object.fromEntries(Object.entries(SETTINGS).map(([key, spec]) => [key, spec.default]));
  for (const row of db.prepare('SELECT key, value FROM user_settings WHERE user_id = ?').all(userId)) {
    // A row nobody can parse falls back to the default.
    if (row.key in SETTINGS) values[row.key] = parseStored(row.value, values[row.key]);
  }
  return values;
}

function updateSettings(userId, patch) {
  if (!patch || typeof patch !== 'object' || Array.isArray(patch)) {
    throw httpError(400, 'Settings must be an object.', 'INVALID_SETTINGS');
  }
  for (const [key, value] of Object.entries(patch)) {
    const spec = Object.hasOwn(SETTINGS, key) ? SETTINGS[key] : null;
    if (!spec) throw httpError(400, `Unknown setting: ${key}`, 'UNKNOWN_SETTING');
    if (!spec.valid(value)) throw httpError(400, `Invalid value for ${key}.`, 'INVALID_SETTING_VALUE');
  }
  const readOne = db.prepare('SELECT value FROM user_settings WHERE user_id = ? AND key = ?');
  const upsert = db.prepare(`
    INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, datetime('now'))
    ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
  `);
  db.transaction(() => {
    for (const [key, value] of Object.entries(patch)) {
      const merge = SETTINGS[key].merge;
      const stored = merge ? merge(parseStored(readOne.get(userId, key)?.value, SETTINGS[key].default), value) : value;
      upsert.run(userId, key, JSON.stringify(stored));
    }
  })();
  return readSettings(userId);
}

module.exports = { readSettings, updateSettings };
