'use strict';

// The admin's side of service mail: reading and changing the SMTP settings,
// the test message, and vouching for a user's address.

const fs = require('fs');
const test = require('node:test');
const assert = require('node:assert/strict');
const dotenv = require('dotenv');
const { STRONG_PASSWORD, cleanup } = require('../helpers/app');
const { createClients } = require('../helpers/addresses');
const { installFakeMail } = require('../helpers/mail');
const { ENV_FILE } = require('../../runtime/paths');
const db = require('../../server/db/database');

const { api, session } = createClients();
const mail = installFakeMail();

test.after(() => {
  mail.restore();
  mail.reset();
  cleanup();
});

const SETTINGS = {
  from: 'NeoCloud <cloud@example.com>',
  smtpHost: 'smtp.example.com',
  smtpPort: 587,
  smtpUser: 'mailer',
  smtpPassword: 'S3cret pass#word',
  replyTo: 'help@example.com',
  brandName: 'Acme Cloud',
  supportUrl: 'https://example.com/help/',
  publicUrl: 'https://cloud.example.com',
  smtpSecure: false,
  smtpRequireTls: true,
  smtpRejectUnauthorized: true,
  requireSignupConfirmation: true,
  requireEmailChangeConfirmation: true,
  notifyNewLogin: false,
  notifyAccountChanges: true,
  tokenTtlHours: 12,
};

let admin;
let guest;

async function signUp(username) {
  const agent = session();
  const res = await agent.post('/api/auth/register')
    .send({ username, email: `${username}@example.com`, password: STRONG_PASSWORD });
  return { agent, res };
}

test('only an admin reaches the mail settings', async () => {
  admin = (await signUp('boss')).agent;
  guest = (await signUp('guest')).agent;
  for (const [method, path] of [
    ['get', '/api/admin/config/email'], ['put', '/api/admin/config/email'],
    ['post', '/api/admin/config/email/test'], ['post', '/api/admin/users/1/email/verify'],
  ]) {
    assert.equal((await guest[method](path).send({})).status, 403, `${method} ${path}`);
    assert.equal((await api[method](path).send({})).status, 403, `${method} ${path} signed out`);
  }
});

test('before setup the settings are empty and say what is missing', async () => {
  const res = await admin.get('/api/admin/config/email').expect(200);
  assert.equal(res.body.configured, false);
  assert.deepEqual(res.body.missing, ['Sender address', 'SMTP host']);
  assert.equal(res.body.settings.smtpPort, 587);
  assert.equal(res.body.settings.tokenTtlHours, 24);
  assert.equal(res.body.settings.smtpPasswordConfigured, false);
  assert.equal(res.body.settings.requireSignupConfirmation, true);
  assert.equal(res.body.settings.notifyNewLogin, true);
  assert.equal(res.body.settings.smtpRequireTls, true);
  assert.equal(res.body.settings.smtpSecure, false);
  assert.equal('smtpPassword' in res.body.settings, false);
});

test('a bad value is refused and nothing at all is written', async () => {
  const bad = [
    ['from', 'a@b.example\r\nBcc: x@y.example'],
    ['from', 'not an address'],
    ['from', 'Name <a@b.example> <c@d.example>'],
    ['replyTo', 'a@b.example,c@d.example'],
    ['smtpHost', 'smtp host with spaces'],
    ['smtpHost', 'smtp.example.com\nX'],
    ['supportUrl', 'ftp://example.com'],
    ['publicUrl', 'javascript:alert(1)'],
    ['brandName', 'two\nlines'],
    ['smtpUser', 5],
    ['smtpPort', 0], ['smtpPort', 70000], ['smtpPort', 'abc'], ['smtpPort', 25.5],
    ['tokenTtlHours', 0], ['tokenTtlHours', 9000],
    ['requireSignupConfirmation', 'true'], ['notifyNewLogin', 1],
    ['smtpPassword', 'line\nbreak'],
    ['smtpPassword', `it's "all" \`three\``],
  ];
  for (const [field, value] of bad) {
    const res = await admin.put('/api/admin/config/email').send({ ...SETTINGS, [field]: value });
    assert.equal(res.status, 400, `${field}=${JSON.stringify(value)}`);
    assert.equal(res.body.code, 'INVALID_VALUE');
  }
  assert.equal((await admin.put('/api/admin/config/email').send([1])).status, 400);
  const after = await admin.get('/api/admin/config/email').expect(200);
  assert.equal(after.body.configured, false);
  assert.equal(after.body.settings.smtpHost, '');
  assert.equal(fs.existsSync(ENV_FILE) && fs.readFileSync(ENV_FILE, 'utf8').includes('NEOCLOUD_EMAIL'), false);
});

