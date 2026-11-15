'use strict';

// Signing by email: invitations, turns, reminders, the expiry warning, the
// completion mails and the notices to the owner, through a fake SMTP server;
// the outbox that carries them; the delivery status the owner sees.

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser } = require('../helpers/app');
const db = require('../../server/db/database');
const { installFakeMail } = require('../helpers/mail');
const {
  describeDraft, draftFor, mailCount, sendDraft, signWith, stillQuiet, verifyEmail,
} = require('../helpers/signing_mail');
const { publicPost, consent, until } = require('../helpers/signing');
const { BACKOFF_MINUTES, processDue, recoverInterrupted } = require('../../server/services/signing/mail/outbox');
const { remindAll, remindParty, runReminderPass } = require('../../server/services/signing/mail/reminders');
const { setAttachmentLimitForTests } = require('../../server/services/signing/mail/compose');
const { runMailPass, startMailWorker } = require('../../server/services/signing/mail/worker');

const mail = installFakeMail();

test.before(() => mail.configure());
test.after(() => {
  mail.restore();
  cleanup();
});
test.beforeEach(async () => {
  // Every test starts with nothing in flight from the tests before it.
  db.prepare("UPDATE sign_mail_outbox SET status = 'canceled' WHERE status = 'queued'").run();
  db.prepare("UPDATE sign_documents SET status = 'canceled' WHERE status IN ('sent', 'in_progress')").run();
  await until(() => db.prepare("SELECT COUNT(*) AS count FROM sign_mail_outbox WHERE status = 'sending'").get().count === 0);
  db.prepare('DELETE FROM sign_mail_ledger').run();
  mail.reset();
  mail.configure();
  setAttachmentLimitForTests(null);
});

const THREE = [
  { name: 'Alice', email: 'alice@example.com' },
  { name: 'Bob', email: 'bob@example.com' },
  { name: 'Cara', email: 'cara@example.com' },
];

const DAY = 24 * 60 * 60 * 1000;
const rowsOf = (documentId, where = '1 = 1') => db.prepare(`SELECT * FROM sign_mail_outbox WHERE document_id = ? AND ${where} ORDER BY id`).all(documentId);
const recipients = () => mail.sent.map((message) => message.to[0]);

// Accounts are made while mail is off, so no sign-up mail is in the way.
async function registerQuietly(prefix) {
  mail.reset();
  const account = await registerUser(prefix);
  mail.configure();
  return account;
}

// Registration is rate limited: the owner is made once and works on its own documents in every test.
let shared = null;

async function owner() {
  if (!shared) {
    shared = await registerQuietly('mailer');
    verifyEmail(shared.user.id);
  }
  return shared;
}

test('capabilities say whether mail is configured', async () => {
  const { agent } = await owner();
  assert.deepEqual((await agent.get('/api/signing/capabilities').expect(200)).body.email, { configured: true });
  mail.reset();
  assert.deepEqual((await agent.get('/api/signing/capabilities').expect(200)).body.email, { configured: false });
});

