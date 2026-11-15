'use strict';

// "Forgot password" and the reset page, with mail on from the very first
// account (which never has to confirm).

const test = require('node:test');
const assert = require('node:assert/strict');
const { STRONG_PASSWORD, cleanup, request } = require('../helpers/app');
const { createClients } = require('../helpers/addresses');
const { installFakeMail } = require('../helpers/mail');
const db = require('../../server/db/database');
const throttle = require('../../server/services/account/login_throttle');
const { createDeviceToken, resolveDeviceToken } = require('../../server/services/devices/tokens');

const { api, session } = createClients();
const mail = installFakeMail();
// Every request below comes from a new address; the new sign-in notice has its own test.
mail.configure({ NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN: 'false' });

test.after(() => {
  mail.restore();
  cleanup();
});

const NEW_PASSWORD = 'Granite-Lantern-58!river';
let owner;

function userRow(username) {
  return db.prepare('SELECT * FROM users WHERE username = ?').get(username);
}

function signIn(account, password = STRONG_PASSWORD, agent = api) {
  return agent.post('/api/auth/login').send({ username: account, password });
}

// Registers, reads the link from the mail and confirms; returns a signed-in agent.
async function confirmedUser(username) {
  await api.post('/api/auth/register')
    .send({ username, email: `${username}@example.com`, password: STRONG_PASSWORD }).expect(202);
  const message = await mail.next();
  await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(message) }).expect(200);
  const agent = session();
  await signIn(username, STRONG_PASSWORD, agent).expect(200);
  return agent;
}

async function forgot(account, headers = {}) {
  let req = api.post('/api/auth/password/forgot');
  for (const [name, value] of Object.entries(headers)) req = req.set(name, value);
  return req.send({ account });
}

async function resetLink(account) {
  mail.drain();
  assert.equal((await forgot(account)).status, 200);
  return mail.tokenOf(await mail.next());
}

test('the first account signs up and in with mail on, unconfirmed', async () => {
  const res = await api.post('/api/auth/register')
    .send({ username: 'owner', email: 'owner@example.com', password: STRONG_PASSWORD });
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.equal(res.body.user.isAdmin, true);
  assert.equal(res.body.user.emailVerifiedAt, null);
  assert.equal(mail.sent.length, 0);
  owner = userRow('owner');
});

test('forgot password answers alike for everyone and mails only a reachable account', async () => {
  await confirmedUser('hana');
  // dora is still waiting for her sign-up link.
  await api.post('/api/auth/register')
    .send({ username: 'dora', email: 'dora@example.com', password: STRONG_PASSWORD }).expect(202);
  await mail.next();

  const bodies = [];
  for (const account of ['hana', 'HANA@Example.com', 'dora', 'nobody-at-all', '', 42]) {
    const startedAt = Date.now();
    const res = await forgot(account);
    assert.ok(Date.now() - startedAt >= 330, `latency floor for ${JSON.stringify(account)}`);
    assert.equal(res.status, 200);
    bodies.push(res.body);
  }
  assert.ok(bodies.every((body) => body.success === true && body.message === bodies[0].message));

  const first = await mail.next();
  assert.deepEqual(first.to, ['hana@example.com']);
  assert.match(first.subject, /Reset your NeoCloud password/);
  assert.match(first.text, /Reset password: https:\/\/cloud\.example\.com\/api\/auth\/password\/reset\?token=/);
  // by address, too: a second link for the same account
  const second = await mail.next();
  assert.deepEqual(second.to, ['hana@example.com']);
  assert.equal(await mail.quiet(), true, 'nothing for the waiting, the unknown and the empty');
});

test('links in mails never come from the request', async () => {
  assert.equal((await forgot('hana', { Host: 'evil.example', 'X-Forwarded-Host': 'evil.example' })).status, 200);
  const message = await mail.next();
  assert.equal(new URL(mail.linkOf(message)).origin, 'https://cloud.example.com');
  assert.ok(!message.html.includes('evil.example') && !message.raw.includes('evil.example'));
});

test('mails come in German for a German browser', async () => {
  await confirmedUser('greta');
  assert.equal((await forgot('greta', { 'Accept-Language': 'de-DE,de;q=0.9' })).status, 200);
  const message = await mail.next();
  assert.match(message.subject, /Setze dein NeoCloud-Passwort zurück/);
  assert.match(message.html, /lang="de"/);
  assert.match(message.html, /Passwort zurücksetzen/);
  const page = await api.get(`/api/auth/password/reset?token=${mail.tokenOf(message)}`).set('Accept-Language', 'de');
  assert.match(page.text, /Neues Passwort festlegen/);
});

test('requests for reset mail are limited per account', async () => {
  // hana already holds 3 links from the tests above.
  mail.drain();
  assert.equal((await forgot('hana')).status, 200);
  assert.equal(await mail.quiet(250), true);
  assert.equal(db.prepare("SELECT COUNT(*) AS n FROM user_email_tokens WHERE user_id = ? AND type = 'password_reset'").get(userRow('hana').id).n, 3);
});

test('the reset page: a form for a live link, nothing for the rest, and no side effects', async () => {
  const user = userRow('hana');
  const live = db.prepare("SELECT token_hash FROM user_email_tokens WHERE user_id = ? AND consumed_at IS NULL").all(user.id);
  assert.equal(live.length, 1, 'older links die when a new one is issued');

  const missing = await api.get('/api/auth/password/reset');
  assert.equal(missing.status, 400);
  const bad = await api.get('/api/auth/password/reset?token=%22%3E%3Cscript%3Ealert(1)%3C/script%3E');
  assert.equal(bad.status, 400);
  assert.ok(!bad.text.includes('<script>alert'));
  assert.doesNotMatch(bad.text, /<form/);
});

