'use strict';

// Changing the email address, resending its confirmation, and the notices
// that tell the owner what happened to the account.

const test = require('node:test');
const assert = require('node:assert/strict');
const { STRONG_PASSWORD, cleanup } = require('../helpers/app');
const { createClients } = require('../helpers/addresses');
const { installFakeMail } = require('../helpers/mail');
const db = require('../../server/db/database');

const { api, session } = createClients();
const mail = installFakeMail();
// Sign-ins here come from new addresses on purpose; the sign-in notice has its own test.
const configure = (extra = {}) => mail.configure({ NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN: 'false', ...extra });

test.after(() => {
  mail.restore();
  cleanup();
});

function userRow(username) {
  return db.prepare('SELECT * FROM users WHERE username = ?').get(username);
}

async function signUp(username, { confirm = true } = {}) {
  const res = await api.post('/api/auth/register')
    .send({ username, email: `${username}@example.com`, password: STRONG_PASSWORD });
  if (res.status === 202 && confirm) {
    await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(await mail.next()) }).expect(200);
  }
  const agent = session();
  if (confirm) await agent.post('/api/auth/login').send({ username, password: STRONG_PASSWORD }).expect(200);
  return agent;
}

function changeEmail(agent, email, currentPassword = STRONG_PASSWORD) {
  return agent.put('/api/account/email').send({ email, currentPassword });
}

test('without mail an address changes at once, and nothing is sent', async () => {
  const boss = await signUp('boss');
  const kai = await signUp('kai');
  const res = await changeEmail(kai, 'Kai.New@Example.com');
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.equal(res.body.user.email, 'kai.new@example.com');
  assert.equal(res.body.user.emailVerifiedAt, null);
  assert.equal(res.body.emailChangePending, undefined);
  assert.equal(mail.sent.length, 0);
  await boss.get('/api/account').expect(200);
});

test('the change asks for the password and refuses bad or taken addresses', async () => {
  const kai = session();
  await kai.post('/api/auth/login').send({ username: 'kai', password: STRONG_PASSWORD }).expect(200);
  const wrong = await changeEmail(kai, 'x@example.com', 'Not-The-Password-1!');
  assert.equal(wrong.status, 401);
  assert.equal(wrong.body.code, 'WRONG_PASSWORD');
  assert.equal((await changeEmail(kai, 'not-an-address')).body.code, 'INVALID_EMAIL');
  const taken = await changeEmail(kai, 'boss@example.com');
  assert.equal(taken.status, 409);
  assert.equal(taken.body.code, 'EMAIL_TAKEN');
  const same = await changeEmail(kai, 'kai.new@example.com');
  assert.equal(same.status, 200);
  assert.equal(userRow('kai').email, 'kai.new@example.com');
});

test('with mail on, the new address only counts once its link is opened', async () => {
  configure();
  const lena = await signUp('lena');
  mail.drain();

  const res = await changeEmail(lena, 'Lena.New@example.com');
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.equal(res.body.emailChangePending, true);
  assert.equal(res.body.pendingEmail, 'lena.new@example.com');
  assert.equal(res.body.user.email, 'lena@example.com');

  const confirmation = await mail.next();
  assert.deepEqual(confirmation.to, ['lena.new@example.com']);
  assert.match(confirmation.subject, /Confirm your new NeoCloud email/);
  const requested = await mail.next();
  assert.deepEqual(requested.to, ['lena@example.com'], 'the current address is told');
  assert.match(requested.html, /lena\.new@example\.com/);

  const me = await lena.get('/api/account').expect(200);
  assert.equal(me.body.user.email, 'lena@example.com');
  assert.equal(me.body.user.pendingEmail, 'lena.new@example.com');
  assert.ok(me.body.user.emailVerifiedAt);
  assert.equal((await lena.get('/api/auth/me')).body.user.pendingEmail, 'lena.new@example.com');

  // A reset link sent to the old address dies when the address changes.
  await api.post('/api/auth/password/forgot').send({ account: 'lena' }).expect(200);
  const resetMail = await mail.next();
  assert.deepEqual(resetMail.to, ['lena@example.com']);

  const done = await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(confirmation) });
  assert.equal(done.status, 200);
  assert.match(done.text, /new .*email is confirmed/i);

  const now = userRow('lena');
  assert.equal(now.email, 'lena.new@example.com');
  assert.ok(now.email_verified_at);
  const notices = [await mail.next(), await mail.next()];
  assert.deepEqual(notices.map((m) => m.to[0]).sort(), ['lena.new@example.com', 'lena@example.com']);
  assert.ok(notices.every((m) => /email changed/i.test(m.subject)));
  assert.equal((await lena.get('/api/auth/me')).body.user.pendingEmail, null);

  const reset = await api.post('/api/auth/password/reset').type('form')
    .send({ token: mail.tokenOf(resetMail), password: 'Cobalt-Meadow-41!stone', confirmPassword: 'Cobalt-Meadow-41!stone' });
  assert.equal(reset.status, 400, 'the link to the old address no longer works');
  assert.equal((await api.post('/api/auth/login').send({ username: 'lena.new@example.com', password: STRONG_PASSWORD })).status, 200);
});

