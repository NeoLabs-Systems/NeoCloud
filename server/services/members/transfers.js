'use strict';

// Uploads and downloads for an account that may also be a member of other
// accounts' shared folders; see files.js in this folder for the model. A
// member's upload is stored as the owner's (key, quota, node limit). Kept
// versions stay the owner's alone: a member only ever reads the current
// content.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const uploads = require('../transfer/uploads');
const downloads = require('../transfer/downloads');
const { resolveAccess } = require('./access');

function uploadNotFound() {
  return httpError(404, 'Upload not found or expired', 'UPLOAD_NOT_FOUND');
}

function uploadStream(userId, options, body) {
  const access = resolveAccess(userId, options.parentId, { need: 'write', type: 'folder' });
  if (!access.shared) return uploads.uploadStream(userId, options, body);
  return uploads.uploadStream(access.ownerId, { ...options, parentId: access.node.id }, body);
}

function createUpload(userId, options) {
  const access = resolveAccess(userId, options.parentId, { need: 'write', type: 'folder' });
  if (!access.shared) return uploads.createUpload(userId, options);
  return uploads.createUpload(access.ownerId, { ...options, parentId: access.node.id, createdBy: userId });
}

/**
 * The account a resumable upload belongs to, for the caller. A member's
 * upload into a shared folder belongs to the owner and goes on only while
 * the member may still write there.
 */
function uploadOwner(userId, uploadId) {
  const session = db.prepare('SELECT user_id, created_by, parent_id FROM upload_sessions WHERE id = ?')
    .get(String(uploadId || ''));
  if (!session || session.user_id === userId) return userId;
  if (session.created_by !== userId) throw uploadNotFound();
  const access = resolveAccess(userId, session.parent_id, { need: 'write', type: 'folder' });
  if (!access.shared || access.ownerId !== session.user_id) throw uploadNotFound();
  return session.user_id;
}

function getUpload(userId, uploadId) {
  return uploads.getUpload(uploadOwner(userId, uploadId), uploadId);
}

function putPart(userId, uploadId, index, body, options) {
  return uploads.putPart(uploadOwner(userId, uploadId), uploadId, index, body, options);
}

function completeUpload(userId, uploadId, options) {
  return uploads.completeUpload(uploadOwner(userId, uploadId), uploadId, options);
}

function cancelUpload(userId, uploadId) {
  return uploads.cancelUpload(uploadOwner(userId, uploadId), uploadId);
}

function sendFile(req, res, userId, fileId, options) {
  const access = resolveAccess(userId, fileId, { type: 'file' });
  if (!access.shared) return downloads.sendFile(req, res, userId, fileId, options);
  const hasVersion = options.version !== null && options.version !== undefined && options.version !== '';
  if (hasVersion) throw httpError(404, 'Version not found', 'NOT_FOUND');
  return downloads.sendFile(req, res, access.ownerId, access.node.id, { ...options, version: null });
}

function sendPreview(req, res, userId, fileId, variant) {
  const access = resolveAccess(userId, fileId, { type: 'file' });
  if (!access.shared) return downloads.sendPreview(req, res, userId, fileId, variant);
  return downloads.sendPreview(req, res, access.ownerId, access.node.id, variant);
}

/**
 * A ZIP of the caller's own nodes, or of nodes shared with them by one
 * owner (vault folders left out). A selection mixing accounts is refused.
 */
function sendZip(res, userId, rawIds) {
  const ids = [...new Set(String(rawIds || '').split(',').map((id) => id.trim()).filter(Boolean))];
  // Too many: the account-scoped check refuses it before anything is resolved.
  if (ids.length > downloads.MAX_ZIP_SELECTION) return downloads.sendZip(res, userId, rawIds);
  const owners = new Set(ids.map((id) => resolveAccess(userId, id).ownerId));
  if (owners.size > 1) {
    throw httpError(400, 'Download your own files and shared files separately.', 'ZIP_MIXED_OWNERS');
  }
  const [ownerId] = owners.size === 1 ? owners : [userId];
  if (ownerId === userId) return downloads.sendZip(res, userId, rawIds);
  return downloads.sendZip(res, ownerId, rawIds, { skipVaults: true });
}

module.exports = {
  cancelUpload,
  completeUpload,
  createUpload,
  getUpload,
  putPart,
  sendFile,
  sendPreview,
  sendZip,
  uploadStream,
};
