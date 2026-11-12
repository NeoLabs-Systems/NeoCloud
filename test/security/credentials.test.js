'use strict';

// Sign-in throttling, second-factor replacement, and what a password change
// or reset ends: browser sessions, device tokens, app passwords and sockets.

const test = require('node:test');
const assert = require('node:assert/strict');
const { generateSync } = require('otplib');
const { io: connectSocket } = require('socket.io-client');
const { STRONG_PASSWORD, cleanup, request } = require('../helpers/app');
const { createApp } = require('../../server/app');
const { bindSocketSessions, createSocketServer } = require('../../server/http/socket');
const { attachSocketServer } = require('../../server/services/events');
const { validateOrigin } = require('../../server/config/origins');
const db = require('../../server/db/database');

// Trusts X-Forwarded-For from loopback, so every request below can come from
// its own address: the throttle must hold anyway, and the per-address
// limiter never gets in the way of the test.
const { app, sessionMiddleware } = createApp({ publicUrl: null, secureCookies: false, trustProxy: ['loopback'] });
const httpServer = app.listen(0);
const socketServer = createSocketServer(httpServer, { validateOrigin });
bindSocketSessions(socketServer, sessionMiddleware);
attachSocketServer(socketServer);

test.after(() => {
  socketServer.close();
  httpServer.close();
  cleanup();
});

let addressCounter = 0;
function fromNewAddress(req) {
  addressCounter += 1;
  return req.set('X-Forwarded-For', `198.51.${Math.floor(addressCounter / 250)}.${(addressCounter % 250) + 1}`);
}

let userCounter = 0;
async function register(prefix) {
  userCounter += 1;
  const username = `${prefix}${userCounter}`;
  const agent = request.agent(app);
  const res = await fromNewAddress(agent.post('/api/auth/register')).send({
    username,
    email: `${username}@example.com`,
    password: STRONG_PASSWORD,
  });
  assert.equal(res.status, 200, JSON.stringify(res.body));
  return { agent, user: res.body.user, username };
}

function login(agent, username, password = STRONG_PASSWORD) {
  return fromNewAddress(agent.post('/api/auth/login')).send({ username, password });
}

function sessionCookie(res) {
  const cookie = (res.headers['set-cookie'] || []).find((value) => value.startsWith('neocloud.sid='));
  assert.ok(cookie, 'a session cookie is set');
  return cookie.split(';')[0];
}

// A code works once per time step (the server keeps the last one used), so
// a second code in the same test is taken `ahead` steps later.
function totp(secret, ahead = 0) {
  return generateSync({
    secret, strategy: 'totp', digits: 6, period: 30, epoch: Math.floor(Date.now() / 1000) + 30 * ahead,
  });
}

// A six-digit code that is not valid for `secret` in the accepted window.
function wrongCode(secret) {
  const valid = new Set([-30, 0, 30].map((offset) => generateSync({
    secret, strategy: 'totp', digits: 6, period: 30, epoch: Math.floor(Date.now() / 1000) + offset,
  })));
  let code = 100000;
  while (valid.has(String(code))) code += 1;
  return String(code);
}

async function enableTwoFactor(agent) {
  const setup = await agent.post('/api/account/2fa/setup').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  await agent.post('/api/account/2fa/enable').send({ code: totp(setup.body.manualKey) }).expect(200);
  return setup.body.manualKey;
}

function failureRows(key) {
  return db.prepare('SELECT COUNT(*) AS n FROM login_failures WHERE account_key = ?').get(key).n;
}

function ageFailures(key, ms) {
  db.prepare('UPDATE login_failures SET failed_at_ms = failed_at_ms - ? WHERE account_key = ?').run(ms, key);
}

let admin;

test.before(async () => {
  admin = await register('admin');
});

test('sign-in failures are counted per account, whatever address they come from', async () => {
  const victim = await register('victim');
  const key = `user:${victim.user.id}`;
  const anon = request.agent(app);
  for (let i = 0; i < 5; i += 1) {
    assert.equal((await login(anon, victim.username, `wrong-${i}`).expect(401)).body.code, 'INVALID_CREDENTIALS');
  }
  // From the 5th failure on, even the right password waits.
  const slowed = await login(anon, victim.username).expect(429);
  assert.equal(slowed.body.code, 'LOGIN_LOCKED');
  assert.ok(slowed.body.retryAfter >= 1 && slowed.body.retryAfter <= 2);
  assert.equal(slowed.headers['retry-after'], String(slowed.body.retryAfter));
  assert.match(slowed.body.error, /Try again in/);
  assert.equal(failureRows(key), 5, 'a refused try is not a guess');

  // Ten in 15 minutes lock sign-in for the rest of the window, no longer.
  ageFailures(key, 60e3);
  await login(anon, victim.username, 'wrong-5').expect(401);
  const insert = db.prepare("INSERT INTO login_failures (account_key, kind, failed_at_ms) VALUES (?, 'password', ?)");
  for (let i = 0; i < 4; i += 1) insert.run(key, Date.now() - 60e3);
  const locked = await login(anon, victim.username).expect(429);
  assert.ok(locked.body.retryAfter > 10 * 60 && locked.body.retryAfter <= 15 * 60, `retryAfter ${locked.body.retryAfter}`);

  ageFailures(key, 15 * 60e3);
  await login(anon, victim.username).expect(200);
  assert.equal(failureRows(key), 0, 'the right password clears password failures');

  // A name no account has is throttled the same way, so 429 says nothing
  // about which names exist.
  for (let i = 0; i < 5; i += 1) await login(request.agent(app), 'nobody-here', `wrong-${i}`).expect(401);
  assert.equal((await login(request.agent(app), 'nobody-here', 'x').expect(429)).body.code, 'LOGIN_LOCKED');
});

