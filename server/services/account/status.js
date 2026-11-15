'use strict';

// Whether an account may be used at all. An admin can disable an account
// (services/admin/users.js); a disabled or deleted account is refused on
// every sign-in route and every authenticated request. One primary-key read.
// An account that signed up while mail confirmation was required can't sign
// in until it confirmed its address (assertEmailConfirmed).

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { requireSignupEmailConfirmation } = require('./service_email');

function isAccountActive(userId) {
  return Boolean(db.prepare('SELECT 1 FROM users WHERE id = ? AND disabled_at IS NULL').get(userId));
}

function accountDisabled() {
  return httpError(403, 'This account is disabled.', 'ACCOUNT_DISABLED');
}

function assertAccountActive(userId) {
  if (!isAccountActive(userId)) throw accountDisabled();
}

/** True while the account has a sign-up confirmation mail it has not answered. */
function isAwaitingSignupConfirmation(userId) {
  return Boolean(db.prepare(`
    SELECT 1 FROM user_email_tokens WHERE user_id = ? AND type = 'signup_confirmation' AND consumed_at IS NULL LIMIT 1
  `).get(userId));
}

/**
 * Refuses a sign-in (403 EMAIL_NOT_CONFIRMED, requiresEmailConfirmation) while
 * the account owes its sign-up confirmation and mail confirmation is on. Call
 * it only after the credential checked out, so it never says which accounts
 * exist. An account made while mail was off never owes one.
 */
function assertEmailConfirmed(userId) {
  if (!requireSignupEmailConfirmation()) return;
  const user = db.prepare('SELECT email, email_verified_at FROM users WHERE id = ?').get(userId);
  if (!user || user.email_verified_at || !user.email || !isAwaitingSignupConfirmation(userId)) return;
  const error = httpError(403, 'Confirm your email address before signing in.', 'EMAIL_NOT_CONFIRMED');
  error.requiresEmailConfirmation = true;
  throw error;
}

module.exports = { accountDisabled, assertAccountActive, assertEmailConfirmed, isAccountActive, isAwaitingSignupConfirmation };
