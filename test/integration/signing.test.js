'use strict';

// The signing lifecycle: one party, several parties in order and at once,
// decline, cancel, links, expiry, the final file and the audit trail.

const test = require('node:test');
const assert = require('node:assert/strict');
const { PDFDocument } = require('pdf-lib');
const { STRONG_PASSWORD, cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const {
  area, consent, createDraft, makePng, publicGet, publicPost, publicPut, send, setUp, uploadImage, until,
} = require('../helpers/signing');
const engine = require('../../server/services/signing/pdf_engine');
const { runPass } = require('../../server/services/signing/worker');
const { finalizeDocument } = require('../../server/services/signing/finalize');

test.after(cleanup);

const documentUrl = (draft) => `/api/signing/documents/${draft.document.id}`;

async function waitForStatus(agent, draft, status) {
  return until(async () => {
    const res = await agent.get(documentUrl(draft)).expect(200);
    return res.body.document.status === status ? res.body : null;
  });
}

// A text field and a signature field for each named party.
async function twoPartyDocument(agent, signingOrder) {
  const draft = await createDraft(agent);
  const { fields } = await setUp(agent, draft.document.id, {
    parties: [{ name: 'Landlord' }, { name: 'Tenant' }],
    fields: [
      { party: 0, type: 'text', name: 'Landlord name', areas: [area(0.1, 0.1)] },
      { party: 0, type: 'signature', name: 'Landlord signature', areas: [area(0.1, 0.5)] },
      { party: 1, type: 'text', name: 'Tenant name', areas: [area(0.1, 0.2)] },
      { party: 1, type: 'signature', name: 'Tenant signature', areas: [area(0.1, 0.6)] },
    ],
  });
  await agent.put(documentUrl(draft)).send({ signingOrder }).expect(200);
  return { draft, fields, ...(await send(agent, draft.document.id)) };
}

async function signAs(token, fields, name, png) {
  await consent(token, name);
  await publicPut(token, `/fields/${fields[0].id}`, { value: name }).expect(200);
  await uploadImage(token, fields[1].id, png, 'method=type&text=Signed').expect(201);
  return publicPost(token, '/complete');
}

test('one party signs and the final file appears next to the original', async () => {
  const { agent } = await registerUser('signer');
  const folder = await agent.post('/api/folders').send({ parentId: 'root', name: 'Contracts' }).expect(201);
  const draft = await createDraft(agent, { name: 'lease.pdf', parentId: folder.body.id });
  const { fields } = await setUp(agent, draft.document.id, {
    parties: [{ name: 'Tenant' }],
    fields: [
      { type: 'text', name: 'Full name', areas: [area()] },
      { type: 'signature', name: 'Signature', areas: [area(0.1, 0.8)] },
    ],
  });
  const sent = await send(agent, draft.document.id);
  const [token] = sent.tokens;

  const page = await publicGet(token).expect(200);
  assert.equal(page.body.state, 'ready');
  assert.equal(page.body.fields.length, 2);
  await consent(token);
  await publicPut(token, `/fields/${fields[0].id}`, { value: 'Alex Signer' }).expect(200);
  const missing = await publicPost(token, '/complete').expect(400);
  assert.equal(missing.body.code, 'FIELDS_INCOMPLETE');
  assert.deepEqual(missing.body.fieldIds, [fields[1].id]);
  await uploadImage(token, fields[1].id, await makePng()).expect(201);
  const done = await publicPost(token, '/complete').expect(200);
  assert.equal(done.body.state, 'completed');

  const finished = await waitForStatus(agent, draft, 'completed');
  assert.ok(finished.document.finalNodeId);
  assert.equal(finished.document.finalSha256.length, 64);
  const children = await agent.get(`/api/folders/${folder.body.id}/children`).expect(200);
  assert.deepEqual(children.body.items.map((item) => item.name).sort(), ['lease (signed).pdf', 'lease.pdf']);

  const finalFile = await agent.get(`${documentUrl(draft)}/final`).expect(200).buffer(true).parse(require('supertest').parse?.binary ?? ((res, cb) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  }));
  const pdf = await PDFDocument.load(finalFile.body);
  assert.ok(pdf.getPageCount() >= 3, 'the original pages plus the audit pages');

  const result = await publicGet(token, '/result').expect(200);
  assert.deepEqual(result.body, { state: 'completed', finalAvailable: true });
  await publicGet(token, '/final').expect(200);

  const events = await agent.get(`${documentUrl(draft)}/events`).expect(200);
  assert.equal(events.body.verify.chainValid, true);
  const types = events.body.events.map((event) => event.type);
  for (const expected of ['created', 'sent', 'opened', 'consented', 'party_completed', 'completed', 'finalized', 'final_downloaded']) {
    assert.ok(types.includes(expected), `${expected} was recorded`);
  }
  assert.ok(!types.includes('field_saved'));
});

