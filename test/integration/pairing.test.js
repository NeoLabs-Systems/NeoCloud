'use strict';

const crypto = require('crypto');
const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');
const { attachSocketServer } = require('../../server/services/events');

function hashToken(code) {
  return crypto.createHash('sha256').update(code).digest('hex');
}

test.after(cleanup);

let owner;
let other;
const emitted = [];

test.before(async () => {
  owner = await registerUser('pairing');
  other = await registerUser('pairother');
  attachSocketServer({ to: (room) => ({ emit: (event, payload) => emitted.push({ room, event, payload }) }) });
});

test.after(() => attachSocketServer(null));

function pair(body) {
  return request(app).post('/api/devices/pair').send(body);
}

function expire(code) {
  db.prepare("UPDATE device_pairings SET expires_at = datetime('now', '-1 second') WHERE code_hash = ?").run(hashToken(code));
}

test('only a signed-in session can create or read pairing codes', async () => {
  await request(app).post('/api/account/devices/pairing').send({}).expect(401);
  await request(app).get('/api/account/devices/pairing/whatever').expect(401);
});

test('a pairing code is long, short-lived and only stored hashed', async () => {
  const res = await owner.agent.post('/api/account/devices/pairing').send({}).expect(200);
  const { code, expiresAt, server, qrPayload } = res.body;
  assert.match(code, /^[A-Za-z0-9_-]{32,}$/);
  assert.match(server, /^http:\/\/127\.0\.0\.1:\d+$/);
  const qr = new URL(qrPayload);
  assert.equal(qr.protocol, 'neocloud:');
  assert.equal(qr.host, 'pair');
  assert.equal(qr.searchParams.get('server'), server);
  assert.equal(qr.searchParams.get('code'), code);

  const row = db.prepare('SELECT * FROM device_pairings WHERE code_hash = ?').get(hashToken(code));
  assert.ok(row, 'the hash is stored');
  assert.equal(row.expires_at, expiresAt);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM device_pairings WHERE code_hash = ?').get(code).n, 0);
  const lifetime = db.prepare("SELECT (julianday(expires_at) - julianday(created_at)) * 86400 AS s FROM device_pairings WHERE id = ?").get(row.id).s;
  assert.ok(Math.abs(lifetime - 300) < 2, `lives 5 minutes, got ${lifetime}`);

  const status = await owner.agent.get(`/api/account/devices/pairing/${code}`).expect(200);
  assert.deepEqual(status.body, { status: 'pending' });
  // Another account can't see it.
  await other.agent.get(`/api/account/devices/pairing/${code}`).expect(404);
});

test('web hints are validated', async () => {
  const bad = await owner.agent.post('/api/account/devices/pairing').send({ platform: 'amiga' }).expect(400);
  assert.equal(bad.body.code, 'PLATFORM_INVALID');
});

test('redeeming issues a working token, once, and tells the web page', async () => {
  const { body: start } = await owner.agent.post('/api/account/devices/pairing').send({}).expect(200);
  emitted.length = 0;

  const missingName = await pair({ code: start.code, platform: 'android' }).expect(400);
  assert.equal(missingName.body.code, 'NAME_REQUIRED');

  const res = await pair({ code: start.code, name: 'Pixel 9', platform: 'android' }).expect(200);
  assert.match(res.body.token, /^nc_/);
  assert.equal(res.body.device.name, 'Pixel 9');
  assert.equal(res.body.device.platform, 'android');
  assert.deepEqual(res.body.user, { id: owner.user.id, username: owner.username });

  const me = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${res.body.token}`).expect(200);
  assert.equal(me.body.device.id, res.body.device.id);
  assert.equal(me.body.user.id, owner.user.id);
  await request(app).get('/api/folders/root/children').set('Authorization', `Bearer ${res.body.token}`).expect(200);

  assert.deepEqual(emitted, [{
    room: `user:${owner.user.id}`,
    event: 'devices:paired',
    payload: { device: res.body.device },
  }]);

  const status = await owner.agent.get(`/api/account/devices/pairing/${start.code}`).expect(200);
  assert.deepEqual(status.body, { status: 'used', device: res.body.device });

  // Single use.
  const again = await pair({ code: start.code, name: 'Thief', platform: 'android' }).expect(410);
  assert.equal(again.body.code, 'EXPIRED_TOKEN');
  const devices = await owner.agent.get('/api/account/devices').expect(200);
  assert.equal(devices.body.devices.filter((d) => d.name === 'Thief').length, 0);
});

test('the web page may name the device for the phone', async () => {
  const { body: start } = await owner.agent.post('/api/account/devices/pairing')
    .send({ name: 'Tablet', platform: 'ios' }).expect(200);
  const res = await pair({ code: start.code }).expect(200);
  assert.equal(res.body.device.name, 'Tablet');
  assert.equal(res.body.device.platform, 'ios');
});

test('expired, unknown and missing codes are 410', async () => {
  const { body: start } = await owner.agent.post('/api/account/devices/pairing').send({}).expect(200);
  expire(start.code);
  const expired = await pair({ code: start.code, name: 'Late', platform: 'android' }).expect(410);
  assert.equal(expired.body.code, 'EXPIRED_TOKEN');
  const status = await owner.agent.get(`/api/account/devices/pairing/${start.code}`).expect(200);
  assert.deepEqual(status.body, { status: 'expired' });

  const wrong = await pair({ code: 'x'.repeat(43), name: 'Guess', platform: 'android' }).expect(410);
  assert.equal(wrong.body.code, 'EXPIRED_TOKEN');
  const missing = await pair({ name: 'Nothing', platform: 'android' }).expect(410);
  assert.equal(missing.body.code, 'EXPIRED_TOKEN');
  const notString = await pair({ code: { $ne: '' }, name: 'Obj', platform: 'android' }).expect(410);
  assert.equal(notString.body.code, 'EXPIRED_TOKEN');
});

test('each account has at most five open codes', async () => {
  const fresh = await registerUser('paircap');
  const codes = [];
  for (let i = 0; i < 5; i += 1) {
    codes.push((await fresh.agent.post('/api/account/devices/pairing').send({}).expect(200)).body.code);
  }
  const capped = await fresh.agent.post('/api/account/devices/pairing').send({}).expect(429);
  assert.equal(capped.body.code, 'TOO_MANY_PAIRING_CODES');

  // A used or expired code frees a slot.
  await pair({ code: codes[0], name: 'Phone', platform: 'android' }).expect(200);
  await fresh.agent.post('/api/account/devices/pairing').send({}).expect(200);
  expire(codes[1]);
  await fresh.agent.post('/api/account/devices/pairing').send({}).expect(200);
  await fresh.agent.post('/api/account/devices/pairing').send({}).expect(429);
});

// Last: it spends this address's budget on /api/devices/pair.
test('redeeming is rate-limited per address', async () => {
  let limited = null;
  for (let i = 0; i < 40 && !limited; i += 1) {
    const res = await pair({ code: `guess-${i}`, name: 'Bot', platform: 'android' });
    if (res.status === 429) limited = res;
    else assert.equal(res.status, 410);
  }
  assert.ok(limited, 'expected a 429 within 40 attempts');
  assert.equal(limited.body.code, 'RATE_LIMITED');
});
