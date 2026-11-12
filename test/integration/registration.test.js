'use strict';

// Registration is a plain on/off switch, and the first account on a fresh
// install can always register.

process.env.NEOCLOUD_ALLOW_SIGNUP = 'false';

const test = require('node:test');
const assert = require('node:assert/strict');
const { STRONG_PASSWORD, app, cleanup, request } = require('../helpers/app');

test.after(cleanup);

function register(username) {
  return request(app).post('/api/auth/register').send({
    username,
    email: `${username}@example.com`,
    password: STRONG_PASSWORD,
  });
}

test('the first account registers while sign-up is off, the second does not', async () => {
  const fresh = await request(app).get('/api/auth/status').expect(200);
  assert.equal(fresh.body.hasUser, false);
  assert.equal(fresh.body.registrationOpen, true);
  assert.equal('signupKeyRequired' in fresh.body, false);

  const first = await register('owner');
  assert.equal(first.status, 200, JSON.stringify(first.body));

  const status = await request(app).get('/api/auth/status').expect(200);
  assert.equal(status.body.registrationOpen, false);
  const second = await register('guest');
  assert.equal(second.status, 403);
  assert.equal(second.body.code, 'SIGNUP_DISABLED');
});

test('the sign-up key routes are gone', async () => {
  await request(app).post('/api/auth/signup-key/check').send({ key: 'ABCD-EFGH-JKMN' }).expect(404);
});