test('sequential order: the second party waits until the first has signed', async () => {
  const { agent } = await registerUser('sequential');
  const png = await makePng();
  const { draft, fields, tokens } = await twoPartyDocument(agent, 'preserved');
  const [first, second] = tokens;

  const waiting = await publicGet(second).expect(200);
  assert.equal(waiting.body.state, 'waiting');
  assert.deepEqual(waiting.body.turn, { canSign: false, waitingCount: 1 });
  assert.deepEqual(waiting.body.progress, { done: 0, total: 2 });
  assert.equal(waiting.body.parties, undefined);
  assert.deepEqual(waiting.body.fields, []);
  await publicGet(second, '/pdf').expect(409);
  await publicPost(second, '/consent', { accepted: true, signerName: 'Early Bird' }).expect(409);
  await publicPut(second, `/fields/${fields[2].id}`, { value: 'x' }).expect(409);
  // Being "waiting" is not "opened".
  assert.equal((await agent.get(documentUrl(draft)).expect(200)).body.parties[1].openedAt, null);

  await signAs(first, [fields[0], fields[1]], 'Lena Landlord', png).then((res) => assert.equal(res.status, 200));
  const turn = await publicGet(second).expect(200);
  assert.equal(turn.body.state, 'ready');
  // The first party's signed fields are shown as read-only render info.
  const others = turn.body.fields.filter((field) => !field.own);
  assert.equal(others.length, 2);
  assert.equal(others.find((field) => field.type === 'text').value, 'Lena Landlord');
  const image = await publicGet(second, `/values/${fields[1].id}/image`).expect(200);
  assert.equal(image.headers['content-type'], 'image/png');

  await signAs(second, [fields[2], fields[3]], 'Tom Tenant', png).then((res) => assert.equal(res.status, 200));
  const finished = await waitForStatus(agent, draft, 'completed');
  assert.deepEqual(finished.parties.map((party) => party.status), ['completed', 'completed']);
  assert.equal(finished.parties[0].signerName, 'Lena Landlord');
});

test('random order: both parties can sign at once and the last one finishes the document', async () => {
  const { agent } = await registerUser('random');
  const png = await makePng();
  const { draft, fields, tokens } = await twoPartyDocument(agent, 'random');
  assert.equal((await publicGet(tokens[0]).expect(200)).body.state, 'ready');
  assert.equal((await publicGet(tokens[1]).expect(200)).body.state, 'ready');
  await signAs(tokens[1], [fields[2], fields[3]], 'Tom Tenant', png);
  assert.equal((await agent.get(documentUrl(draft)).expect(200)).body.document.status, 'in_progress');
  await signAs(tokens[0], [fields[0], fields[1]], 'Lena Landlord', png);
  await waitForStatus(agent, draft, 'completed');
});

