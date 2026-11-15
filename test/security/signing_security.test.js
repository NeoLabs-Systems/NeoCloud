'use strict';

// The public signing links and the owner routes under hostile use.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const sharp = require('sharp');
const { app, cleanup, registerUser, request } = require('../helpers/app');
const db = require('../../server/db/database');
const { redactSecrets } = require('../../server/utils/logger');
const {
  area, consent, createDraft, makePng, publicGet, publicPost, publicPut, send, setUp, sign, until, uploadImage,
} = require('../helpers/signing');

test.after(cleanup);

async function ready() {
  const owner = await registerUser('sec');
  const draft = await createDraft(owner.agent);
  const { fields } = await setUp(owner.agent, draft.document.id, {
    parties: [{ name: 'A' }, { name: 'B' }],
    fields: [
      { party: 0, type: 'signature', areas: [area()] },
      { party: 0, type: 'file', areas: [area(0.1, 0.3)] },
      { party: 0, type: 'text', areas: [area(0.1, 0.5)] },
      { party: 1, type: 'text', areas: [area(0.1, 0.7)] },
    ],
  });
  const sent = await send(owner.agent, draft.document.id);
  return { owner, draft, fields, tokens: sent.tokens };
}

test('unknown, malformed and revoked tokens are all the same 404', async () => {
  const { owner, draft, tokens } = await ready();
  const parties = (await owner.agent.get(`/api/signing/documents/${draft.document.id}`).expect(200)).body.parties;
  await owner.agent.post(`/api/signing/documents/${draft.document.id}/parties/${parties[0].id}/revoke`).expect(200);
  const guesses = [crypto.randomBytes(32).toString('base64url'), 'short', `${tokens[1]}x`, tokens[1].toUpperCase(), tokens[0], '%00', 'a'.repeat(43)];
  const answers = [];
  for (const guess of guesses) {
    for (const path of ['', '/pdf', '/result', '/final', '/values/x/image']) {
      const res = await request(app).get(`/api/public/sign/${guess}${path}`);
      answers.push([res.status, JSON.stringify(res.body)]);
    }
    const post = await request(app).post(`/api/public/sign/${guess}/complete`).send({});
    answers.push([post.status, JSON.stringify(post.body)]);
  }
  assert.ok(answers.every(([status, body]) => status === 404 && body === answers[0][1]), 'identical answers');
});

test('public answers are uncacheable, unindexed, referrer-free and set no cookie', async () => {
  const { tokens } = await ready();
  for (const res of [await publicGet(tokens[0]).expect(200), await publicGet('x'.repeat(43)).expect(404)]) {
    assert.match(res.headers['cache-control'], /no-store/);
    assert.match(res.headers['x-robots-tag'], /noindex/);
    assert.equal(res.headers['referrer-policy'], 'no-referrer');
    assert.equal(res.headers['set-cookie'], undefined);
  }
  // Answers that come from the body parser, before the signing routes run.
  const badJson = await request(app).post(sign(tokens[0], '/consent')).set('Content-Type', 'application/json').send('{bad');
  assert.equal(badJson.status, 400);
  assert.equal(badJson.headers['referrer-policy'], 'no-referrer');
  assert.match(badJson.headers['x-robots-tag'], /noindex/);
  const pdf = await publicGet(tokens[0], '/pdf').expect(200);
  assert.equal(pdf.headers['content-type'], 'application/pdf');
  assert.match(pdf.headers['content-security-policy'], /sandbox/);
});

test('only the hash of a link is stored and the token path is redacted in logs', async () => {
  const { tokens } = await ready();
  const hash = crypto.createHash('sha256').update(tokens[0]).digest('hex');
  assert.ok(db.prepare('SELECT 1 FROM sign_parties WHERE token_hash = ?').get(hash));
  const dump = JSON.stringify(db.prepare('SELECT * FROM sign_parties').all()) + JSON.stringify(db.prepare('SELECT * FROM sign_events').all());
  assert.ok(!dump.includes(tokens[0]));
  assert.equal(redactSecrets(`GET /api/public/sign/${tokens[0]}/complete`), 'GET /api/public/sign/[redacted]/complete');
  assert.equal(redactSecrets(`GET /app/sign/${tokens[0]}`), 'GET /app/sign/[redacted]');
});

