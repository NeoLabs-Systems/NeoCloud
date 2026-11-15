'use strict';

// The abuse limits of signing mail (verified sender, daily caps, per-address
// cap, kill switch), the footer and headers of every request mail, friendly
// delivery errors, and the signed file for a signer who came through the inbox.

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser } = require('../helpers/app');
const db = require('../../server/db/database');
const { installFakeMail } = require('../helpers/mail');
const { createDraft, area, setUp, until } = require('../helpers/signing');
const {
  draftFor, mailCount, sendDraft, signWith, stillQuiet, verifyEmail,
} = require('../helpers/signing_mail');
const { MAIL_LIMITS } = require('../../server/services/signing/mail/limits');
const { processDue } = require('../../server/services/signing/mail/outbox');
const { classifyMailError, friendlyMailError } = require('../../server/services/signing/mail/errors');

const mail = installFakeMail();

test.before(() => mail.configure());
test.after(() => {
  mail.restore();
  cleanup();
});
test.beforeEach(async () => {
  db.prepare("UPDATE sign_mail_outbox SET status = 'canceled' WHERE status = 'queued'").run();
  db.prepare("UPDATE sign_documents SET status = 'canceled' WHERE status IN ('sent', 'in_progress')").run();
  await until(() => db.prepare("SELECT COUNT(*) AS count FROM sign_mail_outbox WHERE status = 'sending'").get().count === 0);
  db.prepare('DELETE FROM sign_mail_ledger').run();
  mail.reset();
  mail.configure();
});

const one = (address) => [{ name: 'Alice', email: address }];
let counter = 0;
const fresh = () => `signer${counter += 1}@example.org`;

async function quietAccount(prefix) {
  mail.reset();
  const account = await registerUser(prefix);
  mail.configure();
  return account;
}

let shared = null;
async function owner() {
  if (!shared) {
    shared = await quietAccount('limits');
    verifyEmail(shared.user.id);
  }
  return shared;
}

test('mail needs a verified sender address; links still work without', async () => {
  const account = await quietAccount('unverifiedsender');
  const draft = await draftFor(account.agent, one(fresh()));
  const refused = await account.agent.post(`${draft.url}/send`).send({}).expect(403);
  assert.equal(refused.body.code, 'EMAIL_NOT_VERIFIED');
  // Nothing happened to the draft; link-only still works.
  const sent = await sendDraft(account.agent, draft, { deliverEmail: false });
  assert.equal(sent.links.length, 1);
  await account.agent.post(`${draft.url}/parties/${draft.parties[0].id}/remind`).send({}).expect(403);
  assert.ok(await stillQuiet(mail, 200));
  assert.equal(mail.sent.length, 0);
  // A document without a mailable party needs no verification either.
  const noMail = await draftFor(account.agent, [{ name: 'Nomail' }]);
  await sendDraft(account.agent, noMail);
});

test('one address gets at most three invitations a day from the whole instance, whoever asks', async () => {
  const first = await owner();
  const second = await quietAccount('limits2');
  verifyEmail(second.user.id);
  const target = fresh();
  for (let index = 0; index < MAIL_LIMITS.recipientPerDay - 1; index += 1) {
    await sendDraft(first.agent, await draftFor(first.agent, one(target), { order: 'random' }));
  }
  await sendDraft(second.agent, await draftFor(second.agent, one(target), { order: 'random' }));
  await mailCount(mail, 3);
  const blocked = await draftFor(first.agent, one(target), { order: 'random' });
  const refused = await first.agent.post(`${blocked.url}/send`).send({}).expect(429);
  assert.equal(refused.body.code, 'MAIL_QUOTA_EXCEEDED');
  assert.equal((await first.agent.get(blocked.url).expect(200)).body.document.status, 'draft', 'a refused send changes nothing');
  // Another address is fine, and so is the same address by link only.
  await sendDraft(first.agent, await draftFor(first.agent, one(fresh()), { order: 'random' }));
  await sendDraft(first.agent, blocked, { deliverEmail: false });
});

