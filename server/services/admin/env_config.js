'use strict';

// Server-wide settings the admin API edits live in the .env file. Each change
// is written there and applied to process.env so it takes effect without a
// restart.

const { ENV_FILE, upsertEnvValue } = require('../../../runtime/paths');

function persistEnv(key, value) {
  const text = String(value);
  upsertEnvValue(ENV_FILE, key, text);
  if (text === '') {
    delete process.env[key];
  } else {
    process.env[key] = text;
  }
}

module.exports = { persistEnv };
