'use strict';

// Security key sign-in for native clients (/api/devices/webauthn/*): the
// client builds clientDataJSON itself and gets a device token, not a
// session. Driven by the software authenticator in test/helpers/webauthn.js.

const test = require('node:test');
const assert = require('node:assert/strict');
const { generateSync } = require('otplib');
const { STRONG_PASSWORD, cleanup, request } = require('../helpers/app');
const { createVirtualAuthenticator } = require('../helpers/webauthn');
const { createApp } = require('../../server/app');

const ORIGIN = 'http://localhost:3000';

// PUBLIC_URL fixes the relying party, as on a real server.
const { app } = createApp({ publicUrl: ORIGIN, secureCookies: false, trustProxy: ['loopback'] });

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

async function registerSecurityKey(agent, authenticator) {
  const optionsRes = await agent.post('/api/account/security-keys/register/options').set('Origin', ORIGIN)
    .send({ currentPassword: STRONG_PASSWORD }).expect(200);
  const attestation = authenticator.register({ options: optionsRes.body.options, origin: ORIGIN });
  await agent.post('/api/account/security-keys/register/verify').set('Origin', ORIGIN)
    .send({ response: attestation, label: 'Key' }).expect(200);
}

// The native client has no cookies and sends no Origin header.
function options(body = {}) {
  return newAgent().post('/api/devices/webauthn/options').send(body);
}

function verify(body) {
  return newAgent().post('/api/devices/webauthn/verify').send({ name: 'Laptop', platform: 'lite', ...body });
}

async function signIn(authenticator, { userHandle, origin = ORIGIN, username } = {}) {
  const started = await options(username ? { username } : {}).expect(200);
  const assertion = authenticator.authenticate({ options: started.body.options, origin, userHandle });
  return { started, assertion, res: await verify({ challengeId: started.body.challengeId, response: assertion }) };
}

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

test('refuses at once while no key is registered', async () => {
  await registerUser('admin');
  const res = await options().expect(409);
  assert.equal(res.body.code, 'NO_SECURITY_KEYS');
});

test('a key signs a native client in and the token works', async () => {
  const { agent, user, username } = await registerUser('lite');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator);

  const { started, res } = await signIn(authenticator, { userHandle: user.id });
  assert.equal(started.body.origin, ORIGIN);
  assert.equal(started.body.options.rpId, 'localhost');
  assert.ok(started.body.challengeId.length >= 40);
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.match(res.body.token, /^nc_/);
  assert.deepEqual(res.body.device.name, 'Laptop');
  assert.deepEqual(res.body.user, { id: user.id, username });
  // No session: a native client gets a bearer token only.
  assert.equal(res.headers['set-cookie'], undefined);

  const me = await request(app).get('/api/devices/me').set('Authorization', `Bearer ${res.body.token}`).expect(200);
  assert.equal(me.body.device.platform, 'lite');
  assert.equal(me.body.user.username, username);

  // With a username the options name the account's keys.
  const named = await options({ username }).expect(200);
  assert.deepEqual(named.body.options.allowCredentials.map((entry) => entry.id), [authenticator.credentialId]);
});

test('checks the device name before spending the challenge', async () => {
  const { agent, user } = await registerUser('named');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator);
  const started = await options().expect(200);
  const assertion = authenticator.authenticate({ options: started.body.options, origin: ORIGIN, userHandle: user.id });
  const unnamed = await verify({ challengeId: started.body.challengeId, response: assertion, name: '' }).expect(400);
  assert.equal(unnamed.body.code, 'NAME_REQUIRED');
  await verify({ challengeId: started.body.challengeId, response: assertion }).expect(200);
});

test('the origin is the public URL, not what the request claims', async () => {
  const { agent, user } = await registerUser('origin');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator);

  const phished = await signIn(authenticator, { userHandle: user.id, origin: 'https://evil.example' });
  assert.equal(phished.res.status, 401);
  assert.equal(phished.res.body.code, 'SECURITY_KEY_REJECTED');

  // Another Host header does not move the relying party either.
  const started = await options().set('Host', 'evil.example').expect(200);
  assert.equal(started.body.origin, ORIGIN);
  const assertion = authenticator.authenticate({ options: started.body.options, origin: 'http://evil.example', userHandle: user.id });
  const res = await newAgent().post('/api/devices/webauthn/verify').set('Host', 'evil.example')
    .send({ challengeId: started.body.challengeId, response: assertion, name: 'x', platform: 'lite' });
  assert.equal(res.status, 401);
  assert.equal(res.body.code, 'SECURITY_KEY_REJECTED');
});

