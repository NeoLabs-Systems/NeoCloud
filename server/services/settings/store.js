'use strict';

// Per-account preferences the apps share. Each key has a default and a
// validator; anything else is refused, so the table never fills with junk.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

const oneOf = (...values) => (value) => values.includes(value);
const isBoolean = (value) => typeof value === 'boolean';

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
};

function readSettings(userId) {
  const values = Object.fromEntries(Object.entries(SETTINGS).map(([key, spec]) => [key, spec.default]));
  for (const row of db.prepare('SELECT key, value FROM user_settings WHERE user_id = ?').all(userId)) {
    if (!(row.key in SETTINGS)) continue;
    try {
      values[row.key] = JSON.parse(row.value);
    } catch {
      // A row nobody can parse falls back to the default.
    }
  }
  return values;
}

function updateSettings(userId, patch) {
  if (!patch || typeof patch !== 'object' || Array.isArray(patch)) {
    throw httpError(400, 'Settings must be an object.', 'INVALID_SETTINGS');
  }
  for (const [key, value] of Object.entries(patch)) {
    const spec = SETTINGS[key];
    if (!spec) throw httpError(400, `Unknown setting: ${key}`, 'UNKNOWN_SETTING');
    if (!spec.valid(value)) throw httpError(400, `Invalid value for ${key}.`, 'INVALID_SETTING_VALUE');
  }
  const upsert = db.prepare(`
    INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, datetime('now'))
    ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
  `);
  db.transaction(() => {
    for (const [key, value] of Object.entries(patch)) upsert.run(userId, key, JSON.stringify(value));
  })();
  return readSettings(userId);
}

module.exports = { readSettings, updateSettings };