test('a pending second-factor sign-in takes at most 5 codes', async () => {
  const owner = await register('totp');
  const secret = await enableTwoFactor(owner.agent);
  const key = `user:${owner.user.id}`;

  const visitor = request.agent(app);
  assert.equal((await login(visitor, owner.username).expect(200)).body.requiresTwoFactor, true);
  for (let i = 0; i < 5; i += 1) {
    const res = await fromNewAddress(visitor.post('/api/auth/login/2fa')).send({ code: wrongCode(secret) }).expect(401);
    assert.equal(res.body.code, 'INVALID_CODE');
  }
  // The challenge is used up: even the right code needs a new password sign-in.
  const spent = await fromNewAddress(visitor.post('/api/auth/login/2fa')).send({ code: totp(secret) }).expect(401);
  assert.ok(['TWO_FACTOR_EXPIRED', 'TWO_FACTOR_ATTEMPTS_EXCEEDED'].includes(spent.body.code), spent.body.code);
  await visitor.get('/api/account').expect(401);

  // Wrong codes keep counting against the account across challenges: a
  // right password doesn't clear them. (Each wrong code also tried the
  // recovery codes, which is slow: date the failures to now for the check.)
  db.prepare('UPDATE login_failures SET failed_at_ms = ? WHERE account_key = ?').run(Date.now(), key);
  assert.equal((await login(visitor, owner.username).expect(429)).body.code, 'LOGIN_LOCKED');
  ageFailures(key, 60e3);
  assert.equal((await login(visitor, owner.username).expect(200)).body.requiresTwoFactor, true);
  assert.equal(failureRows(key), 5);
  await fromNewAddress(visitor.post('/api/auth/login/2fa')).send({ code: totp(secret, 1) }).expect(200);
  assert.equal(failureRows(key), 0, 'a full sign-in clears the account');
  await visitor.get('/api/account').expect(200);
});

test('an enrolled second factor is replaced only with a current code', async () => {
  const owner = await register('replace');
  const oldSecret = await enableTwoFactor(owner.agent);

  const bare = await owner.agent.post('/api/account/2fa/setup').send({ currentPassword: STRONG_PASSWORD }).expect(401);
  assert.equal(bare.body.code, 'TWO_FACTOR_REQUIRED');
  const wrong = await owner.agent.post('/api/account/2fa/setup')
    .send({ currentPassword: STRONG_PASSWORD, code: wrongCode(oldSecret) }).expect(401);
  assert.equal(wrong.body.code, 'INVALID_CODE');

  const setup = await owner.agent.post('/api/account/2fa/setup')
    .send({ currentPassword: STRONG_PASSWORD, code: totp(oldSecret, 1) }).expect(200);
  const newSecret = setup.body.manualKey;
  // Each step's code works once; forget the one setup took instead of waiting
  // for the step after.
  db.prepare("DELETE FROM user_settings WHERE user_id = ? AND key = 'two_factor_last_step'").run(owner.user.id);
  // enable needs the current factor as well, not only a code of the new one.
  assert.equal((await owner.agent.post('/api/account/2fa/enable').send({ code: totp(newSecret) }).expect(401)).body.code, 'TWO_FACTOR_REQUIRED');
  assert.equal(db.prepare('SELECT enabled FROM user_two_factor WHERE user_id = ?').get(owner.user.id).enabled, 1);
  await owner.agent.post('/api/account/2fa/enable').send({ code: totp(newSecret), currentCode: totp(oldSecret, 1) }).expect(200);

  const visitor = request.agent(app);
  await login(visitor, owner.username).expect(200);
  await fromNewAddress(visitor.post('/api/auth/login/2fa')).send({ code: totp(newSecret, 1) }).expect(200);
});

async function deviceCredentials(agent) {
  const device = (await agent.post('/api/account/devices').send({ name: 'laptop', platform: 'linux' }).expect(201)).body;
  const webdav = (await agent.post('/api/account/devices').send({ name: 'dav', platform: 'webdav' }).expect(201)).body;
  const pairing = (await agent.post('/api/account/devices/pairing').send({ name: 'phone' }).expect(200)).body;
  return { bearer: device.token, appPassword: webdav.token, pairingCode: pairing.code };
}

