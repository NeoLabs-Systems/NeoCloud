'use strict';

// CheckFileInfo, GetFile and PutFile. A file's WOPI version is NeoCloud's
// version number; PutFile goes through the normal upload commit, so quota,
// blob references, the change journal, encryption and the version history
// behave exactly as for an upload, and the editing account is recorded as
// the author of the content.

const { httpError } = require('../../utils/http_error');
const { assertDiskSpace } = require('../../utils/disk');
const { removeQuietly, writeTemporaryBlob } = require('../storage/blob_store');
const { assertUploadFits, uploadLimit } = require('../storage/quota');
const { commitUploadedBlob } = require('../files/commit');
const downloads = require('../transfer/downloads');
const { currentLock } = require('./locks');
const { assertLockedWith } = require('./wopi_locks');

// SQLite stores UTC as "YYYY-MM-DD HH:MM:SS".
function isoTime(text) {
  return new Date(`${text.replace(' ', 'T')}Z`).toISOString();
}

function modifiedTime(node) {
  return isoTime(node.modified_at);
}

function checkFileInfo({ ownerId, node, canWrite, userName }, userId) {
  return {
    BaseFileName: node.name,
    OwnerId: String(ownerId),
    UserId: String(userId),
    UserFriendlyName: userName,
    Size: node.size,
    Version: String(node.version),
    LastModifiedTime: modifiedTime(node),
    UserCanWrite: canWrite,
    UserCanNotWriteRelative: true,
    SupportsUpdate: true,
    SupportsLocks: true,
    SupportsGetLock: true,
    SupportsRename: false,
    UserCanRename: false,
  };
}

function sendContents(req, res, { ownerId, node }) {
  res.set('X-WOPI-ItemVersion', String(node.version));
  return downloads.sendFile(req, res, ownerId, node.id, {});
}

// Collabora sends the modified time it opened the file with; a different one
// means somebody else saved since (its own status code makes it reload).
function assertNotChangedSince(node, stamp) {
  if (stamp && stamp !== modifiedTime(node)) {
    const error = httpError(409, 'The file changed since it was opened.', 'FILE_CHANGED');
    error.wopiBody = { COOL_STATUS_CODE: 1010 };
    throw error;
  }
}

function assertSaveAllowed(node, lock) {
  if (lock) return assertLockedWith(node.id, lock);
  // Without a lock only an empty file may be written (a new document).
  if (currentLock(node.id) || node.size > 0) {
    const error = httpError(409, 'Lock the file before saving it.', 'LOCK_REQUIRED');
    error.wopiLock = currentLock(node.id);
    throw error;
  }
  return undefined;
}

async function putContents(req, { ownerId, node, canWrite }, userId, { lock, timestamp }) {
  if (!canWrite) throw httpError(403, 'You may only view this file.', 'READ_ONLY');
  assertSaveAllowed(node, lock);
  assertNotChangedSince(node, timestamp);
  const declared = req.get('content-length');
  const expectedSize = declared === undefined ? null : Number(declared);
  if (expectedSize !== null) {
    if (!Number.isSafeInteger(expectedSize) || expectedSize < 0) throw httpError(400, 'Invalid Content-Length.', 'INVALID_LENGTH');
    assertUploadFits(ownerId, expectedSize);
  }
  assertDiskSpace(expectedSize ?? 0);
  const blob = await writeTemporaryBlob(ownerId, req, { ...uploadLimit(ownerId), expectedSize });
  if (blob.sha256 === node.etag && blob.size === node.size) {
    await removeQuietly(blob.tmpPath);
    return { version: node.version, modifiedAt: modifiedTime(node) };
  }
  const saved = await commitUploadedBlob(ownerId, {
    parentId: node.parent_id,
    name: node.name,
    conflict: 'replace',
    authorId: userId,
  }, blob);
  return { version: saved.version, modifiedAt: isoTime(saved.modifiedAt) };
}

module.exports = { checkFileInfo, putContents, sendContents };
