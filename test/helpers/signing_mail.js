'use strict';

// Shared set-up for the signing mail and account tests: verified addresses,
// a document with one text field per party, and a signer that finishes it.

const db = require('../../server/db/database');
const { area, consent, createDraft, publicPost, publicPut, setUp, until } = require('./signing');

function verifyEmail(userId) {
  db.prepare("UPDATE users SET email_verified_at = datetime('now') WHERE id = ?").run(userId);
}

/** A draft with one required text field per party, ready to send. `parties`: [{ name, email? }]. */
async function draftFor(agent, parties, { order = 'preserved', locale = 'en', title, expiresAt } = {}) {
  const draft = await createDraft(agent, { name: 'agreement.pdf' });
  const documentUrl = `/api/signing/documents/${draft.document.id}`;
  const stored = await setUp(agent, draft.document.id, {
    parties,
    fields: parties.map((party, index) => ({ party: index, type: 'text', name: `Field ${index + 1}`, areas: [area(0.1, 0.1 + index * 0.1)] })),
  });
  await agent.put(documentUrl).send({ signingOrder: order, locale, ...(title ? { title } : {}), ...(expiresAt ? { expiresAt } : {}) }).expect(200);
  return { id: draft.document.id, url: documentUrl, parties: stored.parties, fields: stored.fields };
}

async function sendDraft(agent, draft, body = {}) {
  const res = await agent.post(`${draft.url}/send`).send(body).expect(200);
  return { ...res.body, tokens: res.body.links.map((link) => link.url.split('/').pop()) };
}

/** The signer opens the link, agrees, fills the one field and finishes. */
async function signWith(token, fieldId, name = 'Sam Signer') {
  await consent(token, name);
  await publicPut(token, `/fields/${fieldId}`, { value: name }).expect(200);
  return publicPost(token, '/complete').expect(200);
}

const describeDraft = (agent, draft) => agent.get(draft.url).expect(200).then((res) => res.body);

/** Waits until the mailbox holds `count` messages. */
const mailCount = (mail, count) => until(() => mail.sent.length >= count && mail.sent, { timeoutMs: 8000 });

/** True when no mail arrives within `ms` (the fake mailbox's own quiet() counts only unread mail). */
async function stillQuiet(mail, ms = 200) {
  const before = mail.sent.length;
  await new Promise((resolve) => setTimeout(resolve, ms));
  return mail.sent.length === before;
}

module.exports = { describeDraft, draftFor, mailCount, sendDraft, signWith, stillQuiet, verifyEmail };
