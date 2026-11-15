'use strict';

// The file a WOPI call is about, re-authorised on every call through the same
// resolver as the file routes: the owner, or an account the file's folder is
// shared with (write share -> may save, read share -> may only view). Vault
// files, trashed files and other accounts' files read as missing.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { isAccountActive } = require('../account/status');
const { getLiveNode } = require('../files/tree');
const { resolveAccess } = require('../members/access');
const { EDITABLE } = require('./discovery');

function notFound() {
  return httpError(404, 'File not found', 'NOT_FOUND');
}

function extensionOf(name) {
  const dot = String(name).lastIndexOf('.');
  return dot < 0 ? '' : String(name).slice(dot + 1).toLowerCase();
}

function isEditableName(name) {
  return EDITABLE.includes(extensionOf(name));
}

/**
 * { ownerId, node, canWrite, userName } for `userId` on `nodeId`. `canWrite`
 * is the account's right now; a write token (`wantWrite`) only counts while
 * it still holds. Throws 404 when the file is out of reach.
 */
function openFile(userId, nodeId, { wantWrite = false } = {}) {
  if (!isAccountActive(userId)) throw httpError(401, 'Account unavailable', 'UNAUTHORIZED');
  const access = resolveAccess(userId, nodeId, { type: 'file' });
  const node = access.shared ? access.node : getLiveNode(userId, nodeId, { type: 'file' });
  if (node.vault_id) throw notFound();
  const canWrite = wantWrite && (!access.shared || access.permission === 'write');
  const userName = db.prepare('SELECT username FROM users WHERE id = ?').get(userId)?.username || '';
  return { ownerId: access.ownerId, node, canWrite, userName };
}

module.exports = { extensionOf, isEditableName, openFile };
