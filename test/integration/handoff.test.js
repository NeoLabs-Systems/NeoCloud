'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('http');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { createDeviceToken, revokeDeviceToken } = require('../../server/services/devices/tokens');
const { purgeExpired } = require('../../server/services/handoff/delivery');
const presence = require('../../server/services/handoff/presence');

test.after(cleanup);

let owner;
let other;
let phone;
let desktop;
let tablet;
const events = { phone: [], desktop: [] };

function bearer(device) {
  return (req) => req.set('Authorization', `Bearer ${device.token}`);
}

function as(device, method, url) {
  return bearer(device)(request(app)[method](url));
}

function goOnline(userId, device, log, canReceive = true) {
  return presence.connect(userId, {
    key: `d:${device.device.id}`,
    kind: 'app',
    canReceive,
    push: (event, payload) => log?.push({ event, payload }),
    close: () => {},
  });
}

test.before(async () => {
  owner = await registerUser('handoff');
  other = await registerUser('handoffother');
  phone = createDeviceToken(owner.user.id, { name: 'Anna’s iPhone', platform: 'ios' });
  desktop = createDeviceToken(owner.user.id, { name: 'Office PC', platform: 'windows' });
  tablet = createDeviceToken(owner.user.id, { name: 'Tablet', platform: 'android' });
  goOnline(owner.user.id, phone, events.phone);
  goOnline(owner.user.id, desktop, events.desktop);
  goOnline(owner.user.id, tablet, null);
});

const phoneKey = () => `d:${phone.device.id}`;
const desktopKey = () => `d:${desktop.device.id}`;

async function send(files, { to = desktopKey(), from = phone, contents } = {}) {
  const declared = files.map((name, i) => ({ name, size: Buffer.byteLength(contents?.[i] ?? `data-${i}`) }));
  const res = await as(from, 'post', '/api/handoff/offers').send({ targetId: to, files: declared }).expect(201);
  for (let i = 0; i < files.length; i += 1) {
    await as(from, 'put', `/api/handoff/offers/${res.body.id}/files/${res.body.files[i].id}`)
      .set('Content-Type', 'application/octet-stream').send(Buffer.from(contents?.[i] ?? `data-${i}`)).expect(200);
  }
  return (await as(from, 'get', `/api/handoff/offers/${res.body.id}`).expect(200)).body;
}

// A signed-in app's event stream; `chunks` is everything the server wrote.
async function openStream(device) {
  const server = http.createServer(app).listen(0);
  const chunks = [];
  const req = await new Promise((resolve) => {
    const request_ = http.get({ port: server.address().port, path: '/api/handoff/events', headers: { Authorization: `Bearer ${device.token}` } }, (res) => {
      res.on('data', (chunk) => chunks.push(chunk.toString()));
      resolve(request_);
    });
  });
  const close = async () => {
    req.destroy();
    await new Promise((resolve) => setTimeout(resolve, 100));
    server.close();
  };
  return { chunks, close };
}

function usedBytes() {
  return db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(owner.user.id).used_bytes;
}

test('lists online devices with names and platform, never another account’s', async () => {
  const res = await as(phone, 'get', '/api/handoff/devices').expect(200);
  const byName = Object.fromEntries(res.body.devices.map((d) => [d.name, d]));
  assert.equal(byName['Office PC'].online, true);
  assert.equal(byName['Office PC'].platform, 'windows');
  assert.equal(byName['Anna’s iPhone'].current, true);
  assert.equal(res.body.devices.length, 3);
  const foreign = await owner.agent.get('/api/handoff/devices').expect(200);
  assert.ok(!foreign.body.devices.some((d) => d.current));
  const otherList = await other.agent.get('/api/handoff/devices').expect(200);
  assert.deepEqual(otherList.body.devices, []);
});

