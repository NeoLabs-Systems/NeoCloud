'use strict';

// What an attacker can and cannot do with the mail routes: guess or reuse
// links, tell accounts apart, flood the endpoints, read tokens out of logs,
// or send a form from another site.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { STRONG_PASSWORD, cleanup, registerUser } = require('../helpers/app');
const { createClients } = require('../helpers/addresses');
const { installFakeMail } = require('../helpers/mail');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');
const { redactSecrets, loggedRequestPath, formatLogArgs } = require('../../server/utils/logger');

const { api, session } = createClients();
const mail = installFakeMail();
mail.configure({ NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN: 'false' });

test.after(() => {
  mail.restore();
  mail.reset();
  cleanup();
});

let owner;

async function confirmedUser(username) {
  const res = await api.post('/api/auth/register')
    .send({ username, email: `${username}@example.com`, password: STRONG_PASSWORD });
  assert.equal(res.status, 202, JSON.stringify(res.body));
  await api.post('/api/auth/email/confirm').type('form').send({ token: mail.tokenOf(await mail.next()) }).expect(200);
  const agent = session();
  await agent.post('/api/auth/login').send({ username, password: STRONG_PASSWORD }).expect(200);
  return agent;
}

test.before(async () => {
  owner = await registerUser('owner');
});

test('only a hash of a link is ever stored, and the hash is no link', async () => {
  await confirmedUser('ann');
  await api.post('/api/auth/password/forgot').send({ account: 'ann' }).expect(200);
  const message = await mail.next();
  const token = mail.tokenOf(message);
  const rows = db.prepare('SELECT token_hash FROM user_email_tokens').all();
  assert.ok(rows.length > 0);
  for (const { token_hash: hash } of rows) {
    assert.equal(hash.includes(token), false);
    assert.equal((await api.get(`/api/auth/password/reset?token=${hash}`)).status, 400);
    assert.equal((await api.post('/api/auth/email/confirm').type('form').send({ token: hash })).status, 400);
  }
  assert.ok(rows.some((row) => row.token_hash === crypto.createHash('sha256').update(token).digest('hex')));
  // Another kind of link does not open the other kind of door.
  assert.equal((await api.post('/api/auth/email/confirm').type('form').send({ token })).status, 400);
  assert.equal((await api.get(`/api/auth/email/confirm?token=${token}`)).status, 400);
  assert.equal((await api.get(`/api/auth/password/reset?token=${token}`)).status, 200);
});

test('a link that is not there leaves no trace on the page', async () => {
  const hostile = '"><img src=x onerror=alert(1)>';
  const confirm = await api.post('/api/auth/email/confirm').type('form').send({ token: hostile });
  const reset = await api.post('/api/auth/password/reset').type('form').send({ token: hostile, password: 'x', confirmPassword: 'x' });
  const got = await api.get(`/api/auth/password/reset?token=${encodeURIComponent(hostile)}`);
  for (const res of [confirm, reset, got]) {
    assert.equal(res.status, 400);
    assert.equal(res.text.includes('onerror'), false);
    assert.equal(res.text.includes(hostile), false);
  }
});