async function assertDevicesSignedOut(username, { bearer, appPassword, pairingCode }) {
  await request(app).get('/api/devices/me').set('Authorization', `Bearer ${bearer}`).expect(401);
  await request(app).get('/api/folders/root/children').set('Authorization', `Bearer ${bearer}`).expect(401);
  await request(app).propfind(`/dav/files/${username}/`).auth(username, appPassword).set('Depth', '0').expect(401);
  const redeemed = await request(app).post('/api/devices/pair').send({ code: pairingCode, name: 'phone' });
  assert.notEqual(redeemed.status, 200, 'an open pairing code no longer turns into a token');
}

function openSocket(cookie) {
  const { port } = httpServer.address();
  const socket = connectSocket(`http://127.0.0.1:${port}`, {
    transports: ['websocket'],
    extraHeaders: { cookie },
    reconnection: false,
  });
  const connected = new Promise((resolve, reject) => {
    socket.once('connect', resolve);
    socket.once('connect_error', reject);
  });
  const disconnected = new Promise((resolve) => socket.once('disconnect', resolve));
  return { socket, connected, disconnected };
}

function within(promise, ms, label) {
  return Promise.race([
    promise,
    new Promise((_, reject) => setTimeout(() => reject(new Error(`${label} timed out`)), ms)),
  ]);
}

test('a password change ends every other session, device and app password', async () => {
  const owner = await register('changer');
  const self = request.agent(app);
  const oldSelfCookie = sessionCookie(await login(self, owner.username).expect(200));
  const otherCookie = sessionCookie(await login(request.agent(app), owner.username).expect(200));
  const devices = await deviceCredentials(owner.agent);
  await request(app).get('/api/devices/me').set('Authorization', `Bearer ${devices.bearer}`).expect(200);
  await request(app).propfind(`/dav/files/${owner.username}/`).auth(owner.username, devices.appPassword).set('Depth', '0').expect(207);
  const otherSocket = openSocket(otherCookie);
  await within(otherSocket.connected, 5000, 'socket connect');

  const newPassword = 'Velvet-Comet-58!harbor';
  const changed = await self.put('/api/account/password')
    .send({ currentPassword: STRONG_PASSWORD, newPassword }).expect(200);
  // This session stays, under a new id; a copy of its old cookie is dead.
  assert.notEqual(sessionCookie(changed), oldSelfCookie);
  const sessions = (await self.get('/api/account/sessions').expect(200)).body.sessions;
  assert.equal(sessions.length, 1);
  assert.equal(sessions[0].current, true);
  await request(app).get('/api/account').set('Cookie', oldSelfCookie).expect(401);

  await owner.agent.get('/api/account').expect(401);
  await request(app).get('/api/account').set('Cookie', otherCookie).expect(401);
  await within(otherSocket.disconnected, 5000, 'socket disconnect');
  await assertDevicesSignedOut(owner.username, devices);
  await login(request.agent(app), owner.username, newPassword).expect(200);
});

test('an admin password reset signs the account out everywhere', async () => {
  const target = await register('reset');
  const cookie = sessionCookie(await login(request.agent(app), target.username).expect(200));
  const devices = await deviceCredentials(target.agent);
  const live = openSocket(cookie);
  await within(live.connected, 5000, 'socket connect');

  await admin.agent.post(`/api/admin/users/${target.user.id}/password`).send({ password: 'Amber-Signal-41!meadow' }).expect(200);
  await target.agent.get('/api/account').expect(401);
  await within(live.disconnected, 5000, 'socket disconnect');
  await assertDevicesSignedOut(target.username, devices);
});

test('revoking a session or signing out closes its socket, and only its socket', async () => {
  const owner = await register('sockets');
  const second = request.agent(app);
  const secondCookie = sessionCookie(await login(second, owner.username).expect(200));
  const third = request.agent(app);
  const thirdCookie = sessionCookie(await login(third, owner.username).expect(200));
  const revoked = openSocket(secondCookie);
  const signedOut = openSocket(thirdCookie);
  await within(Promise.all([revoked.connected, signedOut.connected]), 5000, 'socket connect');

  const sessions = (await owner.agent.get('/api/account/sessions').expect(200)).body.sessions;
  // Sessions are numbered in sign-in order: the second sign-in has the lower id.
  const secondId = Math.min(...sessions.filter((session) => !session.current).map((session) => session.id));
  await owner.agent.delete(`/api/account/sessions/${secondId}`).expect(200);
  await within(revoked.disconnected, 5000, 'revoked socket disconnect');
  assert.equal(signedOut.socket.connected, true, 'other sessions keep their socket');

  await third.post('/api/auth/logout').expect(200);
  await within(signedOut.disconnected, 5000, 'signed-out socket disconnect');
  const reconnect = openSocket(thirdCookie);
  await assert.rejects(within(reconnect.connected, 5000, 'reconnect'));
  reconnect.socket.close();
});