test('deleting a document does not give its mails back', async () => {
  const account = await owner();
  const target = fresh();
  for (let index = 0; index < MAIL_LIMITS.recipientPerDay; index += 1) {
    const draft = await draftFor(account.agent, one(target), { order: 'random' });
    await sendDraft(account.agent, draft);
    await account.agent.post(`${draft.url}/cancel`).send({}).expect(200);
    await mailCount(mail, index + 1);
    await account.agent.delete(draft.url).expect(200);
  }
  const again = await draftFor(account.agent, one(target), { order: 'random' });
  await account.agent.post(`${again.url}/send`).send({}).expect(429);
});

test('the daily cap per account, with a higher one for an administrator', async () => {
  const account = await owner();
  db.prepare("UPDATE users SET is_admin = 0 WHERE id = ?").run(account.user.id);
  const insert = db.prepare("INSERT INTO sign_mail_ledger (user_id, recipient_hash, kind, mail_id, at) VALUES (?, ?, 'invitation', NULL, ?)");
  const now = new Date().toISOString();
  for (let index = 0; index < MAIL_LIMITS.creatorPerDay; index += 1) insert.run(account.user.id, `h${index}`, now);
  const draft = await draftFor(account.agent, one(fresh()), { order: 'random' });
  assert.equal((await account.agent.post(`${draft.url}/send`).send({}).expect(429)).body.code, 'MAIL_QUOTA_EXCEEDED');
  db.prepare('UPDATE users SET is_admin = 1 WHERE id = ?').run(account.user.id);
  try {
    await sendDraft(account.agent, draft);
    await mailCount(mail, 1);
  } finally {
    db.prepare('UPDATE users SET is_admin = 0 WHERE id = ?').run(account.user.id);
  }
  // Entries older than a day do not count.
  db.prepare('UPDATE sign_mail_ledger SET at = ?').run(new Date(Date.now() - 25 * 3600 * 1000).toISOString());
  await sendDraft(account.agent, await draftFor(account.agent, one(fresh()), { order: 'random' }));
});

test('a regenerated link does not buy more mail than the limits allow', async () => {
  const account = await owner();
  const target = fresh();
  const draft = await draftFor(account.agent, one(target), { order: 'random' });
  await sendDraft(account.agent, draft);
  await mailCount(mail, 1);
  const partyId = draft.parties[0].id;
  const outcomes = [];
  for (let index = 0; index < 4; index += 1) {
    const renewed = await account.agent.post(`${draft.url}/parties/${partyId}/link`).send({}).expect(200);
    outcomes.push(renewed.body.delivery?.status);
    await new Promise((resolve) => setTimeout(resolve, 60));
    await processDue();
  }
  assert.deepEqual(outcomes.slice(0, 2), ['queued', 'queued']);
  assert.equal(outcomes[3], 'skipped');
  const invitations = db.prepare("SELECT COUNT(*) AS count FROM sign_mail_outbox WHERE document_id = ? AND status = 'sent'").get(draft.id).count;
  assert.ok(invitations <= MAIL_LIMITS.recipientPerDay);
});

test('the admin kill switch stops signing mail and drops what waits; nothing else changes', async () => {
  const account = await owner();
  const draft = await draftFor(account.agent, one(fresh()), { order: 'random' });
  mail.configure({ NEOCLOUD_EMAIL_SIGNING_MAILS: 'false' });
  assert.equal((await account.agent.get('/api/signing/capabilities').expect(200)).body.email.configured, false);
  const sent = await sendDraft(account.agent, draft);
  assert.deepEqual(sent.delivery.map((entry) => entry.status), ['skipped']);
  assert.ok(await stillQuiet(mail, 200));
  assert.equal(mail.sent.length, 0);
  // Service mail (sign-in, reset) is not affected by the switch.
  const { sendServiceEmail } = require('../../server/services/account/service_email');
  await sendServiceEmail('someone@example.org', { subject: 'Hello', title: 'Hello', intro: 'Hi' });
  assert.equal(mail.sent.length, 1);
  mail.configure();
});

