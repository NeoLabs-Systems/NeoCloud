'use strict';

// Asking a signed-in account who it is again: adding a security key and
// exporting the master key take the password and, with 2FA on, a code; those
// confirmations are throttled per account; a TOTP code works once and a
// recovery code is spent by one request only. Also: request logs carry no
// query strings, and an unset TRUST_PROXY is warned about.

const test = require('node:test');
const assert = require('node:assert/strict');
const { generateSync } = require('otplib');
const { STRONG_PASSWORD, cleanup, request } = require('../helpers/app');
const { createVirtualAuthenticator } = require('../helpers/webauthn');
const { createApp } = require('../../server/app');
const { trustProxyWarning, readHttpRuntimeConfig } = require('../../server/config/http');
const { loggedRequestPath } = require('../../server/utils/logger');
const twoFactor = require('../../server/services/account/two_factor');
const db = require('../../server/db/database');

const ORIGIN = 'http://localhost:3000';

// Each agent comes from its own address, so only the per-account throttle
// can decide a test.
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

// A code works once per time step, so a later code in a test is taken
// `ahead` steps on.
function totp(secret, ahead = 0) {
  return generateSync({
    secret, strategy: 'totp', digits: 6, period: 30, epoch: Math.floor(Date.now() / 1000) + 30 * ahead,
  });
}

async function enableTwoFactor(agent) {
  const setup = await agent.post('/api/account/2fa/setup').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  const enabled = await agent.post('/api/account/2fa/enable').send({ code: totp(setup.body.manualKey) }).expect(200);
  return { secret: setup.body.manualKey, recoveryCodes: enabled.body.recoveryCodes };
}

function startKeyRegistration(agent, body) {
  return agent.post('/api/account/security-keys/register/options').set('Origin', ORIGIN).send(body);
}

function auditActions(userId) {
  return db.prepare('SELECT action, detail_json FROM access_audit_log WHERE subject_user_id = ? ORDER BY id')
    .all(userId);
}

// The first account is the admin.
let admin;
test.before(async () => {
  admin = await registerUser('admin');
});

test('adding a security key asks for the password first', async () => {
  const { agent, user } = await registerUser('keypw');

  const bare = await startKeyRegistration(agent, {}).expect(401);
  assert.equal(bare.body.code, 'WRONG_PASSWORD');
  const wrong = await startKeyRegistration(agent, { currentPassword: 'not-it' }).expect(401);
  assert.equal(wrong.body.code, 'WRONG_PASSWORD');

  const authenticator = createVirtualAuthenticator();
  const started = await startKeyRegistration(agent, { currentPassword: STRONG_PASSWORD }).expect(200);
  const attestation = authenticator.register({ options: started.body.options, origin: ORIGIN });
  const done = await agent.post('/api/account/security-keys/register/verify').set('Origin', ORIGIN)
    .send({ response: attestation, label: 'Laptop' }).expect(200);
  const [key] = done.body.credentials;
  assert.ok(key.createdAt, 'the key shows when it was added');

  // On the record, by id only.
  const added = auditActions(user.id).find((row) => row.action === 'account.security_key_added');
  assert.ok(added);
  assert.deepEqual(JSON.parse(added.detail_json), { securityKeyId: key.id });
  await agent.delete(`/api/account/security-keys/${key.id}`).expect(200);
  assert.ok(auditActions(user.id).some((row) => row.action === 'account.security_key_removed'));
});

test('with 2FA on, adding a security key also asks for a code', async () => {
  const { agent } = await registerUser('key2fa');
  const { secret } = await enableTwoFactor(agent);

  const noCode = await startKeyRegistration(agent, { currentPassword: STRONG_PASSWORD }).expect(401);
  assert.equal(noCode.body.code, 'TWO_FACTOR_REQUIRED');
  const badCode = await startKeyRegistration(agent, { currentPassword: STRONG_PASSWORD, code: '000000' }).expect(401);
  assert.equal(badCode.body.code, 'INVALID_CODE');
  await startKeyRegistration(agent, { currentPassword: STRONG_PASSWORD, code: totp(secret, 1) }).expect(200);
});

test('master key export asks the admin for a 2FA code when it is on', async () => {
  const { secret } = await enableTwoFactor(admin.agent);
  const noCode = await admin.agent.post('/api/admin/keys/master').send({ password: STRONG_PASSWORD }).expect(401);
  assert.equal(noCode.body.code, 'TWO_FACTOR_REQUIRED');
  const res = await admin.agent.post('/api/admin/keys/master')
    .send({ password: STRONG_PASSWORD, code: totp(secret, 1) }).expect(200);
  assert.equal(res.body.masterKey, process.env.NEOCLOUD_MASTER_KEY);
});

