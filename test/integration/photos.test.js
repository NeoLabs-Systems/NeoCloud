'use strict';

// The Photos timeline: ordering by the time a photo was taken (EXIF, the
// video's own date, the file name, the modification time), keyset paging,
// day jumps, the summary counts, and what stays out: vaults, the trash,
// other files, other accounts.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const sharp = require('sharp');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const { createDeviceToken } = require('../../server/services/devices/tokens');
const { runPhotoIndexerUntilIdle } = require('../../server/services/photos/indexer');
const db = require('../../server/db/database');

test.after(cleanup);

let alice;
let bob;

function jpeg({ width = 40, height = 30, taken = null } = {}) {
  let image = sharp({ create: { width, height, channels: 3, background: '#7a8899' } }).jpeg();
  if (taken) image = image.withExif({ IFD2: { DateTimeOriginal: taken } });
  return image.toBuffer();
}

function box(type, ...parts) {
  const body = Buffer.concat(parts);
  const header = Buffer.alloc(8);
  header.writeUInt32BE(8 + body.length, 0);
  header.write(type, 4, 'latin1');
  return Buffer.concat([header, body]);
}

/** A movie with its `moov` box after the media data, as phones write it. */
function movie({ recordedAt, seconds, width, height }) {
  const mvhd = Buffer.alloc(100);
  mvhd.writeUInt32BE(Math.floor(Date.parse(`${recordedAt}Z`) / 1000) + 2082844800, 4);
  mvhd.writeUInt32BE(1000, 12);
  mvhd.writeUInt32BE(seconds * 1000, 16);
  const tkhd = Buffer.alloc(84);
  // Turned 90°: the second matrix value is set.
  tkhd.writeUInt32BE(65536, 44);
  tkhd.writeUInt32BE(width * 65536, 76);
  tkhd.writeUInt32BE(height * 65536, 80);
  return Buffer.concat([
    box('ftyp', Buffer.from('isom\0\0\0\0isom', 'latin1')),
    box('mdat', crypto.randomBytes(4096)),
    box('moov', box('mvhd', mvhd), box('trak', box('tkhd', tkhd))),
  ]);
}

async function uploadFile(agent, name, content, parentId = 'root') {
  const res = await upload(agent, name, content, { parentId }).expect(201);
  return res.body;
}

async function allPages(agent, limit) {
  const items = [];
  let cursor = null;
  for (;;) {
    const query = new URLSearchParams({ limit: String(limit), ...(cursor ? { cursor } : {}) });
    const page = (await agent.get(`/api/photos?${query}`).expect(200)).body;
    items.push(...page.items);
    if (!page.nextCursor) return items;
    cursor = page.nextCursor;
  }
}

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
});

test('orders photos and videos by when they were taken', async () => {
  const old = await uploadFile(alice.agent, 'beach.jpg', await jpeg({ width: 60, height: 40, taken: '2019:05:04 12:34:56' }));
  const named = await uploadFile(alice.agent, 'IMG_20210101_101010.png', await sharp({ create: { width: 8, height: 8, channels: 3, background: '#000' } }).png().toBuffer());
  const clip = await uploadFile(alice.agent, 'clip.mp4', movie({ recordedAt: '2020-07-01T08:00:00', seconds: 42, width: 1920, height: 1080 }));
  const fresh = await uploadFile(alice.agent, 'today.jpg', await jpeg());
  await uploadFile(alice.agent, 'notes.txt', 'not a photo');
  await uploadFile(alice.agent, 'logo.svg', '<svg xmlns="http://www.w3.org/2000/svg"/>');

  // Before any file was read, uploads show at once, dated by name or time.
  const early = (await alice.agent.get('/api/photos').expect(200)).body.items;
  assert.deepEqual(new Set(early.map((item) => item.id)), new Set([old.id, named.id, clip.id, fresh.id]));
  assert.equal(early.find((item) => item.id === named.id).takenAt, '2021-01-01T10:10:10');

  await runPhotoIndexerUntilIdle();
  const { items, nextCursor } = (await alice.agent.get('/api/photos').expect(200)).body;
  assert.equal(nextCursor, null);
  assert.deepEqual(items.map((item) => item.id), [fresh.id, named.id, clip.id, old.id]);
  const beach = items.find((item) => item.id === old.id);
  assert.equal(beach.takenAt, '2019-05-04T12:34:56');
  assert.deepEqual([beach.kind, beach.width, beach.height, beach.name, beach.mime], ['photo', 60, 40, 'beach.jpg', 'image/jpeg']);
  assert.equal(beach.parentId, old.parentId);
  assert.equal(beach.etag, old.etag);
  const video = items.find((item) => item.id === clip.id);
  assert.deepEqual(
    [video.kind, video.takenAt, video.durationSeconds, video.width, video.height],
    ['video', '2020-07-01T08:00:00', 42, 1080, 1920],
  );
});

