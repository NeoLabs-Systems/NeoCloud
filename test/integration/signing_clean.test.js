'use strict';

// The signed file without certificate pages: both variants through the owner,
// signer and library routes, the cached blob, and its clean-up.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { cleanup, registerUser } = require('../helpers/app');
const db = require('../../server/db/database');
const { BLOB_DIR } = require('../../runtime/paths');
const inspect = require('../helpers/signing_pdf_inspect');
const {
  area, consent, createDraft, makePdf, makePng, publicGet, publicPost, publicPut, send, setUp, until, uploadImage,
} = require('../helpers/signing');
const engine = require('../../server/services/signing/pdf');
const { eraseAccount } = require('../../server/services/account/erasure');

test.after(cleanup);

const documentUrl = (draft) => `/api/signing/documents/${draft.document.id}`;

const binary = (res, done) => {
  const chunks = [];
  res.on('data', (chunk) => chunks.push(chunk));
  res.on('end', () => done(null, Buffer.concat(chunks)));
};

const download = (req) => req.buffer(true).parse(binary);

const cleanRow = (draft) => db.prepare('SELECT clean_blob_id, clean_sha256, clean_size FROM sign_documents WHERE id = ?').get(draft.document.id);
const blobFiles = (userId) => {
  const dir = path.join(BLOB_DIR, String(userId));
  return fs.existsSync(dir) ? fs.readdirSync(dir, { recursive: true }).filter((name) => !fs.statSync(path.join(dir, name)).isDirectory()).length : 0;
};
const usedBytes = (userId) => db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(userId).used_bytes;

// One signer, two source pages; resolves once the signed file exists.
async function signedDocument(owner, { locale } = {}) {
  const { agent } = owner;
  const draft = await createDraft(agent, { name: 'lease.pdf', pdf: await makePdf(2) });
  const { fields } = await setUp(agent, draft.document.id, {
    parties: [{ name: 'Tenant' }],
    fields: [
      { type: 'text', name: 'Full name', areas: [area()] },
      { type: 'signature', name: 'Signature', areas: [area(0.1, 0.8)] },
    ],
  });
  if (locale) await agent.put(documentUrl(draft)).send({ locale }).expect(200);
  const { tokens } = await send(agent, draft.document.id);
  const [token] = tokens;
  assert.equal((await publicGet(token, '/final?certificate=0')).status, 404, 'not before completion');
  await agent.get(`${documentUrl(draft)}/final?certificate=0`).expect(404);
  await consent(token, 'Tom Tenant');
  await publicPut(token, `/fields/${fields[0].id}`, { value: 'Visible Tenant' }).expect(200);
  await uploadImage(token, fields[1].id, await makePng(), 'method=type&text=Signed').expect(201);
  await publicPost(token, '/complete').expect(200);
  const done = await until(async () => {
    const body = (await agent.get(documentUrl(draft)).expect(200)).body;
    return body.document.status === 'completed' ? body : null;
  });
  return { draft, token, finalNodeId: done.document.finalNodeId };
}

test('both variants download: same content, clean has no certificate pages, both sealed', async () => {
  const owner = await registerUser('clean');
  const { draft, token, finalNodeId } = await signedDocument(owner);
  const { agent } = owner;

  const full = await download(agent.get(`${documentUrl(draft)}/final`)).expect(200);
  const defaulted = await download(agent.get(`${documentUrl(draft)}/final?certificate=1`)).expect(200);
  assert.ok(full.body.equals(defaulted.body));
  assert.match(full.headers['content-disposition'], /lease \(signed\)\.pdf/);

  const clean = await download(agent.get(`${documentUrl(draft)}/final?certificate=0`)).expect(200);
  assert.equal(clean.headers['content-type'], 'application/pdf');
  assert.match(clean.headers['content-disposition'], /^attachment;/);
  assert.match(decodeURIComponent(clean.headers['content-disposition']), /lease \(signed, no certificate\)\.pdf/);
  assert.equal(clean.headers['accept-ranges'], 'bytes');
  assert.ok(!clean.body.equals(full.body));

  assert.equal((await engine.probePdf(clean.body)).pageCount, 2, 'the original page count');
  assert.ok((await engine.probePdf(full.body)).pageCount >= 3, 'original pages plus the certificate');
  assert.deepEqual(await engine.verifySignedPdf(clean.body), { signed: true, intact: true });
  assert.deepEqual(await engine.verifySignedPdf(full.body), { signed: true, intact: true });
  const texts = await inspect.pageTexts(clean.body);
  assert.match(texts[0], /Visible Tenant/);
  assert.ok(texts.every((text) => !/Signing certificate/.test(text)));
  assert.equal(clean.headers.etag, `"${cleanRow(draft).clean_sha256}"`);
  assert.notEqual(cleanRow(draft).clean_sha256, (await agent.get(documentUrl(draft)).expect(200)).body.document.finalSha256);

  // The signer's link and the library file give the same bytes.
  const viaLink = await download(publicGet(token, '/final?certificate=0')).expect(200);
  assert.ok(viaLink.body.equals(clean.body));
  assert.match(decodeURIComponent(viaLink.headers['content-disposition']), /\(signed, no certificate\)\.pdf/);
  const viaNode = await download(agent.get(`/api/signing/nodes/${finalNodeId}/clean`)).expect(200);
  assert.ok(viaNode.body.equals(clean.body));
  const fullViaLink = await download(publicGet(token, '/final?certificate=1')).expect(200);
  assert.ok(fullViaLink.body.equals(full.body));

  // Range, HEAD and bad input.
  const ranged = await download(agent.get(`${documentUrl(draft)}/final?certificate=0`).set('Range', 'bytes=0-9')).expect(206);
  assert.equal(ranged.body.length, 10);
  assert.ok(ranged.body.equals(clean.body.subarray(0, 10)));
  assert.match(ranged.headers['content-range'], new RegExp(`^bytes 0-9/${clean.body.length}$`));
  await agent.get(`${documentUrl(draft)}/final?certificate=0`).set('Range', 'bytes=999999999-').expect(416);
  const head = await agent.head(`${documentUrl(draft)}/final?certificate=0`).expect(200);
  assert.equal(head.headers['content-length'], String(clean.body.length));
  assert.equal((await agent.get(`${documentUrl(draft)}/final?certificate=2`)).body.code, 'INVALID_REQUEST');
  assert.equal((await publicGet(token, '/final?certificate=yes')).status, 400);

  // The library shows where the signed file belongs to.
  const listing = (await agent.get('/api/folders/root/children').expect(200)).body.items;
  assert.equal(listing.find((node) => node.id === finalNodeId).signedDocumentId, draft.document.id);

  // The trail has one entry per variant and person.
  const events = (await agent.get(`${documentUrl(draft)}/events`).expect(200)).body;
  const downloads = events.events.filter((event) => event.type === 'final_downloaded');
  assert.ok(downloads.some((event) => event.data?.certificate === false && event.data?.by === 'owner'));
  assert.ok(downloads.some((event) => event.data?.certificate === false && event.data?.by === 'signer'));
  assert.ok(downloads.some((event) => event.data?.certificate === undefined));
  assert.equal(events.verify.chainValid, true);
});

