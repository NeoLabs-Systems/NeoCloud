'use strict';

// Sign-up with and without service mail: the confirmation link, the sign-in
// gate, and resending (docs/email.md).

const test = require('node:test');
const assert = require('node:assert/strict');
const { STRONG_PASSWORD, cleanup, registerUser } = require('../helpers/app');
const { createClients } = require('../helpers/addresses');
const { installFakeMail } = require('../helpers/mail');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');

const { api } = createClients();
const mail = installFakeMail();
// Sign-ins here come from new addresses on purpose; the sign-in notice has its own test.
const configure = (extra = {}) => mail.configure({ NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN: 'false', ...extra });

test.after(() => {
  mail.restore();
  cleanup();
});

let admin;
let counter = 0;

function signUp(username, extra = {}) {
  return api.post('/api/auth/register').send({
    username,
    email: `${username}@example.com`,
    password: STRONG_PASSWORD,
    ...extra,
  });
}

function signIn(account, password = STRONG_PASSWORD) {
  return api.post('/api/auth/login').send({ username: account, password });
}

function userRow(username) {
  return db.prepare('SELECT * FROM users WHERE username = ?').get(username);
}

test('without mail configured, sign-up and the status are exactly as before', async () => {
  admin = await registerUser('admin');
  const status = await api.get('/api/auth/status').expect(200);
  assert.equal(status.body.email.configured, false);
  assert.equal(status.body.email.enabled, false);
  assert.equal(status.body.email.signupConfirmationRequired, false);

  const second = await signUp('plain1');
  assert.equal(second.status, 200, JSON.stringify(second.body));
  assert.equal(second.body.success, true);
  assert.equal(second.body.user.emailVerifiedAt, null);
  assert.equal(second.body.user.pendingEmail, null);
  assert.equal(mail.sent.length, 0);
  await signIn('plain1').expect(200);
});

test('with mail configured, status says so and a new account must confirm', async () => {
  configure();
  const status = await api.get('/api/auth/status').expect(200);
  assert.deepEqual(status.body.email, {
    enabled: true, configured: true, signupConfirmationRequired: true, emailChangeConfirmationRequired: true,
  });

  const res = await signUp('carol');
  assert.equal(res.status, 202, JSON.stringify(res.body));
  assert.equal(res.body.success, false);
  assert.equal(res.body.requiresEmailConfirmation, true);
  assert.equal(res.body.email, 'carol@example.com');
  assert.equal(typeof res.body.message, 'string');
  assert.equal(res.headers['set-cookie'], undefined, 'no session before confirmation');

  const message = await mail.next();
  assert.deepEqual(message.to, ['carol@example.com']);
  assert.equal(message, mail.sent[0]);
  assert.match(mail.linkOf(message), /^https:\/\/cloud\.example\.com\/api\/auth\/email\/confirm\?token=[\w-]{43}$/);
  assert.match(message.subject, /Confirm your NeoCloud email/);
  assert.match(message.html, /Confirm email/);
});

test('the account made before mail existed still signs in', async () => {
  const first = db.prepare('SELECT id, email_verified_at FROM users ORDER BY id LIMIT 1').get();
  assert.equal(first.email_verified_at, null, 'it was never confirmed, and never asked to be');
  await signIn(admin.username).expect(200);
});

test('an unconfirmed account is refused only after the right password', async () => {
  const wrong = await signIn('carol', 'Wrong-Password-1!');
  assert.equal(wrong.status, 401);
  assert.equal(wrong.body.code, 'INVALID_CREDENTIALS');
  const unknown = await signIn('nobody', 'Wrong-Password-1!');
  assert.equal(unknown.status, 401);

  const gated = await signIn('carol');
  assert.equal(gated.status, 403);
  assert.equal(gated.body.code, 'EMAIL_NOT_CONFIRMED');
  assert.equal(gated.body.requiresEmailConfirmation, true);
  assert.equal(typeof gated.body.error, 'string');
  assert.equal(gated.headers['set-cookie']?.some((c) => c.startsWith('neocloud.sid=')) || false, false);

  const byEmail = await signIn('carol@example.com');
  assert.equal(byEmail.status, 403);
  assert.equal(byEmail.body.code, 'EMAIL_NOT_CONFIRMED');
});