test('saving the settings applies them at once and never returns the password', async () => {
  const res = await admin.put('/api/admin/config/email').send(SETTINGS);
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.equal(res.body.configured, true);
  assert.deepEqual(res.body.missing, []);
  assert.equal(res.body.settings.smtpPasswordConfigured, true);
  assert.equal(res.body.settings.supportUrl, 'https://example.com/help');
  assert.equal(res.body.settings.tokenTtlHours, 12);
  assert.equal(JSON.stringify(res.body).includes('S3cret'), false);
  const read = await admin.get('/api/admin/config/email').expect(200);
  assert.equal(JSON.stringify(read.body).includes('S3cret'), false);
  assert.equal((await api.get('/api/auth/status')).body.email.configured, true);

  // The file holds what dotenv reads back as exactly the password.
  const parsed = dotenv.parse(fs.readFileSync(ENV_FILE, 'utf8'));
  assert.equal(parsed.NEOCLOUD_EMAIL_SMTP_PASS, 'S3cret pass#word');
  assert.equal(parsed.NEOCLOUD_EMAIL_FROM, 'NeoCloud <cloud@example.com>');
  assert.equal(parsed.NEOCLOUD_EMAIL_SMTP_PORT, '587');
  assert.equal(process.env.NEOCLOUD_EMAIL_SMTP_PASS, 'S3cret pass#word');
  if (process.platform !== 'win32') assert.equal(fs.statSync(ENV_FILE).mode & 0o777, 0o600);
});

test('an update without the password keeps it; clearing removes it', async () => {
  const { smtpPassword, ...withoutPassword } = SETTINGS;
  const kept = await admin.put('/api/admin/config/email').send({ ...withoutPassword, smtpPort: 465, smtpSecure: true, smtpRequireTls: false });
  assert.equal(kept.body.settings.smtpPasswordConfigured, true);
  assert.equal(kept.body.settings.smtpPort, 465);
  const blank = await admin.put('/api/admin/config/email').send({ ...withoutPassword, smtpPassword: '' });
  assert.equal(blank.body.settings.smtpPasswordConfigured, true, 'an empty field is not a clear');
  const cleared = await admin.put('/api/admin/config/email').send({ ...withoutPassword, clearSmtpPassword: true });
  assert.equal(cleared.body.settings.smtpPasswordConfigured, false);
  assert.equal(process.env.NEOCLOUD_EMAIL_SMTP_PASS, undefined);
  // A partial update leaves the rest alone.
  const partial = await admin.put('/api/admin/config/email').send({ brandName: 'Zed' });
  assert.equal(partial.body.settings.brandName, 'Zed');
  assert.equal(partial.body.settings.smtpHost, 'smtp.example.com');
  await admin.put('/api/admin/config/email').send(SETTINGS).expect(200);
});