test('declining ends the document for everyone', async () => {
  const { agent } = await registerUser('decliner');
  const { draft, tokens } = await twoPartyDocument(agent, 'random');
  await publicPost(tokens[0], '/decline', { reason: 'x'.repeat(501) }).expect(400);
  const declined = await publicPost(tokens[0], '/decline', { reason: 'Wrong amount' }).expect(200);
  assert.equal(declined.body.state, 'declined');
  const body = (await agent.get(documentUrl(draft)).expect(200)).body;
  assert.equal(body.document.status, 'declined');
  assert.equal(body.parties[0].declineReason, 'Wrong amount');
  const other = await publicGet(tokens[1]).expect(200);
  assert.equal(other.body.state, 'declined');
  assert.equal(other.body.document.title, undefined, 'a closed link does not show the title');
  await publicPost(tokens[1], '/consent', { accepted: true, signerName: 'Late' }).expect(409);
  await agent.delete(documentUrl(draft)).expect(200);
});

test('canceling closes every link', async () => {
  const { agent } = await registerUser('canceler');
  const { draft, tokens } = await twoPartyDocument(agent, 'preserved');
  await publicGet(tokens[0]).expect(200);
  await agent.post(`${documentUrl(draft)}/cancel`).expect(200);
  for (const token of tokens) assert.equal((await publicGet(token).expect(200)).body.state, 'canceled');
  await agent.post(`${documentUrl(draft)}/cancel`).expect(409);
  await agent.delete(documentUrl(draft)).expect(200);
});

test('a link can be replaced or turned off, and only the newest works', async () => {
  const { agent } = await registerUser('relinker');
  const { draft, parties, tokens } = await (async () => {
    const result = await twoPartyDocument(agent, 'random');
    return { ...result, parties: (await agent.get(documentUrl(result.draft)).expect(200)).body.parties };
  })();
  const fresh = await agent.post(`${documentUrl(draft)}/parties/${parties[0].id}/link`).expect(200);
  const newToken = fresh.body.url.split('/').pop();
  assert.notEqual(newToken, tokens[0]);
  await publicGet(tokens[0]).expect(404);
  assert.equal((await publicGet(newToken).expect(200)).body.state, 'ready');

  const revoked = await agent.post(`${documentUrl(draft)}/parties/${parties[0].id}/revoke`).expect(200);
  assert.equal(revoked.body.party.hasLink, false);
  await publicGet(newToken).expect(404);
  const body = (await agent.get(documentUrl(draft)).expect(200)).body;
  assert.equal(body.parties[0].hasLink, false);
  assert.equal(body.parties[1].hasLink, true);
  const types = (await agent.get(`${documentUrl(draft)}/events`).expect(200)).body.events.map((event) => event.type);
  assert.ok(types.includes('link_regenerated') && types.includes('revoked'));
});

test('a document past its expiry answers expired, also before the sweep ran', async () => {
  const { agent } = await registerUser('expirer');
  const { draft, tokens } = await twoPartyDocument(agent, 'preserved');
  db.prepare('UPDATE sign_documents SET expires_at = ? WHERE id = ?').run(new Date(Date.now() - 1000).toISOString(), draft.document.id);
  const page = await publicGet(tokens[0]).expect(200);
  assert.equal(page.body.state, 'expired');
  assert.equal((await agent.get(documentUrl(draft)).expect(200)).body.document.status, 'expired');

  // The periodic pass finds the ones nobody opened.
  const other = await twoPartyDocument(agent, 'preserved');
  db.prepare('UPDATE sign_documents SET expires_at = ? WHERE id = ?').run(new Date(Date.now() - 1000).toISOString(), other.draft.document.id);
  await runPass();
  assert.equal((await agent.get(documentUrl(other.draft)).expect(200)).body.document.status, 'expired');
  const types = (await agent.get(`${documentUrl(other.draft)}/events`).expect(200)).body.events.map((event) => event.type);
  assert.ok(types.includes('expired'));
});

