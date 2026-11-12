'use strict';

// Whether an account may be used at all. An admin can disable an account
// (services/admin/users.js); a disabled or deleted account is refused on
// every sign-in route and every authenticated request. One primary-key read.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');

function isAccountActive(userId) {
  return Boolean(db.prepare('SELECT 1 FROM users WHERE id = ? AND disabled_at IS NULL').get(userId));
}

function accountDisabled() {
  return httpError(403, 'This account is disabled.', 'ACCOUNT_DISABLED');
}

function assertAccountActive(userId) {
  if (!isAccountActive(userId)) throw accountDisabled();
}

module.exports = { accountDisabled, assertAccountActive, isAccountActive };
