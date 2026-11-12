'use strict';

// Whether AI may run for an account: the admin's server-wide switch
// (NEOCLOUD_AI_ENABLED, off by default) and the person's own opt-in.

const { readSettings } = require('../settings/store');
const { httpError } = require('../../utils/http_error');

const ENABLED_ENV = 'NEOCLOUD_AI_ENABLED';

function isAiEnabled() {
  return String(process.env[ENABLED_ENV] || '').toLowerCase() === 'true';
}

function assertAiUsable(userId) {
  if (!isAiEnabled()) throw httpError(403, 'AI features are turned off on this server.', 'AI_DISABLED');
  if (readSettings(userId).ai_opt_in !== true) {
    throw httpError(403, 'Turn on AI features in Settings › AI first.', 'AI_NOT_OPTED_IN');
  }
}

module.exports = { ENABLED_ENV, assertAiUsable, isAiEnabled };
