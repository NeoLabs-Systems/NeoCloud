'use strict';

// Hostile input to the signing feature: PDF bombs, odd text, ids from another
// document of the same account, and what happens to the result file.

const test = require('node:test');
const assert = require('node:assert/strict');
const zlib = require('zlib');
const { PDFDocument } = require('pdf-lib');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const {
  area, consent, createDraft, makePng, publicGet, publicPost, publicPut, send, setUp, until, uploadImage,
} = require('../helpers/signing');

test.after(cleanup);

// A tiny file whose object stream inflates to `megabytes` of blanks.
function streamBombPdf(megabytes) {
  const header = '1 0 ';
  const inflated = Buffer.concat([Buffer.from(header), Buffer.alloc(megabytes * 1024 * 1024, 0x20)]);
  const packed = zlib.deflateSync(inflated, { level: 9 });
  const parts = [];
  let offset = 0;
  const push = (chunk) => {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk, 'latin1');
    parts.push(buffer);
    offset += buffer.length;
  };
  push('%PDF-1.5\n');
  const first = offset;
  push(`1 0 obj\n<< /Type /ObjStm /N 1 /First ${header.length} /Length ${packed.length} /Filter /FlateDecode >>\nstream\n`);
  push(packed);
  push('\nendstream\nendobj\n');
  const second = offset;
  push('2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n');
  const table = offset;
  push(`xref\n0 3\n0000000000 65535 f \n${String(first).padStart(10, '0')} 00000 n \n${String(second).padStart(10, '0')} 00000 n \n`);
  push(`trailer\n<< /Size 3 /Root 3 0 R >>\nstartxref\n${table}\n%%EOF\n`);
  return Buffer.concat(parts);
}

test('a small PDF that inflates to gigabytes is refused before it can use the memory', async () => {
  const { agent } = await registerUser('bomb');
  const bomb = streamBombPdf(300);
  assert.ok(bomb.length < 1024 * 1024, 'the file itself is small');
  const file = await upload(agent, 'bomb.pdf', bomb).expect(201);
  const before = process.memoryUsage().rss;
  const started = Date.now();
  const res = await agent.post('/api/signing/documents').send({ nodeId: file.body.id });
  assert.equal(res.status, 400);
  assert.equal(res.body.code, 'INVALID_PDF');
  assert.ok(Date.now() - started < 2000, 'refused quickly');
  assert.ok(process.memoryUsage().rss - before < 400 * 1024 * 1024, 'no huge allocation');
});

test('malformed and hostile PDFs get a clean refusal and the server keeps answering', async () => {
  const { agent } = await registerUser('mal');
  const nested = ['<< /Type /Catalog /Pages 2 0 R >>'];
  for (let index = 0; index < 20000; index += 1) nested.push(`<< /Type /Pages /Kids [${index + 3} 0 R] /Count 1 >>`);
  nested.push('<< /Type /Page /MediaBox [0 0 100 100] >>');
  let text = '%PDF-1.4\n';
  const offsets = nested.map((body, index) => {
    const at = text.length;
    text += `${index + 1} 0 obj\n${body}\nendobj\n`;
    return at;
  });
  text += `xref\n0 ${nested.length + 1}\n0000000000 65535 f \n${offsets.map((at) => `${String(at).padStart(10, '0')} 00000 n \n`).join('')}`;
  text += `trailer\n<< /Size ${nested.length + 1} /Root 1 0 R >>\nstartxref\n${text.length - 1}\n%%EOF\n`;
  const cases = {
    'deep.pdf': Buffer.from(text, 'latin1'),
    'cut.pdf': Buffer.from('%PDF-1.4\n1 0 obj\n<< /Type /Catalog'),
    'noise.pdf': Buffer.concat([Buffer.from('%PDF-1.7\n'), require('crypto').randomBytes(4000)]),
  };
  for (const [name, bytes] of Object.entries(cases)) {
    const file = await upload(agent, name, bytes).expect(201);
    const res = await agent.post('/api/signing/documents').send({ nodeId: file.body.id });
    assert.equal(res.status, 400, name);
    assert.equal(res.body.code, 'INVALID_PDF', name);
  }
  await agent.get('/api/signing/capabilities').expect(200);
});

test('field preferences named like object internals are a 400, not a crash', async () => {
  const { agent } = await registerUser('proto');
  const draft = await createDraft(agent);
  const partyId = draft.parties[0].id;
  for (const prefs of [JSON.parse('{"__proto__":{"x":1}}'), { constructor: 1 }, { toString: 'x' }, { hasOwnProperty: true }]) {
    const res = await agent.put(`/api/signing/documents/${draft.document.id}/fields`)
      .send({ fields: [{ type: 'text', partyId, areas: [area()], prefs }] });
    assert.equal(res.status, 400, JSON.stringify(prefs));
    assert.equal(res.body.code, 'INVALID_FIELD');
  }
});