test('rejects replayed challenges, unknown ids and unknown keys', async () => {
  const { agent, user } = await registerUser('replay');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator);

  const { started, assertion, res } = await signIn(authenticator, { userHandle: user.id });
  assert.equal(res.status, 200);
  const replayed = await verify({ challengeId: started.body.challengeId, response: assertion });
  assert.equal(replayed.status, 400);
  assert.equal(replayed.body.code, 'SECURITY_KEY_CHALLENGE_EXPIRED');

  const made = await options().expect(200);
  const forged = await verify({ challengeId: 'not-a-challenge', response: authenticator.authenticate({ options: made.body.options, origin: ORIGIN, userHandle: user.id }) });
  assert.equal(forged.status, 400);
  assert.equal(forged.body.code, 'SECURITY_KEY_CHALLENGE_EXPIRED');

  const unknown = await signIn(createVirtualAuthenticator());
  assert.equal(unknown.res.status, 401);
  assert.equal(unknown.res.body.code, 'SECURITY_KEY_UNKNOWN');
});

test('an expired challenge is refused and spent', async () => {
  const { agent, user } = await registerUser('expired');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(agent, authenticator);
  const started = await options().expect(200);
  const assertion = authenticator.authenticate({ options: started.body.options, origin: ORIGIN, userHandle: user.id });

  // Six minutes later: past the five-minute challenge TTL.
  const realNow = Date.now;
  const later = realNow() + 6 * 60 * 1000;
  Date.now = () => later;
  let res;
  try {
    res = await verify({ challengeId: started.body.challengeId, response: assertion });
  } finally {
    Date.now = realNow;
  }
  assert.equal(res.status, 400);
  assert.equal(res.body.code, 'SECURITY_KEY_CHALLENGE_EXPIRED');
  const retried = await verify({ challengeId: started.body.challengeId, response: assertion });
  assert.equal(retried.body.code, 'SECURITY_KEY_CHALLENGE_EXPIRED');
});

test('a user-verifying key stands in for the second factor', async () => {
  const { agent, user } = await registerUser('uv');
  const authenticator = createVirtualAuthenticator({ userVerified: true });
  await registerSecurityKey(agent, authenticator);
  await enableTwoFactor(agent);
  const { res } = await signIn(authenticator, { userHandle: user.id });
  assert.equal(res.status, 200);
  assert.match(res.body.token, /^nc_/);
});

test('a presence-only key still has to pass 2FA', async () => {
  const { agent, user } = await registerUser('presence');
  const authenticator = createVirtualAuthenticator({ userVerified: false });
  await registerSecurityKey(agent, authenticator);
  const secret = await enableTwoFactor(agent);

  const { started, res } = await signIn(authenticator, { userHandle: user.id });
  assert.equal(res.status, 200);
  assert.deepEqual(res.body, { requiresTwoFactor: true, challengeId: started.body.challengeId });
  assert.equal(res.body.token, undefined);

  const twoFactor = (code, challengeId = started.body.challengeId) => newAgent()
    .post('/api/devices/webauthn/2fa').send({ challengeId, code });
  const wrong = await twoFactor('000000');
  assert.equal(wrong.status, 401);
  assert.equal(wrong.body.code, 'INVALID_CODE');
  const otherId = await twoFactor(totp(secret), 'another-id');
  assert.equal(otherId.body.code, 'TWO_FACTOR_EXPIRED');

  const done = await twoFactor(totp(secret, 1)).expect(200);
  assert.match(done.body.token, /^nc_/);
  assert.equal(done.body.user.id, user.id);
  const again = await twoFactor(totp(secret, 1));
  assert.equal(again.body.code, 'TWO_FACTOR_EXPIRED');
});

test('five wrong codes end the second-factor step', async () => {
  const { agent, user } = await registerUser('codes');
  const authenticator = createVirtualAuthenticator({ userVerified: false });
  await registerSecurityKey(agent, authenticator);
  const secret = await enableTwoFactor(agent);
  const { started } = await signIn(authenticator, { userHandle: user.id });
  const send = (code) => newAgent().post('/api/devices/webauthn/2fa').send({ challengeId: started.body.challengeId, code });
  for (let index = 0; index < 5; index += 1) assert.equal((await send('000000')).body.code, 'INVALID_CODE');
  const after = await send(totp(secret));
  assert.equal(after.status, 401);
  assert.equal(after.body.code, 'TWO_FACTOR_EXPIRED');
});