test('the pages are private, unframed, and allow no code but their own', async () => {
  const link = await (async () => {
    await api.post('/api/auth/password/forgot').send({ account: 'ann' });
    return mail.tokenOf(await mail.next());
  })();
  const res = await api.get(`/api/auth/password/reset?token=${link}`).expect(200);
  const csp = res.headers['content-security-policy'];
  assert.match(csp, /default-src 'none'/);
  assert.match(csp, /frame-ancestors 'none'/);
  assert.match(csp, /form-action 'self'/);
  assert.doesNotMatch(csp, /unsafe-inline'[^;]*;?\s*(?:font|img)/);
  const script = res.text.match(/<script>([\s\S]*?)<\/script>/)[1];
  const hash = crypto.createHash('sha256').update(script).digest('base64');
  assert.ok(csp.includes(`script-src 'sha256-${hash}'`), 'the inline script is allowed by hash only');
  assert.equal(res.headers['x-robots-tag'], 'noindex, nofollow');
  assert.equal(res.headers['cache-control'], 'no-store, max-age=0');
  assert.equal(res.headers['referrer-policy'], 'strict-origin');
  assert.equal(res.headers['set-cookie'], undefined, 'no session for a mail link');
  assert.doesNotMatch(res.text, /https?:\/\/(?!cloud\.example\.com)/, 'no other host is loaded');
});

test('tokens never reach the log', async () => {
  const secret = 'SECRET-TOKEN-VALUE-1234567890abcdefghijklmnop';
  assert.equal(redactSecrets(`GET /api/auth/email/confirm?token=${secret} 400`).includes(secret), false);
  assert.equal(redactSecrets(`https://x/api/auth/password/reset?token=${secret}&a=1`).includes(secret), false);
  assert.equal(loggedRequestPath(`/api/auth/password/reset?token=${secret}`).includes(secret), false);
  assert.equal(formatLogArgs([{ url: `/api/auth/email/confirm?token=${secret}`, token: secret }]).includes(secret), false);

  const lines = [];
  const originals = {};
  for (const level of ['log', 'info', 'warn', 'error']) {
    originals[level] = console[level];
    console[level] = (...args) => lines.push(formatLogArgs(args));
  }
  try {
    await api.get(`/api/auth/email/confirm?token=${secret}`);
    await api.get(`/api/auth/password/reset?token=${secret}`);
    await api.post(`/api/auth/email/confirm?token=${secret}`).type('form').send({ token: secret });
    await api.post('/api/auth/password/reset').type('form').send({ token: secret, password: 'Plum-Orbit-73!quartz-secret', confirmPassword: 'Plum-Orbit-73!quartz-secret' });
  } finally {
    Object.assign(console, originals);
  }
  assert.ok(lines.some((line) => line.includes('/api/auth/email/confirm')), 'the requests were logged');
  assert.equal(lines.some((line) => line.includes(secret)), false);
  assert.equal(lines.some((line) => line.includes('quartz-secret')), false);
});

test('no sign-in tells an unconfirmed account from an unknown one before the password is right', async () => {
  await api.post('/api/auth/register').send({ username: 'bob', email: 'bob@example.com', password: STRONG_PASSWORD }).expect(202);
  await mail.next();
  const [waiting, unknown, wrong] = await Promise.all([
    api.post('/api/auth/login').send({ username: 'bob', password: 'Wrong-Password-1!' }),
    api.post('/api/auth/login').send({ username: 'nobody', password: 'Wrong-Password-1!' }),
    api.post('/api/auth/login').send({ username: 'ann', password: 'Wrong-Password-1!' }),
  ]);
  for (const res of [waiting, unknown, wrong]) {
    assert.equal(res.status, 401);
    assert.deepEqual(res.body, { error: 'Invalid credentials', code: 'INVALID_CREDENTIALS' });
  }
  const gated = await api.post('/api/auth/login').send({ username: 'bob', password: STRONG_PASSWORD });
  assert.equal(gated.status, 403);
  assert.equal(gated.body.requiresEmailConfirmation, true);
  // It is not a way around two-factor or the disabled flag either.
  db.prepare("UPDATE users SET disabled_at = datetime('now') WHERE username = 'bob'").run();
  const disabled = await api.post('/api/auth/login').send({ username: 'bob', password: STRONG_PASSWORD });
  assert.equal(disabled.status, 403);
  db.prepare("UPDATE users SET disabled_at = NULL WHERE username = 'bob'").run();
});

test('forgot password takes about as long for an account that exists as for one that does not', async () => {
  const time = async (account) => {
    const startedAt = process.hrtime.bigint();
    await api.post('/api/auth/password/forgot').send({ account });
    return Number(process.hrtime.bigint() - startedAt) / 1e6;
  };
  const real = [await time('ann'), await time('ann')];
  const fake = [await time('no-such-person'), await time('no-such-person-2')];
  mail.drain();
  for (const ms of [...real, ...fake]) assert.ok(ms >= 330 && ms < 5000, `${ms}`);
  assert.ok(Math.abs(Math.max(...real) - Math.max(...fake)) < 600);
});

test('a disabled account gets no reset mail', async () => {
  await confirmedUser('cyrus');
  db.prepare("UPDATE users SET disabled_at = datetime('now') WHERE username = 'cyrus'").run();
  mail.drain();
  await api.post('/api/auth/password/forgot').send({ account: 'cyrus' }).expect(200);
  assert.equal(await mail.quiet(250), true);
});

test('changing the address needs a browser or the phone app, and the password', async () => {
  const me = await confirmedUser('dee');
  const desktop = createDeviceToken(db.prepare("SELECT id FROM users WHERE username = 'dee'").get().id, { name: 'PC', platform: 'windows' });
  const phone = createDeviceToken(db.prepare("SELECT id FROM users WHERE username = 'dee'").get().id, { name: 'Phone', platform: 'android' });
  const body = { email: 'dee.new@example.com', currentPassword: STRONG_PASSWORD };
  assert.equal((await api.put('/api/account/email').send(body)).status, 401);
  assert.equal((await api.put('/api/account/email').set('Authorization', `Bearer ${desktop.token}`).send(body)).status, 403);
  assert.equal((await api.post('/api/account/email/resend').set('Authorization', `Bearer ${desktop.token}`).send({})).status, 403);
  const withPhone = await api.put('/api/account/email').set('Authorization', `Bearer ${phone.token}`).send(body);
  assert.equal(withPhone.status, 200, JSON.stringify(withPhone.body));
  assert.equal(withPhone.body.emailChangePending, true);
  const wrong = await me.put('/api/account/email').send({ ...body, currentPassword: 'nope' });
  assert.equal(wrong.status, 401);
  await mail.next(); // the confirmation link
  await mail.next(); // the notice to the old address
});

test('a form from another site cannot spend a link', async () => {
  await api.post('/api/auth/password/forgot').send({ account: 'dee' });
  const token = mail.tokenOf(await mail.next());
  for (const origin of ['https://evil.example', 'null']) {
    const res = await api.post('/api/auth/password/reset').set('Origin', origin).type('form')
      .send({ token, password: 'Cobalt-Meadow-41!stone', confirmPassword: 'Cobalt-Meadow-41!stone' });
    assert.equal(res.status, 403, origin);
  }
  assert.equal((await api.get(`/api/auth/password/reset?token=${token}`)).status, 200, 'still unspent');
  const own = await api.post('/api/auth/password/reset').set('Origin', 'http://localhost').type('form')
    .send({ token, password: 'Cobalt-Meadow-41!stone', confirmPassword: 'Cobalt-Meadow-41!stone' });
  assert.equal(own.status, 200);
});

test('every mail route is limited per address, and the limit answers in the page’s own words', async () => {
  const fixed = (req) => req.set('X-Forwarded-For', '198.51.100.200');
  const statuses = async (make, count) => {
    const out = [];
    for (let i = 0; i < count; i += 1) out.push((await fixed(make())).status);
    return out;
  };
  const forgot = await statuses(() => api.post('/api/auth/password/forgot').send({ account: 'nobody' }), 12);
  assert.deepEqual(forgot.slice(0, 10), Array(10).fill(200));
  assert.deepEqual(forgot.slice(10), [429, 429]);
  const resend = await statuses(() => api.post('/api/auth/email/resend').send({ account: 'nobody' }), 11);
  assert.equal(resend[10], 429);

  const fixed2 = (req) => req.set('X-Forwarded-For', '198.51.100.201');
  const resets = [];
  for (let i = 0; i < 10; i += 1) {
    resets.push(await fixed2(api.post('/api/auth/password/reset').type('form').send({ token: `bad${i}`, password: 'x', confirmPassword: 'x' })));
  }
  assert.deepEqual(resets.map((res) => res.status), [...Array(8).fill(400), 429, 429]);
  assert.match(resets[9].text, /Too many tries/);
  assert.match(resets[9].headers['content-type'], /text\/html/);
  const confirms = [];
  for (let i = 0; i < 10; i += 1) {
    confirms.push(await fixed2(api.post('/api/auth/email/confirm').type('form').send({ token: `bad${i}` })));
  }
  assert.deepEqual(confirms.map((res) => res.status), [...Array(8).fill(400), 429, 429]);
});
