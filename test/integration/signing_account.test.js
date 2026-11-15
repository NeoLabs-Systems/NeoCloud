'use strict';

// Signing as an account holder: the signing profile and saved signatures,
// claiming a link and the prefill, saving signed files to the signer's cloud,
// the inbox and grant tokens, signing yourself, export and erasure.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { cleanup, registerUser, request, app } = require('../helpers/app');
const db = require('../../server/db/database');
const { installFakeMail } = require('../helpers/mail');
const {
  area, consent, createDraft, makePng, publicGet, publicPost, publicPut, setUp, until,
} = require('../helpers/signing');
const { describeDraft, mailCount, sendDraft, signWith, verifyEmail } = require('../helpers/signing_mail');
const { retryPendingAutosaves } = require('../../server/services/signing/autosave');
const { eraseAccount } = require('../../server/services/account/erasure');

const mail = installFakeMail();

test.after(() => {
  mail.restore();
  cleanup();
});

const DAY = 24 * 60 * 60 * 1000;
const events = (documentId, type) => db.prepare('SELECT * FROM sign_events WHERE document_id = ? AND type = ? ORDER BY id').all(documentId, type);

// Registration is rate limited, so the accounts are made once and shared; every test works on its own documents.
const pool = {};

async function member(name) {
  if (!pool[name]) {
    pool[name] = await registerUser(name);
    verifyEmail(pool[name].user.id);
  }
  return pool[name];
}

// A profile as a new account has it.
function freshProfile(account) {
  db.prepare('DELETE FROM sign_profile_images WHERE user_id = ?').run(account.user.id);
  db.prepare('DELETE FROM sign_profiles WHERE user_id = ?').run(account.user.id);
  db.prepare('UPDATE users SET quota_bytes = NULL WHERE id = ?').run(account.user.id);
  db.prepare('DELETE FROM sign_party_grants WHERE user_id = ?').run(account.user.id);
  db.prepare('UPDATE sign_parties SET user_id = NULL WHERE user_id = ?').run(account.user.id);
  return account;
}

const putProfile = (agent, body) => agent.put('/api/signing/profile').send(body);
const uploadSignature = (agent, png, query = 'kind=signature&method=draw&label=Main') => (
  agent.post(`/api/signing/profile/images?${query}`).set('Content-Type', 'image/png').send(png)
);

// A document with one signer whose fields cover what the profile can answer.
async function documentWithFields(agent, { fields, order = 'preserved', extraParties = [], title = 'Rental agreement' } = {}) {
  const draft = await createDraft(agent, { name: 'contract.pdf' });
  const parties = [{ name: 'Tenant' }, ...extraParties];
  const stored = await setUp(agent, draft.document.id, {
    parties,
    fields: fields.map((field, index) => ({ party: 0, areas: [area(0.1, 0.05 + index * 0.06)], ...field })),
  });
  const url = `/api/signing/documents/${draft.document.id}`;
  await agent.put(url).send({ signingOrder: order, title }).expect(200);
  return { id: draft.document.id, url, parties: stored.parties, fields: stored.fields };
}

const send = async (agent, document) => sendDraft(agent, document);

test('the profile starts from the account, validates its input and belongs to one account', async () => {
  const alice = freshProfile(await member('p1'));
  const bob = freshProfile(await member('p2'));
  const initial = (await alice.agent.get('/api/signing/profile').expect(200)).body;
  assert.deepEqual(initial, {
    displayName: alice.username,
    email: `${alice.username}@example.com`,
    phone: '',
    presets: [],
    autoFill: true,
    autoSave: true,
    saveFolderId: null,
    notifyOnEachSignature: false,
    signatures: [],
  });

  const saved = (await putProfile(alice.agent, {
    displayName: 'Alice Anders', email: 'alice@work.example', phone: '+49 170 1234567',
    presets: [{ label: 'Adresse', value: 'Hauptstr. 1\n10115 Berlin' }, { label: 'Company', value: 'ACME GmbH' }],
    autoFill: false, notifyOnEachSignature: true,
  }).expect(200)).body;
  assert.equal(saved.displayName, 'Alice Anders');
  assert.equal(saved.presets.length, 2);
  assert.match(saved.presets[0].id, /^[0-9a-f-]{36}$/);
  assert.equal(saved.autoFill, false);
  assert.equal(saved.autoSave, true, 'what the body leaves out stays');
  assert.equal(saved.notifyOnEachSignature, true);
  // A partial change keeps the rest; the stored details are encrypted at rest.
  const again = (await putProfile(alice.agent, { phone: '' }).expect(200)).body;
  assert.equal(again.displayName, 'Alice Anders');
  assert.equal(again.phone, '');
  assert.deepEqual(again.presets.map((preset) => preset.id), saved.presets.map((preset) => preset.id), 'ids stay when sent back');
  const stored = db.prepare('SELECT data_enc FROM sign_profiles WHERE user_id = ?').get(alice.user.id);
  assert.match(stored.data_enc, /^enc:v1:/);
  assert.ok(!stored.data_enc.includes('ACME'));

  // Bob never sees it.
  assert.equal((await bob.agent.get('/api/signing/profile').expect(200)).body.displayName, bob.username);

  for (const body of [
    { displayName: 5 },
    { displayName: 'x'.repeat(121) },
    { email: 'not-an-email' },
    { phone: 'x'.repeat(41) },
    { autoFill: 'yes' },
    { presets: 'nope' },
    { presets: Array.from({ length: 31 }, (_, index) => ({ label: `L${index}`, value: 'v' })) },
    { presets: [{ label: 'x'.repeat(61), value: 'v' }] },
    { presets: [{ label: 'Ok', value: 'v'.repeat(501) }] },
    { presets: [{ label: '', value: 'v' }] },
    { presets: [{ label: 'Firma', value: 'a' }, { label: 'FIRMA', value: 'b' }] },
  ]) {
    assert.equal((await putProfile(alice.agent, body)).status, 400, JSON.stringify(body).slice(0, 60));
  }
  await alice.agent.get('/api/signing/profile').expect(200);
  // Thirty presets are fine.
  await putProfile(alice.agent, { presets: Array.from({ length: 30 }, (_, index) => ({ label: `L${index}`, value: 'v' })) }).expect(200);
});