test('a link whose address was taken meanwhile fails and stays unspent', async () => {
  const mia = await signUp('mia');
  mail.drain();
  await changeEmail(mia, 'contested@example.com').expect(200);
  const link = await mail.next();
  await mail.next(); // the notice to the old address
  db.prepare("UPDATE users SET email = 'contested@example.com' WHERE username = 'boss'").run();
  const res = await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(link) });
  assert.equal(res.status, 409);
  assert.match(res.text, /already used/);
  assert.equal(userRow('mia').email, 'mia@example.com');
  assert.equal(db.prepare("SELECT COUNT(*) AS n FROM user_email_tokens WHERE user_id = ? AND consumed_at IS NULL").get(userRow('mia').id).n, 1);
  db.prepare("UPDATE users SET email = 'boss@example.com' WHERE username = 'boss'").run();
});

test('resending: to the pending address, or to an address that was never confirmed', async () => {
  const nora = await signUp('nora');
  mail.drain();
  // Nothing to confirm.
  const none = await nora.post('/api/account/email/resend').send({});
  assert.equal(none.status, 409);
  assert.equal(none.body.code, 'NOTHING_TO_CONFIRM');

  // Pending change.
  await changeEmail(nora, 'nora.new@example.com').expect(200);
  const first = await mail.next();
  await mail.next(); // the notice to the old address
  const again = await nora.post('/api/account/email/resend').send({});
  assert.equal(again.status, 200, JSON.stringify(again.body));
  assert.equal(again.body.pendingEmail, 'nora.new@example.com');
  const second = await mail.next();
  assert.deepEqual(second.to, ['nora.new@example.com']);
  assert.notEqual(mail.tokenOf(second), mail.tokenOf(first));
  assert.equal((await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(first) })).status, 400);
  await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(second) }).expect(200);
  await mail.next();
  await mail.next(); // both addresses are told

  // An address that was never confirmed (made while mail was off) confirms from settings.
  const oscar = await signUp('oscar');
  db.prepare('UPDATE users SET email_verified_at = NULL WHERE username = ?').run('oscar');
  mail.drain();
  const verify = await oscar.post('/api/account/email/resend').send({});
  assert.equal(verify.status, 200);
  assert.equal(verify.body.pendingEmail, null);
  const message = await mail.next();
  assert.deepEqual(message.to, ['oscar@example.com']);
  mail.drain();
  await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(message) }).expect(200);
  assert.ok(userRow('oscar').email_verified_at);
  assert.equal(await mail.quiet(200), true, 'confirming the address you already have changes nothing, so nobody is told');
  await oscar.get('/api/account').expect(200);
});