test('the grid thumbnail is made ahead of time and cached for good by etag', async () => {
  const photo = (await alice.agent.get('/api/photos').expect(200)).body.items.find((item) => item.name === 'beach.jpg');
  const node = db.prepare('SELECT blob_id FROM nodes WHERE id = ?').get(photo.id);
  assert.ok(db.prepare("SELECT 1 FROM previews WHERE blob_id = ? AND variant = 'thumb-square'").get(node.blob_id));
  const res = await alice.agent.get(`/api/files/${photo.id}/thumbnail?size=square&v=${photo.etag}`).expect(200);
  assert.equal(res.headers['content-type'], 'image/webp');
  assert.match(res.headers['cache-control'], /immutable/);
  const square = await sharp(res.body).metadata();
  assert.equal(square.width, square.height);
  const unpinned = await alice.agent.get(`/api/files/${photo.id}/thumbnail?size=square`).expect(200);
  assert.doesNotMatch(unpinned.headers['cache-control'], /immutable/);
  await alice.agent.get(`/api/files/${photo.id}/thumbnail?size=huge`).expect(400);
});

test('pages continue from a cursor without gaps or repeats', async () => {
  const folder = (await alice.agent.post('/api/folders').send({ name: 'Burst' }).expect(201)).body;
  for (let n = 0; n < 23; n += 1) {
    // Same second for some, so the node id breaks the tie.
    await uploadFile(alice.agent, `IMG_20220202_1200${String(n % 5).padStart(2, '0')}_${n}.jpg`, await jpeg(), folder.id);
  }
  const expected = (await alice.agent.get('/api/photos?limit=500').expect(200)).body.items.map((item) => item.id);
  assert.equal(expected.length, 27);
  const paged = (await allPages(alice.agent, 4)).map((item) => item.id);
  assert.deepEqual(paged, expected);
  await alice.agent.get('/api/photos?cursor=nonsense').expect(400);
  await alice.agent.get('/api/photos?limit=0').expect(400);
  await alice.agent.get('/api/photos?day=2022-2-2').expect(400);
});

test('a day starts the page at that day', async () => {
  const page = (await alice.agent.get('/api/photos?day=2021-06-30&limit=2').expect(200)).body;
  assert.deepEqual(page.items.map((item) => item.takenAt), ['2021-01-01T10:10:10', '2020-07-01T08:00:00']);
  assert.ok(page.nextCursor);
  const rest = (await alice.agent.get(`/api/photos?cursor=${page.nextCursor}`).expect(200)).body;
  assert.deepEqual(rest.items.map((item) => item.takenAt), ['2019-05-04T12:34:56']);
  assert.equal(rest.nextCursor, null);
});

