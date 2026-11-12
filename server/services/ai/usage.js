'use strict';

// What AI calls cost, as metadata only: who, which feature, which provider
// and model, how many tokens, how long, and whether it worked. Never a prompt,
// a file name or an answer.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

const BUDGET_ENV = 'NEOCLOUD_AI_MONTHLY_TOKENS';
const MONTH_START = "strftime('%Y-%m-01 00:00:00', 'now')";

function finiteToken(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : 0;
}

function recordModelUsage({ userId, feature, provider, model, usage, latencyMs = 0, ok = true, errorCode = null }) {
  const inputTokens = finiteToken(usage?.inputTokens);
  const outputTokens = finiteToken(usage?.outputTokens);
  const totalTokens = finiteToken(usage?.totalTokens) || inputTokens + outputTokens;
  db.prepare(`
    INSERT INTO ai_usage (user_id, feature, provider, model, input_tokens, output_tokens, total_tokens, latency_ms, ok, error_code)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).run(
    userId,
    String(feature),
    String(provider),
    String(model),
    inputTokens,
    outputTokens,
    totalTokens,
    Math.max(0, Math.floor(Number(latencyMs) || 0)),
    ok ? 1 : 0,
    errorCode,
  );
}

function monthlyTokensUsed(userId) {
  return db.prepare(`SELECT COALESCE(SUM(total_tokens), 0) AS used FROM ai_usage WHERE user_id = ? AND created_at >= ${MONTH_START}`)
    .get(userId).used;
}

/** The server-wide monthly token budget per account; null is no limit. */
function defaultMonthlyTokens() {
  const value = Number.parseInt(process.env[BUDGET_ENV] || '', 10);
  return Number.isInteger(value) && value > 0 ? value : null;
}

/** The account's monthly token budget (its own override, else the default); null is no limit. */
function monthlyTokenLimit(userId) {
  const override = db.prepare('SELECT ai_monthly_tokens FROM users WHERE id = ?').get(userId)?.ai_monthly_tokens;
  if (override === null || override === undefined) return defaultMonthlyTokens();
  return override > 0 ? override : null;
}

function budgetOf(userId) {
  return { used: monthlyTokensUsed(userId), limit: monthlyTokenLimit(userId) };
}

function assertWithinBudget(userId) {
  const { used, limit } = budgetOf(userId);
  if (limit !== null && used >= limit) {
    throw httpError(429, 'This month’s AI budget is used up. It resets on the 1st.', 'AI_BUDGET_EXCEEDED');
  }
}

/** This month's usage per account and per feature, for the admin. */
function usageSummary() {
  const perUser = db.prepare(`
    SELECT u.id, u.username, u.ai_monthly_tokens,
           COALESCE(SUM(a.total_tokens), 0) AS tokens, COUNT(a.id) AS calls,
           COALESCE(SUM(1 - a.ok), 0) AS errors
    FROM ai_usage a JOIN users u ON u.id = a.user_id
    WHERE a.created_at >= ${MONTH_START}
    GROUP BY u.id ORDER BY tokens DESC
  `).all();
  const perFeature = db.prepare(`
    SELECT feature, COALESCE(SUM(total_tokens), 0) AS tokens, COUNT(*) AS calls
    FROM ai_usage WHERE created_at >= ${MONTH_START}
    GROUP BY feature ORDER BY tokens DESC
  `).all();
  const perModel = db.prepare(`
    SELECT provider, model, COALESCE(SUM(total_tokens), 0) AS tokens, COUNT(*) AS calls,
           CAST(COALESCE(AVG(latency_ms), 0) AS INTEGER) AS avgLatencyMs
    FROM ai_usage WHERE created_at >= ${MONTH_START}
    GROUP BY provider, model ORDER BY tokens DESC
  `).all();
  return {
    since: db.prepare(`SELECT ${MONTH_START} AS since`).get().since,
    users: perUser.map((row) => ({
      userId: row.id,
      username: row.username,
      tokens: row.tokens,
      calls: row.calls,
      errors: row.errors,
      limit: monthlyTokenLimit(row.id),
    })),
    features: perFeature,
    models: perModel,
  };
}

module.exports = {
  BUDGET_ENV,
  assertWithinBudget,
  budgetOf,
  defaultMonthlyTokens,
  recordModelUsage,
  usageSummary,
};