test('sequential: the invitation goes to the first party, the next gets "your turn", the last signature completes with attachments', async () => {
  const { agent, username } = await owner();
  const draft = await draftFor(agent, THREE, { title: 'Lease <deal> & "terms"' });
  const sent = await sendDraft(agent, draft, { message: 'Please <b>sign</b> today\nThanks & bye', reminderDays: 0 });
  assert.deepEqual(sent.delivery.map((entry) => entry.status), ['queued', 'skipped', 'skipped']);
  assert.deepEqual(sent.delivery.map((entry) => entry.partyId), draft.parties.map((party) => party.id));

  await mailCount(mail, 1);
  const invitation = mail.sent[0];
  assert.deepEqual(invitation.to, ['alice@example.com']);
  assert.match(invitation.subject, new RegExp(`${username} asked you to sign "Lease <deal> & "terms""`));
  assert.ok(!/[\r\n]/.test(invitation.subject));
  assert.ok(invitation.text.includes(`https://cloud.example.com/app/sign/${sent.tokens[0]}`));
  assert.ok(invitation.html.includes(`href="https://cloud.example.com/app/sign/${sent.tokens[0]}"`));
  // The note is a quote, escaped; nothing leaks another party's link.
  assert.ok(invitation.html.includes('Please &lt;b&gt;sign&lt;/b&gt; today'));
  assert.ok(!invitation.html.includes('<b>sign'));
  assert.ok(invitation.text.includes('> Please <b>sign</b> today'));
  assert.ok(invitation.html.includes('Lease &lt;deal&gt; &amp; &quot;terms&quot;'));
  for (const other of sent.tokens.slice(1)) {
    assert.ok(!invitation.text.includes(other) && !invitation.html.includes(other) && !invitation.raw.includes(other));
  }
  assert.equal(invitation.payload.attachments, undefined);
  assert.equal(invitation.raw.includes('Auto-Submitted: auto-generated'), true);

  // The party sees its delivery state while the others do not have one yet.
  const afterSend = await describeDraft(agent, draft);
  assert.equal(afterSend.parties[0].delivery.status, 'sent');
  assert.ok(afterSend.parties[0].delivery.lastSentAt);
  assert.equal(afterSend.parties[1].delivery.status, 'skipped');
  assert.equal(afterSend.document.message, 'Please <b>sign</b> today\nThanks & bye');
  assert.equal(afterSend.document.reminderDays, 0);

  await signWith(sent.tokens[0], draft.fields[0].id, 'Alice A');
  await mailCount(mail, 2);
  const turn = mail.sent[1];
  assert.deepEqual(turn.to, ['bob@example.com']);
  assert.ok(turn.text.includes(sent.tokens[1]));
  assert.ok(!turn.text.includes(sent.tokens[0]) && !turn.text.includes(sent.tokens[2]));
  assert.match(turn.subject, /Your turn to sign/);
  assert.ok(turn.html.includes('Please &lt;b&gt;sign&lt;/b&gt;'), 'the first contact repeats the personal note');

  await signWith(sent.tokens[1], draft.fields[1].id, 'Bob B');
  await mailCount(mail, 3);
  assert.deepEqual(mail.sent[2].to, ['cara@example.com']);
  await signWith(sent.tokens[2], draft.fields[2].id, 'Cara C');

  await until(async () => (await describeDraft(agent, draft)).document.status === 'completed');
  await mailCount(mail, 7);
  const completion = mail.sent.slice(3);
  assert.deepEqual(completion.map((message) => message.to[0]).sort(), [
    'alice@example.com', 'bob@example.com', 'cara@example.com', `${username}@example.com`,
  ]);
  for (const message of completion) {
    assert.equal(message.payload.attachments.length, 1);
    assert.equal(message.payload.attachments[0].contentType, 'application/pdf');
    assert.ok(message.payload.attachments[0].content.subarray(0, 5).toString() === '%PDF-');
    assert.match(message.payload.attachments[0].filename, /\(signed\)\.pdf$/);
    assert.ok(message.text.includes('without the signature pages'));
  }
  const creatorMail = completion.find((message) => message.to[0] === `${username}@example.com`);
  assert.ok(creatorMail.text.includes(`https://cloud.example.com/app/signing/${draft.id}`));
  const partyMail = completion.find((message) => message.to[0] === 'bob@example.com');
  assert.ok(partyMail.text.includes(`/app/sign/${sent.tokens[1]}`));
  for (const token of [sent.tokens[0], sent.tokens[2]]) assert.ok(!partyMail.raw.includes(token));
  const types = (await agent.get(`${draft.url}/events`).expect(200)).body.events.map((event) => event.type);
  for (const type of ['mail_queued', 'mail_sent']) assert.ok(types.includes(type));
});

