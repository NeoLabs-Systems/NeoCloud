'use strict';

// Your data (export, delete your account) and the admin's disable / enable
// and "sign out everywhere".

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { generateSync } = require('otplib');
const { STRONG_PASSWORD, cleanup, request } = require('../helpers/app');
const { createVirtualAuthenticator } = require('../helpers/webauthn');
const { createApp } = require('../../server/app');
const { BLOB_DIR } = require('../../runtime/paths');
const db = require('../../server/db/database');
const users = require('../../server/services/admin/users');
const { attachSocketServer } = require('../../server/services/events');

const ORIGIN = 'http://localhost:3000';

// Each agent comes from its own address, so the per-address limiters never
// decide a test.
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
  const res = await agent.post('/api/auth/register').send({ username, email: `${username}@example.com`, password: STRONG_PASSWORD });
  assert.equal(res.status, 200, JSON.stringify(res.body));
  return { agent, id: res.body.user.id, username };
}

function login(username, password = STRONG_PASSWORD) {
  return newAgent().post('/api/auth/login').send({ username, password });
}

function upload(agent, name, content, parentId = 'root') {
  return agent.put(`/api/upload?${new URLSearchParams({ parentId, name, conflict: 'replace' })}`)
    .set('Content-Type', 'application/octet-stream').send(Buffer.from(content));
}

function totp(secret) {
  return generateSync({ secret, strategy: 'totp', digits: 6, period: 30 });
}

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

async function deviceToken(agent, platform = 'lite') {
  return (await agent.post('/api/account/devices').send({ name: `Client ${platform}`, platform }).expect(201)).body.token;
}

function withToken(token) {
  return newAgent().get('/api/devices/me').set('Authorization', `Bearer ${token}`);
}

function dav(username, password) {
  return newAgent().propfind(`/dav/files/${username}/`).set('Depth', '0').auth(username, password);
}

async function registerSecurityKey(agent, authenticator) {
  const started = await agent.post('/api/account/security-keys/register/options').set('Origin', ORIGIN).expect(200);
  const attestation = authenticator.register({ options: started.body.options, origin: ORIGIN });
  await agent.post('/api/account/security-keys/register/verify').set('Origin', ORIGIN)
    .send({ response: attestation, label: 'Key' }).expect(200);
}

async function browserKeySignIn(authenticator, username) {
  const agent = newAgent();
  const started = await agent.post('/api/auth/webauthn/login/options').set('Origin', ORIGIN).send({ username }).expect(200);
  const assertion = authenticator.authenticate({ options: started.body.options, origin: ORIGIN });
  return agent.post('/api/auth/webauthn/login/verify').set('Origin', ORIGIN).send({ response: assertion });
}

async function deviceKeySignIn(authenticator, username) {
  const started = await newAgent().post('/api/devices/webauthn/options').send({ username }).expect(200);
  const assertion = authenticator.authenticate({ options: started.body.options, origin: ORIGIN });
  return newAgent().post('/api/devices/webauthn/verify')
    .send({ challengeId: started.body.challengeId, response: assertion, name: 'Laptop', platform: 'lite' });
}

let admin;

test.before(async () => {
  admin = await registerUser('admin');
});

