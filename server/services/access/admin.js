'use strict';

// Admin is a flag on the account row, read fresh on every request, so granting
// or revoking it takes effect immediately. The first account on an install is
// promoted by a schema trigger; later changes come from an admin in the app,
// the `neocloud admin` CLI, or the NEOCLOUD_ADMIN_USERS env list.

const db = require('../../db/database');
const { recordAccessEvent } = require('./audit');
const { httpError } = require('../../utils/http_error');

const ADMIN_USERS_ENV_KEY = 'NEOCLOUD_ADMIN_USERS';

function isAdminUser(userId) {
  const row = db.prepare('SELECT is_admin FROM users WHERE id = ?').get(userId);
  return Number(row?.is_admin) === 1;
}

function listAdmins() {
  return db.prepare('SELECT id, username FROM users WHERE is_admin = 1 ORDER BY id').all();
}

function findAccount(username) {
  const name = String(username || '').trim();
  const user = db.prepare('SELECT id, username, is_admin FROM users WHERE lower(username) = lower(?)').get(name);
  if (!user) throw httpError(404, `No account named "${name}".`, 'NOT_FOUND');
  return user;
}

function setAdmin(userId, isAdmin, { actorUserId = null, source }) {
  const user = db.prepare('SELECT id, username, is_admin FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  if ((Number(user.is_admin) === 1) === isAdmin) return { username: user.username, changed: false };
  if (!isAdmin && listAdmins().length === 1) {
    throw httpError(409, 'The last admin can’t be removed.', 'LAST_ADMIN');
  }
  db.transaction(() => {
    db.prepare('UPDATE users SET is_admin = ? WHERE id = ?').run(isAdmin ? 1 : 0, user.id);
    recordAccessEvent(isAdmin ? 'admin.grant' : 'admin.revoke', {
      actorUserId,
      subjectUserId: user.id,
      detail: { source },
    });
  })();
  return { username: user.username, changed: true };
}

function grantAdmin(username, { source }) {
  return setAdmin(findAccount(username).id, true, { source });
}

function revokeAdmin(username, { source }) {
  return setAdmin(findAccount(username).id, false, { source });
}

function envAdminUsernames(env = process.env) {
  return String(env[ADMIN_USERS_ENV_KEY] || '')
    .split(',')
    .map((name) => name.trim())
    .filter(Boolean);
}

// A name in NEOCLOUD_ADMIN_USERS with no account behind it would hand admin to
// whoever registers it first, so nobody may take it.
function isReservedAdminUsername(username, env = process.env) {
  const name = String(username || '').trim().toLowerCase();
  const reserved = envAdminUsernames(env).find((item) => item.toLowerCase() === name);
  if (!reserved) return false;
  return !db.prepare('SELECT 1 FROM users WHERE lower(username) = lower(?)').get(reserved);
}

// Additive only: removing a name from the list does not revoke it.
function applyEnvAdminGrants(env = process.env) {
  const granted = [];
  const missing = [];
  for (const name of envAdminUsernames(env)) {
    const user = db.prepare('SELECT id FROM users WHERE lower(username) = lower(?)').get(name);
    if (!user) {
      missing.push(name);
    } else if (setAdmin(user.id, true, { source: 'env' }).changed) {
      granted.push(name);
    }
  }
  return { granted, missing };
}

module.exports = {
  applyEnvAdminGrants,
  grantAdmin,
  isAdminUser,
  isReservedAdminUsername,
  listAdmins,
  revokeAdmin,
  setAdmin,
};