test('a failed finalization is retried and never makes a second file', async (t) => {
  const { agent } = await registerUser('retrier');
  const png = await makePng();
  const { draft, fields, tokens } = await twoPartyDocument(agent, 'random');
  await signAs(tokens[0], [fields[0], fields[1]], 'Lena', png);

  const broken = t.mock.method(engine, 'buildFinalPdf', () => { throw new Error('engine exploded at /Users/secret/path'); });
  await signAs(tokens[1], [fields[2], fields[3]], 'Tom', png);
  const stuck = await until(async () => {
    const body = (await agent.get(documentUrl(draft)).expect(200)).body;
    return body.document.finalizeError ? body : null;
  });
  assert.equal(stuck.document.status, 'finalizing');
  assert.ok(!stuck.document.finalizeError.includes('/Users'), 'internals are not shown');
  assert.equal(stuck.document.finalNodeId, null);

  broken.mock.restore();
  // Not due yet: the pause after a failure holds.
  await runPass();
  assert.equal((await agent.get(documentUrl(draft)).expect(200)).body.document.status, 'finalizing');
  await agent.post(`${documentUrl(draft)}/finalize`).expect(200);
  const done = await waitForStatus(agent, draft, 'completed');

  // Finalizing again changes nothing.
  await Promise.all([finalizeDocument(draft.document.id), finalizeDocument(draft.document.id)]);
  const after = (await agent.get(documentUrl(draft)).expect(200)).body;
  assert.equal(after.document.finalNodeId, done.document.finalNodeId);
  const finals = db.prepare(`SELECT COUNT(*) AS n FROM nodes WHERE name LIKE '% (signed)%' AND user_id = ?`).get(done.document.finalNodeId ? db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(done.document.finalNodeId).user_id : 0).n;
  assert.equal(finals, 1);
  await agent.post(`${documentUrl(draft)}/finalize`).expect(409);
});

test('the final file counts against the quota; with no room it waits and succeeds once there is', async () => {
  const { agent, user } = await registerUser('quota');
  const png = await makePng();
  const { draft, fields, tokens } = await twoPartyDocument(agent, 'random');
  const used = db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(user.id).used_bytes;
  db.prepare('UPDATE users SET quota_bytes = ? WHERE id = ?').run(used + 10 * 1024, user.id);
  await signAs(tokens[0], [fields[0], fields[1]], 'Lena', png);
  await signAs(tokens[1], [fields[2], fields[3]], 'Tom', png);
  const stuck = await until(async () => {
    const body = (await agent.get(documentUrl(draft)).expect(200)).body;
    return body.document.finalizeError ? body : null;
  });
  assert.match(stuck.document.finalizeError, /storage/i);
  db.prepare('UPDATE users SET quota_bytes = NULL WHERE id = ?').run(user.id);
  await agent.post(`${documentUrl(draft)}/finalize`).expect(200);
  await waitForStatus(agent, draft, 'completed');
  const owner = db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(user.id).used_bytes;
  assert.ok(owner > used);
});

test('the document is frozen at creation: editing or deleting the source changes nothing', async () => {
  const { agent } = await registerUser('freezer');
  const draft = await createDraft(agent, { name: 'frozen.pdf' });
  await upload(agent, 'frozen.pdf', 'now something else entirely', { conflict: 'replace' }).expect(201);
  const frozen = await agent.get(`${documentUrl(draft)}/pdf`).expect(200).buffer(true).parse((res, cb) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  });
  assert.equal(require('crypto').createHash('sha256').update(frozen.body).digest('hex'), draft.document.snapshotSha256);

  const { fields } = await setUp(agent, draft.document.id, { parties: [{ name: 'A' }], fields: [{ type: 'text', areas: [area()] }] });
  await agent.delete(`/api/nodes/${draft.nodeId}`).expect(200);
  await agent.delete(`/api/trash/${draft.nodeId}`).catch(() => {});
  const { tokens } = await send(agent, draft.document.id);
  await consent(tokens[0]);
  await publicPut(tokens[0], `/fields/${fields[0].id}`, { value: 'ok' }).expect(200);
  await publicPost(tokens[0], '/complete').expect(200);
  const done = await waitForStatus(agent, draft, 'completed');
  const root = await agent.get('/api/folders/root/children').expect(200);
  assert.ok(root.body.items.some((item) => item.id === done.document.finalNodeId), 'with the source gone, the file lands in the root');
});