test('export: the account as JSON, without file contents or credentials', async () => {
  const alice = await registerUser('alice');
  const bob = await registerUser('bob');
  const folder = (await alice.agent.post('/api/folders').send({ name: 'Projects' }).expect(201)).body;
  const file = (await upload(alice.agent, 'plan.txt', 'secret plan contents', folder.id).expect(201)).body;
  await upload(alice.agent, 'plan.txt', 'second version of the plan', folder.id).expect(201);
  const link = (await alice.agent.post('/api/shares').send({ nodeId: folder.id, password: 'link-password-1' }).expect(201)).body;
  await alice.agent.post(`/api/nodes/${folder.id}/members`).send({ recipient: bob.username, permission: 'read' }).expect(201);
  const token = await deviceToken(alice.agent);

  const wrong = await alice.agent.post('/api/account/export').send({ currentPassword: 'not-it' }).expect(401);
  assert.equal(wrong.body.code, 'WRONG_PASSWORD');

  const res = await alice.agent.post('/api/account/export').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  assert.match(res.headers['content-disposition'], /attachment; filename="?neocloud-account-alice\d+-\d{4}-\d{2}-\d{2}\.json/);
  assert.equal(res.headers['cache-control'], 'no-store');
  const data = res.body;
  assert.equal(data.kind, 'neocloud-account');
  assert.equal(data.profile.username, alice.username);
  for (const key of ['profile', 'storage', 'settings', 'security', 'sessions', 'devices', 'links', 'sharing', 'files', 'vaults', 'imports', 'audit']) {
    assert.ok(key in data, key);
  }
  const exported = data.files.find((node) => node.id === file.id);
  assert.equal(exported.name, 'plan.txt');
  assert.equal(exported.versions.length, 1);
  assert.ok(data.files.some((node) => node.id === folder.id && node.type === 'folder'));
  assert.equal(data.links[0].hasPassword, true);
  assert.equal(data.sharing.byMe[0].with, bob.username);
  assert.equal(data.devices[0].platform, 'lite');
  assert.ok(data.sessions.length >= 1);

  const text = JSON.stringify(data);
  const hash = db.prepare('SELECT password FROM users WHERE id = ?').get(alice.id).password;
  for (const secret of [link.token, token, hash, 'secret plan contents', 'second version']) {
    assert.ok(!text.includes(secret), 'export leaks a secret or file contents');
  }
  assert.doesNotMatch(text, /password_hash|token_hash|session_hash|wrapped|"salt"|public_key|password_enc/);

  // Bob sees his own side of the share.
  const bobs = (await bob.agent.post('/api/account/export').send({ currentPassword: STRONG_PASSWORD }).expect(200)).body;
  assert.equal(bobs.sharing.withMe[0].owner, alice.username);
});

test('delete: wrong password, wrong code, wrong confirmation, then gone with everything', async () => {
  const carol = await registerUser('carol');
  const bob = await registerUser('bob');
  const secret = await enableTwoFactor(carol.agent);
  const folder = (await carol.agent.post('/api/folders').send({ name: 'Shared' }).expect(201)).body;
  await upload(carol.agent, 'a.txt', 'alpha', folder.id).expect(201);
  await upload(carol.agent, 'a.txt', 'alpha two', folder.id).expect(201);
  const link = (await carol.agent.post('/api/shares').send({ nodeId: folder.id }).expect(201)).body;
  await carol.agent.post(`/api/nodes/${folder.id}/members`).send({ recipient: bob.username, permission: 'write' }).expect(201);
  const bobFolder = (await bob.agent.post('/api/folders').send({ name: 'For carol' }).expect(201)).body;
  await bob.agent.post(`/api/nodes/${bobFolder.id}/members`).send({ recipient: carol.username, permission: 'read' }).expect(201);
  const token = await deviceToken(carol.agent);
  assert.equal((await bob.agent.get('/api/shared-with-me').expect(200)).body.shares.length, 1);
  await newAgent().get(`/api/public/shares/${link.token}`).expect(200);
  const blobDir = path.join(BLOB_DIR, String(carol.id));
  assert.ok(fs.existsSync(blobDir));

  const body = { currentPassword: STRONG_PASSWORD, code: totp(secret), confirmUsername: carol.username };
  const wrongPassword = await carol.agent.post('/api/account/delete').send({ ...body, currentPassword: 'nope' }).expect(401);
  assert.equal(wrongPassword.body.code, 'WRONG_PASSWORD');
  const noCode = await carol.agent.post('/api/account/delete').send({ ...body, code: undefined }).expect(401);
  assert.equal(noCode.body.code, 'TWO_FACTOR_REQUIRED');
  const badCode = await carol.agent.post('/api/account/delete').send({ ...body, code: wrongCode(secret) }).expect(401);
  assert.equal(badCode.body.code, 'INVALID_CODE');
  const mismatch = await carol.agent.post('/api/account/delete').send({ ...body, confirmUsername: 'someone' }).expect(400);
  assert.equal(mismatch.body.code, 'CONFIRMATION_MISMATCH');

  const emitted = [];
  const sockets = { emit: () => {}, disconnectSockets: () => {} };
  attachSocketServer({
    to: (room) => ({ emit: (event, payload) => emitted.push({ room, event, payload }) }),
    in: () => sockets,
  });
  try {
    await carol.agent.post('/api/account/delete').send({ ...body, code: totp(secret) }).expect(200);
  } finally {
    attachSocketServer(null);
  }
  // Bob's screens refresh: his shared-with-me list and the folder he had open.
  const toBob = emitted.filter((entry) => entry.room === `user:${bob.id}`);
  assert.ok(toBob.some((entry) => entry.event === 'shares:changed'));
  assert.ok(toBob.some((entry) => entry.event === 'files:changed' && entry.payload.folderIds.includes(folder.id)));

  // Signed out, and the account is gone everywhere.
  await carol.agent.get('/api/account').expect(401);
  assert.equal((await login(carol.username)).status, 401);
  await withToken(token).expect(401);
  await newAgent().get(`/api/public/shares/${link.token}`).expect(404);
  assert.ok(!fs.existsSync(blobDir));
  for (const [table, column] of [
    ['nodes', 'user_id'], ['blobs', 'user_id'], ['shares', 'user_id'], ['device_tokens', 'user_id'],
    ['user_sessions', 'user_id'], ['user_two_factor', 'user_id'], ['user_recovery_codes', 'user_id'],
    ['search_docs', 'user_id'], ['changes', 'user_id'], ['user_shares', 'owner_id'], ['user_shares', 'grantee_id'],
  ]) {
    assert.equal(db.prepare(`SELECT COUNT(*) AS n FROM ${table} WHERE ${column} = ?`).get(carol.id).n, 0, `${table}.${column}`);
  }
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM file_versions v LEFT JOIN nodes n ON n.id = v.node_id WHERE n.id IS NULL').get().n, 0);

  // Bob lost carol's folder; his own folder stays his.
  assert.equal((await bob.agent.get('/api/shared-with-me').expect(200)).body.shares.length, 0);
  await bob.agent.get(`/api/nodes/${folder.id}`).expect(404);
  assert.equal((await bob.agent.get(`/api/nodes/${bobFolder.id}/members`).expect(200)).body.members.length, 0);
});

test('delete: the last admin is refused', async () => {
  const res = await admin.agent.post('/api/account/delete')
    .send({ currentPassword: STRONG_PASSWORD, confirmUsername: admin.username }).expect(409);
  assert.equal(res.body.code, 'LAST_ADMIN');
  await admin.agent.get('/api/account').expect(200);
});

test('disable: every sign-in route, session, token and link stops; enable restores sign-in', async () => {
  const dave = await registerUser('dave');
  const authenticator = createVirtualAuthenticator();
  await registerSecurityKey(dave.agent, authenticator);
  const token = await deviceToken(dave.agent);
  const appPassword = await deviceToken(dave.agent, 'webdav');
  const folder = (await dave.agent.post('/api/folders').send({ name: 'Public' }).expect(201)).body;
  const link = (await dave.agent.post('/api/shares').send({ nodeId: folder.id }).expect(201)).body;
  await withToken(token).expect(200);
  await dav(dave.username, appPassword).expect(207);

  // Sign-ins already under way when the account is disabled.
  const deviceCode = (await newAgent().post('/api/devices/authorize').send({ name: 'Laptop', platform: 'macos' }).expect(200)).body;
  await dave.agent.post(`/api/account/devices/authorize/${deviceCode.userCode}`).send({ approve: true }).expect(200);
  const pairing = (await dave.agent.post('/api/account/devices/pairing').send({}).expect(200)).body;
  const qrBrowser = newAgent();
  const qr = (await qrBrowser.post('/api/auth/qr-login/challenge').send({}).expect(200)).body;
  const qrSecret = new URL(qr.qrPayload).searchParams.get('secret');
  await dave.agent.post('/api/account/qr-login/approve').send({ challengeId: qr.challengeId, secret: qrSecret }).expect(200);

  const disabled = await admin.agent.put(`/api/admin/users/${dave.id}/disabled`).send({ disabled: true }).expect(200);
  assert.equal(disabled.body.disabled, true);
  const listed = (await admin.agent.get('/api/admin/users').expect(200)).body.users.find((user) => user.id === dave.id);
  assert.equal(listed.disabled, true);

  await dave.agent.get('/api/account').expect(401);
  await dave.agent.get('/api/nodes/root').expect(401);
  await withToken(token).expect(401);
  await dav(dave.username, appPassword).expect(401);
  await newAgent().get(`/api/public/shares/${link.token}`).expect(404);
  const password = await login(dave.username);
  assert.equal(password.status, 403);
  assert.equal(password.body.code, 'ACCOUNT_DISABLED');
  assert.equal((await login(dave.username, 'wrong-password')).status, 401);
  assert.equal((await browserKeySignIn(authenticator, dave.username)).status, 403);
  assert.equal((await deviceKeySignIn(authenticator, dave.username)).status, 403);
  assert.notEqual((await newAgent().post('/api/devices/token').send({ deviceCode: deviceCode.deviceCode })).status, 200);
  assert.notEqual((await newAgent().post('/api/devices/pair').send({ code: pairing.code, name: 'Phone', platform: 'android' })).status, 200);
  assert.notEqual((await qrBrowser.post(`/api/auth/qr-login/challenge/${qr.challengeId}/claim`).send({ token: qr.pollToken })).status, 200);

  const enabled = await admin.agent.put(`/api/admin/users/${dave.id}/disabled`).send({ disabled: false }).expect(200);
  assert.equal(enabled.body.disabled, false);
  assert.equal((await login(dave.username)).status, 200);
  assert.equal((await browserKeySignIn(authenticator, dave.username)).status, 200);
  await newAgent().get(`/api/public/shares/${link.token}`).expect(200);
  // What disabling ended stays ended.
  await dave.agent.get('/api/account').expect(401);
  await withToken(token).expect(401);
});

test('disable: a session or token the account still holds is refused on the next request', async () => {
  const erin = await registerUser('erin');
  const token = await deviceToken(erin.agent);
  // Straight in the database, as if revoking had missed them.
  db.prepare("UPDATE users SET disabled_at = datetime('now') WHERE id = ?").run(erin.id);
  await erin.agent.get('/api/account').expect(401);
  await withToken(token).expect(401);
  db.prepare('UPDATE users SET disabled_at = NULL WHERE id = ?').run(erin.id);
  await erin.agent.get('/api/account').expect(200);
  await withToken(token).expect(200);
});

test('disable: not yourself, not the last admin, only true or false', async () => {
  const self = await admin.agent.put(`/api/admin/users/${admin.id}/disabled`).send({ disabled: true }).expect(400);
  assert.equal(self.body.code, 'CANNOT_DISABLE_SELF');
  const other = await registerUser('frank');
  const invalid = await admin.agent.put(`/api/admin/users/${other.id}/disabled`).send({ disabled: 'yes' }).expect(400);
  assert.equal(invalid.body.code, 'INVALID_VALUE');
  await other.agent.put(`/api/admin/users/${admin.id}/disabled`).send({ disabled: true }).expect(403);
  // Only another admin could try this; the server still refuses.
  assert.throws(() => users.setUserDisabled(admin.id, true, { actorUserId: other.id }), { code: 'LAST_ADMIN' });
  await admin.agent.get('/api/account').expect(200);
});

test('sign out everywhere: every session and device token, sign-in still works', async () => {
  const gina = await registerUser('gina');
  const second = await login(gina.username);
  assert.equal(second.status, 200);
  const token = await deviceToken(gina.agent);
  const appPassword = await deviceToken(gina.agent, 'webdav');

  const res = await admin.agent.delete(`/api/admin/users/${gina.id}/sessions`).expect(200);
  assert.equal(res.body.sessions, 2);
  assert.equal(res.body.devices, 2);
  await gina.agent.get('/api/account').expect(401);
  await withToken(token).expect(401);
  await dav(gina.username, appPassword).expect(401);
  assert.equal((await login(gina.username)).status, 200);

  await gina.agent.delete(`/api/admin/users/${admin.id}/sessions`).expect(403);
  // An admin signing themselves out everywhere keeps this session.
  await admin.agent.delete(`/api/admin/users/${admin.id}/sessions`).expect(200);
  await admin.agent.get('/api/account').expect(200);
});