test('a reset sets the password, ends every session and device, clears throttles and confirms the address', async () => {
  const agent = await confirmedUser('iris');
  const other = session();
  await signIn('iris', STRONG_PASSWORD, other).expect(200);
  await agent.get('/api/account').expect(200);
  const iris = userRow('iris');
  const device = createDeviceToken(iris.id, { name: 'Laptop', platform: 'linux' });
  assert.ok(resolveDeviceToken(device.token));
  // Someone has been guessing: the account is locked.
  for (let i = 0; i < 10; i += 1) throttle.recordFailure(throttle.accountKey(iris), { kind: 'password' });
  assert.equal((await signIn('iris')).status, 429);
  // Make her address look unconfirmed (an account from before confirmation was required).
  db.prepare('UPDATE users SET email_verified_at = NULL WHERE id = ?').run(iris.id);

  const token = await resetLink('iris');
  const form = await api.get(`/api/auth/password/reset?token=${token}`);
  assert.equal(form.status, 200);
  assert.match(form.text, /name="password"/);
  assert.match(form.text, /name="confirmPassword"/);
  assert.ok(form.text.includes(`name="token" value="${token}"`));
  assert.match(form.headers['content-security-policy'], /script-src 'sha256-/);
  assert.equal(form.headers['cache-control'], 'no-store, max-age=0');

  // A mismatch and a weak password both keep the link usable.
  const mismatch = await api.post('/api/auth/password/reset').type('form')
    .send({ token, password: NEW_PASSWORD, confirmPassword: 'something else' });
  assert.equal(mismatch.status, 400);
  assert.match(mismatch.text, /do not match/);
  const weak = await api.post('/api/auth/password/reset').type('form')
    .send({ token, password: 'password1', confirmPassword: 'password1' });
  assert.equal(weak.status, 400);
  assert.match(weak.text, /too weak|at least/i);
  assert.match(weak.text, /<form/);
  const own = await api.post('/api/auth/password/reset').type('form')
    .send({ token, password: 'iris-iris-123!X', confirmPassword: 'iris-iris-123!X' });
  assert.equal(own.status, 400, 'a password containing the username is refused');

  const done = await api.post('/api/auth/password/reset').type('form')
    .send({ token, password: NEW_PASSWORD, confirmPassword: NEW_PASSWORD });
  assert.equal(done.status, 200, done.text.slice(0, 400));
  assert.match(done.text, /Password changed/);

  await agent.get('/api/account').expect(401);
  await other.get('/api/account').expect(401);
  assert.equal(resolveDeviceToken(device.token), null);
  assert.ok(userRow('iris').email_verified_at, 'owning the inbox confirms the address');
  assert.equal((await signIn('iris')).status, 401, 'the old password is gone');
  const fresh = await signIn('iris', NEW_PASSWORD);
  assert.equal(fresh.status, 200, 'the lock from the guessing is gone too');

  const notice = await mail.next();
  assert.deepEqual(notice.to, ['iris@example.com']);
  assert.match(notice.subject, /password changed/i);

  const again = await api.post('/api/auth/password/reset').type('form')
    .send({ token, password: NEW_PASSWORD, confirmPassword: NEW_PASSWORD });
  assert.equal(again.status, 400);
  assert.match(again.text, /does not work/);
});

test('two submissions of one link set exactly one password', async () => {
  await confirmedUser('jade');
  const token = await resetLink('jade');
  const passwords = ['Cobalt-Meadow-41!stone', 'Velvet-Harbor-92!cloud'];
  const results = await Promise.all(passwords.map((password) => api.post('/api/auth/password/reset')
    .type('form').send({ token, password, confirmPassword: password })));
  assert.deepEqual(results.map((res) => res.status).sort(), [200, 400]);
  const winner = passwords[results.findIndex((res) => res.status === 200)];
  assert.equal((await signIn('jade', winner)).status, 200);
  assert.equal((await signIn('jade', passwords.find((p) => p !== winner))).status, 401);
});

test('an expired link, and a link whose address left the account, no longer work', async () => {
  const token = await resetLink('jade');
  db.prepare("UPDATE user_email_tokens SET expires_at = datetime('now', '-1 second') WHERE user_id = ?").run(userRow('jade').id);
  assert.equal((await api.get(`/api/auth/password/reset?token=${token}`)).status, 400);
  const res = await api.post('/api/auth/password/reset').type('form')
    .send({ token, password: NEW_PASSWORD, confirmPassword: NEW_PASSWORD });
  assert.equal(res.status, 400);

  const second = await resetLink('jade');
  db.prepare("UPDATE users SET email = 'jade-new@example.com' WHERE username = 'jade'").run();
  const moved = await api.post('/api/auth/password/reset').type('form')
    .send({ token: second, password: NEW_PASSWORD, confirmPassword: NEW_PASSWORD });
  assert.equal(moved.status, 400);
});

test('with mail off, forgot password does nothing and says the same', async () => {
  mail.reset();
  mail.drain();
  const res = await forgot('owner');
  assert.equal(res.status, 200);
  assert.equal(res.body.success, true);
  assert.equal(await mail.quiet(), true);
  assert.equal(owner.username, 'owner');
});
