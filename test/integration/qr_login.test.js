'use strict';

const crypto = require('crypto');
const test = require('node:test');
const assert = require('node:assert/strict');
const { STRONG_PASSWORD, app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let owner;
let phoneBearer;

test.before(async () => {
  owner = await registerUser('qrlogin');
  // The phone app holds a device token, not a session.
  const { body: pairing } = await owner.agent.post('/api/account/devices/pairing').send({}).expect(200);
  const { body: paired } = await request(app).post('/api/devices/pair')
    .send({ code: pairing.code, name: 'Pixel 9', platform: 'android' }).expect(200);
  phoneBearer = `Bearer ${paired.token}`;
});

const DESKTOP_SAFARI = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15';

/** A signed-out browser: its own cookie jar, and the challenge it showed. */
async function showCode() {
  const browser = request.agent(app);
  const res = await browser.post('/api/auth/qr-login/challenge').set('User-Agent', DESKTOP_SAFARI).send({}).expect(200);
  const qr = new URL(res.body.qrPayload);
  return {
    browser,
    body: res.body,
    challengeId: res.body.challengeId,
    pollToken: res.body.pollToken,
    secret: qr.searchParams.get('secret'),
    qr,
  };
}

function phone(path, body) {
  return request(app).post(`/api/account/qr-login/${path}`).set('Authorization', phoneBearer).send(body);
}

function status(code, knownStatus) {
  return request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/status`)
    .send({ token: code.pollToken, ...(knownStatus ? { knownStatus } : {}) });
}

function claim(code) {
  return code.browser.post(`/api/auth/qr-login/challenge/${code.challengeId}/claim`).send({ token: code.pollToken });
}

test('a challenge carries a neocloud:// payload and stores only hashes', async () => {
  const code = await showCode();
  assert.equal(code.qr.protocol, 'neocloud:');
  assert.equal(code.qr.host, 'qr-login');
  assert.equal(code.qr.searchParams.get('v'), '1');
  assert.equal(code.qr.searchParams.get('backend'), code.body.backendUrl);
  assert.equal(code.qr.searchParams.get('challenge'), code.challengeId);
  assert.match(code.secret, /^[A-Za-z0-9_-]{32}$/);
  assert.notEqual(code.secret, code.pollToken);
  assert.equal(code.body.status, 'pending');

  const row = db.prepare('SELECT * FROM user_qr_login_challenges WHERE id = ?').get(code.challengeId);
  const sha = (value) => crypto.createHash('sha256').update(value).digest('hex');
  assert.equal(row.poll_token_hash, sha(code.pollToken));
  assert.equal(row.approve_secret_hash, sha(code.secret));
  const lifetime = db.prepare('SELECT (julianday(expires_at) - julianday(created_at)) * 86400 AS s FROM user_qr_login_challenges WHERE id = ?')
    .get(code.challengeId).s;
  assert.ok(Math.abs(lifetime - 120) < 2, `lives 2 minutes, got ${lifetime}`);
});

test('scan, approve and claim sign the browser in, once', async () => {
  const code = await showCode();
  await code.browser.get('/api/auth/me').expect(401);
  assert.equal((await status(code)).body.status, 'pending');

  // The browser waits on its known status; the scan wakes it.
  const waiting = status(code, 'pending');
  const preview = await phone('resolve', {
    challengeId: code.challengeId,
    secret: code.secret,
    scannerMetadata: { deviceLabel: 'Pixel 9', platformLabel: 'Android', deviceClass: 'mobile' },
  }).expect(200);
  assert.equal(preview.body.status, 'pending', 'still undecided to the phone');
  assert.equal(preview.body.requestedDevice.label, 'macOS · Safari');
  assert.equal(preview.body.requestedDevice.deviceClass, 'desktop');
  assert.ok(preview.body.requestLocation.ipAddress);
  const woke = await waiting.expect(200);
  assert.equal(woke.body.status, 'scanned');
  assert.equal(woke.body.longPoll, true);
  assert.equal(woke.body.scannedBy.label, 'Pixel 9');
  assert.equal(woke.body.requestedDevice.label, 'macOS · Safari');

  await claim(code).expect(409);

  const approved = await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(200);
  assert.equal(approved.body.status, 'approved');
  assert.equal((await status(code, 'scanned')).body.status, 'approved');
  const row = db.prepare('SELECT * FROM user_qr_login_challenges WHERE id = ?').get(code.challengeId);
  assert.equal(row.approved_by_user_id, owner.user.id);
  assert.ok(row.approved_device_id, 'the approving device is recorded');

  const signedIn = await claim(code).expect(200);
  assert.equal(signedIn.body.success, true);
  assert.equal(signedIn.body.user.id, owner.user.id);
  const me = await code.browser.get('/api/auth/me').expect(200);
  assert.equal(me.body.user.username, owner.username);
  assert.equal((await status(code)).body.status, 'claimed');

  const twice = await claim(code).expect(409);
  assert.equal(twice.body.code, 'QR_LOGIN_USED');
  const late = await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(409);
  assert.equal(late.body.code, 'QR_LOGIN_USED');
});

test('a browser session may approve too', async () => {
  const code = await showCode();
  await owner.agent.post('/api/account/qr-login/approve').send({ challengeId: code.challengeId, secret: code.secret }).expect(200);
  await claim(code).expect(200);
});

test('the phone can decline', async () => {
  const code = await showCode();
  await phone('resolve', { challengeId: code.challengeId, secret: code.secret }).expect(200);
  await phone('decline', { challengeId: code.challengeId, secret: code.secret }).expect(200);
  assert.equal((await status(code)).body.status, 'declined');
  const approve = await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(409);
  assert.equal(approve.body.code, 'QR_LOGIN_DECLINED');
  await claim(code).expect(409);
  await code.browser.get('/api/auth/me').expect(401);
});

test('the browser can cancel its code', async () => {
  const code = await showCode();
  const wrong = await request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/cancel`).send({ token: 'nope' }).expect(404);
  assert.equal(wrong.body.code, 'NOT_FOUND');
  await request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/cancel`).send({ token: code.pollToken }).expect(200);
  assert.equal((await status(code)).body.status, 'declined');
  const resolve = await phone('resolve', { challengeId: code.challengeId, secret: code.secret }).expect(409);
  assert.equal(resolve.body.code, 'QR_LOGIN_DECLINED');
  await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(409);
});

test('an approved code cannot be declined or cancelled', async () => {
  const code = await showCode();
  await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(200);
  const decline = await phone('decline', { challengeId: code.challengeId, secret: code.secret }).expect(409);
  assert.equal(decline.body.code, 'QR_LOGIN_APPROVED');
  await request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/cancel`).send({ token: code.pollToken }).expect(409);
});