test('the save folder: a folder of your own, never a vault, never someone else’s', async () => {
  const alice = freshProfile(await member('p3'));
  const bob = freshProfile(await member('p4'));
  const folder = (await alice.agent.post('/api/folders').send({ name: 'Signed stuff' }).expect(201)).body;
  const theirs = (await bob.agent.post('/api/folders').send({ name: 'Theirs' }).expect(201)).body;
  const vault = (await alice.agent.post('/api/vaults').send({
    parentId: 'root', name: 'Tresor', salt: crypto.randomBytes(16).toString('base64'), iterations: 600000, wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  assert.equal((await putProfile(alice.agent, { saveFolderId: folder.id }).expect(200)).body.saveFolderId, folder.id);
  for (const saveFolderId of [vault.folderId, theirs.id, crypto.randomUUID(), 'nonsense', 5]) {
    const res = await putProfile(alice.agent, { saveFolderId });
    assert.equal(res.status, 400, String(saveFolderId));
    assert.equal(res.body.code, 'INVALID_FOLDER');
  }
  assert.equal((await putProfile(alice.agent, { saveFolderId: null }).expect(200)).body.saveFolderId, null);
});

test('saved signatures: PNG only, five per kind, owner only, one default per kind', async () => {
  const alice = freshProfile(await member('p1'));
  const bob = freshProfile(await member('p2'));
  const png = await makePng(400, 150);
  const first = (await uploadSignature(alice.agent, png).expect(201)).body;
  assert.deepEqual(Object.keys(first).sort(), ['createdAt', 'id', 'isDefault', 'kind', 'label', 'method']);
  assert.equal(first.isDefault, true, 'the first of a kind is its default');
  assert.equal(first.label, 'Main');
  const second = (await uploadSignature(alice.agent, png, 'kind=signature&method=type&text=Alice&label=Typed&default=1').expect(201)).body;
  assert.equal(second.isDefault, true);
  const initials = (await uploadSignature(alice.agent, png, 'kind=initials&method=upload').expect(201)).body;
  let signatures = (await alice.agent.get('/api/signing/profile').expect(200)).body.signatures;
  assert.equal(signatures.length, 3);
  assert.deepEqual(signatures.filter((item) => item.isDefault).map((item) => item.id).sort(), [second.id, initials.id].sort());

  // The picture comes back as a PNG, re-encoded and limited in size, for its owner only.
  const picture = await alice.agent.get(`/api/signing/profile/images/${first.id}`).expect(200).buffer(true).parse((res, done) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => done(null, Buffer.concat(chunks)));
  });
  assert.equal(picture.headers['content-type'], 'image/png');
  assert.equal(picture.body.subarray(1, 4).toString(), 'PNG');
  await bob.agent.get(`/api/signing/profile/images/${first.id}`).expect(404);
  await bob.agent.put(`/api/signing/profile/images/${first.id}`).send({ label: 'mine now' }).expect(404);
  await bob.agent.delete(`/api/signing/profile/images/${first.id}`).expect(404);
  assert.equal((await bob.agent.get('/api/signing/profile').expect(200)).body.signatures.length, 0);

  const renamed = (await alice.agent.put(`/api/signing/profile/images/${first.id}`).send({ label: 'Formal', isDefault: true }).expect(200)).body;
  assert.deepEqual([renamed.label, renamed.isDefault], ['Formal', true]);
  signatures = (await alice.agent.get('/api/signing/profile').expect(200)).body.signatures;
  assert.equal(signatures.find((item) => item.id === second.id).isDefault, false);
  await alice.agent.put(`/api/signing/profile/images/${first.id}`).send({ isDefault: 'x' }).expect(400);

  for (let index = 0; index < 3; index += 1) await uploadSignature(alice.agent, png, `kind=signature&label=S${index}`).expect(201);
  const full = await uploadSignature(alice.agent, png);
  assert.equal(full.status, 409);
  assert.equal(full.body.code, 'IMAGE_LIMIT');
  await uploadSignature(alice.agent, png, 'kind=initials&label=Two').expect(201);

  for (const query of ['kind=stamp', 'method=draw', 'kind=signature&method=scan']) {
    assert.equal((await uploadSignature(alice.agent, png, query)).status, 400, query);
  }
  const initialsQuery = 'kind=initials&method=draw';
  assert.equal((await uploadSignature(alice.agent, Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"></svg>'), initialsQuery)).status, 400);
  assert.equal((await uploadSignature(alice.agent, Buffer.from('not a picture'), initialsQuery)).status, 400);
  // Too big is refused before the body is read; the connection may close under the sender.
  const huge = await uploadSignature(alice.agent, Buffer.alloc(9 * 1024 * 1024, 1), initialsQuery).then((res) => res.status, (err) => err.code);
  assert.ok([413, 'EPIPE', 'ECONNRESET'].includes(huge), String(huge));

  // Deleting the default promotes another one and releases the storage.
  const usedBefore = db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(alice.user.id).used_bytes;
  await alice.agent.delete(`/api/signing/profile/images/${first.id}`).expect(200);
  await alice.agent.get(`/api/signing/profile/images/${first.id}`).expect(404);
  const left = (await alice.agent.get('/api/signing/profile').expect(200)).body.signatures.filter((item) => item.kind === 'signature');
  assert.equal(left.filter((item) => item.isDefault).length, 1);
  assert.ok(db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(alice.user.id).used_bytes < usedBefore);
});

test('claiming a link prefills the fields the profile can answer, and never overwrites', async () => {
  const owner = await member('owner');
  const signer = freshProfile(await member('s1'));
  const thief = await member('s2');
  await putProfile(signer.agent, {
    displayName: 'Maria Anna Müller', email: 'maria@example.org', phone: '+49 30 123456',
    presets: [{ label: 'Adresse', value: 'Hauptstr. 1, Berlin' }, { label: 'Firma', value: 'ACME GmbH' }, { label: 'Titel', value: 'Dr.' }],
  }).expect(200);
  const typed = (await uploadSignature(signer.agent, await makePng(), 'kind=signature&method=type&text=Maria&label=Default').expect(201)).body;
  await uploadSignature(signer.agent, await makePng(200, 80), 'kind=initials&method=draw').expect(201);

  const doc = await documentWithFields(owner.agent, {
    fields: [
      { type: 'text', name: 'Vollständiger Name' },
      { type: 'text', name: 'E-Mail' },
      { type: 'phone', name: 'Telefon' },
      { type: 'text', name: 'Company name' },
      { type: 'text', name: 'Straße' },
      { type: 'text', name: 'Nachname' },
      { type: 'text', name: 'Tenant name' },
      { type: 'text', name: 'Name', defaultValue: 'Sender default' },
      { type: 'text', name: 'Email', readonly: true, defaultValue: 'locked@example.com' },
      { type: 'date', name: 'Datum', prefs: { autoToday: true, format: 'DD.MM.YYYY' } },
      { type: 'date', name: 'Date of birth' },
      { type: 'signature', name: 'Signature' },
      { type: 'initials', name: 'Initials' },
      { type: 'number', name: 'Telefon number' },
    ],
  });
  const sent = await send(owner.agent, doc);
  const [token] = sent.tokens;
  const byName = (name) => doc.fields.find((field) => field.name === name).id;
  // The signer types one value after agreeing, then claims.
  await consent(token, 'Maria Anna Müller');
  await publicPut(token, `/fields/${byName('Straße')}`, { value: 'Typed by hand' }).expect(200);

  const preview = (await signer.agent.post('/api/signing/claim').send({ token, today: '2026-03-15' }).expect(200)).body;
  assert.equal(preview.partyId, doc.parties[0].id);
  assert.equal(preview.documentTitle, 'Rental agreement');
  assert.deepEqual(preview.applied, []);
  const table = Object.fromEntries(preview.prefill.map((item) => [doc.fields.find((field) => field.id === item.fieldId).name, item]));
  assert.deepEqual(Object.keys(table).sort(), [
    'Company name', 'Datum', 'E-Mail', 'Initials', 'Nachname', 'Signature', 'Telefon', 'Vollständiger Name',
  ].sort());
  assert.equal(table['Vollständiger Name'].value, 'Maria Anna Müller');
  assert.equal(table['E-Mail'].value, 'maria@example.org');
  assert.equal(table.Telefon.value, '+49 30 123456');
  assert.equal(table['Company name'].value, 'ACME GmbH');
  assert.equal(table.Nachname.value, 'Müller');
  assert.equal(table.Datum.value, '2026-03-15');
  assert.equal(table.Signature.imageId, typed.id);
  assert.ok(table.Initials.imageId);
  assert.deepEqual(Object.keys(table.Signature).sort(), ['fieldId', 'imageId', 'name', 'source', 'type']);
  assert.equal(table.Signature.source, 'profile');
  assert.equal(preview.canPrefill, true);
  // Nothing was written by a preview.
  const stillEmpty = (await publicGet(token).expect(200)).body.myValues;
  assert.deepEqual(Object.keys(stillEmpty).sort(), [byName('Straße'), byName('Name'), byName('Email')].sort());

  const applied = (await signer.agent.post('/api/signing/claim').send({ token, apply: true, today: '2026-03-15' }).expect(200)).body;
  assert.deepEqual([...applied.applied].sort(), preview.prefill.map((item) => item.fieldId).sort());
  const values = (await publicGet(token).expect(200)).body.myValues;
  assert.equal(values[byName('Vollständiger Name')], 'Maria Anna Müller');
  assert.equal(values[byName('Straße')], 'Typed by hand', 'what the signer entered stays');
  assert.equal(values[byName('Name')], 'Sender default', 'a default of the sender stays');
  assert.equal(values[byName('Datum')], '2026-03-15');
  assert.equal(values[byName('Signature')].method, 'type');
  assert.equal(values[byName('Signature')].text, 'Maria');
  // The saved picture is now the party's value, in the document owner's storage.
  const shown = await publicGet(token, `/values/${byName('Signature')}/image`).expect(200);
  assert.equal(shown.headers['content-type'], 'image/png');
  // Claiming again changes nothing and finds nothing left to fill.
  const repeat = (await signer.agent.post('/api/signing/claim').send({ token, apply: true }).expect(200)).body;
  assert.deepEqual([repeat.prefill, repeat.applied, repeat.canPrefill], [[], [], false]);

  // Another account cannot take a claimed party; a wrong token is a 404.
  const taken = await thief.agent.post('/api/signing/claim').send({ token });
  assert.equal(taken.status, 409);
  assert.equal(taken.body.code, 'ALREADY_CLAIMED');
  await thief.agent.post('/api/signing/claim').send({ token: 'x'.repeat(43) }).expect(404);
  await thief.agent.post('/api/signing/claim').send({}).expect(400);
  assert.equal(events(doc.id, 'claimed').length, 1);
  assert.equal(db.prepare('SELECT user_id FROM sign_parties WHERE id = ?').get(doc.parties[0].id).user_id, signer.user.id);
  // Without a session there is no claim at all.
  await request(app).post('/api/signing/claim').send({ token }).expect(401);
});

test('prefill respects the switch, the turn and the type checks', async () => {
  const owner = await member('owner');
  const signer = freshProfile(await member('s3'));
  await putProfile(signer.agent, { displayName: 'Sam Signer', email: 'sam@example.org', phone: 'abc' }).expect(200);
  const doc = await documentWithFields(owner.agent, {
    order: 'preserved',
    extraParties: [{ name: 'Landlord' }],
    fields: [
      { type: 'text', name: 'Full name' },
      { type: 'phone', name: 'Phone' },
      { type: 'text', name: 'Email', prefs: { format: 'email', maxLength: 8 } },
    ],
  });
  // The second party's field, so the document can be sent.
  await owner.agent.put(`${doc.url}/fields`).send({
    fields: [...doc.fields.map((field) => ({ ...field, partyId: doc.parties[0].id })), {
      partyId: doc.parties[1].id, type: 'text', name: 'Full name', areas: [area(0.1, 0.7)],
    }],
  }).expect(200);
  const sent = await send(owner.agent, doc);
  const waiting = (await signer.agent.post('/api/signing/claim').send({ token: sent.tokens[1], apply: true }).expect(200)).body;
  assert.deepEqual([waiting.prefill, waiting.applied], [[], []], 'a party waiting for its turn is claimed but not filled');

  const first = (await signer.agent.post('/api/signing/claim').send({ token: sent.tokens[0], apply: true }).expect(200)).body;
  // The phone number is not a valid phone, the email is longer than the field takes: neither is offered.
  assert.deepEqual(first.prefill.map((item) => item.name), ['Full name']);

  await putProfile(signer.agent, { autoFill: false }).expect(200);
  const fresh = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Full name' }] });
  const freshSent = await send(owner.agent, fresh);
  const off = (await signer.agent.post('/api/signing/claim').send({ token: freshSent.tokens[0], apply: true }).expect(200)).body;
  assert.deepEqual([off.prefill, off.applied, off.canPrefill], [[], [], false]);
  assert.equal(db.prepare('SELECT user_id FROM sign_parties WHERE id = ?').get(fresh.parties[0].id).user_id, signer.user.id, 'still claimed');

  // A closed document claims but fills nothing.
  await owner.agent.post(`${fresh.url}/cancel`).send({}).expect(200);
  const closed = (await signer.agent.post('/api/signing/claim').send({ token: freshSent.tokens[0], apply: true }).expect(200)).body;
  assert.deepEqual(closed.prefill, []);
});

test('finishing still needs the signer’s consent, with or without a prefill', async () => {
  const owner = await member('owner');
  const signer = freshProfile(await member('s3'));
  await putProfile(signer.agent, { displayName: 'Casey Consent' }).expect(200);
  const doc = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }] });
  const [token] = (await send(owner.agent, doc)).tokens;
  await signer.agent.post('/api/signing/claim').send({ token, apply: true }).expect(200);
  const early = await publicPost(token, '/complete').expect(409);
  assert.equal(early.body.code, 'CONSENT_REQUIRED');
  await consent(token, 'Casey Consent');
  await publicPost(token, '/complete').expect(200);
});