test('no route hands an unconfirmed account a device token', () => {
  assert.throws(
    () => createDeviceToken(userRow('carol').id, { name: 'Laptop', platform: 'linux' }),
    (err) => err.code === 'EMAIL_NOT_CONFIRMED' && err.status === 403 && err.requiresEmailConfirmation === true,
  );
});

test('opening the link in a browser does not spend it; the button does, once', async () => {
  const link = new URL(mail.linkOf(mail.sent[0]));
  const token = link.searchParams.get('token');
  const path = `${link.pathname}${link.search}`;

  for (let i = 0; i < 2; i += 1) {
    const page = await api.get(path);
    assert.equal(page.status, 200);
    assert.match(page.headers['content-type'], /text\/html/);
    assert.match(page.text, /method="post" action="\/api\/auth\/email\/confirm"/);
    assert.ok(page.text.includes(`name="token" value="${token}"`));
    assert.equal(page.headers['cache-control'], 'no-store, max-age=0');
    assert.equal(page.headers['referrer-policy'], 'strict-origin');
    assert.match(page.headers['content-security-policy'], /default-src 'none'/);
  }
  assert.equal((await signIn('carol')).status, 403, 'still unconfirmed after two GETs');

  const done = await api.post('/api/auth/email/confirm').type('form').send({ token });
  assert.equal(done.status, 200);
  assert.match(done.text, /Email confirmed/);
  assert.match(done.text, /href="\/app"/);

  const signedIn = await signIn('carol');
  assert.equal(signedIn.status, 200, JSON.stringify(signedIn.body));
  assert.ok(signedIn.body.user.emailVerifiedAt);

  const again = await api.post('/api/auth/email/confirm').type('form').send({ token });
  assert.equal(again.status, 400);
  assert.match(again.text, /does not work/);
  const gone = await api.get(path);
  assert.equal(gone.status, 400);
  assert.doesNotMatch(gone.text, /<form/);
});

test('wrong, missing, and expired links fail without a trace', async () => {
  const bad = await api.get('/api/auth/email/confirm?token=not-a-token');
  assert.equal(bad.status, 400);
  const missing = await api.get('/api/auth/email/confirm');
  assert.equal(missing.status, 400);
  const post = await api.post('/api/auth/email/confirm').type('form').send({ token: 'x'.repeat(43) });
  assert.equal(post.status, 400);
  const empty = await api.post('/api/auth/email/confirm').type('form').send({});
  assert.equal(empty.status, 400);

  await signUp('dave').expect(202);
  const message = await mail.next();
  const token = mail.tokenOf(message);
  db.prepare("UPDATE user_email_tokens SET expires_at = datetime('now', '-1 minute') WHERE user_id = ?").run(userRow('dave').id);
  const expired = await api.post('/api/auth/email/confirm').type('form').send({ token });
  assert.equal(expired.status, 400);
  assert.equal(userRow('dave').email_verified_at, null);
  assert.equal((await signIn('dave')).status, 403);
});

test('a link of a different type or account cannot be reused', async () => {
  // dave's expired link, and a password-reset token, are not confirmation links.
  await api.post('/api/auth/password/forgot').send({ account: 'carol' }).expect(200);
  const reset = await mail.next();
  const token = mail.tokenOf(reset);
  const res = await api.post('/api/auth/email/confirm').type('form').send({ token });
  assert.equal(res.status, 400);
  const form = await api.get(`/api/auth/email/confirm?token=${token}`);
  assert.equal(form.status, 400);
});

test('resend answers the same for every account and sends only where it should', async () => {
  const answers = [];
  for (const account of ['dave', 'nobody-here', 'carol', admin.username, '']) {
    const startedAt = Date.now();
    const res = await api.post('/api/auth/email/resend').send({ account });
    assert.ok(Date.now() - startedAt >= 330, `latency floor for "${account}"`);
    assert.equal(res.status, 200);
    answers.push(res.body);
  }
  assert.ok(answers.every((body) => body.success === true && body.message === answers[0].message));

  const message = await mail.next();
  assert.deepEqual(message.to, ['dave@example.com']);
  assert.equal(await mail.quiet(), true, 'nothing for unknown, confirmed or empty accounts');

  // The new link works; the old one died with it.
  const fresh = await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(message) });
  assert.equal(fresh.status, 200);
  assert.equal((await signIn('dave')).status, 200);
});