test('every request mail names who asked, can be ignored or reported, and is marked automatic and bulk', async () => {
  const account = await owner();
  mail.configure({ NEOCLOUD_EMAIL_SUPPORT_URL: 'https://help.example.org/report', NEOCLOUD_EMAIL_REPLY_TO: 'abuse@example.org' });
  const hostile = '<script>alert(1)</script> javascript:alert(2) https://evil.example/x ‮evil';
  const draft = await draftFor(account.agent, [...one(fresh()), { name: 'Bob', email: fresh() }], { order: 'random', title: 'Deed <b>one</b>' });
  await sendDraft(account.agent, draft, { message: hostile });
  await mailCount(mail, 2);
  for (const message of mail.sent) {
    assert.match(message.raw, /Auto-Submitted: auto-generated/i);
    assert.match(message.raw, /Precedence: bulk/i);
    assert.match(message.text, /asked you to sign a document/);
    assert.match(message.text, /ignore this email/);
    assert.match(message.text, /https:\/\/help\.example\.org\/report/);
    assert.ok(!message.html.includes('<script>'), 'the note is escaped');
    assert.ok(!message.html.includes('<b>one</b>'));
    assert.ok(!/href="javascript:/i.test(message.html));
    assert.ok(!message.html.includes('‮'));
    assert.equal(message.to.length, 1, 'one mail per recipient');
  }
  assert.ok(!mail.sent[0].raw.includes(mail.sent[1].to[0]) && !mail.sent[1].raw.includes(mail.sent[0].to[0]), 'no recipient sees another');
});

test('the message is plain text of at most 1000 characters', async () => {
  const account = await owner();
  const draft = await draftFor(account.agent, one(fresh()));
  await account.agent.post(`${draft.url}/send`).send({ message: 'x'.repeat(1001) }).expect(400);
  await account.agent.post(`${draft.url}/send`).send({ message: { html: '<b>' } }).expect(400);
  await sendDraft(account.agent, draft, { message: 'x'.repeat(1000) });
});

test('a failed mail shows a friendly sentence and a stable code, never the server text', async () => {
  const account = await owner();
  const german = await draftFor(account.agent, one(fresh()), { locale: 'de' });
  mail.failNext(Object.assign(new Error('connect ECONNREFUSED 10.0.0.5:587 secret-host.internal'), { code: 'ECONNECTION' }));
  await sendDraft(account.agent, german);
  await until(() => db.prepare('SELECT attempts FROM sign_mail_outbox WHERE document_id = ?').get(german.id)?.attempts >= 1);
  const described = (await account.agent.get(german.url).expect(200)).body.parties[0].delivery;
  assert.equal(described.errorCode, 'SMTP_UNREACHABLE');
  assert.equal(described.lastError, 'Der Mailserver ist nicht erreichbar.');
  assert.ok(!JSON.stringify(described).includes('10.0.0.5') && !JSON.stringify(described).includes('secret-host'));
  for (const [error, code] of [
    [Object.assign(new Error('550 5.1.1 <a@b.c> user unknown'), { responseCode: 550 }), 'INVALID_ADDRESS'],
    [Object.assign(new Error('Authentication credentials rejected'), { code: 'EAUTH', responseCode: 535 }), 'SMTP_REJECTED'],
    [Object.assign(new Error('421 too many connections'), { responseCode: 421 }), 'RATE_LIMITED'],
    [new Error('boom'), 'UNKNOWN'],
    [Object.assign(new Error('x'), { code: 'ETIMEDOUT' }), 'SMTP_UNREACHABLE'],
  ]) assert.equal(classifyMailError(error), code);
  for (const code of ['SMTP_UNREACHABLE', 'SMTP_REJECTED', 'INVALID_ADDRESS', 'RATE_LIMITED', 'UNKNOWN']) {
    assert.ok(friendlyMailError(code, 'en').length > 10 && friendlyMailError(code, 'de').length > 10);
  }
});

// The inbox route for the signed file after a grant session.
test('the signed file after a grant session comes from the inbox, for the party’s own account only', async () => {
  const creator = await owner();
  const signer = await quietAccount('granted');
  verifyEmail(signer.user.id);
  const stranger = await quietAccount('granted2');
  verifyEmail(stranger.user.id);
  const draft = await createDraft(creator.agent, { name: 'contract.pdf' });
  const stored = await setUp(creator.agent, draft.document.id, {
    parties: [{ name: 'Signer', email: `${signer.username}@example.com` }, { name: 'Second', email: fresh() }],
    fields: [0, 1].map((party) => ({ party, type: 'text', name: `F${party}`, areas: [area(0.1, 0.1 + party * 0.1)] })),
  });
  const url = `/api/signing/documents/${draft.document.id}`;
  await creator.agent.put(url).send({ signingOrder: 'random' }).expect(200);
  const sent = await sendDraft(creator.agent, { id: draft.document.id, url });
  await mailCount(mail, 2);
  const partyId = stored.parties[0].id;

  const grant = (await signer.agent.post(`/api/signing/inbox/${partyId}/open`).send({}).expect(200)).body.url.split('/').pop();
  await signWith(grant, stored.fields[0].id, 'Grant Signer');
  // The grant died with the signature; the public result route no longer answers.
  await require('../helpers/signing').publicGet(grant, '/result').expect(404);
  // Not completed yet: the inbox says the file is not there.
  assert.equal((await signer.agent.get(`/api/signing/inbox/${partyId}/final`).expect(404)).body.code, 'FINAL_UNAVAILABLE');
  await signWith(sent.tokens[1], stored.fields[1].id, 'Second Signer');
  await until(() => db.prepare('SELECT status FROM sign_documents WHERE id = ?').get(draft.document.id).status === 'completed', { timeoutMs: 10000 });

  const items = (await signer.agent.get('/api/signing/inbox?status=done').expect(200)).body.items;
  assert.equal(items[0].finalAvailable, true);
  const pdf = (variant) => signer.agent.get(`/api/signing/inbox/${partyId}/final${variant}`).buffer(true).parse((res, done) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => done(null, Buffer.concat(chunks)));
  });
  const full = await pdf('').expect(200);
  assert.equal(full.body.subarray(0, 5).toString(), '%PDF-');
  assert.match(full.headers['content-disposition'], /attachment; filename=/);
  assert.ok(full.headers.etag);
  const clean = await pdf('?certificate=0').expect(200);
  assert.equal(clean.body.subarray(0, 5).toString(), '%PDF-');
  assert.notEqual(clean.headers.etag, full.headers.etag);
  assert.notEqual(clean.headers['content-disposition'], full.headers['content-disposition']);
  await signer.agent.get(`/api/signing/inbox/${partyId}/final?certificate=2`).expect(400);
  const range = await signer.agent.get(`/api/signing/inbox/${partyId}/final`).set('Range', 'bytes=0-9').expect(206);
  assert.equal(range.headers['content-range'].startsWith('bytes 0-9/'), true);
  assert.ok(db.prepare("SELECT COUNT(*) AS count FROM sign_events WHERE document_id = ? AND type = 'final_downloaded'").get(draft.document.id).count >= 1);

  // Somebody else, a made-up id, and a signed-out caller get nothing.
  await stranger.agent.get(`/api/signing/inbox/${partyId}/final`).expect(404);
  await signer.agent.get('/api/signing/inbox/not-an-id/final').expect(404);
  await require('../helpers/app').request(require('../helpers/app').app).get(`/api/signing/inbox/${partyId}/final`).expect(401);
  // A party another account holds is not the signer's even with the same address.
  db.prepare('UPDATE sign_parties SET user_id = ? WHERE id = ?').run(stranger.user.id, partyId);
  await signer.agent.get(`/api/signing/inbox/${partyId}/final`).expect(404);
});

test('previewing "use my details" lists what would be filled and binds and writes nothing', async () => {
  const creator = await owner();
  const signer = await quietAccount('previewer');
  verifyEmail(signer.user.id);
  const draft = await createDraft(creator.agent, { name: 'contract.pdf' });
  const stored = await setUp(creator.agent, draft.document.id, {
    parties: [{ name: 'Signer' }],
    fields: [{ party: 0, type: 'text', name: 'Email', areas: [area(0.1, 0.1)] }],
  });
  const url = `/api/signing/documents/${draft.document.id}`;
  const [token] = (await sendDraft(creator.agent, { id: draft.document.id, url })).tokens;
  const preview = (await signer.agent.post('/api/signing/claim').send({ token, preview: true }).expect(200)).body;
  assert.equal(preview.prefill.length, 1);
  assert.equal(preview.prefill[0].name, 'Email');
  assert.deepEqual(preview.applied, []);
  assert.equal(db.prepare('SELECT user_id FROM sign_parties WHERE id = ?').get(stored.parties[0].id).user_id, null);
  assert.equal(db.prepare('SELECT COUNT(*) AS c FROM sign_values WHERE document_id = ?').get(draft.document.id).c, 0);
});