test('a failed mail leaves the old address in place and no live link behind', async () => {
  const pia = await signUp('pia');
  mail.drain();
  mail.failNext();
  const res = await changeEmail(pia, 'pia.new@example.com');
  assert.equal(res.status, 503);
  assert.equal(res.body.code, 'EMAIL_SEND_FAILED');
  assert.equal(userRow('pia').email, 'pia@example.com');
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM user_email_tokens WHERE user_id = ? AND consumed_at IS NULL').get(userRow('pia').id).n, 0);
  assert.equal((await pia.get('/api/auth/me')).body.user.pendingEmail, null);
});

test('with change confirmation off the address changes at once, unconfirmed, and both are told', async () => {
  configure({ NEOCLOUD_EMAIL_REQUIRE_EMAIL_CHANGE_CONFIRMATION: 'false' });
  const quin = await signUp('quin');
  mail.drain();
  const res = await changeEmail(quin, 'quin.new@example.com');
  assert.equal(res.status, 200);
  assert.equal(res.body.user.email, 'quin.new@example.com');
  assert.equal(res.body.user.emailVerifiedAt, null);
  assert.equal(res.body.emailChangePending, undefined);
  const notices = [await mail.next(), await mail.next()];
  assert.deepEqual(notices.map((m) => m.to[0]).sort(), ['quin.new@example.com', 'quin@example.com']);
  configure();
});

test('notices follow the account-change switch', async () => {
  const ray = await signUp('ray');
  mail.drain();
  await ray.put('/api/account/password')
    .send({ currentPassword: STRONG_PASSWORD, newPassword: 'Granite-Lantern-58!river' }).expect(200);
  const notice = await mail.next();
  assert.deepEqual(notice.to, ['ray@example.com']);
  assert.match(notice.subject, /password changed/i);

  configure({ NEOCLOUD_EMAIL_NOTIFY_ACCOUNT_CHANGES: 'false' });
  mail.drain();
  await ray.put('/api/account/password')
    .send({ currentPassword: 'Granite-Lantern-58!river', newPassword: 'Cobalt-Meadow-41!stone' }).expect(200);
  await changeEmail(ray, 'ray.new@example.com', 'Cobalt-Meadow-41!stone').expect(200);
  // Only the confirmation link itself goes out; no "requested" notice to the old address.
  const only = await mail.next();
  assert.deepEqual(only.to, ['ray.new@example.com']);
  assert.equal(await mail.quiet(250), true);
  configure();
});

test('a sign-in from a new address or browser is announced; a known one is not', async () => {
  mail.configure({ NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN: 'true' });
  const signIn = (username, ip, agent) => api.post('/api/auth/login')
    .set('X-Forwarded-For', ip).set('User-Agent', agent).send({ username, password: STRONG_PASSWORD });
  await signUp('sam', { confirm: false });
  const confirm = await mail.next();
  await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(confirm) }).expect(200);
  mail.drain();

  await signIn('sam', '198.51.100.7', 'Browser/1 (Mac)').expect(200);
  assert.equal(await mail.quiet(200), true, 'the very first sign-in says nothing');
  await signIn('sam', '198.51.100.7', 'Browser/1 (Mac)').expect(200);
  assert.equal(await mail.quiet(200), true, 'same address and browser');

  await signIn('sam', '203.0.113.9', 'Phone/2 (Android)').expect(200);
  const message = await mail.next();
  assert.deepEqual(message.to, ['sam@example.com']);
  assert.match(message.subject, /New NeoCloud sign-in/);
  assert.match(message.html, /203\.0\.113\.9/);
  assert.match(message.html, /Phone\/2 \(Android\)/);

  await signIn('sam', '203.0.113.99', 'Tablet/3').expect(200);
  assert.equal(await mail.quiet(200), true, 'at most one notice an hour');
});

test('the sign-in notice can be switched off', async () => {
  mail.configure({ NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN: 'false' });
  await signUp('tess');
  mail.drain();
  await api.post('/api/auth/login').set('User-Agent', 'Other/9').send({ username: 'tess', password: STRONG_PASSWORD }).expect(200);
  assert.equal(await mail.quiet(250), true);
});