test('the signed file is saved to the signer’s cloud, owned by the signer', async () => {
  const owner = await member('owner');
  const signer = freshProfile(await member('s4'));
  const doc = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }], title: 'Sale: "deal"/2026' });
  const [token] = (await send(owner.agent, doc)).tokens;
  await signer.agent.post('/api/signing/claim').send({ token }).expect(200);
  await signWith(token, doc.fields[0].id, 'Sam Signer');
  const saved = await until(() => events(doc.id, 'autosaved')[0]);
  const nodeId = JSON.parse(saved.data_json).nodeId;
  assert.equal(saved.party_id, doc.parties[0].id);

  const root = (await signer.agent.get('/api/folders/root/children').expect(200)).body.items;
  const folder = root.find((item) => item.name === 'Signed documents' && item.type === 'folder');
  assert.ok(folder, 'the automatic folder is made on demand');
  const files = (await signer.agent.get(`/api/folders/${folder.id}/children`).expect(200)).body.items;
  assert.deepEqual(files.map((file) => file.name), ['Sale: "deal"-2026 (signed).pdf']);
  assert.equal(files[0].id, nodeId);
  assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(nodeId).user_id, signer.user.id);
  const download = await signer.agent.get(`/api/files/${nodeId}/content`).expect(200).buffer(true).parse((res, done) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => done(null, Buffer.concat(chunks)));
  });
  assert.equal(download.body.subarray(0, 5).toString(), '%PDF-');
  // The owner has the file next to the original, and it is not the signer's node.
  const ownerDoc = (await describeDraft(owner.agent, doc)).document;
  assert.notEqual(ownerDoc.finalNodeId, nodeId);
  assert.equal(events(doc.id, 'autosave_failed').length, 0);
  assert.equal(events(doc.id, 'autosaved').length, 1, 'saved once');
  // The background pass finds nothing left to do.
  assert.equal(await retryPendingAutosaves({ now: Date.now() + 10 * 60 * 1000 }), 0);
});

