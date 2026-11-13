'use strict';

// Review › Duplicates: exact copies are known at once; similar photos are
// known once the background worker hashed them, and the answer says how far
// it got (pending, hashed, total). The worker starts and stops cleanly.

const test = require('node:test');
const assert = require('node:assert/strict');
const sharp = require('sharp');
const { cleanup, registerUser, upload } = require('../helpers/app');
const {
  runDuplicateHashingUntilIdle,
  startDuplicateHasher,
  stopDuplicateHasher,
} = require('../../server/services/ai/duplicates');

test.after(async () => {
  await stopDuplicateHasher();
  cleanup();
});

/** A picture with enough detail for a dHash: a pseudo-random grid of grays. */
function pattern(seed, quality) {
  const width = 90;
  const height = 80;
  const pixels = Buffer.alloc(width * height);
  let state = seed;
  const cells = [];
  for (let i = 0; i < 9 * 8; i += 1) {
    state = (state * 1103515245 + 12345) % 2147483648;
    cells.push(state % 256);
  }
  for (let y = 0; y < height; y += 1) {
    for (let x = 0; x < width; x += 1) pixels[y * width + x] = cells[Math.floor(y / 10) * 9 + Math.floor(x / 10)];
  }
  return sharp(pixels, { raw: { width, height, channels: 1 } }).jpeg({ quality }).toBuffer();
}

test('similar photos appear once hashed, with progress meanwhile', async () => {
  const alice = await registerUser('alice');
  await upload(alice.agent, 'a.jpg', await pattern(7, 95)).expect(201);
  await upload(alice.agent, 'b.jpg', await pattern(7, 60)).expect(201);
  await upload(alice.agent, 'other.jpg', await pattern(99, 90)).expect(201);
  await upload(alice.agent, 'copy1.txt', 'same text').expect(201);
  await upload(alice.agent, 'copy2.txt', 'same text').expect(201);

  const before = (await alice.agent.get('/api/ai/duplicates').expect(200)).body;
  assert.deepEqual([before.pending, before.hashed, before.total], [3, 0, 3]);
  assert.deepEqual(before.groups.map((group) => group.kind), ['exact']);

  assert.equal(await runDuplicateHashingUntilIdle(), 3);
  const after = (await alice.agent.get('/api/ai/duplicates').expect(200)).body;
  assert.deepEqual([after.pending, after.hashed, after.total], [0, 3, 3]);
  const similar = after.groups.filter((group) => group.kind === 'similar');
  assert.equal(similar.length, 1);
  assert.deepEqual(similar[0].items.map((item) => item.node.name).sort(), ['a.jpg', 'b.jpg']);

  // Nothing left: another run hashes nothing.
  assert.equal(await runDuplicateHashingUntilIdle(), 0);
});

test('the worker starts and stops without holding the process', async () => {
  const stop = startDuplicateHasher();
  const started = Date.now();
  await stop();
  assert.ok(Date.now() - started < 1000);
});