test('any order: everybody is invited at once', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE, { order: 'random' });
  const sent = await sendDraft(agent, draft);
  assert.deepEqual(sent.delivery.map((entry) => entry.status), ['queued', 'queued', 'queued']);
  await mailCount(mail, 3);
  assert.deepEqual(recipients().sort(), ['alice@example.com', 'bob@example.com', 'cara@example.com']);
  for (const message of mail.sent) {
    const own = sent.tokens.filter((token) => message.text.includes(token));
    assert.equal(own.length, 1, 'exactly one link per mail');
  }
});

test('German documents get German mail; a party without an address is reported', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, [{ name: 'Anna', email: 'anna@example.com' }, { name: 'Ben' }], { order: 'random', locale: 'de', title: 'Mietvertrag' });
  const sent = await sendDraft(agent, draft);
  assert.deepEqual(sent.delivery.map((entry) => entry.status), ['queued', 'no_email']);
  await mailCount(mail, 1);
  assert.match(mail.sent[0].subject, /bittet dich, „Mietvertrag“ zu unterschreiben/);
  assert.ok(mail.sent[0].html.includes('lang="de"'));
  assert.ok(mail.sent[0].text.includes('Prüfen und unterschreiben'));
  const stored = db.prepare('SELECT token_enc FROM sign_parties WHERE id = ?').get(draft.parties[1].id);
  assert.equal(stored.token_enc, null, 'no encrypted copy for a party without an address');
});

test('without a mail service, or with mail switched off, sending stays link-only', async () => {
  const { agent } = await owner();
  mail.reset();
  const link = await draftFor(agent, THREE.slice(0, 1));
  const first = await sendDraft(agent, link);
  assert.deepEqual(first.delivery.map((entry) => entry.status), ['skipped']);
  assert.equal(rowsOf(link.id).length, 0);
  assert.equal(first.links.length, 1);

  mail.configure();
  const off = await draftFor(agent, THREE.slice(0, 1));
  const second = await sendDraft(agent, off, { deliverEmail: false });
  assert.deepEqual(second.delivery.map((entry) => entry.status), ['skipped']);
  assert.ok(await stillQuiet(mail, 200));
  assert.equal(rowsOf(off.id).length, 0);
});

test('send options are validated', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1));
  await agent.post(`${draft.url}/send`).send({ message: 'x'.repeat(1001) }).expect(400);
  await agent.post(`${draft.url}/send`).send({ reminderDays: 4 }).expect(400);
  await agent.post(`${draft.url}/send`).send({ deliverEmail: 'yes' }).expect(400);
  await agent.post(`${draft.url}/send`).send({ message: 5 }).expect(400);
  assert.equal((await describeDraft(agent, draft)).document.status, 'draft');
});

test('the outbox retries with growing pauses, then gives up; a failure is visible, without addresses', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1));
  // The first attempt (started by the send) runs against a failing server.
  mail.failNext(Object.assign(new Error('450 4.2.0 <alice@example.com>: Mailbox busy'), { code: 'EENVELOPE' }));
  await sendDraft(agent, draft);
  await until(() => rowsOf(draft.id)[0].attempts >= 1 && rowsOf(draft.id)[0].status === 'queued');
  let row = rowsOf(draft.id)[0];
  assert.equal(row.attempts, 1);
  assert.equal(row.last_error, 'RATE_LIMITED', 'the outbox keeps a code, never the server\'s text');
  const visible = (await describeDraft(agent, draft)).parties[0].delivery;
  assert.equal(visible.status, 'queued');
  assert.equal(visible.attempts, 1);
  assert.equal(visible.errorCode, 'RATE_LIMITED');
  assert.match(visible.lastError, /limiting/);
  assert.ok(!JSON.stringify(visible).includes('alice@example.com') && !/Mailbox busy/.test(JSON.stringify(visible)));
  assert.ok(Math.abs(Date.parse(row.next_attempt_at) - Date.now() - BACKOFF_MINUTES[0] * 60000) < 5000);

  // Nothing is due yet.
  assert.equal(await processDue({ now: Date.now() + 30 * 1000 }), 0);

  let now = Date.now();
  for (let attempt = 2; attempt <= 6; attempt += 1) {
    now = Date.parse(rowsOf(draft.id)[0].next_attempt_at) + 1000;
    mail.failNext();
    await processDue({ now });
    row = rowsOf(draft.id)[0];
    assert.equal(row.attempts, attempt);
    if (attempt < 6) {
      assert.equal(row.status, 'queued');
      assert.equal(Math.round((Date.parse(row.next_attempt_at) - now) / 60000), BACKOFF_MINUTES[attempt - 1]);
    }
  }
  assert.equal(row.status, 'failed');
  const failed = (await describeDraft(agent, draft)).parties[0].delivery;
  assert.equal(failed.status, 'failed');
  assert.equal(failed.attempts, 6);
  const types = (await agent.get(`${draft.url}/events`).expect(200)).body.events;
  const failure = types.find((event) => event.type === 'mail_failed');
  assert.ok(failure);
  assert.equal(failure.data.to, 'a***@example.com');
  assert.equal(JSON.stringify(types).includes('alice@example.com'), false, 'no address in the trail');
  // A failed mail is not retried by itself.
  assert.equal(await processDue({ now: now + 10 * DAY }), 0);
});