test('documents list newest first with parties, and page by cursor', async () => {
  const { agent } = await registerUser('lister');
  const ids = [];
  for (let index = 0; index < 3; index += 1) ids.push((await createDraft(agent, { name: `doc${index}.pdf` })).document.id);
  const first = await agent.get('/api/signing/documents?limit=2').expect(200);
  assert.equal(first.body.items.length, 2);
  assert.ok(first.body.nextCursor);
  assert.deepEqual(first.body.items.map((item) => item.id), [ids[2], ids[1]]);
  assert.equal(first.body.items[0].parties.length, 1);
  const second = await agent.get(`/api/signing/documents?limit=2&cursor=${first.body.nextCursor}`).expect(200);
  assert.deepEqual(second.body.items.map((item) => item.id), [ids[0]]);
  assert.equal(second.body.nextCursor, null);
  assert.equal((await agent.get('/api/signing/documents?status=sent').expect(200)).body.items.length, 0);
  await agent.get('/api/signing/documents?status=bogus').expect(400);
  await agent.get('/api/signing/documents?cursor=not-a-cursor').expect(400);
});

test('duplicate copies pages, parties and fields into a new draft; delete frees what it held', async () => {
  const { agent } = await registerUser('duplicator');
  const draft = await createDraft(agent, { name: 'dup.pdf' });
  await setUp(agent, draft.document.id, { parties: [{ name: 'A' }, { name: 'B' }], fields: [{ party: 1, type: 'date', areas: [area()] }] });
  const copy = await agent.post(`${documentUrl(draft)}/duplicate`).expect(201);
  assert.notEqual(copy.body.document.id, draft.document.id);
  assert.equal(copy.body.document.status, 'draft');
  assert.deepEqual(copy.body.parties.map((party) => party.name), ['A', 'B']);
  assert.equal(copy.body.fields[0].partyId, copy.body.parties[1].id);
  const blobId = db.prepare('SELECT snapshot_blob_id AS id FROM sign_documents WHERE id = ?').get(draft.document.id).id;
  assert.equal(db.prepare('SELECT ref_count FROM blobs WHERE id = ?').get(blobId).ref_count, 3, 'file, document and copy');
  await agent.delete(`/api/signing/documents/${copy.body.document.id}`).expect(200);
  await agent.delete(documentUrl(draft)).expect(200);
  assert.equal(db.prepare('SELECT ref_count FROM blobs WHERE id = ?').get(blobId).ref_count, 1);
  await agent.get(documentUrl(draft)).expect(404);
});

test('sending needs a party with something to fill in, and only happens once', async () => {
  const { agent } = await registerUser('sender');
  const draft = await createDraft(agent);
  const empty = await agent.post(`${documentUrl(draft)}/send`).send({}).expect(400);
  assert.equal(empty.body.code, 'PARTY_WITHOUT_FIELDS');
  await setUp(agent, draft.document.id, { parties: [{ name: 'A' }], fields: [{ type: 'heading', defaultValue: 'Title', areas: [area()] }] });
  await agent.post(`${documentUrl(draft)}/send`).send({}).expect(400);
  await setUp(agent, draft.document.id, { parties: [{ name: 'A' }], fields: [{ type: 'checkbox', areas: [area()] }] });
  await agent.post(`${documentUrl(draft)}/send`).send({}).expect(200);
  const again = await agent.post(`${documentUrl(draft)}/send`).send({}).expect(409);
  assert.equal(again.body.code, 'ALREADY_SENT');
  await agent.delete(documentUrl(draft)).expect(409);
});