test('the save follows the profile: switch, chosen folder, a folder that is gone, the owner themselves', async () => {
  const owner = await member('owner');
  const off = freshProfile(await member('s5'));
  const chosen = freshProfile(await member('s6'));
  const gone = freshProfile(await member('s7'));
  await putProfile(off.agent, { autoSave: false }).expect(200);
  const folder = (await chosen.agent.post('/api/folders').send({ name: 'Contracts' }).expect(201)).body;
  await putProfile(chosen.agent, { saveFolderId: folder.id }).expect(200);
  const lost = (await gone.agent.post('/api/folders').send({ name: 'Lost' }).expect(201)).body;
  await putProfile(gone.agent, { saveFolderId: lost.id }).expect(200);
  await gone.agent.delete(`/api/nodes/${lost.id}`).expect(200);

  const doc = await documentWithFields(owner.agent, {
    order: 'random', extraParties: [{ name: 'Two' }, { name: 'Three' }, { name: 'Me' }],
    fields: [{ type: 'text', name: 'Name' }],
  });
  const fields = [0, 1, 2, 3].map((party) => ({ partyId: doc.parties[party].id, type: 'text', name: 'Name', areas: [area(0.1, 0.1 + party * 0.1)] }));
  const stored = (await owner.agent.put(`${doc.url}/fields`).send({ fields }).expect(200)).body.fields;
  const sent = await send(owner.agent, doc);
  const accounts = [off, chosen, gone];
  for (let index = 0; index < 3; index += 1) {
    await accounts[index].agent.post('/api/signing/claim').send({ token: sent.tokens[index] }).expect(200);
  }
  await owner.agent.post(`${doc.url}/parties/${doc.parties[3].id}/open-as-owner`).send({}).expect(200);
  for (let index = 0; index < 4; index += 1) await signWith(sent.tokens[index], stored[index].id, `Signer ${index}`);
  await until(() => events(doc.id, 'autosaved').length >= 2);
  await new Promise((resolve) => setTimeout(resolve, 300));

  const saved = events(doc.id, 'autosaved').map((event) => event.party_id).sort();
  assert.deepEqual(saved, [doc.parties[1].id, doc.parties[2].id].sort(), 'not for the switch that is off, not for the owner');
  const inFolder = (await chosen.agent.get(`/api/folders/${folder.id}/children`).expect(200)).body.items;
  assert.equal(inFolder.length, 1);
  const goneRoot = (await gone.agent.get('/api/folders/root/children').expect(200)).body.items;
  assert.ok(goneRoot.some((item) => item.name === 'Signed documents'), 'a vanished folder falls back to the automatic one');
  const offRoot = (await off.agent.get('/api/folders/root/children').expect(200)).body.items;
  assert.ok(!offRoot.some((item) => item.name === 'Signed documents'));
});

