'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

const USER_CODE = /^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$/;

let owner;

test.before(async () => {
  owner = await registerUser('devices');
});

function startLogin(body = { name: 'Work laptop', platform: 'macos' }) {
  return request(app).post('/api/devices/authorize').send(body);
}

function poll(deviceCode) {
  return request(app).post('/api/devices/token').send({ deviceCode });
}

test('authorize hands out a code pair without an account', async () => {
  const res = await startLogin().expect(200);
  assert.match(res.body.userCode, USER_CODE);
  assert.ok(res.body.deviceCode.length >= 40);
  assert.equal(res.body.expiresIn, 600);
  assert.equal(res.body.interval, 3);
  assert.match(res.body.verificationUrl, /^http:\/\/127\.0\.0\.1:\d+\/app\/device\?code=[A-Z2-9]{4}-[A-Z2-9]{4}$/);
  assert.ok(res.body.verificationUrl.endsWith(res.body.userCode));

  const missing = await startLogin({ platform: 'macos' }).expect(400);
  assert.equal(missing.body.code, 'NAME_REQUIRED');
  const platform = await startLogin({ name: 'x', platform: 'amiga' }).expect(400);
  assert.equal(platform.body.code, 'PLATFORM_INVALID');
});

test('a browser on a foreign origin can’t start a device login', async () => {
  const res = await request(app)
    .post('/api/devices/authorize')
    .set('Origin', 'https://evil.example')
    .send({ name: 'x', platform: 'lite' })
    .expect(403);
  assert.equal(res.body.code, 'ORIGIN_NOT_ALLOWED');
});

test('full flow: pending, approve, token once, bearer access, revoke', async () => {
  const { body: start } = await startLogin({ name: 'Lite on NAS', platform: 'lite' }).expect(200);

  const pending = await poll(start.deviceCode).expect(428);
  assert.equal(pending.body.code, 'AUTHORIZATION_PENDING');

  // Only a signed-in user may look at or decide a request.
  await request(app).get(`/api/account/devices/authorize/${start.userCode}`).expect(401);
  await request(app).post(`/api/account/devices/authorize/${start.userCode}`).send({ approve: true }).expect(401);

  // The code is accepted in any case and without the dash.
  const loose = start.userCode.replace('-', '').toLowerCase();
  const info = await owner.agent.get(`/api/account/devices/authorize/${loose}`).expect(200);
  assert.equal(info.body.name, 'Lite on NAS');
  assert.equal(info.body.platform, 'lite');
  assert.ok(info.body.createdAt && info.body.expiresAt);

  const invalid = await owner.agent.post(`/api/account/devices/authorize/${start.userCode}`).send({ approve: 'yes' }).expect(400);
  assert.equal(invalid.body.code, 'INVALID_DECISION');
  await owner.agent.post(`/api/account/devices/authorize/${start.userCode}`).send({ approve: true }).expect(200);
  // Decided codes can't be looked up or decided again.
  await owner.agent.get(`/api/account/devices/authorize/${start.userCode}`).expect(404);
  await owner.agent.post(`/api/account/devices/authorize/${start.userCode}`).send({ approve: false }).expect(404);

  const issued = await poll(start.deviceCode).expect(200);
  assert.match(issued.body.token, /^nc_/);
  assert.equal(issued.body.device.name, 'Lite on NAS');
  assert.equal(issued.body.device.platform, 'lite');
  assert.deepEqual(issued.body.user, { id: owner.user.id, username: owner.username });

  const again = await poll(start.deviceCode).expect(410);
  assert.equal(again.body.code, 'EXPIRED_TOKEN');

  const bearer = `Bearer ${issued.body.token}`;
  const me = await request(app).get('/api/devices/me').set('Authorization', bearer).expect(200);
  assert.equal(me.body.device.id, issued.body.device.id);
  assert.deepEqual(me.body.user, { id: owner.user.id, username: owner.username });
  assert.equal(typeof me.body.quota.used, 'number');
  assert.equal(typeof me.body.quota.limit, 'number');
  await request(app).get('/api/folders/root/children').set('Authorization', bearer).expect(200);

  // /me is for bearer tokens; a session alone doesn't name a device.
  await owner.agent.get('/api/devices/me').expect(401);

  const listed = await owner.agent.get('/api/account/devices').expect(200);
  assert.ok(listed.body.devices.some((device) => device.id === issued.body.device.id));
  await owner.agent.delete(`/api/account/devices/${issued.body.device.id}`).expect(200);
  await request(app).get('/api/devices/me').set('Authorization', bearer).expect(401);
});

test('a denied login answers 403 to the device', async () => {
  const { body: start } = await startLogin().expect(200);
  await owner.agent.post(`/api/account/devices/authorize/${start.userCode}`).send({ approve: false }).expect(200);
  const denied = await poll(start.deviceCode).expect(403);
  assert.equal(denied.body.code, 'ACCESS_DENIED');
});

test('expired and unknown codes answer 410', async () => {
  const { body: start } = await startLogin().expect(200);
  db.prepare("UPDATE device_authorizations SET expires_at = datetime('now', '-1 minute') WHERE user_code = ?").run(start.userCode);
  await owner.agent.get(`/api/account/devices/authorize/${start.userCode}`).expect(404);
  await owner.agent.post(`/api/account/devices/authorize/${start.userCode}`).send({ approve: true }).expect(404);
  const expired = await poll(start.deviceCode).expect(410);
  assert.equal(expired.body.code, 'EXPIRED_TOKEN');

  // Approved in time but collected too late is just as expired.
  const { body: late } = await startLogin().expect(200);
  await owner.agent.post(`/api/account/devices/authorize/${late.userCode}`).send({ approve: true }).expect(200);
  db.prepare("UPDATE device_authorizations SET expires_at = datetime('now', '-1 minute') WHERE user_code = ?").run(late.userCode);
  await poll(late.deviceCode).expect(410);

  await poll('not-a-real-device-code').expect(410);
  await request(app).post('/api/devices/token').send({}).expect(410);
  await owner.agent.get('/api/account/devices/authorize/AAAA-BBBB').expect(404);
});

test('only the device code’s hash is stored', async () => {
  const { body: start } = await startLogin().expect(200);
  const row = db.prepare('SELECT * FROM device_authorizations WHERE user_code = ?').get(start.userCode);
  assert.notEqual(row.device_code_hash, start.deviceCode);
  assert.ok(!JSON.stringify(row).includes(start.deviceCode));
});
