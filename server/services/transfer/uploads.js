'use strict';

// Two ways in:
//   - one request carrying the whole file (small files, simple clients)
//   - a resumable upload: create a session, send fixed-size parts in any
//     order and as often as needed, then complete it. Parts are encrypted on
//     disk like any blob, so nothing is ever stored in plaintext.
// Both stream: the server never holds more than one segment in memory.

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { Readable } = require('stream');
const db = require('../../db/database');
const { UPLOAD_TMP_DIR } = require('../../../runtime/paths');
const { httpError } = require('../../utils/http_error');
const { assertDiskSpace } = require('../../utils/disk');
const { validateName } = require('../../utils/file_names');
const { createServiceLogger } = require('../../utils/logger');
const { openTemporaryBlob, removeQuietly, writeTemporaryBlob } = require('../storage/blob_store');
const { assertUploadFits, uploadLimit } = require('../storage/quota');
const { commitUploadedBlob } = require('../files/commit');
const { conflictMode, nameExists } = require('../files/operations');
const { findLiveChildByName, getLiveNode } = require('../files/tree');

const logger = createServiceLogger('Uploads');

const PART_SIZE = 8 * 1024 * 1024;
const UPLOAD_TTL_HOURS = 24;
const PARTS_DIR = path.join(UPLOAD_TMP_DIR, 'parts');
const SHA256_PATTERN = /^[0-9a-f]{64}$/;

function parseContentLength(value) {
  if (value === undefined || value === null || value === '') return null;
  const length = Number(value);
  if (!Number.isSafeInteger(length) || length < 0) throw httpError(400, 'Invalid Content-Length.', 'INVALID_LENGTH');
  return length;
}

function normalizeSha256(value) {
  if (value === undefined || value === null || value === '') return null;
  const hash = String(value).trim().toLowerCase();
  if (!SHA256_PATTERN.test(hash)) throw httpError(400, 'sha256 must be 64 hex characters.', 'INVALID_HASH');
  return hash;
}

// Checks that would fail at the end anyway, done before any byte is read.
function precheckTarget(userId, parentId, name, conflict) {
  const parent = getLiveNode(userId, parentId, { type: 'folder' });
  const cleanName = validateName(name);
  const existing = findLiveChildByName(parent.id, cleanName);
  if (existing && (conflict === 'fail' || existing.type === 'folder') && conflict !== 'rename') {
    throw nameExists(existing);
  }
  return { parentId: parent.id, name: cleanName };
}

/**
 * The whole file in one request body. `cap` ({ limit, limitError }) lowers
 * the byte limit further, e.g. a share link's own upload allowance.
 */
async function uploadStream(userId, { parentId, name, conflict, baseEtag, contentLength, sha256, cap = null }, body) {
  const mode = conflictMode(conflict);
  const expectedHash = normalizeSha256(sha256);
  const target = precheckTarget(userId, parentId, name, mode);
  const expectedSize = parseContentLength(contentLength);
  if (expectedSize !== null) {
    if (cap && expectedSize > cap.limit) throw cap.limitError();
    assertUploadFits(userId, expectedSize);
  }
  assertDiskSpace(expectedSize ?? 0);
  const quotaLimit = uploadLimit(userId);
  const limits = cap && cap.limit < quotaLimit.limit ? cap : quotaLimit;

  const blob = await writeTemporaryBlob(userId, body, { ...limits, expectedSize });
  if (expectedHash && expectedHash !== blob.sha256) {
    await removeQuietly(blob.tmpPath);
    throw httpError(422, 'The file arrived damaged (checksum mismatch).', 'CHECKSUM_MISMATCH');
  }
  return commitUploadedBlob(userId, { ...target, conflict: mode, baseEtag }, blob);
}

function partCount(totalSize) {
  return Math.max(1, Math.ceil(totalSize / PART_SIZE));
}

function expectedPartSize(session, index) {
  const last = partCount(session.total_size) - 1;
  return index < last ? session.chunk_size : session.total_size - session.chunk_size * last;
}

function partBlobId(uploadId, index) {
  return `upload-${uploadId}-${index}`;
}

function partPath(uploadId, index) {
  return path.join(PARTS_DIR, uploadId, String(index));
}

function serializeSession(session) {
  const received = db.prepare('SELECT part_index FROM upload_parts WHERE upload_id = ? ORDER BY part_index')
    .all(session.id)
    .map((row) => row.part_index);
  return {
    id: session.id,
    parentId: session.parent_id,
    name: session.name,
    size: session.total_size,
    partSize: session.chunk_size,
    partCount: partCount(session.total_size),
    receivedParts: received,
    expiresAt: session.expires_at,
  };
}

function getSession(userId, uploadId) {
  const session = db.prepare(`
    SELECT * FROM upload_sessions
    WHERE id = ? AND user_id = ? AND datetime(expires_at) > datetime('now')
  `).get(String(uploadId || ''), userId);
  if (!session) throw httpError(404, 'Upload not found or expired', 'UPLOAD_NOT_FOUND');
  return session;
}