test('mail left half-sent by a stop is picked up again, and a retry delivers', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1));
  await sendDraft(agent, draft);
  await mailCount(mail, 1);
  // A queued mail that "was being sent" when the process stopped.
  db.prepare("UPDATE sign_mail_outbox SET status = 'sending', sent_at = NULL, attempts = 1 WHERE document_id = ?").run(draft.id);
  assert.equal(recoverInterrupted(Date.now(), { all: true }), 1);
  await processDue();
  assert.equal(rowsOf(draft.id)[0].status, 'sent');
  assert.equal(mail.sent.length, 2);
});

test('reminders: throttled to one a day, at most five, with their own errors', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, [...THREE.slice(0, 2), { name: 'Dora' }], { order: 'preserved' });
  const sent = await sendDraft(agent, draft);
  await mailCount(mail, 1);
  const [alice, bob, dora] = draft.parties;

  const first = await agent.post(`${draft.url}/parties/${alice.id}/remind`).send({ message: 'Ping <3' }).expect(202);
  assert.deepEqual(first.body.delivery, { partyId: alice.id, status: 'queued' });
  await mailCount(mail, 2);
  assert.match(mail.sent[1].subject, /^Reminder: please sign/);
  assert.ok(mail.sent[1].html.includes('Ping &lt;3'));
  assert.ok(mail.sent[1].text.includes(sent.tokens[0]));

  const soon = await agent.post(`${draft.url}/parties/${alice.id}/remind`).send({}).expect(429);
  assert.equal(soon.body.code, 'REMINDER_TOO_SOON');
  assert.ok(soon.body.retryAfter > 23 * 3600 && soon.body.retryAfter <= 24 * 3600);
  assert.equal(soon.headers['retry-after'], String(soon.body.retryAfter));

  // Not waiting yet (sequential), nobody to mail, no address.
  assert.equal((await agent.post(`${draft.url}/parties/${bob.id}/remind`).send({}).expect(409)).body.code, 'PARTY_NOT_ACTIVE');
  assert.equal((await agent.post(`${draft.url}/parties/${dora.id}/remind`).send({}).expect(409)).body.code, 'PARTY_NOT_ACTIVE');

  // Five in all, a day apart.
  const userId = db.prepare('SELECT owner_id FROM sign_documents WHERE id = ?').get(draft.id).owner_id;
  let now = Date.now();
  for (let count = 2; count <= 5; count += 1) {
    now += DAY + 1000;
    remindParty(userId, draft.id, alice.id, {}, { now });
  }
  now += DAY + 1000;
  assert.throws(() => remindParty(userId, draft.id, alice.id, {}, { now }), (err) => err.status === 429 && err.code === 'REMINDER_LIMIT');

  // Once the party signed, there is nothing to remind of.
  await signWith(sent.tokens[0], draft.fields[0].id, 'Alice');
  assert.equal((await agent.post(`${draft.url}/parties/${alice.id}/remind`).send({}).expect(409)).body.code, 'PARTY_NOT_ACTIVE');
  // Reminders still queued for her are dropped.
  assert.equal(rowsOf(draft.id, "kind = 'reminder' AND status = 'queued'").length, 0);

  mail.reset();
  assert.equal((await agent.post(`${draft.url}/parties/${bob.id}/remind`).send({}).expect(503)).body.code, 'EMAIL_NOT_CONFIGURED');
});

