'use strict';

const db = require('../../db/database');
const { persistEnv } = require('./env_config');
const { recordAccessEvent } = require('../access/audit');
const { ENABLED_ENV, isAiEnabled } = require('../ai/policy');
const { BUDGET_ENV, defaultMonthlyTokens } = require('../ai/usage');
const { httpError } = require('../../utils/http_error');

// The admin's AI switches: AI on or off for the whole server, and how many
// tokens each account may use per month (null: no limit).

function getAiConfig() {
  return { enabled: isAiEnabled(), monthlyTokens: defaultMonthlyTokens() };
}

// null or 0 is "no limit"; otherwise a whole number of tokens.
function budgetValue(value, field) {
  if (value === null || value === 0) return 0;
  if (!Number.isInteger(value) || value < 0) {
    throw httpError(400, `${field} must be a whole number of tokens, or null for no limit.`, 'INVALID_VALUE');
  }
  return value;
}

/** Each field is optional: { enabled, monthlyTokens }. */
function updateAiConfig(body = {}) {
  if (body.enabled !== undefined) {
    if (typeof body.enabled !== 'boolean') throw httpError(400, 'enabled must be true or false.', 'INVALID_VALUE');
    persistEnv(ENABLED_ENV, body.enabled ? 'true' : 'false');
  }
  if (body.monthlyTokens !== undefined) {
    const tokens = budgetValue(body.monthlyTokens, 'monthlyTokens');
    persistEnv(BUDGET_ENV, tokens ? String(tokens) : '');
  }
  return getAiConfig();
}

/**
 * One account's monthly token budget. `monthlyTokens`: a positive number, 0
 * for no limit, or null to follow the server default.
 */
function setUserAiBudget(userId, monthlyTokens, { actorUserId }) {
  const value = monthlyTokens === null ? null : budgetValue(monthlyTokens, 'monthlyTokens');
  const result = db.prepare('UPDATE users SET ai_monthly_tokens = ? WHERE id = ?').run(value, userId);
  if (result.changes === 0) throw httpError(404, 'User not found', 'NOT_FOUND');
  recordAccessEvent('account.ai_budget_changed', { actorUserId, subjectUserId: userId, detail: { monthlyTokens: value } });
  return { userId, monthlyTokens: value };
}

module.exports = { getAiConfig, setUserAiBudget, updateAiConfig };
