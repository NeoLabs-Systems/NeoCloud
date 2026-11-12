'use strict';

// Security key (WebAuthn) registration and sign-in, driven by a software
// authenticator (test/helpers/webauthn.js) that signs like a real key.

const test = require('node:test');
const assert = require('node:assert/strict');
const { generateSync } = require('otplib');
const { STRONG_PASSWORD, cleanup, request } = require('../helpers/app');
const { createVirtualAuthenticator } = require('../helpers/webauthn');
const { createApp } = require('../../server/app');
const throttle = require('../../server/services/account/login_throttle');
const db = require('../../server/db/database');

const ORIGIN = 'http://localhost:3000';

// Trusts X-Forwarded-For from loopback so each agent below comes from its own
// address and the per-address limiters never decide a test.
const { app } = createApp({ publicUrl: null, secureCookies: false, trustProxy: ['loopback'] });

test.after(cleanup);

let addressCounter = 0;
function newAgent() {
  addressCounter += 1;
  return request.agent(app).set('X-Forwarded-For', `198.51.100.${addressCounter}`);
}

let userCounter = 0;
async function registerUser(prefix) {
  userCounter += 1;
  const username = `${prefix}${userCounter}`;
  const agent = newAgent();
  const res = await agent.post('/api/auth/register').send({
    username,
    email: `${username}@example.com`,
    password: STRONG_PASSWORD,
  });
  assert.equal(res.status, 200, JSON.stringify(res.body));
  return { agent, user: res.body.user, username };
}

async function registerSecurityKey(agent, authenticator, label, { origin = ORIGIN } = {}) {
  const optionsRes = await agent
    .post('/api/account/security-keys/register/options')
    .set('Origin', ORIGIN)
    .send({ currentPassword: STRONG_PASSWORD })
    .expect(200);
  const attestation = authenticator.register({ options: optionsRes.body.options, origin });
  return agent
    .post('/api/account/security-keys/register/verify')
    .set('Origin', ORIGIN)
    .send({ response: attestation, label });
}

async function signInWithSecurityKey(agent, authenticator, { username, userHandle, origin = ORIGIN } = {}) {
  const optionsRes = await agent
    .post('/api/auth/webauthn/login/options')
    .set('Origin', ORIGIN)
    .send(username ? { username } : {})
    .expect(200);
  const assertion = authenticator.authenticate({ options: optionsRes.body.options, origin, userHandle });
  return agent
    .post('/api/auth/webauthn/login/verify')
    .set('Origin', ORIGIN)
    .send({ response: assertion });
}

// The admin: the first account. Registered first so later accounts are not.
let admin;

test('sign-in refuses at once while no key is registered', async () => {
  admin = await registerUser('admin');
  const empty = await request(app)
    .post('/api/auth/webauthn/login/options')
    .set('Origin', ORIGIN)
    .send({})
    .expect(409);
  assert.equal(empty.body.code, 'NO_SECURITY_KEYS');
});

test('registers a key, signs in without a password, and manages the key', async () => {
  const { agent, user, username } = await registerUser('keyuser');
  const authenticator = createVirtualAuthenticator();

  const registered = await registerSecurityKey(agent, authenticator, 'YubiKey 5');
  assert.equal(registered.status, 200, JSON.stringify(registered.body));
  assert.equal(registered.body.credentials.length, 1);
  assert.equal(registered.body.credentials[0].label, 'YubiKey 5');
  assert.equal(registered.body.credentials[0].rpId, 'localhost');

  const account = await agent.get('/api/account').expect(200);
  assert.equal(account.body.securityKeys.length, 1);

  // The same key twice is refused.
  const again = await registerSecurityKey(agent, authenticator, 'Again');
  assert.equal(again.status, 409);
  assert.equal(again.body.code, 'SECURITY_KEY_EXISTS');

  // Usernameless: the browser picks the key, the user handle names the account.
  const signedOut = newAgent();
  const login = await signInWithSecurityKey(signedOut, authenticator, { userHandle: user.id });
  assert.equal(login.status, 200, JSON.stringify(login.body));
  assert.equal(login.body.success, true);
  assert.equal(login.body.user.username, username);
  assert.equal((await signedOut.get('/api/auth/me').expect(200)).body.user.username, username);
  const sessions = await signedOut.get('/api/account/sessions').expect(200);
  assert.ok(sessions.body.sessions.some((session) => session.current));

  // With a username the options name the account's keys.
  const named = await request(app)
    .post('/api/auth/webauthn/login/options')
    .set('Origin', ORIGIN)
    .send({ username })
    .expect(200);
  assert.deepEqual(named.body.options.allowCredentials.map((entry) => entry.id), [authenticator.credentialId]);

  const keyId = account.body.securityKeys[0].id;
  const renamed = await signedOut.put(`/api/account/security-keys/${keyId}`).send({ label: 'Backup key' }).expect(200);
  assert.equal(renamed.body.credentials[0].label, 'Backup key');
  assert.ok(renamed.body.credentials[0].lastUsedAt);
  const tooLong = await signedOut.put(`/api/account/security-keys/${keyId}`).send({ label: 'x'.repeat(49) }).expect(400);
  assert.equal(tooLong.body.code, 'SECURITY_KEY_NAME_TOO_LONG');

  // Another account's key behaves like a missing one.
  await admin.agent.put(`/api/account/security-keys/${keyId}`).send({ label: 'Mine' }).expect(404);
  await admin.agent.delete(`/api/account/security-keys/${keyId}`).expect(404);

  const removed = await signedOut.delete(`/api/account/security-keys/${keyId}`).expect(200);
  assert.equal(removed.body.credentials.length, 0);
  assert.deepEqual((await signedOut.get('/api/account/security-keys').expect(200)).body.securityKeys, []);
});