test('a party without an address cannot be reminded', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, [{ name: 'Ben' }, { name: 'Anna', email: 'anna@example.com' }], { order: 'random' });
  await sendDraft(agent, draft);
  const res = await agent.post(`${draft.url}/parties/${draft.parties[0].id}/remind`).send({}).expect(400);
  assert.equal(res.body.code, 'NO_EMAIL');
});

test('remind-all reaches every waiting party and reports the others', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE, { order: 'random' });
  const sent = await sendDraft(agent, draft);
  await mailCount(mail, 3);
  await signWith(sent.tokens[2], draft.fields[2].id, 'Cara');
  const res = await agent.post(`${draft.url}/remind-all`).send({}).expect(202);
  assert.deepEqual(res.body.results, [
    { partyId: draft.parties[0].id, status: 'queued' },
    { partyId: draft.parties[1].id, status: 'queued' },
  ]);
  const again = await agent.post(`${draft.url}/remind-all`).send({}).expect(202);
  assert.equal(again.body.results[0].status, 'skipped');
  assert.equal(again.body.results[0].code, 'REMINDER_TOO_SOON');
  assert.ok(again.body.results[0].retryAfter > 0);
  assert.equal(remindAll(db.prepare('SELECT owner_id FROM sign_documents WHERE id = ?').get(draft.id).owner_id, draft.id, {}).results.length, 2);
});

test('automatic reminders follow reminderDays; the expiry warning goes out once', async () => {
  const { agent } = await owner();
  const expiresAt = new Date(Date.now() + 10 * DAY).toISOString();
  const draft = await draftFor(agent, THREE.slice(0, 2), { order: 'random', expiresAt });
  const sent = await sendDraft(agent, draft, { reminderDays: 2 });
  await mailCount(mail, 2);
  const before = (await describeDraft(agent, draft)).parties[0].delivery;
  assert.ok(before.nextReminderAt);
  assert.ok(Math.abs(Date.parse(before.nextReminderAt) - (Date.now() + 2 * DAY)) < 60000);

  // Day 1: nothing yet.
  assert.equal(runReminderPass({ now: Date.now() + DAY }), 0);
  // Day 2: both parties are reminded.
  const dayTwo = Date.now() + 2 * DAY + 5000;
  assert.equal(runReminderPass({ now: dayTwo }), 2);
  assert.equal(runReminderPass({ now: dayTwo }), 0, 'not twice');
  assert.equal(rowsOf(draft.id, "kind = 'reminder'").length, 2);
  await processDue({ now: dayTwo });
  assert.equal(mail.sent.filter((message) => /^Reminder/.test(message.subject)).length, 2, JSON.stringify(mail.sent.map((m) => [m.to, m.subject])));
  // One party signs: it is not reminded again, the other is warned of the expiry.
  await signWith(sent.tokens[0], draft.fields[0].id, 'Alice');
  const warnAt = Date.parse(expiresAt) - 23 * 60 * 60 * 1000;
  const queued = runReminderPass({ now: warnAt });
  assert.equal(queued, 1);
  assert.equal(runReminderPass({ now: warnAt + 1000 }), 0, 'the warning is once');
  await processDue({ now: warnAt });
  const warning = mail.sent.find((message) => /expires soon/.test(message.subject));
  assert.deepEqual(warning.to, ['bob@example.com']);
  assert.ok(warning.text.includes(sent.tokens[1]));
  const state = (await describeDraft(agent, draft)).parties[1].delivery;
  assert.equal(state.remindersSent, 1);
});