test('a failed save never blocks the document, is recorded, and is tried once more later', async () => {
  const owner = await member('owner');
  const signer = freshProfile(await member('s4'));
  const doc = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }] });
  const [token] = (await send(owner.agent, doc)).tokens;
  await signer.agent.post('/api/signing/claim').send({ token }).expect(200);
  db.prepare('UPDATE users SET quota_bytes = 1 WHERE id = ?').run(signer.user.id);
  await signWith(token, doc.fields[0].id, 'Sam');
  await until(async () => (await describeDraft(owner.agent, doc)).document.status === 'completed');
  const failure = await until(() => events(doc.id, 'autosave_failed')[0]);
  assert.equal(JSON.parse(failure.data_json).code, 'QUOTA_EXCEEDED');
  assert.equal(events(doc.id, 'autosaved').length, 0);

  // Too early: nothing. Later, with room: saved.
  assert.equal(await retryPendingAutosaves({ now: Date.now() + 60 * 1000 }), 0);
  db.prepare('UPDATE users SET quota_bytes = NULL WHERE id = ?').run(signer.user.id);
  assert.equal(await retryPendingAutosaves({ now: Date.now() + 6 * 60 * 1000 }), 1);
  assert.equal(events(doc.id, 'autosaved').length, 1);
  assert.equal((await verifyChain(owner.agent, doc)), true);

  // A second failure is final.
  const second = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }] });
  const [secondToken] = (await send(owner.agent, second)).tokens;
  await signer.agent.post('/api/signing/claim').send({ token: secondToken }).expect(200);
  db.prepare('UPDATE users SET quota_bytes = 1 WHERE id = ?').run(signer.user.id);
  await signWith(secondToken, second.fields[0].id, 'Sam');
  await until(() => events(second.id, 'autosave_failed').length === 1);
  assert.equal(await retryPendingAutosaves({ now: Date.now() + 6 * 60 * 1000 }), 1);
  assert.equal(events(second.id, 'autosave_failed').length, 2);
  assert.equal(await retryPendingAutosaves({ now: Date.now() + 60 * 60 * 1000 }), 0, 'no third try');
  db.prepare('UPDATE users SET quota_bytes = NULL WHERE id = ?').run(signer.user.id);
});

