'use strict';

// Open sign-up is the default. Turning it off does not close registration:
// new accounts then need a one-time sign-up key from an admin.
function getAllowSignup(env = process.env) {
  const raw = String(env.NEOCLOUD_ALLOW_SIGNUP ?? '').trim().toLowerCase();
  return !['false', '0', 'no', 'off'].includes(raw);
}

module.exports = { getAllowSignup };