test('reminders off: no automatic reminder, only the warning', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1), { order: 'random' });
  await sendDraft(agent, draft);
  await mailCount(mail, 1);
  assert.equal(runReminderPass({ now: Date.now() + 10 * DAY }), 0);
  assert.equal((await describeDraft(agent, draft)).parties[0].delivery.nextReminderAt, null);
});

test('completion mail: a link instead of the attachment when the file is too large', async () => {
  const { agent, username } = await owner();
  setAttachmentLimitForTests(10);
  const draft = await draftFor(agent, THREE.slice(0, 1), { order: 'random' });
  const sent = await sendDraft(agent, draft);
  await mailCount(mail, 1);
  await signWith(sent.tokens[0], draft.fields[0].id, 'Alice');
  await mailCount(mail, 3);
  const completion = mail.sent.slice(1);
  assert.deepEqual(completion.map((message) => message.to[0]).sort(), ['alice@example.com', `${username}@example.com`]);
  for (const message of completion) {
    assert.equal(message.payload.attachments, undefined);
    assert.ok(message.text.includes('too large to attach'));
  }
  assert.ok(completion.find((message) => message.to[0] === 'alice@example.com').text.includes(`/app/sign/${sent.tokens[0]}`));
});

test('notices to the owner: each signature only when the profile asks, a decline always, an expiry too', async () => {
  const { agent, username } = await owner();
  const mine = `${username}@example.com`;
  const draft = await draftFor(agent, THREE, { order: 'random' });
  const sent = await sendDraft(agent, draft);
  await mailCount(mail, 3);

  await signWith(sent.tokens[0], draft.fields[0].id, 'Alice A');
  const lenBefore = mail.sent.length;
  const quiet = await stillQuiet(mail, 250);
  assert.ok(quiet, JSON.stringify([lenBefore, mail.sent.length, mail.sent.map((m) => [m.to, m.subject])]));
  assert.ok(!recipients().includes(mine), 'off by default');

  await agent.put('/api/signing/profile').send({ notifyOnEachSignature: true }).expect(200);
  await signWith(sent.tokens[1], draft.fields[1].id, 'Bob B');
  await mailCount(mail, 4);
  const notice = mail.sent[3];
  assert.deepEqual(notice.to, [mine]);
  assert.match(notice.subject, /Bob B signed/);
  assert.ok(notice.html.includes('2 of 3'));
  assert.ok(notice.text.includes(`/app/signing/${draft.id}`));
  assert.ok(!notice.raw.includes(sent.tokens[0]) && !notice.raw.includes(sent.tokens[2]), 'no party link in a notice');

  await consent(sent.tokens[2], 'Cara C');
  await publicPost(sent.tokens[2], '/decline', { reason: 'Not for me <script>' }).expect(200);
  await mailCount(mail, 5);
  const declined = mail.sent[4];
  assert.match(declined.subject, /Cara C declined/);
  assert.ok(declined.html.includes('Not for me &lt;script&gt;'));
  await agent.put('/api/signing/profile').send({ notifyOnEachSignature: false }).expect(200);
});

test('an owner without a verified address gets no notices, silently', async () => {
  const account = await registerQuietly('unverified');
  // Sending needs a verified address; the address then stops being verified (changed, or taken away by an admin).
  verifyEmail(account.user.id);
  const draft = await draftFor(account.agent, THREE.slice(0, 2), { order: 'random' });
  const sent = await sendDraft(account.agent, draft);
  await mailCount(mail, 2);
  db.prepare('UPDATE users SET email_verified_at = NULL WHERE id = ?').run(account.user.id);
  await account.agent.put('/api/signing/profile').send({ notifyOnEachSignature: true }).expect(200);
  await signWith(sent.tokens[0], draft.fields[0].id, 'Alice');
  await consent(sent.tokens[1], 'Bob');
  await publicPost(sent.tokens[1], '/decline', { reason: 'no' }).expect(200);
  assert.ok(await stillQuiet(mail, 250));
  assert.equal(mail.sent.length, 2);
});