test('German documents name the clean file in German', async () => {
  const owner = await registerUser('cleande');
  const { draft } = await signedDocument(owner, { locale: 'de' });
  const clean = await download(owner.agent.get(`${documentUrl(draft)}/final?certificate=0`)).expect(200);
  assert.match(decodeURIComponent(clean.headers['content-disposition']), /lease \(signiert, ohne Protokoll\)\.pdf/);
});

test('the clean variant is built once, even for simultaneous first requests, and is reused', async () => {
  const owner = await registerUser('cleanonce');
  const { draft, token } = await signedDocument(owner);
  const before = { files: blobFiles(owner.user.id), used: usedBytes(owner.user.id) };
  assert.equal(cleanRow(draft).clean_blob_id, null);

  const answers = await Promise.all(Array.from({ length: 6 }, (_, index) => download(
    index % 2 ? owner.agent.get(`${documentUrl(draft)}/final?certificate=0`) : publicGet(token, '/final?certificate=0'),
  ).expect(200)));
  assert.ok(answers.every((res) => res.body.equals(answers[0].body)));
  const row = cleanRow(draft);
  assert.ok(row.clean_blob_id);
  assert.equal(blobFiles(owner.user.id), before.files + 1, 'one blob file, not six');
  assert.equal(usedBytes(owner.user.id), before.used + row.clean_size, 'counted against the quota once');
  assert.equal(db.prepare('SELECT ref_count FROM blobs WHERE id = ?').get(row.clean_blob_id).ref_count, 1);

  await download(owner.agent.get(`${documentUrl(draft)}/final?certificate=0`)).expect(200);
  assert.equal(cleanRow(draft).clean_blob_id, row.clean_blob_id, 'reused');
  assert.equal(blobFiles(owner.user.id), before.files + 1);

  // Deleting the document releases the blob (file and quota); the signed file in the library stays.
  const built = { files: blobFiles(owner.user.id), used: usedBytes(owner.user.id) };
  await owner.agent.delete(documentUrl(draft)).expect(200);
  assert.equal(db.prepare('SELECT 1 FROM blobs WHERE id = ?').get(row.clean_blob_id), undefined);
  assert.ok(blobFiles(owner.user.id) < built.files);
  assert.ok(usedBytes(owner.user.id) <= built.used - row.clean_size);
});

test('only the owner reaches the clean variant, by document or by file', async () => {
  const owner = await registerUser('cleanowner');
  const other = await registerUser('cleanother');
  const { draft, finalNodeId } = await signedDocument(owner);
  await other.agent.get(`${documentUrl(draft)}/final?certificate=0`).expect(404);
  await other.agent.get(`/api/signing/nodes/${finalNodeId}/clean`).expect(404);
  assert.equal(cleanRow(draft).clean_blob_id, null, 'nothing was built for the stranger');

  // A file that is not a signing result, and a malformed id.
  await owner.agent.get(`/api/signing/nodes/${draft.nodeId}/clean`).expect(404);
  await owner.agent.get('/api/signing/nodes/not-a-node/clean').expect(404);

  // A signed file that was trashed is no longer there to download.
  await owner.agent.delete(`/api/nodes/${finalNodeId}`).expect(200);
  await owner.agent.get(`/api/signing/nodes/${finalNodeId}/clean`).expect(404);
  assert.equal((await owner.agent.get(`${documentUrl(draft)}/final?certificate=0`)).body.code, 'FINAL_UNAVAILABLE');
});

test('deleting the account removes the cached clean blob with everything else', async () => {
  await registerUser('cleanadmin');
  const victim = await registerUser('cleanvictim');
  const { draft } = await signedDocument(victim);
  await download(victim.agent.get(`${documentUrl(draft)}/final?certificate=0`)).expect(200);
  const { clean_blob_id: blobId } = cleanRow(draft);
  assert.ok(blobId);
  await eraseAccount(victim.user.id, { actorUserId: victim.user.id });
  assert.equal(db.prepare('SELECT 1 FROM sign_documents WHERE id = ?').get(draft.document.id), undefined);
  assert.equal(db.prepare('SELECT 1 FROM blobs WHERE id = ?').get(blobId), undefined);
  assert.equal(blobFiles(victim.user.id), 0);
});