test('a login rotates the session id', async () => {
  const { agent, user } = await registerUser('rotate');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator, 'Key');

  const visitor = newAgent();
  const optionsRes = await visitor.post('/api/auth/webauthn/login/options').set('Origin', ORIGIN).send({}).expect(200);
  const before = optionsRes.headers['set-cookie'].find((value) => value.startsWith('neocloud.sid=')).split(';')[0];
  const assertion = authenticator.authenticate({ options: optionsRes.body.options, origin: ORIGIN, userHandle: user.id });
  const login = await visitor.post('/api/auth/webauthn/login/verify').set('Origin', ORIGIN).send({ response: assertion }).expect(200);
  const after = login.headers['set-cookie'].find((value) => value.startsWith('neocloud.sid=')).split(';')[0];
  assert.notEqual(after, before);
});

test('rejects a wrong origin', async () => {
  const { agent, user } = await registerUser('origin');
  const authenticator = createVirtualAuthenticator();

  // A browser on an origin that is not allowed cannot start a ceremony.
  const blocked = await agent
    .post('/api/account/security-keys/register/options')
    .set('Origin', 'https://evil.example')
    .expect(403);
  assert.equal(blocked.body.code, 'ORIGIN_NOT_ALLOWED');

  // A key response signed for another origin is refused, at registration and sign-in.
  const forged = await registerSecurityKey(agent, authenticator, 'Forged', { origin: 'https://evil.example' });
  assert.equal(forged.status, 400);
  assert.equal(forged.body.code, 'SECURITY_KEY_REJECTED');

  assert.equal((await registerSecurityKey(agent, authenticator, 'Real')).status, 200);
  const phished = await signInWithSecurityKey(newAgent(), authenticator, {
    userHandle: user.id,
    origin: 'https://evil.example',
  });
  assert.equal(phished.status, 401);
  assert.equal(phished.body.code, 'SECURITY_KEY_REJECTED');
});

test('stores the signature counter and refuses one that went backwards', async () => {
  const { agent, user } = await registerUser('counter');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator, 'Key');
  const counter = () => db.prepare('SELECT counter FROM user_webauthn_credentials WHERE user_id = ?').get(user.id).counter;

  assert.equal((await signInWithSecurityKey(newAgent(), authenticator, { userHandle: user.id })).status, 200);
  assert.equal(counter(), 1);
  assert.equal((await signInWithSecurityKey(newAgent(), authenticator, { userHandle: user.id })).status, 200);
  assert.equal(counter(), 2);

  // A clone of the key would sign with a counter the server has already seen.
  db.prepare('UPDATE user_webauthn_credentials SET counter = 10 WHERE user_id = ?').run(user.id);
  const cloned = await signInWithSecurityKey(newAgent(), authenticator, { userHandle: user.id });
  assert.equal(cloned.status, 401);
  assert.equal(counter(), 10);
});