test('the test message goes to the admin, or to the address given, and the audit log records only the outcome', async () => {
  const res = await admin.post('/api/admin/config/email/test').send({});
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.equal(res.body.ok, true);
  const message = await mail.next();
  assert.deepEqual(message.to, ['boss@example.com']);
  assert.match(message.subject, /Acme Cloud test email/);
  assert.match(message.raw, /Reply-To: help@example\.com/);
  assert.match(message.text, /Support: https:\/\/example\.com\/help/);

  await admin.post('/api/admin/config/email/test').send({ to: 'Other@Example.com' }).expect(200);
  assert.deepEqual((await mail.next()).to, ['other@example.com']);
  assert.equal((await admin.post('/api/admin/config/email/test').send({ to: 'nope' })).body.code, 'INVALID_EMAIL');

  db.prepare("UPDATE users SET email = NULL WHERE username = 'boss'").run();
  assert.equal((await admin.post('/api/admin/config/email/test').send({})).body.code, 'EMAIL_REQUIRED');
  db.prepare("UPDATE users SET email = 'boss@example.com' WHERE username = 'boss'").run();

  const audit = await admin.get('/api/admin/audit').expect(200);
  const events = audit.body.entries.filter((entry) => entry.action === 'email.test');
  assert.ok(events.length >= 2);
  assert.ok(events.every((entry) => entry.detail.ok === true));
  assert.equal(JSON.stringify(audit.body).includes('other@example.com'), false);
});

test('a refused test explains why without the password, and an unset server refuses', async () => {
  await admin.put('/api/admin/config/email').send({ ...SETTINGS, smtpPassword: 'hunter2-hunter2' }).expect(200);
  mail.failNext(Object.assign(new Error('Invalid login: 535 5.7.8 rejected hunter2-hunter2 for mailer'), { code: 'EAUTH' }));
  const res = await admin.post('/api/admin/config/email/test').send({});
  assert.equal(res.status, 422);
  assert.equal(res.body.code, 'EMAIL_TEST_FAILED');
  assert.match(res.body.error, /Invalid login/);
  assert.match(res.body.error, /EAUTH/);
  assert.equal(res.body.error.includes('hunter2'), false);
  const audit = await admin.get('/api/admin/audit').expect(200);
  assert.ok(audit.body.entries.some((entry) => entry.action === 'email.test' && entry.detail.ok === false));

  mail.reset();
  const off = await admin.post('/api/admin/config/email/test').send({});
  assert.equal(off.status, 409);
  assert.equal(off.body.code, 'EMAIL_NOT_CONFIGURED');
  await admin.put('/api/admin/config/email').send(SETTINGS).expect(200);
});

test('test messages are limited to five per quarter hour per address', async () => {
  const fixed = { 'X-Forwarded-For': '198.51.100.77' };
  const statuses = [];
  for (let i = 0; i < 7; i += 1) {
    const res = await admin.post('/api/admin/config/email/test').set(fixed).send({});
    statuses.push(res.status);
    if (res.status === 429) assert.equal(res.body.code, 'RATE_LIMITED');
  }
  assert.deepEqual(statuses, [200, 200, 200, 200, 200, 429, 429]);
});

test('an admin can vouch for an address and lift a pending sign-up confirmation', async () => {
  const waiting = await signUp('waits');
  assert.equal(waiting.res.status, 202);
  const list = async () => (await admin.get('/api/admin/users').expect(200)).body.users;
  let user = (await list()).find((u) => u.username === 'waits');
  assert.equal(user.emailVerified, false);
  assert.equal(user.emailConfirmationPending, true);
  assert.equal((await list()).find((u) => u.username === 'boss').emailVerified, false);

  const login = () => api.post('/api/auth/login').send({ username: 'waits', password: STRONG_PASSWORD });
  assert.equal((await login()).status, 403);
  const res = await admin.post(`/api/admin/users/${user.id}/email/verify`).send({});
  assert.equal(res.status, 200);
  assert.equal(res.body.success, true);
  assert.equal((await login()).status, 200);
  user = (await list()).find((u) => u.username === 'waits');
  assert.equal(user.emailVerified, true);
  assert.equal(user.emailConfirmationPending, false);

  assert.equal((await admin.post('/api/admin/users/99999/email/verify').send({})).status, 404);
  assert.equal((await admin.post('/api/admin/users/abc/email/verify').send({})).status, 400);
  const audit = await admin.get('/api/admin/audit').expect(200);
  assert.ok(audit.body.entries.some((entry) => entry.action === 'account.email_verify'));
});