test('a send reaches only its target, downloads resumably, and its staged files vanish when delivered', async () => {
  const before = usedBytes();
  const offer = await send(['a.txt', 'b.txt'], { contents: ['hello world', 'second'] });
  assert.equal(offer.status, 'pending');
  assert.ok(usedBytes() > before, 'staged bytes count while waiting');

  const arrived = events.desktop.filter((e) => e.event === 'handoff:offer' && e.payload.id === offer.id);
  assert.equal(arrived.length, 1);
  assert.equal(arrived[0].payload.role, 'receiver');
  assert.ok(!events.phone.some((e) => e.event === 'handoff:offer'), 'the sender is not offered its own send');

  const inbox = await as(desktop, 'get', '/api/handoff/offers').expect(200);
  assert.equal(inbox.body.inbox.length, 1);
  assert.equal((await as(tablet, 'get', '/api/handoff/offers').expect(200)).body.inbox.length, 0);
  await as(tablet, 'get', `/api/handoff/offers/${offer.id}`).expect(404);
  await as(tablet, 'post', `/api/handoff/offers/${offer.id}/accept`).expect(404);

  // Not before accepting; never for anyone else.
  const fileId = offer.files[0].id;
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).expect(409);
  await as(desktop, 'post', `/api/handoff/offers/${offer.id}/accept`).expect(200);
  await as(tablet, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).expect(404);
  await as(phone, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).expect(404);
  await owner.agent.get(`/api/handoff/offers/${offer.id}/files/${fileId}`).expect(404);

  const whole = await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).expect(200);
  assert.equal(whole.body.toString(), 'hello world');
  assert.match(whole.headers['content-disposition'], /attachment; filename="a\.txt"/);
  const resumed = await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).set('Range', 'bytes=6-').expect(206);
  assert.equal(resumed.body.toString(), 'world');
  assert.equal(resumed.headers['content-range'], 'bytes 6-10/11');
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).set('Range', 'bytes=50-').expect(416);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${offer.id}`).expect(200)).body.status, 'accepted', 'one of two files fetched');
  const zip = await as(desktop, 'get', `/api/handoff/offers/${offer.id}/zip`).expect(200);
  assert.equal(zip.headers['content-type'], 'application/zip');
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${offer.id}`).expect(200)).body.status, 'delivered', 'a browser never says it is done');
  const done = await as(desktop, 'post', `/api/handoff/offers/${offer.id}/complete`).expect(200);
  assert.equal(done.body.status, 'delivered');
  assert.equal(usedBytes(), before, 'the staged bytes are released');
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM handoff_files WHERE offer_id = ? AND blob_id IS NOT NULL').get(offer.id).n, 0);
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${fileId}`).expect(409);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${offer.id}`).expect(200)).body.status, 'delivered');
  await new Promise((resolve) => setTimeout(resolve, 100)); // the receiver's confirmation and the sender's push race
  assert.ok(events.phone.some((e) => e.event === 'handoff:updated' && e.payload.offer.status === 'delivered'));
});

test('a send is delivered once every file was streamed to its end, without a confirmation', async () => {
  const before = usedBytes();
  const offer = await send(['one.txt', 'two.txt'], { contents: ['11', '22'] });
  await as(desktop, 'post', `/api/handoff/offers/${offer.id}/accept`).expect(200);
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${offer.files[0].id}`).set('Range', 'bytes=0-0').expect(206);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${offer.id}`).expect(200)).body.status, 'accepted', 'a partial read is not a delivery');
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${offer.files[0].id}`).expect(200);
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${offer.files[1].id}`).expect(200);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${offer.id}`).expect(200)).body.status, 'delivered');
  assert.equal(usedBytes(), before);
});

test('declining or cancelling removes the staged files and tells the sender', async () => {
  const before = usedBytes();
  const declined = await send(['no.txt']);
  await as(desktop, 'post', `/api/handoff/offers/${declined.id}/decline`).expect(200);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${declined.id}`).expect(200)).body.status, 'declined');
  assert.equal(usedBytes(), before);

  const cancelled = await send(['x.txt']);
  await as(tablet, 'delete', `/api/handoff/offers/${cancelled.id}`).expect(404);
  const res = await as(phone, 'delete', `/api/handoff/offers/${cancelled.id}`).expect(200);
  assert.equal(res.body.status, 'cancelled');
  assert.equal(usedBytes(), before);
  await as(desktop, 'post', `/api/handoff/offers/${cancelled.id}/accept`).expect(409);
});