test('fields and parties lock once a link was opened; before that fields can still change', async () => {
  const { agent } = await registerUser('locker');
  const draft = await createDraft(agent);
  const { fields } = await setUp(agent, draft.document.id, { parties: [{ name: 'A' }], fields: [{ type: 'checkbox', areas: [area()] }] });
  const { tokens, parties } = await send(agent, draft.document.id).then(async (sent) => ({
    ...sent, parties: (await agent.get(documentUrl(draft)).expect(200)).body.parties,
  }));
  const edited = await agent.put(`${documentUrl(draft)}/fields`)
    .send({ fields: [{ ...fields[0], name: 'Agree', partyId: parties[0].id }] }).expect(200);
  assert.equal(edited.body.fields[0].name, 'Agree');
  await agent.put(`${documentUrl(draft)}/parties`).send({ parties: [{ id: parties[0].id, name: 'Renamed' }] }).expect(409);
  await publicGet(tokens[0]).expect(200);
  const locked = await agent.put(`${documentUrl(draft)}/fields`).send({ fields: [] }).expect(409);
  assert.equal(locked.body.code, 'DOCUMENT_LOCKED');
  await agent.put(documentUrl(draft)).send({ title: 'New title' }).expect(409);
});

test('the audit trail detects a changed or removed entry', async () => {
  const { agent } = await registerUser('auditor');
  const draft = await createDraft(agent);
  await setUp(agent, draft.document.id, { parties: [{ name: 'A' }], fields: [{ type: 'checkbox', areas: [area()] }] });
  const { tokens } = await send(agent, draft.document.id);
  await publicGet(tokens[0]).expect(200);
  await consent(tokens[0]);
  const check = async () => (await agent.get(`${documentUrl(draft)}/events`).expect(200)).body.verify;
  const intact = await check();
  assert.equal(intact.chainValid, true);
  assert.equal(intact.count, 4);

  const rows = db.prepare('SELECT id, ip FROM sign_events WHERE document_id = ? ORDER BY id').all(draft.document.id);
  db.prepare('UPDATE sign_events SET ip = ? WHERE id = ?').run('203.0.113.9', rows[2].id);
  const tampered = await check();
  assert.equal(tampered.chainValid, false);
  assert.equal(tampered.brokenAt, rows[2].id);
  db.prepare('UPDATE sign_events SET ip = ? WHERE id = ?').run(rows[2].ip, rows[2].id);
  assert.equal((await check()).chainValid, true);

  db.prepare('DELETE FROM sign_events WHERE id = ?').run(rows[1].id);
  assert.equal((await check()).chainValid, false);
  const record = await agent.get(`${documentUrl(draft)}/audit.json`).expect(200);
  assert.equal(record.body.kind, 'neocloud-signing-audit');
  assert.equal(record.body.verify.chainValid, false);
});