async function verifyChain(agent, doc) {
  return (await agent.get(`${doc.url}/events`).expect(200)).body.verify.chainValid;
}

test('the inbox lists requests for a verified address or a bound account, and only those', async () => {
  const owner = await member('owner');
  const verified = freshProfile(await member('s1'));
  const stranger = freshProfile(await member('s2'));
  if (!pool.unverified) pool.unverified = await registerUser('unverified');
  const unverified = pool.unverified;
  const addressed = `${verified.username}@example.com`;

  const doc = await documentWithFields(owner.agent, {
    order: 'preserved', extraParties: [{ name: 'Second' }, { name: 'Third' }, { name: 'Fourth' }],
    fields: [{ type: 'text', name: 'Name' }],
  });
  const parties = [addressed, `${unverified.username}@example.com`, 'unrelated@example.org', null];
  await owner.agent.put(`${doc.url}/parties`).send({ parties: doc.parties.map((party, index) => ({ id: party.id, name: party.name, email: parties[index] || undefined })) }).expect(200);
  const fields = doc.parties.map((party, index) => ({ partyId: party.id, type: 'text', name: 'Name', areas: [area(0.1, 0.1 + index * 0.1)] }));
  const stored = (await owner.agent.put(`${doc.url}/fields`).send({ fields }).expect(200)).body.fields;
  const sent = await send(owner.agent, doc);

  const inbox = (await verified.agent.get('/api/signing/inbox').expect(200)).body;
  assert.equal(inbox.items.length, 1);
  assert.deepEqual(Object.keys(inbox.items[0]).sort(), ['completedAt', 'creatorName', 'documentId', 'expiresAt', 'finalAvailable', 'partyId', 'sentAt', 'status', 'title'].sort());
  assert.equal(inbox.items[0].status, 'ready');
  assert.equal(inbox.items[0].creatorName, owner.username);
  assert.equal(inbox.items[0].title, 'Rental agreement');
  assert.equal(inbox.items[0].partyId, doc.parties[0].id);
  assert.deepEqual(inbox.counts, { todo: 1 });
  assert.equal(JSON.stringify(inbox).includes(sent.tokens[0]), false, 'the party token never shows');

  // An address that was never verified entitles to nothing; a stranger sees nothing.
  assert.equal((await unverified.agent.get('/api/signing/inbox').expect(200)).body.items.length, 0);
  assert.equal((await stranger.agent.get('/api/signing/inbox').expect(200)).body.items.length, 0);
  assert.equal((await unverified.agent.post(`/api/signing/inbox/${doc.parties[1].id}/open`).send({}).expect(404)).body.code, 'NOT_FOUND');
  await stranger.agent.post(`/api/signing/inbox/${doc.parties[0].id}/open`).send({}).expect(404);
  await stranger.agent.post('/api/signing/inbox/nonsense/open').send({}).expect(404);
  await request(app).get('/api/signing/inbox').expect(401);
  // The same address verified later does.
  verifyEmail(unverified.user.id);
  assert.equal((await unverified.agent.get('/api/signing/inbox').expect(200)).body.items[0].status, 'waiting');
  assert.deepEqual((await unverified.agent.get('/api/signing/inbox').expect(200)).body.counts, { todo: 0 });

  // Binding by claim works without an address match.
  await stranger.agent.post('/api/signing/claim').send({ token: sent.tokens[3] }).expect(200);
  assert.equal((await stranger.agent.get('/api/signing/inbox?status=todo').expect(200)).body.items.length, 1);
  assert.equal((await stranger.agent.get('/api/signing/inbox?status=done').expect(200)).body.items.length, 0);
  await stranger.agent.get('/api/signing/inbox?status=other').expect(400);

  // Finish the first request: it moves to "done"; the claim also shows the saved file.
  const open = (await verified.agent.post(`/api/signing/inbox/${doc.parties[0].id}/open`).send({}).expect(200)).body;
  assert.match(open.url, /^\/app\/sign\/[A-Za-z0-9_-]{43}$/);
  const grant = open.url.split('/').pop();
  assert.notEqual(grant, sent.tokens[0]);
  await signWith(grant, stored[0].id, 'Verified Viewer');
  const after = (await verified.agent.get('/api/signing/inbox').expect(200)).body;
  assert.equal(after.items[0].status, 'completed');
  assert.ok(after.items[0].completedAt);
  assert.deepEqual(after.counts, { todo: 0 });
  assert.equal((await verified.agent.get('/api/signing/inbox?status=done').expect(200)).body.items.length, 1);
  // The next party's turn shows up for the next account.
  assert.equal((await unverified.agent.get('/api/signing/inbox').expect(200)).body.counts.todo, 1);
});