test('names cannot carry direction overrides or line breaks into lists and the certificate', async () => {
  const owner = await registerUser('names');
  const draft = await createDraft(owner.agent);
  const evil = `Al‮ice \nSigned: Eve​`;
  const { parties, fields } = await setUp(owner.agent, draft.document.id, {
    parties: [{ name: evil }],
    fields: [{ party: 0, type: 'text', name: `Label\r\n${'‮'}x`, areas: [area()], prefs: { multiline: true } }],
  });
  assert.equal(parties[0].name, 'Alice Signed: Eve');
  assert.equal(fields[0].name, 'Label x');
  const sent = await send(owner.agent, draft.document.id);
  const token = sent.tokens[0];
  const named = await publicPost(token, '/consent', { accepted: true, signerName: `Max‮\nMuster` }).expect(200);
  assert.equal(named.body.signerName, 'Max Muster');
  const saved = await publicPut(token, `/fields/${fields[0].id}`, { value: `one‮two\nthree` }).expect(200);
  assert.equal(saved.body.value, 'onetwo\nthree');
  const title = await owner.agent.put(`/api/signing/documents/${draft.document.id}`).send({ title: 'T\r\nX-Evil: 1' }).expect(200);
  assert.equal(title.body.document.title, 'T X-Evil: 1');
  const pdf = await owner.agent.get(`/api/signing/documents/${draft.document.id}/pdf`).expect(200);
  assert.ok(!/[\r\n]X-Evil/.test(pdf.headers['content-disposition']));
  const declined = await publicPost(token, '/decline', { reason: 'no‮thanks' }).expect(200);
  assert.equal(declined.body.state, 'declined');
  const reason = db.prepare('SELECT decline_reason FROM sign_parties WHERE document_id = ?').get(draft.document.id).decline_reason;
  assert.equal(reason, 'nothanks');
});

test('ids of another document of the same account reach nothing', async () => {
  const owner = await registerUser('twodocs');
  const first = await createDraft(owner.agent);
  const second = await createDraft(owner.agent);
  const a = await setUp(owner.agent, first.document.id, {
    parties: [{ name: 'A' }, { name: 'A2' }],
    fields: [{ party: 0, type: 'signature', areas: [area()] }, { party: 1, type: 'text', areas: [area(0.1, 0.4)] }],
  });
  const b = await setUp(owner.agent, second.document.id, { parties: [{ name: 'B' }], fields: [{ party: 0, type: 'text', areas: [area()] }] });
  const sentA = await send(owner.agent, first.document.id);
  const sentB = await send(owner.agent, second.document.id);
  const base = `/api/signing/documents/${second.document.id}`;

  assert.equal((await owner.agent.post(`${base}/parties/${a.parties[0].id}/link`)).status, 404);
  assert.equal((await owner.agent.post(`${base}/parties/${a.parties[0].id}/revoke`)).status, 404);
  assert.equal((await owner.agent.get(`${base}/values/${a.fields[0].id}/image`)).status, 404);

  // A public link reaches only the fields of its own party in its own document.
  await consent(sentB.tokens[0]);
  assert.equal((await publicPut(sentB.tokens[0], `/fields/${a.fields[1].id}`, { value: 'x' })).status, 404);
  assert.equal((await publicGet(sentB.tokens[0], `/values/${a.fields[0].id}/image`)).status, 404);
  await consent(sentA.tokens[0]);
  await uploadImage(sentA.tokens[0], a.fields[0].id, await makePng()).expect(201);
  assert.equal((await owner.agent.get(`${base}/values/${a.fields[0].id}/image`)).status, 404);
  assert.equal((await publicGet(sentB.tokens[0], `/values/${a.fields[0].id}/image`)).status, 404);

  // Another document's field id in a field list becomes a new field, never a takeover.
  const fresh = await createDraft(owner.agent);
  const party = fresh.parties[0].id;
  const saved = await owner.agent.put(`/api/signing/documents/${fresh.document.id}/fields`)
    .send({ fields: [{ id: a.fields[1].id, type: 'text', partyId: party, areas: [area()] }] }).expect(200);
  assert.notEqual(saved.body.fields[0].id, a.fields[1].id);
  assert.equal(db.prepare('SELECT document_id FROM sign_fields WHERE id = ?').get(a.fields[1].id).document_id, first.document.id);
  assert.equal(b.fields.length, 1);
});