test('a signer reaches only its own fields, in its own turn, after consent', async () => {
  const { fields, tokens } = await ready();
  const [signature, file, text, other] = fields;
  await publicPut(tokens[0], `/fields/${text.id}`, { value: 'x' }).expect(409); // no consent yet
  await consent(tokens[0]);
  await publicPut(tokens[0], `/fields/${other.id}`, { value: 'x' }).expect(404);
  await publicPut(tokens[0], `/fields/not-a-uuid`, { value: 'x' }).expect(404);
  await publicPut(tokens[0], `/fields/${crypto.randomUUID()}`, { value: 'x' }).expect(404);
  await publicPut(tokens[1], `/fields/${other.id}`, { value: 'x' }).expect(409); // not B's turn
  await publicPost(tokens[0], '/consent', { accepted: false, signerName: 'A' }).expect(400);
  await publicPost(tokens[0], '/consent', { accepted: true, signerName: '' }).expect(400);
  await publicGet(tokens[0], `/values/${other.id}/image`).expect(404);
  await publicPut(tokens[0], `/fields/${signature.id}`, { value: 'text' }).expect(400);
  void file;
});

test('uploads are limited, re-encoded and cannot be replaced after signing', async () => {
  const { fields, tokens } = await ready();
  const [signature, file, text] = fields;
  await consent(tokens[0]);
  await uploadImage(tokens[0], signature.id, Buffer.from('not an image')).expect(400);
  await uploadImage(tokens[0], signature.id, Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="5" height="5"/>')).expect(400);
  await uploadImage(tokens[0], signature.id, await makePng(), 'method=forge').expect(400);
  await uploadImage(tokens[0], signature.id, await makePng(), 'method=type').expect(400);
  await uploadImage(tokens[0], signature.id, Buffer.alloc(0)).expect(400);
  // The server answers 413 and hangs up before the body is sent in full.
  const oversize = await uploadImage(tokens[0], signature.id, Buffer.alloc(9 * 1024 * 1024)).then((res) => res.status, (err) => (err.code === 'EPIPE' ? 413 : err));
  assert.equal(oversize, 413);
  await uploadImage(tokens[0], text.id, await makePng()).expect(400);

  // A big JPEG with metadata comes back as a small PNG without it.
  const jpeg = await sharp({ create: { width: 4000, height: 3000, channels: 3, background: '#335' } })
    .withMetadata({ exif: { IFD0: { Copyright: 'secret' } } }).jpeg().toBuffer();
  const saved = await uploadImage(tokens[0], signature.id, jpeg, 'method=upload').expect(201);
  assert.equal(saved.body.value.mime, 'image/png');
  assert.ok(saved.body.value.width <= 1200 && saved.body.value.height <= 600);
  const stored = await publicGet(tokens[0], `/values/${signature.id}/image`).expect(200).buffer(true).parse((res, cb) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  });
  const meta = await sharp(stored.body).metadata();
  assert.equal(meta.format, 'png');
  assert.equal(meta.exif, undefined);

  const attachment = await request(app)
    .post(`${sign(tokens[0], `/fields/${file.id}/upload`)}?name=${encodeURIComponent('../../etc/passwd')}`)
    .set('Content-Type', 'application/pdf').send(Buffer.from('%PDF-1.4 attachment')).expect(201);
  assert.equal(attachment.body.value.name, 'passwd');
  await publicGet(tokens[0], `/values/${file.id}/image`).expect(404);

  await publicPut(tokens[0], `/fields/${text.id}`, { value: 'final words' }).expect(200);
  await publicPost(tokens[0], '/complete').expect(200);
  await publicPost(tokens[0], '/complete').expect(200); // repeating is harmless
  await publicPut(tokens[0], `/fields/${text.id}`, { value: 'changed' }).expect(409);
  await uploadImage(tokens[0], signature.id, await makePng()).expect(409);
  await publicPut(tokens[0], `/fields/${signature.id}`, { value: null }).expect(409);
  await publicPost(tokens[0], '/decline', {}).expect(409);
  const after = await publicGet(tokens[0]).expect(200);
  assert.equal(after.body.myValues[text.id], 'final words');
});