test('grant tokens: same rights as the link, bound to one party, audited, gone when they should be', async () => {
  const owner = await member('owner');
  const signer = freshProfile(await member('s3'));
  const doc = await documentWithFields(owner.agent, {
    order: 'random', extraParties: [{ name: 'Other' }], fields: [{ type: 'text', name: 'Name' }],
  });
  const fields = doc.parties.map((party, index) => ({ partyId: party.id, type: 'text', name: 'Name', areas: [area(0.1, 0.1 + index * 0.1)] }));
  const stored = (await owner.agent.put(`${doc.url}/fields`).send({ fields }).expect(200)).body.fields;
  await owner.agent.put(`${doc.url}/parties`).send({ parties: [
    { id: doc.parties[0].id, name: 'Tenant', email: `${signer.username}@example.com` },
    { id: doc.parties[1].id, name: 'Other' },
  ] }).expect(200);
  const sent = await send(owner.agent, doc);
  const grantOf = async () => (await signer.agent.post(`/api/signing/inbox/${doc.parties[0].id}/open`).send({}).expect(200)).body.url.split('/').pop();

  const grant = await grantOf();
  const page = (await publicGet(grant).expect(200)).body;
  const viaLink = (await publicGet(sent.tokens[0]).expect(200)).body;
  assert.deepEqual(page, viaLink, 'identical permissions and content');
  assert.equal(JSON.stringify(page).includes('Other'), false, 'no names of other parties');
  assert.equal(db.prepare('SELECT user_id FROM sign_parties WHERE id = ?').get(doc.parties[0].id).user_id, signer.user.id, 'bound on first use');
  assert.equal(events(doc.id, 'claimed').length, 1);
  assert.equal(events(doc.id, 'grant_used').length, 1, 'first use is audited, once per grant');
  await publicGet(grant).expect(200);
  assert.equal(events(doc.id, 'grant_used').length, 1);

  // It writes only the party's own fields; the other party's field is not reachable.
  await publicPut(grant, `/fields/${stored[1].id}`, { value: 'x' }).expect(404);
  await consent(grant, 'Grant Signer');
  await publicPut(grant, `/fields/${stored[0].id}`, { value: 'Grant Signer' }).expect(200);

  // A grant is no party token: it cannot be turned into the link, and it does not open the other party.
  const text = JSON.stringify([page, (await publicGet(grant, '/result').expect(200)).body]);
  assert.equal(text.includes(sent.tokens[0]) || text.includes(sent.tokens[1]), false);

  // It lives sixty minutes.
  const row = db.prepare('SELECT * FROM sign_party_grants WHERE party_id = ?').get(doc.parties[0].id);
  assert.ok(Math.abs(Date.parse(row.expires_at) - Date.parse(row.created_at) - 60 * 60 * 1000) < 1000);
  assert.equal(row.token_hash.length, 64);
  assert.ok(!JSON.stringify(row).includes(grant), 'only the hash is stored');
  db.prepare('UPDATE sign_party_grants SET expires_at = ? WHERE id = ?').run(new Date(Date.now() - 1000).toISOString(), row.id);
  await publicGet(grant).expect(404);
  await publicPost(grant, '/complete').expect(404);

  // Completing through a fresh grant ends it; the inbox opens a new one to see the result.
  const second = await grantOf();
  await publicPost(second, '/complete').expect(200);
  await publicGet(second).expect(404);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM sign_party_grants WHERE party_id = ?').get(doc.parties[0].id).n, 0);
  const third = await grantOf();
  const done = (await publicGet(third).expect(200)).body;
  assert.equal(done.state, 'completed');
  // Declining, canceling and expiring end every grant of the document.
  const doomed = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }] });
  await owner.agent.put(`${doomed.url}/parties`).send({ parties: [{ id: doomed.parties[0].id, name: 'Tenant', email: `${signer.username}@example.com` }] }).expect(200);
  await send(owner.agent, doomed);
  const doomedGrant = (await signer.agent.post(`/api/signing/inbox/${doomed.parties[0].id}/open`).send({}).expect(200)).body.url.split('/').pop();
  await publicGet(doomedGrant).expect(200);
  await owner.agent.post(`${doomed.url}/cancel`).send({}).expect(200);
  await publicGet(doomedGrant).expect(404);
  // Another account's party is not reachable through a grant of yours.
  const foreign = await member('s2');
  await foreign.agent.post(`/api/signing/inbox/${doc.parties[0].id}/open`).send({}).expect(404);
});

test('a grant for a party claimed by someone else does not open', async () => {
  const owner = await member('owner');
  const first = freshProfile(await member('s5'));
  const second = freshProfile(await member('s6'));
  const doc = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }] });
  await owner.agent.put(`${doc.url}/parties`).send({ parties: [{ id: doc.parties[0].id, name: 'Tenant', email: `${second.username}@example.com` }] }).expect(200);
  const sent = await send(owner.agent, doc);
  await first.agent.post('/api/signing/claim').send({ token: sent.tokens[0] }).expect(200);
  // The address matches the second account but the party is the first one's: not listed, not openable.
  assert.equal((await second.agent.get('/api/signing/inbox').expect(200)).body.items.length, 0);
  await second.agent.post(`/api/signing/inbox/${doc.parties[0].id}/open`).send({}).expect(404);
});