test('the signed file is offered only while it is the one NeoCloud made, and downloads do not flood the trail', async () => {
  const owner = await registerUser('finalcheck');
  const draft = await createDraft(owner.agent);
  const base = `/api/signing/documents/${draft.document.id}`;
  const { fields } = await setUp(owner.agent, draft.document.id, { parties: [{ name: 'A' }], fields: [{ party: 0, type: 'text', areas: [area()] }] });
  const sent = await send(owner.agent, draft.document.id);
  await consent(sent.tokens[0]);
  await publicPut(sent.tokens[0], `/fields/${fields[0].id}`, { value: 'x' }).expect(200);
  await publicPost(sent.tokens[0], '/complete').expect(200);
  const finalNode = await until(async () => (await owner.agent.get(base).expect(200)).body.document.finalNodeId);

  assert.equal((await publicGet(sent.tokens[0], '/result').expect(200)).body.finalAvailable, true);
  for (let index = 0; index < 4; index += 1) {
    await publicGet(sent.tokens[0], '/final').expect(200);
    await owner.agent.get(`${base}/final`).expect(200);
  }
  const downloads = db.prepare("SELECT COUNT(*) AS n FROM sign_events WHERE document_id = ? AND type = 'final_downloaded'").get(draft.document.id).n;
  assert.equal(downloads, 2, 'one entry for the signer and one for the owner');

  const other = await upload(owner.agent, 'other.bin', Buffer.from('replacement')).expect(201);
  const replacement = db.prepare('SELECT etag, blob_id, size FROM nodes WHERE id = ?').get(other.body.id);
  db.prepare('UPDATE nodes SET etag = ?, blob_id = ?, size = ? WHERE id = ?').run(replacement.etag, replacement.blob_id, replacement.size, finalNode);
  assert.equal((await publicGet(sent.tokens[0], '/result').expect(200)).body.finalAvailable, false);
  assert.equal((await publicGet(sent.tokens[0], '/final')).status, 404);
  assert.equal((await owner.agent.get(`${base}/final`)).status, 404);
});

test('the PDF’s combed form fields are imported as cells fields', async () => {
  const owner = await registerUser('acro');
  const doc = await PDFDocument.create();
  const page = doc.addPage([595, 842]);
  const form = doc.getForm();
  const code = form.createTextField('code');
  code.setMaxLength(8);
  code.enableCombing();
  code.addToPage(page, { x: 50, y: 700, width: 240, height: 24 });
  const name = form.createTextField('name');
  name.addToPage(page, { x: 50, y: 600, width: 200, height: 24 });
  const draft = await createDraft(owner.agent, { pdf: Buffer.from(await doc.save()) });
  const imported = await owner.agent.post(`/api/signing/documents/${draft.document.id}/import-fields`).send({}).expect(200);
  assert.equal(imported.body.imported, 2);
  const cells = imported.body.fields.find((field) => field.type === 'cells');
  assert.equal(cells.prefs.cells, 8);
});

test('a replaced link starts over at consent: the name typed through the old one does not carry over', async () => {
  const owner = await registerUser('relink');
  const draft = await createDraft(owner.agent);
  const { parties, fields } = await setUp(owner.agent, draft.document.id, {
    parties: [{ name: 'A' }],
    fields: [{ party: 0, type: 'text', areas: [area()] }],
  });
  const sent = await send(owner.agent, draft.document.id);
  await consent(sent.tokens[0], 'Mallory');
  await publicPut(sent.tokens[0], `/fields/${fields[0].id}`, { value: 'typed early' }).expect(200);
  const fresh = await owner.agent.post(`/api/signing/documents/${draft.document.id}/parties/${parties[0].id}/link`).expect(200);
  const token = fresh.body.url.split('/').pop();
  const view = (await publicGet(token).expect(200)).body;
  assert.equal(view.party.consentedAt, null);
  assert.equal(view.party.signerName, null);
  assert.equal(view.myValues[fields[0].id], 'typed early', 'what was filled in stays');
  assert.equal((await publicPut(token, `/fields/${fields[0].id}`, { value: 'x' })).status, 409);
  assert.equal((await publicPost(token, '/complete')).status, 409);
  await consent(token, 'Alice Real');
  await publicPost(token, '/complete').expect(200);
  const record = db.prepare('SELECT signer_name FROM sign_parties WHERE id = ?').get(parties[0].id);
  assert.equal(record.signer_name, 'Alice Real');
});