test('expired codes answer 404 to the phone and expired to the browser', async () => {
  const code = await showCode();
  await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(200);
  db.prepare("UPDATE user_qr_login_challenges SET expires_at = datetime('now', '-1 second') WHERE id = ?").run(code.challengeId);
  assert.equal((await status(code)).body.status, 'expired');
  const resolve = await phone('resolve', { challengeId: code.challengeId, secret: code.secret }).expect(404);
  assert.equal(resolve.body.code, 'NOT_FOUND');
  await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(404);
  await claim(code).expect(404);
  await code.browser.get('/api/auth/me').expect(401);
});

test('a wrong secret or poll token reveals nothing', async () => {
  const code = await showCode();
  await phone('resolve', { challengeId: code.challengeId, secret: 'x'.repeat(32) }).expect(404);
  await phone('approve', { challengeId: code.challengeId, secret: code.pollToken }).expect(404);
  await phone('decline', { challengeId: code.challengeId, secret: 'wrong' }).expect(404);
  const missing = await phone('approve', { challengeId: code.challengeId }).expect(400);
  assert.equal(missing.body.code, 'MISSING_FIELDS');
  // The approve secret is not a poll token.
  const asPoll = await request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/status`).send({ token: code.secret }).expect(200);
  assert.equal(asPoll.body.status, 'expired');
  await request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/claim`).send({ token: code.secret }).expect(404);
  await request(app).post(`/api/auth/qr-login/challenge/${code.challengeId}/status`).send({}).expect(400);
  assert.equal((await status(code)).body.status, 'pending');
});

test('approving needs a signed-in session or device token', async () => {
  const code = await showCode();
  const body = { challengeId: code.challengeId, secret: code.secret };
  await request(app).post('/api/account/qr-login/resolve').send(body).expect(401);
  await request(app).post('/api/account/qr-login/approve').send(body).expect(401);
  await request(app).post('/api/account/qr-login/approve').set('Authorization', 'Bearer nc_bogus').send(body).expect(401);
  // The device token still reaches nothing else under /api/account.
  await request(app).get('/api/account').set('Authorization', phoneBearer).expect(401);
  assert.equal((await status(code)).body.status, 'pending');
});

test('only the phone app token may approve, not WebDAV or desktop tokens', async () => {
  const code = await showCode();
  const body = { challengeId: code.challengeId, secret: code.secret };
  for (const platform of ['webdav', 'lite']) {
    const { body: token } = await owner.agent.post('/api/account/devices').send({ name: platform, platform }).expect(201);
    const bearer = `Bearer ${token.token}`;
    await request(app).post('/api/account/qr-login/approve').set('Authorization', bearer).send(body).expect(403);
  }
  assert.equal((await status(code)).body.status, 'pending');
});

test('a device token may approve a device-code sign-in', async () => {
  const { body: start } = await request(app).post('/api/devices/authorize').send({ name: 'Lite', platform: 'lite' }).expect(200);
  const info = await request(app).get(`/api/account/devices/authorize/${start.userCode}`).set('Authorization', phoneBearer).expect(200);
  assert.equal(info.body.name, 'Lite');
  await request(app).post(`/api/account/devices/authorize/${start.userCode}`).set('Authorization', phoneBearer).send({ approve: true }).expect(200);
  const issued = await request(app).post('/api/devices/token').send({ deviceCode: start.deviceCode }).expect(200);
  assert.match(issued.body.token, /^nc_/);
});

// Last: changing the password revokes the phone's token.
test('changing the password voids an approval not yet claimed', async () => {
  const code = await showCode();
  await phone('approve', { challengeId: code.challengeId, secret: code.secret }).expect(200);
  await owner.agent.put('/api/account/password')
    .send({ currentPassword: STRONG_PASSWORD, newPassword: 'Quartz-Lantern-58!meadow' }).expect(200);
  assert.equal((await status(code)).body.status, 'declined');
  await claim(code).expect(409);
  await code.browser.get('/api/auth/me').expect(401);
});
