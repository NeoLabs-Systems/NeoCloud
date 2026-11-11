'use strict';

const crypto = require('crypto');

let fallbackSecret = null;

// Startup writes SESSION_SECRET into the env file, so the fallback only ever
// serves tests and one-off scripts; production refuses to run without it.
function getSessionSecret() {
  const configured = String(process.env.SESSION_SECRET || '').trim();
  if (configured) return configured;
  if (String(process.env.NODE_ENV || '').trim().toLowerCase() === 'production') {
    throw new Error('SESSION_SECRET must be configured in production.');
  }
  if (!fallbackSecret) fallbackSecret = crypto.randomBytes(32).toString('hex');
  return fallbackSecret;
}

module.exports = { getSessionSecret };