test('another account cannot read a document or its values', async () => {
  const { owner, draft, fields, tokens } = await ready();
  const intruder = await registerUser('intruder');
  await consent(tokens[0]);
  await publicPut(tokens[0], `/fields/${fields[2].id}`, { value: 'private' }).expect(200);
  const base = `/api/signing/documents/${draft.document.id}`;
  for (const path of ['', '/pdf', '/events', '/audit.json', '/preview-values', '/final', `/values/${fields[0].id}/image`]) {
    assert.equal((await intruder.agent.get(`${base}${path}`)).status, 404, path);
  }
  assert.equal((await intruder.agent.put(`${base}/fields`).send({ fields: [] })).status, 404);
  assert.equal((await intruder.agent.post(`${base}/send`).send({})).status, 404);
  assert.equal((await intruder.agent.delete(base)).status, 404);
  assert.equal((await request(app).get(base)).status, 401);
  assert.equal((await owner.agent.get(base)).status, 200);
});

test('the signing page itself is unindexed, referrer-free and uncacheable', async () => {
  const { tokens } = await ready();
  for (const path of [`/app/sign/${tokens[0]}`, `/app/sign/${'y'.repeat(43)}`, '/app/sign/x/extra']) {
    const res = await request(app).get(path);
    assert.equal(res.headers['referrer-policy'], 'no-referrer', path);
    assert.match(res.headers['x-robots-tag'], /noindex/, path);
    assert.match(res.headers['cache-control'], /no-store/, path);
    assert.equal(res.headers['set-cookie'], undefined, path);
    const csp = res.headers['content-security-policy'];
    assert.ok(!/gstatic|googleapis|https?:/.test(csp), `no third party in the CSP: ${csp}`);
    assert.match(csp, /frame-ancestors 'none'/);
  }
  const other = await request(app).get('/app/');
  assert.notEqual(other.headers['referrer-policy'], 'no-referrer');
});

test('the signed result is flagged in the file listing, other files are not', async () => {
  const owner = await registerUser('flag');
  const draft = await createDraft(owner.agent);
  const base = `/api/signing/documents/${draft.document.id}`;
  const { fields } = await setUp(owner.agent, draft.document.id, {
    parties: [{ name: 'A' }],
    fields: [{ party: 0, type: 'text', areas: [area()] }],
  });
  const sent = await send(owner.agent, draft.document.id);
  await consent(sent.tokens[0]);
  await publicPut(sent.tokens[0], `/fields/${fields[0].id}`, { value: 'x' }).expect(200);
  await publicPost(sent.tokens[0], '/complete').expect(200);
  const finalNode = await until(async () => (await owner.agent.get(base).expect(200)).body.document.finalNodeId);
  const listing = (await owner.agent.get('/api/folders/root/children').expect(200)).body.items;
  const flagged = listing.filter((node) => node.signed === true).map((node) => node.id);
  assert.deepEqual(flagged, [finalNode]);
  assert.ok(listing.some((node) => node.id === draft.nodeId && node.signed === undefined));
});

test('a client that keeps trying tokens that do not exist is slowed down', async () => {
  for (let attempt = 0; attempt < 70; attempt += 1) {
    const res = await request(app).get(`/api/public/sign/${crypto.randomBytes(32).toString('base64url')}`);
    if (res.status === 429) {
      assert.equal(res.body.code, 'RATE_LIMITED');
      return;
    }
  }
  assert.fail('never limited');
});