test('files that cannot be signed are refused with a reason', async () => {
  const { agent, user } = await registerUser('refuser');
  const text = await upload(agent, 'note.txt', 'hello').expect(201);
  assert.equal((await agent.post('/api/signing/documents').send({ nodeId: text.body.id }).expect(415)).body.code, 'UNSUPPORTED_TYPE');

  const folder = await agent.post('/api/folders').send({ parentId: 'root', name: 'F' }).expect(201);
  assert.equal((await agent.post('/api/signing/documents').send({ nodeId: folder.body.id }).expect(400)).body.code, 'NOT_A_FILE');
  await agent.post('/api/signing/documents').send({}).expect(400);

  const { makePdf } = require('../helpers/signing');
  const pdf = await upload(agent, 'a.pdf', await makePdf(1)).expect(201);
  db.prepare('UPDATE nodes SET vault_id = ? WHERE id = ?').run('some-vault', pdf.body.id);
  assert.equal((await agent.post('/api/signing/documents').send({ nodeId: pdf.body.id }).expect(400)).body.code, 'VAULT_FILE');

  const trashed = await upload(agent, 'b.pdf', await makePdf(1)).expect(201);
  await agent.delete(`/api/nodes/${trashed.body.id}`).expect(200);
  await agent.post('/api/signing/documents').send({ nodeId: trashed.body.id }).expect(404);

  const huge = await upload(agent, 'c.pdf', await makePdf(1)).expect(201);
  db.prepare('UPDATE nodes SET size = ? WHERE id = ?').run(60 * 1024 * 1024, huge.body.id);
  assert.equal((await agent.post('/api/signing/documents').send({ nodeId: huge.body.id }).expect(413)).body.code, 'DOCUMENT_TOO_LARGE');

  const many = await PDFDocument.create();
  for (let index = 0; index < 301; index += 1) many.addPage([200, 200]);
  const long = await upload(agent, 'long.pdf', Buffer.from(await many.save())).expect(201);
  assert.equal((await agent.post('/api/signing/documents').send({ nodeId: long.body.id }).expect(400)).body.code, 'TOO_MANY_PAGES');

  const classic = await PDFDocument.create();
  classic.addPage([200, 200]);
  const classicBytes = Buffer.from(await classic.save({ useObjectStreams: false })).toString('latin1');
  const encrypted = Buffer.from(classicBytes.replace(/trailer\s*<<\s*/, 'trailer\n<< /Encrypt 1 0 R '), 'latin1');
  const locked = await upload(agent, 'locked.pdf', encrypted).expect(201);
  assert.equal((await agent.post('/api/signing/documents').send({ nodeId: locked.body.id }).expect(400)).body.code, 'PDF_ENCRYPTED');

  const png = await upload(agent, 'scan.png', await makePng(600, 800)).expect(201);
  const fromImage = await agent.post('/api/signing/documents').send({ nodeId: png.body.id }).expect(201);
  assert.equal(fromImage.body.document.pageCount, 1);
  assert.equal(fromImage.body.document.title, 'scan');
  const before = db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(user.id).used_bytes;
  db.prepare('UPDATE users SET quota_bytes = ? WHERE id = ?').run(before, user.id);
  const another = await upload(agent, 'tiny.png', await makePng(8, 8)).catch(() => null);
  assert.ok(another === null || another.status >= 400, 'no room for even a tiny file');
  const again = await agent.post('/api/signing/documents').send({ nodeId: png.body.id });
  assert.equal(again.status, 413);
  assert.equal(again.body.code, 'QUOTA_EXCEEDED');

  const capabilities = await agent.get('/api/signing/capabilities').expect(200);
  assert.equal(capabilities.body.formats.pdf, true);
  assert.equal(typeof capabilities.body.formats.office, 'boolean');
});

test('deleting the account removes its documents, values and blobs', async () => {
  const { agent, user, username } = await registerUser('goner');
  const png = await makePng();
  const { draft, fields, tokens } = await twoPartyDocument(agent, 'random');
  await signAs(tokens[0], [fields[0], fields[1]], 'Lena', png);
  assert.ok(db.prepare('SELECT COUNT(*) AS n FROM sign_values WHERE document_id = ?').get(draft.document.id).n >= 2);
  const exported = await agent.post('/api/account/export').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  assert.equal(exported.body.signing.length, 1);
  assert.ok(!JSON.stringify(exported.body.signing).includes(tokens[0]));
  await agent.post('/api/account/delete').send({ currentPassword: STRONG_PASSWORD, confirmUsername: username }).expect(200);
  for (const table of ['sign_documents', 'sign_parties', 'sign_fields', 'sign_values', 'sign_events']) {
    const column = table === 'sign_documents' ? 'owner_id' : null;
    const count = column
      ? db.prepare(`SELECT COUNT(*) AS n FROM ${table} WHERE ${column} = ?`).get(user.id).n
      : db.prepare(`SELECT COUNT(*) AS n FROM ${table} WHERE document_id = ?`).get(draft.document.id).n;
    assert.equal(count, 0, `${table} is empty`);
  }
  await publicGet(tokens[1]).expect(404);
});
