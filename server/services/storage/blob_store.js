'use strict';

// Encrypted blobs on disk: BLOB_DIR/<user>/<first two chars>/<blob id>. A
// blob is written to UPLOAD_TMP_DIR first and renamed into place only when the
// database records it, so a crash never leaves a half-written blob behind a
// file entry. The file is fsynced before the rename and the directory after
// it, so a committed row never points at bytes still in the page cache.

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { Transform } = require('stream');
const { pipeline } = require('stream/promises');
const { BLOB_DIR, PREVIEW_DIR, UPLOAD_TMP_DIR, ensurePrivateDirectory } = require('../../../runtime/paths');
const { getUserDek } = require('../crypto/keys');
const { SEGMENT_SIZE, createDecryptStream, createEncryptStream, encryptedSize } = require('../crypto/stream_cipher');
const { httpError } = require('../../utils/http_error');
const { syncDirectory, syncFile } = require('../../utils/durable_fs');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('BlobStore');
const BLOB_ID_PATTERN = /^[0-9a-f-]{36}$/;

function blobPath(userId, blobId) {
  if (!BLOB_ID_PATTERN.test(blobId)) throw new Error('Invalid blob id.');
  return path.join(BLOB_DIR, String(Number(userId)), blobId.slice(0, 2), blobId);
}

/**
 * Counts and hashes plaintext on its way to the cipher, and stops the upload
 * the moment it passes `limit` bytes.
 */
function createMeter(limit, limitError) {
  const hash = crypto.createHash('sha256');
  const meter = new Transform({
    transform(chunk, _encoding, callback) {
      meter.bytes += chunk.length;
      if (meter.bytes > limit) return callback(limitError());
      hash.update(chunk);
      return callback(null, chunk);
    },
  });
  meter.bytes = 0;
  meter.digest = () => hash.digest('hex');
  return meter;
}

async function removeQuietly(filePath) {
  try {
    await fs.promises.rm(filePath, { force: true });
  } catch (err) {
    logger.warn(`Could not remove ${path.basename(filePath)}: ${err.message}`);
  }
}

/**
 * Encrypts `source` into a temporary file. Resolves with what the caller
 * needs to record the blob; the file is not yet in its final place.
 * `limit` caps plaintext bytes; `limitError` builds the error thrown past it.
 */
async function writeTemporaryBlob(userId, source, {
  limit = Number.POSITIVE_INFINITY,
  limitError = () => httpError(413, 'File is too large.', 'FILE_TOO_LARGE'),
  expectedSize = null,
  blobId = crypto.randomUUID(),
  directory = UPLOAD_TMP_DIR,
} = {}) {
  ensurePrivateDirectory(directory);
  const tmpPath = path.join(directory, `${blobId}.part`);
  const meter = createMeter(limit, limitError);
  // The source is piped rather than part of the pipeline: when the upload is
  // refused midway, the request must stay open so the client still gets the
  // error response instead of a reset connection.
  const forwardError = (err) => meter.destroy(err);
  const forwardAbort = () => meter.destroy(httpError(400, 'Upload was interrupted.', 'UPLOAD_ABORTED'));
  source.once('error', forwardError);
  source.once('aborted', forwardAbort);
  source.pipe(meter);
  try {
    await pipeline(
      meter,
      createEncryptStream(getUserDek(userId), blobId),
      fs.createWriteStream(tmpPath, { flags: 'wx', mode: 0o600 }),
    );
  } catch (err) {
    source.unpipe(meter);
    await removeQuietly(tmpPath);
    throw err;
  } finally {
    source.removeListener('error', forwardError);
    source.removeListener('aborted', forwardAbort);
  }
  if (expectedSize !== null && meter.bytes !== expectedSize) {
    await removeQuietly(tmpPath);
    throw httpError(400, `Expected ${expectedSize} bytes but received ${meter.bytes}.`, 'SIZE_MISMATCH');
  }
  try {
    await syncFile(tmpPath);
  } catch (err) {
    await removeQuietly(tmpPath);
    throw err;
  }
  return { blobId, tmpPath, size: meter.bytes, sha256: meter.digest() };
}

async function moveIntoPlace(userId, { blobId, tmpPath }) {
  const finalPath = blobPath(userId, blobId);
  const directory = path.dirname(finalPath);
  const created = !fs.existsSync(directory);
  ensurePrivateDirectory(directory);
  await fs.promises.rename(tmpPath, finalPath);
  await syncDirectory(directory);
  if (created) await syncDirectory(path.dirname(directory));
  return finalPath;
}

/**
 * Checks one stored blob without reading all of it: the file exists, has the
 * size its plaintext size implies, and its first and last segments pass the
 * GCM tag check. Returns null when fine, else what is wrong.
 */
async function checkBlob(userId, blobId, size) {
  const filePath = blobPath(userId, blobId);
  let stats;
  try {
    stats = await fs.promises.stat(filePath);
  } catch {
    return 'missing';
  }
  if (stats.size !== encryptedSize(size)) return 'wrong size';
  const last = Math.max(0, size - 1);
  const ranges = last < SEGMENT_SIZE ? [{ start: 0, end: last }] : [{ start: 0, end: 0 }, { start: last, end: last }];
  try {
    const dek = getUserDek(userId);
    for (const range of ranges) {
      // eslint-disable-next-line no-unused-vars
      for await (const _chunk of createDecryptStream(filePath, dek, blobId, size, range));
    }
  } catch {
    return 'damaged';
  }
  return null;
}

function openBlob(userId, blobId, size, range) {
  return createDecryptStream(blobPath(userId, blobId), getUserDek(userId), blobId, size, range);
}

// Decrypts an encrypted temporary file (an upload part) written by
// writeTemporaryBlob with the same id.
function openTemporaryBlob(userId, filePath, blobId, size) {
  return createDecryptStream(filePath, getUserDek(userId), blobId, size);
}

function previewDirectory(userId, blobId) {
  if (!BLOB_ID_PATTERN.test(blobId)) throw new Error('Invalid blob id.');
  return path.join(PREVIEW_DIR, String(Number(userId)), blobId);
}

// Removes blobs and every preview made from them.
async function deleteBlobFiles(userId, blobIds) {
  for (const blobId of blobIds) {
    await removeQuietly(blobPath(userId, blobId));
    await fs.promises.rm(previewDirectory(userId, blobId), { recursive: true, force: true });
  }
}

async function deleteUserBlobDirectory(userId) {
  await fs.promises.rm(path.join(BLOB_DIR, String(Number(userId))), { recursive: true, force: true });
  await fs.promises.rm(path.join(PREVIEW_DIR, String(Number(userId))), { recursive: true, force: true });
}

module.exports = {
  blobPath,
  checkBlob,
  deleteBlobFiles,
  deleteUserBlobDirectory,
  moveIntoPlace,
  openBlob,
  openTemporaryBlob,
  previewDirectory,
  removeQuietly,
  writeTemporaryBlob,
};