test('a TOTP code is accepted once', async () => {
  const { agent, username } = await registerUser('replay');
  const { secret } = await enableTwoFactor(agent);
  const code = totp(secret, 1);

  const first = newAgent();
  await first.post('/api/auth/login').send({ username, password: STRONG_PASSWORD }).expect(200);
  await first.post('/api/auth/login/2fa').send({ code }).expect(200);

  // Seen over a shoulder or in a proxy log, it no longer signs in.
  const second = newAgent();
  await second.post('/api/auth/login').send({ username, password: STRONG_PASSWORD }).expect(200);
  const replay = await second.post('/api/auth/login/2fa').send({ code }).expect(401);
  assert.equal(replay.body.code, 'INVALID_CODE');
  // Nor can it confirm a sensitive action from the first session.
  const confirm = await first.post('/api/account/2fa/recovery-codes')
    .send({ currentPassword: STRONG_PASSWORD, code }).expect(401);
  assert.equal(confirm.body.code, 'INVALID_CODE');
});

test('a recovery code used by two requests at once works for one of them', async () => {
  const { agent, user } = await registerUser('race');
  const { recoveryCodes } = await enableTwoFactor(agent);
  const results = await Promise.all([
    twoFactor.verifyLoginCode(user.id, recoveryCodes[0]),
    twoFactor.verifyLoginCode(user.id, recoveryCodes[0]),
  ]);
  assert.deepEqual(results.sort(), [false, true]);
  assert.equal(await twoFactor.verifyLoginCode(user.id, recoveryCodes[0]), false);
});

test('password confirmations are throttled per account, whatever the address', async () => {
  const { user, username } = await registerUser('confirm');
  const agentFor = async () => {
    const agent = newAgent();
    await agent.post('/api/auth/login').send({ username, password: STRONG_PASSWORD }).expect(200);
    return agent;
  };
  const sessions = [await agentFor(), await agentFor()];
  const change = (index, currentPassword) => sessions[index % 2].put('/api/account/password')
    .set('X-Forwarded-For', `203.0.113.${index + 1}`)
    .send({ currentPassword, newPassword: 'Another-Long-Passphrase-91' });

  for (let i = 0; i < 5; i += 1) {
    assert.equal((await change(i, `wrong-${i}`).expect(401)).body.code, 'WRONG_PASSWORD');
  }
  // From the 5th failure on the next try waits, even with the right password.
  const locked = await change(5, STRONG_PASSWORD).expect(429);
  assert.equal(locked.body.code, 'LOGIN_LOCKED');
  assert.ok(locked.body.retryAfter >= 1);
  assert.equal(locked.headers['retry-after'], String(locked.body.retryAfter));

  // Counted apart from sign-in: the owner can still sign in.
  await newAgent().post('/api/auth/login').send({ username, password: STRONG_PASSWORD }).expect(200);
  // Once the wait is over a right password goes through and clears the count.
  db.prepare('UPDATE login_failures SET failed_at_ms = failed_at_ms - 60000 WHERE account_key = ?')
    .run(`confirm:user:${user.id}`);
  await sessions[0].post('/api/account/2fa/setup').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  const left = db.prepare('SELECT COUNT(*) AS count FROM login_failures WHERE account_key = ? AND kind = ?')
    .get(`confirm:user:${user.id}`, 'password').count;
  assert.equal(left, 0);
});

test('failed requests are logged without query strings or WebDAV file paths', async () => {
  assert.equal(loggedRequestPath('/api/search?q=Tax%20return%202025.pdf&type=file'), '/api/search');
  assert.equal(loggedRequestPath('/api/nodes/42/members'), '/api/nodes/42/members');
  assert.equal(loggedRequestPath('/dav/files/bob/Private/Diary.txt'), '/dav/files/bob/[path]');
  assert.equal(loggedRequestPath('/remote.php/webdav/Diary.txt?x=1'), '/remote.php/webdav/[path]');

  const lines = [];
  const originalWarn = console.warn;
  console.warn = (...args) => lines.push(args.map((arg) => (typeof arg === 'string' ? arg : JSON.stringify(arg))).join(' '));
  try {
    await admin.agent.get('/api/no-such-route?name=Secret%20Plans.pdf').expect(404);
    await new Promise((resolve) => setImmediate(resolve));
  } finally {
    console.warn = originalWarn;
  }
  const logged = lines.filter((line) => line.includes('/api/no-such-route'));
  assert.ok(logged.length > 0, 'the failure is logged');
  assert.ok(logged.every((line) => !line.includes('Secret') && !line.includes('?')), logged.join('\n'));
});

test('an unset TRUST_PROXY that trusts every private address is warned about', () => {
  const env = { PUBLIC_URL: 'https://cloud.example.com' };
  assert.match(trustProxyWarning(readHttpRuntimeConfig(env), env), /TRUST_PROXY/);
  const pinned = { ...env, TRUST_PROXY: '172.18.0.2' };
  assert.equal(trustProxyWarning(readHttpRuntimeConfig(pinned), pinned), null);
  const plain = { PUBLIC_URL: 'http://192.168.1.2:3344' };
  assert.equal(trustProxyWarning(readHttpRuntimeConfig(plain), plain), null);
});