test('expiry tells the owner, the owner\'s own cancel does not, and queued mail is dropped', async () => {
  const { agent, username } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 2), { order: 'preserved' });
  await sendDraft(agent, draft);
  await mailCount(mail, 1);
  await agent.post(`${draft.url}/cancel`).send({}).expect(200);
  assert.ok(await stillQuiet(mail, 250), 'nobody is mailed about the owner\'s own cancel');
  assert.equal(mail.sent.length, 1);

  const second = await draftFor(agent, THREE.slice(0, 1), { order: 'random' });
  await sendDraft(agent, second);
  await mailCount(mail, 2);
  db.prepare("UPDATE sign_documents SET expires_at = ? WHERE id = ?").run(new Date(Date.now() - 1000).toISOString(), second.id);
  require('../../server/services/signing/expiry').expireDueDocuments();
  await mailCount(mail, 3);
  assert.deepEqual(mail.sent[2].to, [`${username}@example.com`]);
  assert.match(mail.sent[2].subject, /^Expired:/);
});

test('a renewed link is mailed again with the new link, the old link is not', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1), { order: 'random' });
  const sent = await sendDraft(agent, draft);
  await mailCount(mail, 1);
  const renewed = await agent.post(`${draft.url}/parties/${draft.parties[0].id}/link`).send({}).expect(200);
  const newToken = renewed.body.url.split('/').pop();
  assert.equal(renewed.body.delivery.status, 'queued');
  await mailCount(mail, 2);
  assert.ok(mail.sent[1].text.includes(newToken));
  assert.ok(!mail.sent[1].text.includes(sent.tokens[0]));
  // Turning the link off leaves nothing to mail.
  await agent.post(`${draft.url}/parties/${draft.parties[0].id}/revoke`).send({}).expect(200);
  const stored = db.prepare('SELECT token_enc, token_hash FROM sign_parties WHERE id = ?').get(draft.parties[0].id);
  assert.deepEqual(stored, { token_enc: null, token_hash: null });
});

test('the stored copy of a token is encrypted and never in the API', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1), { order: 'random' });
  const sent = await sendDraft(agent, draft);
  const stored = db.prepare('SELECT token_enc FROM sign_parties WHERE id = ?').get(draft.parties[0].id);
  assert.match(stored.token_enc, /^enc:v1:/);
  assert.ok(!stored.token_enc.includes(sent.tokens[0]));
  const body = JSON.stringify((await describeDraft(agent, draft)));
  assert.ok(!body.includes(sent.tokens[0]) && !body.includes('token_enc') && !body.includes(stored.token_enc));
  const outbox = JSON.stringify(rowsOf(draft.id));
  assert.ok(!outbox.includes(sent.tokens[0]), 'the outbox holds no link');
});

test('the worker picks up interrupted mail when it starts, queues due reminders and stops cleanly', async () => {
  const { agent } = await owner();
  const draft = await draftFor(agent, THREE.slice(0, 1), { order: 'random' });
  await sendDraft(agent, draft, { reminderDays: 2 });
  await mailCount(mail, 1);
  db.prepare("UPDATE sign_mail_outbox SET status = 'sending' WHERE document_id = ?").run(draft.id);
  const stop = startMailWorker();
  assert.equal(rowsOf(draft.id)[0].status, 'queued', 'a mail cut off by a stop is queued again at start');
  stop();
  // One pass does what the timers would: reminders first, then everything due.
  await runMailPass({ now: Date.now() + 2 * DAY + 10000 });
  assert.equal(rowsOf(draft.id, "kind = 'reminder'").length, 1);
  assert.equal(rowsOf(draft.id).filter((row) => row.status === 'sent').length, 2);
});