test('the summary counts photos per day and month', async () => {
  await runPhotoIndexerUntilIdle();
  const summary = (await alice.agent.get('/api/photos/summary').expect(200)).body;
  assert.equal(summary.total, 27);
  assert.equal(summary.pending, 0);
  assert.deepEqual(summary.days.slice(1), [
    { date: '2022-02-02', count: 23 },
    { date: '2021-01-01', count: 1 },
    { date: '2020-07-01', count: 1 },
    { date: '2019-05-04', count: 1 },
  ]);
  assert.equal(summary.days[0].count, 1);
  assert.deepEqual(summary.months.slice(1), [
    { month: '2022-02', count: 23 },
    { month: '2021-01', count: 1 },
    { month: '2020-07', count: 1 },
    { month: '2019-05', count: 1 },
  ]);
});

test('trash, vaults and other file types stay out; restores come back', async () => {
  const items = (await alice.agent.get('/api/photos?limit=500').expect(200)).body.items;
  const beach = items.find((item) => item.name === 'beach.jpg');
  const burst = items.find((item) => item.name.startsWith('IMG_20220202')).parentId;

  await alice.agent.delete(`/api/nodes/${beach.id}`).expect(200);
  await alice.agent.delete(`/api/nodes/${burst}`).expect(200);
  let summary = (await alice.agent.get('/api/photos/summary').expect(200)).body;
  assert.equal(summary.total, 3);
  let ids = (await alice.agent.get('/api/photos').expect(200)).body.items.map((item) => item.id);
  assert.ok(!ids.includes(beach.id));

  await alice.agent.post(`/api/trash/${burst}/restore`).expect(200);
  summary = (await alice.agent.get('/api/photos/summary').expect(200)).body;
  assert.equal(summary.total, 26);

  // Renamed to another type: no longer a photo.
  const named = items.find((item) => item.name === 'IMG_20210101_101010.png');
  await alice.agent.patch(`/api/nodes/${named.id}`).send({ name: 'IMG_20210101_101010.txt' }).expect(200);
  ids = (await alice.agent.get('/api/photos?limit=500').expect(200)).body.items.map((item) => item.id);
  assert.ok(!ids.includes(named.id));

  const vault = (await alice.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Private',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  const hidden = await uploadFile(alice.agent, 'secret.jpg', await jpeg(), vault.folderId);
  ids = (await alice.agent.get('/api/photos?limit=500').expect(200)).body.items.map((item) => item.id);
  assert.ok(!ids.includes(hidden.id));
  assert.equal((await alice.agent.get('/api/photos/summary').expect(200)).body.total, 25);
});

test('accounts see only their own photos', async () => {
  const mine = await uploadFile(bob.agent, 'bob.jpg', await jpeg({ taken: '2018:01:01 00:00:00' }));
  const bobs = (await bob.agent.get('/api/photos').expect(200)).body;
  assert.deepEqual(bobs.items.map((item) => item.id), [mine.id]);
  assert.equal((await bob.agent.get('/api/photos/summary').expect(200)).body.total, 1);
  const alices = (await alice.agent.get('/api/photos?limit=500').expect(200)).body.items.map((item) => item.id);
  assert.ok(!alices.includes(mine.id));
  // Another account's cursor only moves the start: it never reveals rows.
  const cursor = Buffer.from(JSON.stringify(['2018-01-01T00:00:01', mine.id])).toString('base64url');
  assert.deepEqual((await alice.agent.get(`/api/photos?cursor=${cursor}`).expect(200)).body.items, []);
  await request(app).get('/api/photos').expect(401);
  await request(app).get('/api/photos/summary').expect(401);
});

test('the phone app reads the timeline; desktop tokens do not', async () => {
  const phone = createDeviceToken(alice.user.id, { name: 'Phone', platform: 'android' });
  const desktop = createDeviceToken(alice.user.id, { name: 'Laptop', platform: 'macos' });
  const res = await request(app).get('/api/photos/summary').set('Authorization', `Bearer ${phone.token}`).expect(200);
  assert.equal(res.body.total, 25);
  await request(app).get('/api/photos').set('Authorization', `Bearer ${phone.token}`).expect(200);
  const refused = await request(app).get('/api/photos').set('Authorization', `Bearer ${desktop.token}`);
  assert.ok([401, 403].includes(refused.status));
});