test('signing yourself: one party is you, no mail goes to it, and it opens with a grant', async () => {
  mail.configure({ NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION: 'false' });
  const owner = freshProfile(await member('owner'));
  const guest = 'guest@example.org';
  const doc = await documentWithFields(owner.agent, {
    order: 'preserved', extraParties: [{ name: 'Guest' }], fields: [{ type: 'text', name: 'Name' }],
  });
  const fields = doc.parties.map((party, index) => ({ partyId: party.id, type: 'text', name: 'Name', areas: [area(0.1, 0.1 + index * 0.1)] }));
  const stored = (await owner.agent.put(`${doc.url}/fields`).send({ fields }).expect(200)).body.fields;

  const twice = await owner.agent.put(`${doc.url}/parties`).send({ parties: [
    { id: doc.parties[0].id, name: 'Me', isSelf: true }, { id: doc.parties[1].id, name: 'Guest', email: guest, isSelf: true },
  ] });
  assert.equal(twice.status, 400);
  const saved = (await owner.agent.put(`${doc.url}/parties`).send({ parties: [
    { id: doc.parties[0].id, name: 'Me', isSelf: true }, { id: doc.parties[1].id, name: 'Guest', email: guest },
  ] }).expect(200)).body.parties;
  assert.equal(saved[0].isSelf, true);
  assert.equal(saved[0].email, `${owner.username}@example.com`, 'the owner’s address is the default');
  assert.equal(saved[1].isSelf, false);

  await owner.agent.post(`${doc.url}/parties/${saved[0].id}/open-as-owner`).send({}).expect(409);
  const sent = await sendDraft(owner.agent, doc);
  assert.deepEqual(sent.delivery.map((entry) => entry.status), ['skipped', 'skipped']);
  assert.equal(mail.sent.length, 0, 'no invitation for yourself, and the guest waits for its turn');

  const open = (await owner.agent.post(`${doc.url}/parties/${saved[0].id}/open-as-owner`).send({}).expect(200)).body;
  assert.match(open.url, /^\/app\/sign\/[A-Za-z0-9_-]{43}$/);
  assert.equal((await publicGet(open.url.split('/').pop()).expect(200)).body.state, 'ready');
  // Only one party can be you.
  assert.equal((await owner.agent.post(`${doc.url}/parties/${saved[1].id}/open-as-owner`).send({}).expect(409)).body.code, 'SELF_PARTY_EXISTS');

  await signWith(open.url.split('/').pop(), stored[0].id, 'Me Myself');
  await mailCount(mail, 1);
  assert.deepEqual(mail.sent[0].to, [guest], 'the guest is mailed when its turn comes');
  assert.ok(!mail.sent[0].text.includes(open.url.split('/').pop()));
  // The owner's own account sees the finished request, and the party is bound to them.
  const view = (await describeDraft(owner.agent, doc)).parties[0];
  assert.equal(view.isSelf, true);
  assert.equal(db.prepare('SELECT user_id FROM sign_parties WHERE id = ?').get(saved[0].id).user_id, owner.user.id);
  mail.reset();
});

test('export lists the profile; erasing an account removes the profile, the images and its blobs', async () => {
  const signer = await registerUser('eraser');
  verifyEmail(signer.user.id);
  const keep = freshProfile(await member('s7'));
  await putProfile(signer.agent, { displayName: 'Erin Eraser', presets: [{ label: 'Firma', value: 'Secret Inc' }] }).expect(200);
  await uploadSignature(signer.agent, await makePng()).expect(201);
  await uploadSignature(keep.agent, await makePng()).expect(201);
  const exported = (await signer.agent.post('/api/account/export').send({ currentPassword: require('../helpers/app').STRONG_PASSWORD }).expect(200)).body;
  assert.equal(exported.signingProfile.displayName, 'Erin Eraser');
  assert.equal(exported.signingProfile.presets[0].value, 'Secret Inc');
  assert.equal(exported.signingProfile.signatures.length, 1);
  assert.equal(JSON.stringify(exported).includes('blob_id'), false);

  const blobs = db.prepare('SELECT blob_id FROM sign_profile_images WHERE user_id = ?').all(signer.user.id).map((row) => row.blob_id);
  await eraseAccount(signer.user.id, { actorUserId: signer.user.id });
  for (const table of ['sign_profiles', 'sign_profile_images', 'sign_party_grants']) {
    assert.equal(db.prepare(`SELECT COUNT(*) AS n FROM ${table} WHERE user_id = ?`).get(signer.user.id).n, 0, table);
  }
  for (const blob of blobs) assert.equal(db.prepare('SELECT COUNT(*) AS n FROM blobs WHERE id = ?').get(blob).n, 0);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM sign_profile_images WHERE user_id = ?').get(keep.user.id).n, 1, 'other accounts keep theirs');
});

test('erasing a signer unbinds their parties; erasing the owner removes grants and mail with the document', async () => {
  const owner = await registerUser('eraseowner');
  const signer = await registerUser('erasesigner');
  verifyEmail(signer.user.id);
  const doc = await documentWithFields(owner.agent, { fields: [{ type: 'text', name: 'Name' }] });
  await owner.agent.put(`${doc.url}/parties`).send({ parties: [{ id: doc.parties[0].id, name: 'Tenant', email: `${signer.username}@example.com` }] }).expect(200);
  await send(owner.agent, doc);
  await signer.agent.post(`/api/signing/inbox/${doc.parties[0].id}/open`).send({}).expect(200);
  db.prepare("INSERT INTO sign_mail_outbox (document_id, party_id, kind, to_email, status, next_attempt_at, created_at) VALUES (?, ?, 'reminder', 'x@example.org', 'failed', ?, ?)")
    .run(doc.id, doc.parties[0].id, new Date().toISOString(), new Date().toISOString());
  await eraseAccount(signer.user.id, { actorUserId: signer.user.id });
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM sign_party_grants WHERE party_id = ?').get(doc.parties[0].id).n, 0);
  assert.equal(db.prepare('SELECT user_id FROM sign_parties WHERE id = ?').get(doc.parties[0].id).user_id, null);
  await eraseAccount(owner.user.id, { actorUserId: owner.user.id });
  for (const table of ['sign_mail_outbox', 'sign_parties']) {
    assert.equal(db.prepare(`SELECT COUNT(*) AS n FROM ${table} WHERE document_id = ?`).get(doc.id).n, 0, table);
  }
});
