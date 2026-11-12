'use strict';

// The phone app is a full client: its device token (android/ios) reaches
// account, settings, imports and admin like a browser session. Desktop
// tokens and WebDAV app passwords still don't.

const test = require('node:test');
const assert = require('node:assert/strict');
const { generateSync } = require('otplib');
const { STRONG_PASSWORD, app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');

test.after(cleanup);

let admin;
let member;

test.before(async () => {
  admin = await registerUser('phoneadmin'); // the first account is the admin
  member = await registerUser('phonemember');
});

function totp(secret) {
  return generateSync({ secret, strategy: 'totp', digits: 6, period: 30 });
}

// Issued by the service: the account routes' rate limit is for people.
function issueToken(account, platform) {
  const { token, device } = createDeviceToken(account.user.id, { name: `Client ${platform}`, platform });
  return { bearer: `Bearer ${token}`, id: device.id };
}

function as(bearer) {
  const call = (method) => (path) => request(app)[method](path).set('Authorization', bearer);
  return { get: call('get'), put: call('put'), post: call('post'), delete: call('delete') };
}

test('a phone token reads and updates the account and settings', async () => {
  const phone = as(issueToken(member, 'android').bearer);
  const account = await phone.get('/api/account').expect(200);
  assert.equal(account.body.user.username, member.username);
  assert.equal(account.body.user.isAdmin, false);

  const renamed = await phone.put('/api/account/display-name').send({ displayName: 'Phone Name' }).expect(200);
  assert.equal(renamed.body.user.displayName, 'Phone Name');
  await phone.get('/api/account/usage').expect(200);

  const settings = await phone.put('/api/settings').send({ theme: 'dark' }).expect(200);
  assert.equal(settings.body.settings.theme, 'dark');
  assert.equal((await phone.get('/api/settings').expect(200)).body.settings.theme, 'dark');
  await phone.get('/api/imports').expect(200);
});

test('a phone lists sessions and devices, marks itself, and keeps itself', async () => {
  const issued = issueToken(member, 'ios');
  const phone = as(issued.bearer);
  const before = db.prepare('SELECT COUNT(*) AS n FROM user_sessions WHERE user_id = ?').get(member.user.id).n;
  const sessions = await phone.get('/api/account/sessions').expect(200);
  assert.ok(sessions.body.sessions.length >= 1);
  assert.ok(sessions.body.sessions.every((session) => session.current === false));
  // Listing from a phone records no phantom browser session.
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM user_sessions WHERE user_id = ?').get(member.user.id).n, before);

  const devices = (await phone.get('/api/account/devices').expect(200)).body.devices;
  assert.deepEqual(devices.filter((device) => device.current).map((device) => device.id), [issued.id]);
  const self = await phone.delete(`/api/account/devices/${issued.id}`).expect(400);
  assert.equal(self.body.code, 'CURRENT_DEVICE');

  const other = issueToken(member, 'lite');
  await phone.delete(`/api/account/devices/${other.id}`).expect(200);
  await request(app).get('/api/devices/me').set('Authorization', other.bearer).expect(401);
});

test('2FA setup and disable work from a phone and still ask for the password', async () => {
  const user = await registerUser('phone2fa');
  const phone = as(issueToken(user, 'android').bearer);
  await phone.post('/api/account/2fa/setup').send({ currentPassword: 'wrong' }).expect(401);
  const setup = await phone.post('/api/account/2fa/setup').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  await phone.post('/api/account/2fa/enable').send({ code: totp(setup.body.manualKey) }).expect(200);
  assert.equal((await phone.get('/api/account').expect(200)).body.twoFactor.enabled, true);
  await phone.post('/api/account/2fa/disable').send({ currentPassword: STRONG_PASSWORD }).expect(401);
  await phone.post('/api/account/2fa/disable').send({ currentPassword: STRONG_PASSWORD, code: totp(setup.body.manualKey) }).expect(200);
});

test('security keys are added in a browser only', async () => {
  const phone = as(issueToken(member, 'android').bearer);
  await phone.get('/api/account/security-keys').expect(200);
  const refused = await phone.post('/api/account/security-keys/register/options').send({}).expect(403);
  assert.equal(refused.body.code, 'BROWSER_REQUIRED');
  await phone.post('/api/account/security-keys/register/verify').send({}).expect(403);
});

test('a password change from a phone keeps that phone and signs out everything else', async () => {
  const user = await registerUser('phonepw');
  const phone = issueToken(user, 'android');
  const desktop = issueToken(user, 'lite');
  await as(phone.bearer).put('/api/account/password').send({ currentPassword: 'nope', newPassword: 'Another-Long-Pass-42!' }).expect(401);
  await as(phone.bearer).put('/api/account/password')
    .send({ currentPassword: STRONG_PASSWORD, newPassword: 'Another-Long-Pass-42!' }).expect(200);
  await request(app).get('/api/devices/me').set('Authorization', phone.bearer).expect(200);
  await request(app).get('/api/devices/me').set('Authorization', desktop.bearer).expect(401);
  await user.agent.get('/api/account').expect(401);
});

test('an admin phone uses the admin routes; a member phone gets 403', async () => {
  const phone = as(issueToken(admin, 'ios').bearer);
  const listed = await phone.get('/api/admin/users').expect(200);
  assert.ok(JSON.stringify(listed.body).includes(member.username));
  await phone.get('/api/admin/config/storage').expect(200);
  await phone.get('/api/admin/backups').expect(200);
  await phone.post('/api/admin/keys/master').send({ password: 'wrong' }).expect(401);

  const memberPhone = as(issueToken(member, 'android').bearer);
  assert.equal((await memberPhone.get('/api/admin/users').expect(403)).body.code, 'ADMIN_REQUIRED');
});

test('admin is read on every request, for a phone token too', async () => {
  const user = await registerUser('phonepromoted');
  const phone = as(issueToken(user, 'android').bearer);
  await phone.get('/api/admin/users').expect(403);
  await admin.agent.put(`/api/admin/users/${user.user.id}/admin`).send({ isAdmin: true }).expect(200);
  await phone.get('/api/admin/users').expect(200);
  await admin.agent.put(`/api/admin/users/${user.user.id}/admin`).send({ isAdmin: false }).expect(200);
  await phone.get('/api/admin/users').expect(403);
});

test('an admin signing themselves out everywhere from a phone keeps the phone', async () => {
  const self = await registerUser('phoneselfadmin');
  await admin.agent.put(`/api/admin/users/${self.user.id}/admin`).send({ isAdmin: true }).expect(200);
  const phone = issueToken(self, 'android');
  const desktop = issueToken(self, 'macos');
  await as(phone.bearer).delete(`/api/admin/users/${self.user.id}/sessions`).expect(200);
  await request(app).get('/api/devices/me').set('Authorization', phone.bearer).expect(200);
  await request(app).get('/api/devices/me').set('Authorization', desktop.bearer).expect(401);
  await self.agent.get('/api/account').expect(401);
});

test('desktop tokens and WebDAV app passwords reach no account, settings, imports or admin route', async () => {
  for (const platform of ['lite', 'macos', 'windows', 'linux', 'webdav', '']) {
    const { bearer } = issueToken(admin, platform);
    const client = as(bearer);
    assert.equal((await client.get('/api/account').expect(403)).body.code, 'PHONE_REQUIRED', platform);
    await client.get('/api/account/sessions').expect(403);
    await client.put('/api/account/password').send({ currentPassword: STRONG_PASSWORD, newPassword: 'x' }).expect(403);
    await client.get('/api/settings').expect(403);
    await client.get('/api/imports').expect(403);
    await client.get('/api/admin/users').expect(403);
  }
  await request(app).get('/api/account').set('Authorization', 'Bearer nc_forged').expect(401);
  await request(app).get('/api/admin/users').set('Authorization', 'Bearer nc_forged').expect(403);
});

test("a disabled account's phone token is refused everywhere", async () => {
  const user = await registerUser('phonedisabled');
  const { bearer } = issueToken(user, 'android');
  await admin.agent.put(`/api/admin/users/${user.user.id}/admin`).send({ isAdmin: true }).expect(200);
  await as(bearer).get('/api/admin/users').expect(200);
  // Re-enabling does not bring back the tokens disabling revoked, so mark the
  // row directly: the token itself must stop working while the flag is set.
  db.prepare("UPDATE users SET disabled_at = datetime('now') WHERE id = ?").run(user.user.id);
  await as(bearer).get('/api/account').expect(401);
  await as(bearer).get('/api/settings').expect(401);
  await as(bearer).get('/api/admin/users').expect(403);
  await as(bearer).get('/api/folders/root/children').expect(401);
  db.prepare('UPDATE users SET disabled_at = NULL WHERE id = ?').run(user.user.id);
  await as(bearer).get('/api/account').expect(200);
});

test('a phone deletes its own account', async () => {
  const user = await registerUser('phonedelete');
  const { bearer } = issueToken(user, 'ios');
  await as(bearer).post('/api/account/delete')
    .send({ currentPassword: STRONG_PASSWORD, confirmUsername: 'someone-else' }).expect(400);
  await as(bearer).post('/api/account/delete')
    .send({ currentPassword: STRONG_PASSWORD, confirmUsername: user.username }).expect(200);
  await as(bearer).get('/api/account').expect(401);
  assert.equal(db.prepare('SELECT 1 FROM users WHERE id = ?').get(user.user.id), undefined);
});
