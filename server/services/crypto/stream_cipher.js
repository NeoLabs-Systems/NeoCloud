'use strict';

// Segmented AES-256-GCM for file contents.
//
// Layout on disk:
//   header  (20 bytes)  "NCB1" | version u8 | 3 zero bytes | segment size u32 BE | nonce prefix (8)
//   segment i           ciphertext of plaintext bytes [i*S, (i+1)*S) | 16-byte tag
//
// Each blob gets its own key, HKDF(account DEK, salt = blob id), so nonces
// never repeat across blobs. Nonce i = prefix | i (u32 BE). The AAD of a
// segment binds the header, its index and whether it is the last one, so
// segments cannot be reordered, swapped between blobs or cut off the end.
// Any plaintext byte range decrypts by reading only the segments it touches,
// which is what HTTP Range requests (video seeking, PDF viewers) need.

const crypto = require('crypto');
const fs = require('fs');
const { Readable, Transform } = require('stream');

const MAGIC = Buffer.from('NCB1', 'ascii');
const FORMAT_VERSION = 1;
const HEADER_LENGTH = 20;
const TAG_LENGTH = 16;
const SEGMENT_SIZE = 64 * 1024;

function blobKey(dek, blobId) {
  return Buffer.from(crypto.hkdfSync('sha256', dek, Buffer.from(String(blobId)), 'neocloud:blob', 32));
}

function buildHeader(noncePrefix) {
  const header = Buffer.alloc(HEADER_LENGTH);
  MAGIC.copy(header, 0);
  header.writeUInt8(FORMAT_VERSION, 4);
  header.writeUInt32BE(SEGMENT_SIZE, 8);
  noncePrefix.copy(header, 12);
  return header;
}

function parseHeader(header) {
  if (header.length !== HEADER_LENGTH || !header.subarray(0, 4).equals(MAGIC)) {
    throw new Error('Blob header is missing or damaged.');
  }
  if (header.readUInt8(4) !== FORMAT_VERSION) {
    throw new Error(`Blob format version ${header.readUInt8(4)} is not supported.`);
  }
  // The header is not authenticated on its own; only SEGMENT_SIZE is ever
  // written, and any other value (0 above all) would skip the tag checks.
  if (header.readUInt32BE(8) !== SEGMENT_SIZE) throw new Error('Blob header is missing or damaged.');
  return { noncePrefix: header.subarray(12, 20) };
}

function segmentNonce(noncePrefix, index) {
  const nonce = Buffer.alloc(12);
  noncePrefix.copy(nonce, 0);
  nonce.writeUInt32BE(index, 8);
  return nonce;
}

function segmentAad(header, index, isFinal) {
  const suffix = Buffer.alloc(5);
  suffix.writeUInt32BE(index, 0);
  suffix.writeUInt8(isFinal ? 1 : 0, 4);
  return Buffer.concat([header, suffix]);
}

function segmentCount(size) {
  return size === 0 ? 1 : Math.ceil(size / SEGMENT_SIZE);
}

/** Bytes on disk for a blob of `size` plaintext bytes. */
function encryptedSize(size) {
  return HEADER_LENGTH + size + segmentCount(size) * TAG_LENGTH;
}

/**
 * A Transform that turns plaintext into the on-disk format. The last full
 * segment is held back until more data or the end arrives, because only then
 * is it known whether it is the final one.
 */
