'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { Readable } = require('stream');
const { pipeline } = require('stream/promises');
const {
  SEGMENT_SIZE,
  createDecryptStream,
  createEncryptStream,
  encryptedSize,
} = require('../../server/services/crypto/stream_cipher');

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-cipher-'));
const dek = crypto.randomBytes(32);

async function encrypt(data, blobId = 'blob-1', pieces = 3) {
  const file = path.join(dir, crypto.randomUUID());
  const step = Math.max(1, Math.ceil(data.length / pieces));
  const chunks = [];
  for (let i = 0; i < data.length; i += step) chunks.push(data.subarray(i, i + step));
  await pipeline(Readable.from(chunks), createEncryptStream(dek, blobId), fs.createWriteStream(file));
  return file;
}

async function decrypt(file, size, range, blobId = 'blob-1') {
  const parts = [];
  for await (const part of createDecryptStream(file, dek, blobId, size, range)) parts.push(part);
  return Buffer.concat(parts);
}

test.after(() => fs.rmSync(dir, { recursive: true, force: true }));

test('round-trips sizes around segment boundaries', async () => {
  for (const size of [0, 1, SEGMENT_SIZE - 1, SEGMENT_SIZE, SEGMENT_SIZE + 1, 3 * SEGMENT_SIZE + 17]) {
    const data = crypto.randomBytes(size);
    const file = await encrypt(data);
    assert.equal(fs.statSync(file).size, encryptedSize(size), `encrypted size for ${size}`);
    assert.deepEqual(await decrypt(file, size), data, `round trip for ${size}`);
  }
});

test('decrypts arbitrary ranges across segments', async () => {
  const size = 3 * SEGMENT_SIZE + 500;
  const data = crypto.randomBytes(size);
  const file = await encrypt(data);
  for (const [start, end] of [[0, 0], [SEGMENT_SIZE - 3, SEGMENT_SIZE + 3], [100, size - 1], [size - 1, size - 1]]) {
    assert.deepEqual(await decrypt(file, size, { start, end }), data.subarray(start, end + 1));
  }
});

test('stores no plaintext', async () => {
  const data = Buffer.from('a very recognisable secret sentence '.repeat(100));
  const file = await encrypt(data);
  assert.equal(fs.readFileSync(file).includes(Buffer.from('recognisable')), false);
});

test('rejects tampered, truncated and misattributed blobs', async () => {
  const size = 2 * SEGMENT_SIZE + 10;
  const data = crypto.randomBytes(size);
  const file = await encrypt(data);

  const tampered = path.join(dir, 'tampered');
  const bytes = fs.readFileSync(file);
  bytes[100] ^= 0xff;
  fs.writeFileSync(tampered, bytes);
  await assert.rejects(decrypt(tampered, size));

  // Dropping the final segment and claiming a shorter size must not pass.
  const truncated = path.join(dir, 'truncated');
  fs.writeFileSync(truncated, fs.readFileSync(file).subarray(0, encryptedSize(2 * SEGMENT_SIZE)));
  await assert.rejects(decrypt(truncated, 2 * SEGMENT_SIZE));

  // A blob only opens under its own id.
  await assert.rejects(decrypt(file, size, undefined, 'blob-2'));
});

function headerWithSegmentSize(segmentSize) {
  const header = Buffer.alloc(20);
  header.write('NCB1', 0, 'ascii');
  header.writeUInt8(1, 4);
  header.writeUInt32BE(segmentSize, 8);
  return header;
}

test('refuses a header with any segment size but the one it writes', async () => {
  // Segment size 0 used to decrypt to nothing without checking a single tag.
  for (const segmentSize of [0, 1, SEGMENT_SIZE - 1, SEGMENT_SIZE * 2, 0xffffffff]) {
    const file = path.join(dir, `segment-${segmentSize}`);
    fs.writeFileSync(file, headerWithSegmentSize(segmentSize));
    await assert.rejects(decrypt(file, 1), /header/, `segment size ${segmentSize}`);
    await assert.rejects(decrypt(file, 1024 * 1024), /header/, `segment size ${segmentSize}`);
  }
});

test('never ends without an authenticated segment or with fewer bytes than asked', async () => {
  const data = crypto.randomBytes(100);
  const file = await encrypt(data);
  // A header and nothing else.
  const headerOnly = path.join(dir, 'header-only');
  fs.writeFileSync(headerOnly, fs.readFileSync(file).subarray(0, 20));
  await assert.rejects(decrypt(headerOnly, 0));
  await assert.rejects(decrypt(headerOnly, 100));
  // Ranges and sizes that cannot be satisfied are errors, not empty output.
  await assert.rejects(decrypt(file, 100, { start: 50, end: 10 }), /range/);
  await assert.rejects(decrypt(file, 100, { start: 0, end: 100 }), /range/);
  await assert.rejects(decrypt(file, Number.NaN), /size/);
  await assert.rejects(decrypt(file, -1), /size/);
});
