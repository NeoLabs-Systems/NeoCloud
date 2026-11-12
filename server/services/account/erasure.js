'use strict';

// Deleting an account with everything it owns: files and folders with their
// versions, previews and search index entries, links and shares in both
// directions, vaults, imports with their stored credentials, upload sessions,
// sessions, device tokens and WebDAV app passwords, sign-in challenges,
// passkeys and 2FA. The rows go in one transaction; files on disk are
// removed after it commits. Used by self-service deletion and by admins.

const db = require('../../db/database');
const { recordAccessEvent } = require('../access/audit');
const { revokeAllSessionsForUser } = require('./sessions');
const { emitToUser, disconnectUser } = require('../events');
const { deleteUserBlobDirectory } = require('../storage/blob_store');
const { removePartFiles } = require('../transfer/uploads');
const importRunner = require('../import/runner');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Erasure');

const ACTIVE_IMPORT_STATES = ['queued', 'scanning', 'running', 'paused'];

/** True when no other active account could administer the server. */
function isLastActiveAdmin(user) {
  if (Number(user.is_admin) !== 1) return false;
  const { count } = db.prepare(`
    SELECT COUNT(*) AS count FROM users WHERE is_admin = 1 AND disabled_at IS NULL AND id != ?
  `).get(user.id);
  return count === 0;
}

function assertNotLastAdmin(user) {
  if (isLastActiveAdmin(user)) {
    throw httpError(409, 'You are the only admin. Make someone else an admin first.', 'LAST_ADMIN');
  }
}

// Accounts that shared with this one or that this one shared with, with the
// shared node ids each of them could see: they are told to refresh.
function sharePartners(userId) {
  const partners = new Map();
  const rows = db.prepare(`
    SELECT grantee_id AS partner, node_id FROM user_shares WHERE owner_id = ?
    UNION ALL
    SELECT owner_id AS partner, node_id FROM user_shares WHERE grantee_id = ?
  `).all(userId, userId);
  for (const { partner, node_id: nodeId } of rows) {
    if (!partners.has(partner)) partners.set(partner, []);
    partners.get(partner).push(nodeId);
  }
  return partners;
}

function deleteRows(userId) {
  const run = (sql) => db.prepare(sql).run({ id: userId });
  // Shares and links first (they reference nodes); then the tree, root
  // downwards so children cascade; then blobs, which nodes and versions
  // reference without a cascade. Everything else would cascade with the
  // users row, but is removed by name so nothing depends on that alone.
  run('DELETE FROM user_shares WHERE owner_id = @id OR grantee_id = @id');
  run('DELETE FROM shares WHERE user_id = @id');
  run('DELETE FROM search_docs WHERE user_id = @id');
  run('DELETE FROM vaults WHERE user_id = @id');
  run('DELETE FROM import_jobs WHERE user_id = @id');
  run('DELETE FROM upload_sessions WHERE user_id = @id OR created_by = @id');
  run('DELETE FROM favorites WHERE user_id = @id');
  run('DELETE FROM node_opens WHERE user_id = @id');
  run('DELETE FROM ai_jobs WHERE user_id = @id');
  run('DELETE FROM ai_suggestions WHERE user_id = @id');
  run('DELETE FROM ai_node_meta WHERE user_id = @id');
  run('DELETE FROM ai_summaries WHERE user_id = @id');
  run('DELETE FROM ai_rules WHERE user_id = @id');
  run('DELETE FROM ai_move_stats WHERE user_id = @id');
  run('DELETE FROM ai_folder_profiles WHERE user_id = @id');
  run('DELETE FROM nodes WHERE user_id = @id AND parent_id IS NULL');
  run('DELETE FROM nodes WHERE user_id = @id');
  run('DELETE FROM previews WHERE user_id = @id');
  run('DELETE FROM blobs WHERE user_id = @id');
  run('DELETE FROM changes WHERE user_id = @id');
  run('DELETE FROM device_pairings WHERE user_id = @id');
  run('DELETE FROM device_authorizations WHERE user_id = @id');
  run('DELETE FROM user_qr_login_challenges WHERE approved_by_user_id = @id');
  run('DELETE FROM device_tokens WHERE user_id = @id');
  run('DELETE FROM user_sessions WHERE user_id = @id');
  run('DELETE FROM user_webauthn_credentials WHERE user_id = @id');
  run('DELETE FROM user_recovery_codes WHERE user_id = @id');
  run('DELETE FROM user_two_factor WHERE user_id = @id');
  run('DELETE FROM user_settings WHERE user_id = @id');
  db.prepare('DELETE FROM login_failures WHERE account_key = ?').run(`user:${userId}`);
  run('DELETE FROM users WHERE id = @id');
}

/**
 * Deletes the account. `actorUserId` is the admin doing it, or the account
 * itself. The caller has checked who may do this.
 */
async function eraseAccount(userId, { actorUserId }) {
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');

  // Ends browser sessions (store rows included) and live sockets first, so
  // nothing signed in as this account acts while it is being removed.
  revokeAllSessionsForUser(user.id);
  const imports = db.prepare(`
    SELECT id FROM import_jobs WHERE user_id = ? AND status IN (${ACTIVE_IMPORT_STATES.map(() => '?').join(', ')})
  `).all(user.id, ...ACTIVE_IMPORT_STATES);
  db.prepare(`UPDATE import_jobs SET status = 'cancelled', password_enc = NULL WHERE user_id = ?`).run(user.id);
  for (const { id } of imports) importRunner.interrupt(id, 'cancelled');

  const uploads = db.prepare('SELECT id FROM upload_sessions WHERE user_id = @id OR created_by = @id').all({ id: user.id });
  const partners = sharePartners(user.id);

  db.transaction(() => {
    deleteRows(user.id);
    recordAccessEvent('account.delete', {
      actorUserId: actorUserId === user.id ? null : actorUserId,
      detail: { userId: user.id, self: actorUserId === user.id },
    });
  })();

  // After the commit: files on disk, then the other accounts' screens.
  await deleteUserBlobDirectory(user.id);
  for (const { id } of uploads) await removePartFiles(id);
  disconnectUser(user.id);
  for (const [partnerId, nodeIds] of partners) {
    emitToUser(partnerId, 'shares:changed', {});
    emitToUser(partnerId, 'files:changed', { folderIds: [...new Set(nodeIds)] });
  }
  logger.info(`Account ${user.id} deleted${actorUserId === user.id ? ' by its owner' : ` by admin ${actorUserId}`}`);
  return { success: true };
}

module.exports = { assertNotLastAdmin, eraseAccount, isLastActiveAdmin };
