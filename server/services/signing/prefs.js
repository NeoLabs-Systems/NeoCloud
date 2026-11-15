'use strict';

// Validation of a field's display and validation preferences against a small
// declarative schema: { name: { kind, ...limits } }. Unknown names are
// refused, so what is stored is exactly what the PDF engine and the clients
// understand.

const { httpError } = require('../../utils/http_error');

const COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;

function invalid(message) {
  return httpError(400, message, 'INVALID_FIELD');
}

const CHECKS = {
  bool(value, name) {
    if (typeof value !== 'boolean') throw invalid(`${name} must be true or false.`);
    return value;
  },
  int(value, name, spec) {
    if (!Number.isInteger(value) || value < spec.min || value > spec.max) {
      throw invalid(`${name} must be a whole number from ${spec.min} to ${spec.max}.`);
    }
    return value;
  },
  number(value, name, spec) {
    const min = spec.min ?? -1e12;
    const max = spec.max ?? 1e12;
    if (typeof value !== 'number' || !Number.isFinite(value) || value < min || value > max) {
      throw invalid(`${name} must be a number.`);
    }
    return value;
  },
  enum(value, name, spec) {
    if (!spec.values.includes(value)) throw invalid(`${name} must be one of: ${spec.values.join(', ')}.`);
    return value;
  },
  string(value, name, spec) {
    if (typeof value !== 'string' || value.length > spec.max) throw invalid(`${name} must be text of at most ${spec.max} characters.`);
    return value;
  },
  pattern(value, name, spec) {
    if (typeof value !== 'string' || !spec.regex.test(value)) throw invalid(`${name} is not in a valid format.`);
    return value;
  },
  color(value, name) {
    if (typeof value !== 'string' || !COLOR_PATTERN.test(value)) throw invalid(`${name} must be a color like #1a2b3c.`);
    return value.toLowerCase();
  },
  strings(value, name, spec) {
    if (!Array.isArray(value) || value.length < (spec.min ?? 0) || value.length > spec.maxItems) {
      throw invalid(`${name} must be a list of ${spec.min ?? 0} to ${spec.maxItems} entries.`);
    }
    const seen = new Set();
    return value.map((entry) => {
      const text = typeof entry === 'string' ? entry.trim() : '';
      if (!text || text.length > spec.maxLength) throw invalid(`${name} entries must be 1 to ${spec.maxLength} characters.`);
      if (seen.has(text)) throw invalid(`${name} entries must be different.`);
      seen.add(text);
      return text;
    });
  },
};

/** The preferences as they will be stored: only schema names, each checked. null/undefined count as none. */
function normalizePrefs(schema, input) {
  if (input === undefined || input === null) return {};
  if (typeof input !== 'object' || Array.isArray(input)) throw invalid('Preferences must be an object.');
  const result = {};
  for (const [name, value] of Object.entries(input)) {
    const spec = Object.hasOwn(schema, name) ? schema[name] : null;
    if (!spec) throw invalid(`Unknown preference "${name}".`);
    if (value === null) continue;
    result[name] = CHECKS[spec.kind](value, name, spec);
  }
  return result;
}

module.exports = { normalizePrefs };
