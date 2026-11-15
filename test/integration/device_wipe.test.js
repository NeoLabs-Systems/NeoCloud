'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');

test.after(cleanup);

let owner;
let other;

test.before(async () => {
  owner = await registerUser('wipe');
  other = await registerUser('wipeother');
});

function newDevice(user, platform = 'linux') {
  return createDeviceToken(user.user.id, { name: 'Lost laptop', platform });
}

function wipe(agent, id, body = {}) {
  return agent.post(`/api/account/devices/${id}/wipe`).send(body);
}

test('a wiped device is told once, then stays plainly unauthorized', async () => {
  const { device, token } = newDevice(owner);
  const res = await wipe(owner.agent, device.id, { eraseFiles: true }).expect(200);
  assert.equal(res.body.device.wipe.eraseFiles, true);
  assert.equal(res.body.device.wipe.deliveredAt, null);

  const first = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${token}`).expect(401);
  assert.equal(first.body.code, 'DEVICE_WIPE_REQUESTED');
  assert.equal(first.body.eraseFiles, true);
  const second = await request(app).get('/api/sync/tree').set('Authorization', `Bearer ${token}`).expect(401);
  assert.equal(second.body.code, 'UNAUTHORIZED');

  const listed = (await owner.agent.get('/api/account/devices').expect(200)).body.devices.find((d) => d.id === device.id);
  assert.ok(listed.wipe.deliveredAt);
  const actions = db.prepare("SELECT action FROM access_audit_log WHERE action LIKE 'device.wipe%' ORDER BY id").all().map((r) => r.action);
  assert.deepEqual(actions, ['device.wipe_requested', 'device.wipe_delivered']);
});

test('sign out only leaves files alone and asking again changes nothing', async () => {
  const { device, token } = newDevice(owner);
  const a = await wipe(owner.agent, device.id).expect(200);
  const b = await wipe(owner.agent, device.id).expect(200);
  assert.equal(a.body.device.wipe.requestedAt, b.body.device.wipe.requestedAt);
  const res = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${token}`).expect(401);
  assert.equal(res.body.eraseFiles, false);
  // After delivery a repeat request does not re-arm the wipe.
  await wipe(owner.agent, device.id, { eraseFiles: true }).expect(200);
  const again = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${token}`).expect(401);
  assert.equal(again.body.code, 'UNAUTHORIZED');
});

test('a pending wipe can be called off by removing the device', async () => {
  const { device, token } = newDevice(owner);
  await wipe(owner.agent, device.id).expect(200);
  await owner.agent.delete(`/api/account/devices/${device.id}`).expect(200);
  const res = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${token}`).expect(401);
  assert.equal(res.body.code, 'UNAUTHORIZED');
});

test('only the owner can wipe, never this phone itself, never app passwords', async () => {
  const { device, token } = newDevice(owner);
  await wipe(other.agent, device.id).expect(404);
  await request(app).post(`/api/account/devices/${device.id}/wipe`).send({}).expect(401);
  const untouched = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${token}`).expect(200);
  assert.equal(untouched.body.device.id, device.id);

  const phone = newDevice(owner, 'android');
  const self = await request(app).post(`/api/account/devices/${phone.device.id}/wipe`)
    .set('Authorization', `Bearer ${phone.token}`).send({}).expect(400);
  assert.equal(self.body.code, 'CURRENT_DEVICE');
  const lite = newDevice(owner, 'lite');
  await request(app).post(`/api/account/devices/${device.id}/wipe`)
    .set('Authorization', `Bearer ${lite.token}`).send({}).expect(403);
  const webdav = newDevice(owner, 'webdav');
  await wipe(owner.agent, webdav.device.id).expect(404);
  await wipe(owner.agent, 'abc').expect(400);
});

test('wipe requests are rate limited', async () => {
  const user = await registerUser('wiperate');
  const { device } = newDevice(user);
  let last;
  for (let i = 0; i < 11; i += 1) last = await wipe(user.agent, device.id);
  assert.equal(last.status, 429);
});