function createEncryptStream(dek, blobId) {
  const key = blobKey(dek, blobId);
  const noncePrefix = crypto.randomBytes(8);
  const header = buildHeader(noncePrefix);
  let pending = Buffer.alloc(0);
  let index = 0;
  let headerWritten = false;

  function sealSegment(plaintext, isFinal) {
    const cipher = crypto.createCipheriv('aes-256-gcm', key, segmentNonce(noncePrefix, index));
    cipher.setAAD(segmentAad(header, index, isFinal));
    const sealed = Buffer.concat([cipher.update(plaintext), cipher.final(), cipher.getAuthTag()]);
    index += 1;
    return sealed;
  }

  return new Transform({
    transform(chunk, _encoding, callback) {
      try {
        if (!headerWritten) {
          this.push(header);
          headerWritten = true;
        }
        pending = pending.length ? Buffer.concat([pending, chunk]) : chunk;
        while (pending.length > SEGMENT_SIZE) {
          this.push(sealSegment(pending.subarray(0, SEGMENT_SIZE), false));
          pending = pending.subarray(SEGMENT_SIZE);
        }
        callback();
      } catch (err) {
        callback(err);
      }
    },
    flush(callback) {
      try {
        if (!headerWritten) this.push(header);
        this.push(sealSegment(pending, true));
        callback();
      } catch (err) {
        callback(err);
      }
    },
  });
}

async function readExactly(handle, length, position) {
  const buffer = Buffer.alloc(length);
  let offset = 0;
  while (offset < length) {
    const { bytesRead } = await handle.read(buffer, offset, length - offset, position + offset);
    if (bytesRead === 0) throw new Error('Blob is shorter than its recorded size.');
    offset += bytesRead;
  }
  return buffer;
}

function assertRange(size, start, end) {
  if (!Number.isSafeInteger(size) || size < 0) throw new Error('Invalid blob size.');
  if (size === 0) return;
  const valid = Number.isSafeInteger(start) && Number.isSafeInteger(end) && start >= 0 && start <= end && end < size;
  if (!valid) throw new Error('Invalid byte range.');
}

async function* decryptSegments(filePath, dek, blobId, size, start, end) {
  assertRange(size, start, end);
  const handle = await fs.promises.open(filePath, 'r');
  try {
    const header = await readExactly(handle, HEADER_LENGTH, 0);
    const { noncePrefix } = parseHeader(header);
    const key = blobKey(dek, blobId);
    const lastIndex = segmentCount(size) - 1;
    const firstIndex = size === 0 ? 0 : Math.floor(start / SEGMENT_SIZE);
    const finalIndex = size === 0 ? 0 : Math.floor(end / SEGMENT_SIZE);
    let authenticated = 0;
    let produced = 0;

    for (let index = firstIndex; index <= finalIndex; index += 1) {
      const plainStart = index * SEGMENT_SIZE;
      const plainLength = Math.min(SEGMENT_SIZE, size - plainStart);
      const position = HEADER_LENGTH + index * (SEGMENT_SIZE + TAG_LENGTH);
      const sealed = await readExactly(handle, plainLength + TAG_LENGTH, position);
      const decipher = crypto.createDecipheriv('aes-256-gcm', key, segmentNonce(noncePrefix, index));
      decipher.setAAD(segmentAad(header, index, index === lastIndex));
      decipher.setAuthTag(sealed.subarray(plainLength));
      const plaintext = Buffer.concat([decipher.update(sealed.subarray(0, plainLength)), decipher.final()]);
      authenticated += 1;
      if (size === 0) break;
      const from = Math.max(start, plainStart) - plainStart;
      const to = Math.min(end, plainStart + plainLength - 1) - plainStart + 1;
      produced += to - from;
      yield plaintext.subarray(from, to);
    }
    // Belt and braces: returning without a checked tag, or with fewer bytes
    // than asked for, must never look like success.
    const expected = size === 0 ? 0 : end - start + 1;
    if (authenticated === 0 || produced !== expected) throw new Error('Blob did not decrypt to its recorded size.');
  } finally {
    await handle.close();
  }
}

/**
 * Plaintext bytes [start, end] (inclusive) of a blob as a Readable. Every
 * segment read is authenticated; a tampered or truncated blob errors the
 * stream instead of yielding altered bytes.
 */
function createDecryptStream(filePath, dek, blobId, size, { start = 0, end = size - 1 } = {}) {
  return Readable.from(decryptSegments(filePath, dek, blobId, size, start, end), { objectMode: false });
}

module.exports = {
  HEADER_LENGTH,
  SEGMENT_SIZE,
  TAG_LENGTH,
  createDecryptStream,
  createEncryptStream,
  encryptedSize,
};