test('resend is throttled per account without changing the answer', async () => {
  await signUp('erin').expect(202);
  await mail.next();
  const before = mail.sent.length;
  const bodies = [];
  for (let i = 0; i < 4; i += 1) {
    const res = await api.post('/api/auth/email/resend').send({ account: 'erin' });
    assert.equal(res.status, 200);
    bodies.push(res.body);
  }
  await new Promise((resolve) => setTimeout(resolve, 250));
  // 1 from sign-up + 2 resends fill the 3-per-15-minutes allowance.
  assert.equal(mail.sent.length - before, 2);
  assert.ok(bodies.every((body) => body.message === bodies[0].message));
});

test('a failed confirmation mail takes the new account back', async () => {
  mail.failNext();
  const res = await signUp('frank');
  assert.equal(res.status, 503);
  assert.equal(res.body.code, 'EMAIL_SEND_FAILED');
  assert.equal(userRow('frank'), undefined);
  assert.equal(db.prepare("SELECT COUNT(*) AS n FROM user_email_tokens WHERE email = 'frank@example.com'").get().n, 0);
  // The name and the address are free again.
  await signUp('frank').expect(202);
});

test('with confirmation switched off, accounts sign in at once and stay unconfirmed', async () => {
  configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'false' });
  const before = mail.sent.length;
  const res = await signUp('gina');
  assert.equal(res.status, 200, JSON.stringify(res.body));
  assert.equal(res.body.user.emailVerifiedAt, null);
  assert.equal(mail.sent.length, before);
  assert.equal(userRow('gina').email_verified_at, null);
  // Switching confirmation on later never locks her out: she was never asked to confirm.
  configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'true' });
  await signIn('gina').expect(200);
  await signIn('plain1').expect(200);
});

test('the migration marks existing addresses as confirmed', () => {
  const Database = require('better-sqlite3');
  const { VERSIONED_MIGRATIONS, runVersionedMigrations } = require('../../lib/schema_migrations');
  const before = VERSIONED_MIGRATIONS.find((m) => m.name.startsWith('service email')).version - 1;
  const old = new Database(':memory:');
  old.pragma('foreign_keys = ON');
  for (const migration of VERSIONED_MIGRATIONS.filter((m) => m.version <= before)) {
    migration.up(old);
    old.pragma(`user_version = ${migration.version}`);
  }
  old.prepare("INSERT INTO users (username, email, password, created_at) VALUES ('has', 'has@example.com', 'x', '2020-01-02 03:04:05')").run();
  old.prepare("INSERT INTO users (username, email, password) VALUES ('blank', '  ', 'x')").run();
  old.prepare("INSERT INTO users (username, password) VALUES ('none', 'x')").run();
  runVersionedMigrations(old);
  const rows = Object.fromEntries(old.prepare('SELECT username, email_verified_at AS at FROM users').all().map((row) => [row.username, row.at]));
  assert.equal(rows.has, '2020-01-02 03:04:05');
  assert.equal(rows.blank, null);
  assert.equal(rows.none, null);
  // The token table cascades with the account.
  const id = old.prepare("SELECT id FROM users WHERE username = 'has'").get().id;
  old.prepare("INSERT INTO user_email_tokens (user_id, type, email, token_hash, expires_at) VALUES (?, 'password_reset', 'has@example.com', 'h', datetime('now'))").run(id);
  old.prepare('DELETE FROM users WHERE id = ?').run(id);
  assert.equal(old.prepare('SELECT COUNT(*) AS n FROM user_email_tokens').get().n, 0);
  assert.throws(() => old.prepare("INSERT INTO user_email_tokens (user_id, type, email, token_hash, expires_at) VALUES (1, 'bogus', 'a', 'b', 'c')").run());
  old.close();
});