/** Starts a resumable upload. The full size is reserved against the quota now. */
function createUpload(userId, { parentId, name, size, conflict, sha256 }) {
  const mode = conflictMode(conflict);
  const totalSize = Number(size);
  if (!Number.isSafeInteger(totalSize) || totalSize < 0) throw httpError(400, 'size must be a byte count.', 'INVALID_SIZE');
  const expectedHash = normalizeSha256(sha256);
  const target = precheckTarget(userId, parentId, name, mode);
  const id = crypto.randomUUID();
  db.transaction(() => {
    assertUploadFits(userId, totalSize);
    db.prepare(`
      INSERT INTO upload_sessions (id, user_id, parent_id, name, conflict, total_size, chunk_size, sha256, expires_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now', ?))
    `).run(id, userId, target.parentId, target.name, mode, totalSize, PART_SIZE, expectedHash, `+${UPLOAD_TTL_HOURS} hours`);
  })();
  return serializeSession(getSession(userId, id));
}

function getUpload(userId, uploadId) {
  return serializeSession(getSession(userId, uploadId));
}

/** Stores one part. Sending a part again replaces it. */
async function putPart(userId, uploadId, rawIndex, body, { contentLength } = {}) {
  const session = getSession(userId, uploadId);
  const index = Number(rawIndex);
  if (!Number.isInteger(index) || index < 0 || index >= partCount(session.total_size)) {
    throw httpError(400, 'Part index out of range.', 'INVALID_PART');
  }
  const expectedSize = expectedPartSize(session, index);
  const declared = parseContentLength(contentLength);
  if (declared !== null && declared !== expectedSize) {
    throw httpError(400, `Part ${index} must be ${expectedSize} bytes.`, 'SIZE_MISMATCH');
  }
  assertDiskSpace(expectedSize);

  const blob = await writeTemporaryBlob(userId, body, {
    limit: expectedSize,
    limitError: () => httpError(400, `Part ${index} must be ${expectedSize} bytes.`, 'SIZE_MISMATCH'),
    expectedSize,
    blobId: partBlobId(session.id, index),
    directory: path.join(PARTS_DIR, session.id),
  });
  await fs.promises.rename(blob.tmpPath, partPath(session.id, index));
  db.prepare(`
    INSERT INTO upload_parts (upload_id, part_index, size) VALUES (?, ?, ?)
    ON CONFLICT(upload_id, part_index) DO UPDATE SET size = excluded.size
  `).run(session.id, index, blob.size);
  return { index, size: blob.size, sha256: blob.sha256 };
}

async function* readParts(userId, session) {
  for (let index = 0; index < partCount(session.total_size); index += 1) {
    yield* openTemporaryBlob(userId, partPath(session.id, index), partBlobId(session.id, index), expectedPartSize(session, index));
  }
}

async function listDirectory(directory) {
  try {
    return await fs.promises.readdir(directory);
  } catch {
    return [];
  }
}

async function removePartFiles(uploadId) {
  await fs.promises.rm(path.join(PARTS_DIR, uploadId), { recursive: true, force: true });
}

/** Joins the parts into one encrypted blob and adds the file to the tree. */
async function completeUpload(userId, uploadId, { baseEtag } = {}) {
  const session = getSession(userId, uploadId);
  const { count } = db.prepare('SELECT COUNT(*) AS count FROM upload_parts WHERE upload_id = ?').get(session.id);
  if (count !== partCount(session.total_size)) {
    throw httpError(409, 'Some parts are still missing.', 'UPLOAD_INCOMPLETE');
  }
  assertDiskSpace(session.total_size);
  const blob = await writeTemporaryBlob(userId, Readable.from(readParts(userId, session)), {
    expectedSize: session.total_size,
  });
  if (session.sha256 && session.sha256 !== blob.sha256) {
    await removeQuietly(blob.tmpPath);
    throw httpError(422, 'The file arrived damaged (checksum mismatch).', 'CHECKSUM_MISMATCH');
  }
  const node = await commitUploadedBlob(userId, {
    parentId: session.parent_id,
    name: session.name,
    conflict: session.conflict,
    baseEtag,
    uploadId: session.id,
  }, blob);
  await removePartFiles(session.id);
  return node;
}

async function cancelUpload(userId, uploadId) {
  const session = getSession(userId, uploadId);
  db.prepare('DELETE FROM upload_sessions WHERE id = ?').run(session.id);
  await removePartFiles(session.id);
  return { success: true };
}

/** Drops expired upload sessions and their parts. */
async function purgeExpiredUploads() {
  const expired = db.prepare("SELECT id FROM upload_sessions WHERE datetime(expires_at) <= datetime('now')").all();
  for (const { id } of expired) {
    db.prepare('DELETE FROM upload_sessions WHERE id = ?').run(id);
    await removePartFiles(id);
  }
  // Part folders whose session row is gone, and half-written blobs, both
  // left behind by a crash or a dropped connection.
  for (const folder of await listDirectory(PARTS_DIR)) {
    if (!db.prepare('SELECT 1 FROM upload_sessions WHERE id = ?').get(folder)) {
      await removePartFiles(folder);
      logger.info(`Removed leftover parts of upload ${folder}`);
    }
  }
  const staleBefore = Date.now() - UPLOAD_TTL_HOURS * 60 * 60 * 1000;
  for (const entry of await listDirectory(UPLOAD_TMP_DIR)) {
    const filePath = path.join(UPLOAD_TMP_DIR, entry);
    try {
      const stats = await fs.promises.stat(filePath);
      if (stats.isFile() && entry.endsWith('.part') && stats.mtimeMs < staleBefore) await removeQuietly(filePath);
    } catch {
      // Removed meanwhile.
    }
  }
  return expired.length;
}

module.exports = {
  PART_SIZE,
  cancelUpload,
  completeUpload,
  createUpload,
  getUpload,
  purgeExpiredUploads,
  putPart,
  uploadStream,
};
