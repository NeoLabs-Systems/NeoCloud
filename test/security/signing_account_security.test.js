'use strict';

// Security of the signing mail and the account-holder features: header
// injection through names, addresses that are not plain, grant tokens in
// logs and uniform answers, which tokens may reach the new routes.

const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');
const { redactSecrets } = require('../../server/utils/logger');
const { installFakeMail } = require('../helpers/mail');
const { publicGet, publicPut } = require('../helpers/signing');
const { draftFor, mailCount, sendDraft, signWith, stillQuiet, verifyEmail } = require('../helpers/signing_mail');
const { processDue } = require('../../server/services/signing/mail/outbox');

const mail = installFakeMail();
let owner;
let signer;

test.before(async () => {
  owner = await registerUser('secowner');
  signer = await registerUser('secsigner');
  verifyEmail(owner.user.id);
  verifyEmail(signer.user.id);
  mail.configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'false' });
});
test.after(() => {
  mail.restore();
  cleanup();
});

test('a name with line breaks cannot add headers or recipients to a mail', async () => {
  db.prepare("UPDATE users SET display_name = ? WHERE id = ?").run('Eve\r\nBcc: victim@example.org\r\nSubject: pwned', owner.user.id);
  const draft = await draftFor(owner.agent, [{ name: 'Alice', email: 'alice@example.com' }], { order: 'random', title: 'Plain title' });
  await sendDraft(owner.agent, draft, { message: 'Hi\r\nBcc: victim@example.org' });
  await mailCount(mail, 1);
  const message = mail.sent[0];
  assert.ok(!/[\r\n]/.test(message.subject));
  const headers = message.raw.split('\n\n')[0];
  assert.ok(!/^Bcc:/mi.test(headers));
  assert.deepEqual(message.to, ['alice@example.com']);
  assert.ok(!message.raw.includes('victim@example.org\nSubject'));
  db.prepare('UPDATE users SET display_name = NULL WHERE id = ?').run(owner.user.id);
});

test('lists of addresses are refused, and an address that is not plain is never mailed', async () => {
  mail.reset();
  mail.configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'false' });
  const refused = await draftFor(owner.agent, [{ name: 'One' }], { order: 'random' });
  await owner.agent.put(`${refused.url}/parties`).send({ parties: [{ id: refused.parties[0].id, name: 'One', email: 'a@example.com,b@example.org' }] }).expect(400);
  // Passes the party check (one "@") but is no plain address: the mail service refuses it, once.
  const draft = await draftFor(owner.agent, [
    { name: 'Two', email: 'evil<x>@example.com' },
  ], { order: 'random' });
  const sent = await sendDraft(owner.agent, draft);
  assert.equal(sent.delivery[0].status, 'queued');
  assert.ok(await stillQuiet(mail, 400));
  assert.equal(mail.sent.length, 0);
  const status = (await owner.agent.get(draft.url).expect(200)).body.parties[0].delivery;
  assert.equal(status.status, 'failed');
  assert.equal(status.attempts, 1, 'no retry for an address that can never work');
  assert.ok(!JSON.stringify(status).includes('evil<x>@example.com'));
});

test('grant tokens are redacted from logs and unknown ones answer like unknown links', async () => {
  const token = 'g'.repeat(43);
  assert.equal(redactSecrets(`GET /app/sign/${token} 200`), 'GET /app/sign/[redacted] 200');
  assert.equal(redactSecrets(`PUT /api/public/sign/${token}/fields/x done`), 'PUT /api/public/sign/[redacted]/fields/x done');
  const unknown = await publicGet(token).expect(404);
  const malformed = await publicGet('short').expect(404);
  assert.deepEqual(unknown.body, malformed.body);
  assert.equal(unknown.headers['cache-control'], 'no-store, max-age=0');
  assert.equal(unknown.headers['set-cookie'], undefined);
  await publicPut(token, '/fields/00000000-0000-0000-0000-000000000000', { value: 'x' }).expect(404);
});

test('the account routes take a browser session or the phone app, not a desktop token', async () => {
  const desktop = createDeviceToken(signer.user.id, { name: 'Desktop', platform: 'macos' }).token;
  const phone = createDeviceToken(signer.user.id, { name: 'Phone', platform: 'android' }).token;
  for (const [method, url] of [
    ['get', '/api/signing/profile'], ['get', '/api/signing/inbox'], ['post', '/api/signing/claim'],
    ['put', '/api/signing/profile'], ['post', '/api/signing/inbox/x/open'],
  ]) {
    const refused = await request(app)[method](url).set('Authorization', `Bearer ${desktop}`).send({});
    assert.ok([401, 403].includes(refused.status), `${method} ${url} answered ${refused.status}`);
    const anonymous = await request(app)[method](url).send({});
    assert.equal(anonymous.status, 401, `${method} ${url} without credentials`);
  }
  const allowed = await request(app).get('/api/signing/inbox').set('Authorization', `Bearer ${phone}`);
  assert.equal(allowed.status, 200);
  assert.equal((await request(app).get('/api/signing/profile').set('Authorization', `Bearer ${phone}`)).status, 200);
});

test('the sender’s note and names are text everywhere: nothing the owner types becomes markup in a mail', async () => {
  mail.reset();
  mail.configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'false' });
  const payload = '<img src=x onerror=alert(1)><script>alert(2)</script>';
  const draft = await draftFor(owner.agent, [{ name: 'Alice', email: 'alice@example.com' }], { order: 'random', title: payload });
  await sendDraft(owner.agent, draft, { message: payload });
  await mailCount(mail, 1);
  const html = mail.sent[0].html;
  assert.ok(!html.includes('<script>') && !html.includes('<img src=x'));
  assert.ok(html.includes('&lt;script&gt;'));
});

test('a party’s own mail never carries another party’s link, in any kind', async () => {
  mail.reset();
  mail.configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'false' });
  const draft = await draftFor(owner.agent, [
    { name: 'A', email: 'a@example.com' }, { name: 'B', email: 'b@example.com' }, { name: 'C', email: 'c@example.com' },
  ], { order: 'random' });
  const sent = await sendDraft(owner.agent, draft);
  await mailCount(mail, 3);
  await owner.agent.post(`${draft.url}/remind-all`).send({}).expect(202);
  await mailCount(mail, 6);
  await signWith(sent.tokens[0], draft.fields[0].id, 'A');
  await signWith(sent.tokens[1], draft.fields[1].id, 'B');
  await signWith(sent.tokens[2], draft.fields[2].id, 'C');
  await mailCount(mail, 10);
  await processDue();
  for (const message of mail.sent) {
    const own = sent.tokens.filter((token) => message.raw.includes(token));
    assert.ok(own.length <= 1, `${message.subject} names ${own.length} links`);
    if (message.to[0].endsWith('@example.com') && !message.to[0].startsWith('secowner')) {
      const index = ['a', 'b', 'c'].indexOf(message.to[0][0]);
      assert.ok(own.every((token) => token === sent.tokens[index]), 'only the recipient’s own link');
    }
  }
});
