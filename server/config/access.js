'use strict';

// Open sign-up is the default. Turned off, nobody can register except the
// first account on a fresh install.
function getAllowSignup(env = process.env) {
  const raw = String(env.NEOCLOUD_ALLOW_SIGNUP ?? '').trim().toLowerCase();
  return !['false', '0', 'no', 'off'].includes(raw);
}

module.exports = { getAllowSignup };
