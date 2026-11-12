'use strict';

// Server-wide settings the admin API edits live in the .env file. Each change
// is written there and applied to process.env so it takes effect without a
// restart.

const { ENV_FILE, upsertEnvValue } = require('../../../runtime/paths');

function cleanLine(value) {
  return String(value ?? '').trim().replace(/[\r\n]/g, '');
}

function persistEnv(key, value) {
  const text = String(value);
  upsertEnvValue(ENV_FILE, key, text);
  if (text === '') {
    delete process.env[key];
  } else {
    process.env[key] = text;
  }
}

// Enough of a secret to recognise which one is set, never enough to use it.
function maskSecret(value, prefixLength = 4) {
  if (!value) return '';
  if (value.length <= 8) return '•'.repeat(value.length);
  return `${value.slice(0, prefixLength)}${'•'.repeat(4)}${value.slice(-4)}`;
}

module.exports = { cleanLine, maskSecret, persistEnv };