test('rejects unknown keys, replayed challenges and mismatched user handles', async () => {
  const { agent, user } = await registerUser('rejects');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator, 'Main key');

  const unknown = await signInWithSecurityKey(newAgent(), createVirtualAuthenticator());
  assert.equal(unknown.status, 401);
  assert.equal(unknown.body.code, 'SECURITY_KEY_UNKNOWN');

  const wrongHandle = await signInWithSecurityKey(newAgent(), authenticator, { userHandle: admin.user.id });
  assert.equal(wrongHandle.status, 401);

  const replayClient = newAgent();
  const optionsRes = await replayClient.post('/api/auth/webauthn/login/options').set('Origin', ORIGIN).send({}).expect(200);
  const assertion = authenticator.authenticate({ options: optionsRes.body.options, origin: ORIGIN, userHandle: user.id });
  await replayClient.post('/api/auth/webauthn/login/verify').set('Origin', ORIGIN).send({ response: assertion }).expect(200);
  const replayed = await replayClient.post('/api/auth/webauthn/login/verify').set('Origin', ORIGIN).send({ response: assertion });
  assert.equal(replayed.status, 400);
  assert.equal(replayed.body.code, 'SECURITY_KEY_CHALLENGE_EXPIRED');
});

test('a throttled account cannot sign in with its key either', async () => {
  const { agent, user } = await registerUser('throttled');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator, 'Key');
  const key = throttle.accountKey({ id: user.id });
  for (let index = 0; index < 10; index += 1) throttle.recordFailure(key, { kind: 'password' });

  const locked = await signInWithSecurityKey(newAgent(), authenticator, { userHandle: user.id });
  assert.equal(locked.status, 429);
  assert.equal(locked.body.code, 'LOGIN_LOCKED');
  assert.ok(Number(locked.headers['retry-after']) > 0);
  throttle.clearFailures(key);
});

// A code works once per time step (the server keeps the last one used), so
// a second code in the same test is taken `ahead` steps later.
function totp(secret, ahead = 0) {
  return generateSync({
    secret, strategy: 'totp', digits: 6, period: 30, epoch: Math.floor(Date.now() / 1000) + 30 * ahead,
  });
}

async function enableTwoFactor(agent) {
  const setup = await agent.post('/api/account/2fa/setup').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  await agent.post('/api/account/2fa/enable').send({ code: totp(setup.body.manualKey) }).expect(200);
  return setup.body.manualKey;
}

test('a user-verifying key stands in for the second factor', async () => {
  const { agent, user } = await registerUser('uv');
  const authenticator = createVirtualAuthenticator({ userVerified: true });
  await registerSecurityKey(agent, authenticator, 'Verified key');
  await enableTwoFactor(agent);

  const login = await signInWithSecurityKey(newAgent(), authenticator, { userHandle: user.id });
  assert.equal(login.status, 200);
  assert.equal(login.body.success, true);
  assert.equal(login.body.requiresTwoFactor, undefined);
});

test('a presence-only key still has to pass 2FA', async () => {
  const { agent, user } = await registerUser('presence');
  const authenticator = createVirtualAuthenticator({ userVerified: false });
  await registerSecurityKey(agent, authenticator, 'Presence key');
  const secret = await enableTwoFactor(agent);

  const visitor = newAgent();
  const login = await signInWithSecurityKey(visitor, authenticator, { userHandle: user.id });
  assert.equal(login.status, 200);
  assert.equal(login.body.success, false);
  assert.equal(login.body.requiresTwoFactor, true);
  await visitor.get('/api/auth/me').expect(401);
  await visitor.post('/api/auth/login/2fa').send({ code: totp(secret, 1) }).expect(200);
  await visitor.get('/api/auth/me').expect(200);
});

test('an admin password reset ends sessions started with a key', async () => {
  const { agent, user } = await registerUser('reset');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator, 'Key');
  const keySession = newAgent();
  await signInWithSecurityKey(keySession, authenticator, { userHandle: user.id }).then((res) => assert.equal(res.status, 200));
  await keySession.get('/api/auth/me').expect(200);

  await admin.agent.post(`/api/admin/users/${user.id}/password`).send({ password: 'Another-Plum-84!' }).expect(200);
  await keySession.get('/api/auth/me').expect(401);
  await agent.get('/api/auth/me').expect(401);
});

test('security key endpoints need a session', async () => {
  await request(app).get('/api/account/security-keys').expect(401);
  await request(app).post('/api/account/security-keys/register/options').set('Origin', ORIGIN).expect(401);
  await request(app).delete('/api/account/security-keys/1').set('Origin', ORIGIN).expect(401);
});