test('an unfinished send is not offered and expires with its files', async () => {
  const before = usedBytes();
  const res = await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), files: [{ name: 'big.bin', size: 5 }] }).expect(201);
  assert.equal(res.body.status, 'uploading');
  await as(desktop, 'get', `/api/handoff/offers/${res.body.id}`).expect(404);
  await as(phone, 'put', `/api/handoff/offers/${res.body.id}/files/${res.body.files[0].id}`).send(Buffer.from('1234')).expect(400);
  await as(phone, 'put', `/api/handoff/offers/${res.body.id}/files/${res.body.files[0].id}`).send(Buffer.from('12345')).expect(200);
  db.prepare("UPDATE handoff_offers SET expires_at = datetime('now', '-1 minute') WHERE id = ?").run(res.body.id);
  assert.equal(await purgeExpired(), 1);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${res.body.id}`).expect(200)).body.status, 'expired');
  assert.equal(usedBytes(), before);
  await as(desktop, 'post', `/api/handoff/offers/${res.body.id}/accept`).expect(409);
});

test('an accepted but never completed send expires too', async () => {
  const offer = await send(['late.txt']);
  await as(desktop, 'post', `/api/handoff/offers/${offer.id}/accept`).expect(200);
  db.prepare("UPDATE handoff_offers SET expires_at = datetime('now', '-1 minute') WHERE id = ?").run(offer.id);
  await as(desktop, 'get', `/api/handoff/offers/${offer.id}/files/${offer.files[0].id}`).expect(410);
  await purgeExpired();
  assert.equal((await as(desktop, 'post', `/api/handoff/offers/${offer.id}/complete`).expect(409)).body.code, 'NOT_ACCEPTED');
});

test('files already in NeoCloud are sent as copies and stay in place', async () => {
  const file = (await upload(owner.agent, 'report.txt', 'quarterly').expect(201)).body;
  const before = usedBytes();
  const res = await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), nodeIds: [file.id] }).expect(201);
  assert.equal(res.body.status, 'pending');
  assert.equal(usedBytes(), before, 'a copy shares the blob');
  await as(desktop, 'post', `/api/handoff/offers/${res.body.id}/accept`).expect(200);
  const got = await as(desktop, 'get', `/api/handoff/offers/${res.body.id}/files/${res.body.files[0].id}`).expect(200);
  assert.equal(got.body.toString(), 'quarterly');
  await as(desktop, 'post', `/api/handoff/offers/${res.body.id}/complete`).expect(200);
  const still = await owner.agent.get(`/api/files/${file.id}/content`).expect(200);
  assert.equal(still.text, 'quarterly');
  assert.equal(usedBytes(), before);
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), nodeIds: [(await upload(other.agent, 'theirs.txt', 'x').expect(201)).body.id] }).expect(404);
});

test('another account’s ids and devices are 404 and nothing leaks', async () => {
  const offer = await send(['secret.txt']);
  await other.agent.get(`/api/handoff/offers/${offer.id}`).expect(404);
  await other.agent.post(`/api/handoff/offers/${offer.id}/accept`).expect(404);
  await other.agent.delete(`/api/handoff/offers/${offer.id}`).expect(404);
  await other.agent.get(`/api/handoff/offers/${offer.id}/zip`).expect(404);
  await other.agent.put(`/api/handoff/offers/${offer.id}/files/${offer.files[0].id}`).send(Buffer.from('x')).expect(404);
  await other.agent.post('/api/handoff/offers').send({ targetId: desktopKey(), files: [{ name: 'a', size: 1 }] }).expect(404);
  assert.deepEqual((await other.agent.get('/api/handoff/offers').expect(200)).body, { sent: [], inbox: [] });
  await as(phone, 'delete', `/api/handoff/offers/${offer.id}`).expect(200);
});

test('rules: caps, names, self-send, offline and non-receiving devices', async () => {
  const many = Array.from({ length: 51 }, (_, i) => ({ name: `f${i}`, size: 1 }));
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), files: many }).expect(400);
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), files: [{ name: 'huge', size: 3 * 1024 ** 3 }] }).expect(413);
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), files: [] }).expect(400);
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: desktopKey(), files: [{ name: 'a', size: -1 }] }).expect(400);
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: phoneKey(), files: [{ name: 'a', size: 1 }] }).expect(400);
  await as(phone, 'post', '/api/handoff/offers').send({ targetId: 'd:99999', files: [{ name: 'a', size: 1 }] }).expect(404);

  const sneaky = await as(phone, 'post', '/api/handoff/offers').send({
    targetId: desktopKey(),
    files: ['../../etc/passwd', 'a/b\\c.txt', 'x‮txt.exe', 'CON', 'same', 'SAME', '..', ''].map((name) => ({ name, size: 1 })),
  }).expect(201);
  const names = sneaky.body.files.map((f) => f.name);
  assert.deepEqual(names, ['.._.._etc_passwd'.replace(/^\.+/, ''), 'a_b_c.txt', 'xtxt.exe', '_CON', 'same', 'SAME (1)', 'file', 'file (1)']);
  for (const name of names) assert.ok(!/[/\\‮]/.test(name) && !name.startsWith('.'));
  await as(phone, 'delete', `/api/handoff/offers/${sneaky.body.id}`).expect(200);

  const quiet = createDeviceToken(owner.user.id, { name: 'Quiet', platform: 'linux' });
  const off = goOnline(owner.user.id, quiet, null, false);
  const refused = await as(phone, 'post', '/api/handoff/offers').send({ targetId: `d:${quiet.device.id}`, files: [{ name: 'a', size: 1 }] }).expect(409);
  assert.equal(refused.body.code, 'DEVICE_NOT_RECEIVING');
  off();
});

test('an app that is offline but was seen recently still receives, once it reconnects', async () => {
  const sleeper = createDeviceToken(owner.user.id, { name: 'Sleeper', platform: 'android' });
  db.prepare("UPDATE device_tokens SET last_seen_at = datetime('now') WHERE id = ?").run(sleeper.device.id);
  const listed = (await as(phone, 'get', '/api/handoff/devices').expect(200)).body.devices.find((d) => d.name === 'Sleeper');
  assert.equal(listed.online, false);
  const offer = await send(['later.txt'], { to: listed.id });
  const stream = await openStream(sleeper);
  await new Promise((resolve) => setTimeout(resolve, 100));
  assert.match(stream.chunks.join(''), new RegExp(`event: handoff:offer\\ndata: .*${offer.id}`));
  await stream.close();
  await as(phone, 'delete', `/api/handoff/offers/${offer.id}`).expect(200);
});

test('revoking a device cancels its sends and closes its connection', async () => {
  const doomed = createDeviceToken(owner.user.id, { name: 'Doomed', platform: 'linux' });
  let closed = 0;
  presence.connect(owner.user.id, { key: `d:${doomed.device.id}`, kind: 'app', canReceive: true, push: () => {}, close: () => { closed += 1; } });
  const offer = await send(['bye.txt'], { to: `d:${doomed.device.id}` });
  const before = usedBytes();
  revokeDeviceToken(owner.user.id, doomed.device.id);
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.equal(closed, 1);
  assert.equal((await as(phone, 'get', `/api/handoff/offers/${offer.id}`).expect(200)).body.status, 'cancelled');
  assert.ok(usedBytes() < before);
  await as(doomed, 'get', '/api/handoff/devices').expect(401);
});

test('the event stream announces presence and delivers offers to a signed-in app', async () => {
  const watcher = createDeviceToken(owner.user.id, { name: 'Laptop', platform: 'macos' });
  const stream = await openStream(watcher);
  try {
    const online = (await as(phone, 'get', '/api/handoff/devices').expect(200)).body.devices.find((d) => d.name === 'Laptop');
    assert.equal(online.online, true);
    const offer = await send(['stream.txt'], { to: online.id });
    await new Promise((resolve) => setTimeout(resolve, 100));
    assert.match(stream.chunks.join(''), new RegExp(`event: handoff:offer\\ndata: .*${offer.id}`));
    await as(phone, 'delete', `/api/handoff/offers/${offer.id}`).expect(200);
  } finally {
    await stream.close();
  }
  const gone = (await as(phone, 'get', '/api/handoff/devices').expect(200)).body.devices.find((d) => d.name === 'Laptop');
  assert.equal(gone.online, false);
});

test('webdav app passwords and signed-out callers get nothing', async () => {
  const dav = createDeviceToken(owner.user.id, { name: 'dav', platform: 'webdav' });
  await as(dav, 'get', '/api/handoff/devices').expect(401);
  await request(app).get('/api/handoff/devices').expect(401);
});
