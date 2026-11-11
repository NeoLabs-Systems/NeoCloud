'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { STRONG_PASSWORD, app, cleanup, home, registerUser, request, upload } = require('../helpers/app');

test.after(cleanup);

let alice;
let bob;
let aliceFile;
let aliceFolder;

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
  aliceFolder = (await alice.agent.post('/api/folders').send({ name: 'Privat' }).expect(201)).body;
  aliceFile = (await upload(alice.agent, 'secret.txt', 'top secret content of alice', { parentId: aliceFolder.id }).expect(201)).body;
});

test('only the first account is admin', () => {
  assert.equal(alice.user.isAdmin, true);
  assert.equal(bob.user.isAdmin, false);
});

test('another account sees someone else’s nodes as missing (no IDOR)', async () => {
  const id = aliceFile.id;
  await bob.agent.get(`/api/files/${id}/content`).expect(404);
  await bob.agent.get(`/api/nodes/${id}`).expect(404);
  await bob.agent.get(`/api/folders/${aliceFolder.id}/children`).expect(404);
  await bob.agent.patch(`/api/nodes/${id}`).send({ name: 'mine.txt' }).expect(404);
  await bob.agent.delete(`/api/nodes/${id}`).expect(404);
  await bob.agent.post(`/api/nodes/${id}/copy`).send({}).expect(404);
  await bob.agent.get(`/api/files/${id}/versions`).expect(404);
  await bob.agent.get(`/api/download/zip?ids=${aliceFolder.id}`).expect(404);
  // Bob can't upload into or move his things into Alice's folder either.
  await upload(bob.agent, 'drop.txt', 'x', { parentId: aliceFolder.id }).expect(404);
  const own = (await upload(bob.agent, 'own.txt', 'bob').expect(201)).body;
  await bob.agent.patch(`/api/nodes/${own.id}`).send({ parentId: aliceFolder.id }).expect(404);
  // Alice's file is untouched.
  const res = await alice.agent.get(`/api/files/${id}/content`).expect(200);
  assert.equal(res.text, 'top secret content of alice');
});

test('shares and vaults: another account’s node ids behave as missing', async () => {
  const keys = {
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  };
  await bob.agent.post('/api/shares').send({ nodeId: aliceFile.id }).expect(404);
  await bob.agent.post('/api/shares').send({ nodeId: aliceFolder.id }).expect(404);
  await bob.agent.post('/api/vaults').send({ parentId: aliceFolder.id, name: 'x', ...keys }).expect(404);

  // Bob's own link never reaches Alice's nodes through its node id slots.
  const bobFolder = (await bob.agent.post('/api/folders').send({ name: 'Public' }).expect(201)).body;
  const share = (await bob.agent.post('/api/shares').send({ nodeId: bobFolder.id, allowUpload: true }).expect(201)).body;
  const base = `/api/public/shares/${share.token}`;
  await request(app).get(`${base}?folderId=${aliceFolder.id}`).expect(404);
  await request(app).get(`${base}/files/${aliceFile.id}/content`).expect(404);
  await request(app).get(`${base}/zip?folderId=${aliceFolder.id}`).expect(404);
  await request(app).put(`${base}/upload?name=x.txt&folderId=${aliceFolder.id}`).send(Buffer.from('x')).expect(404);
  await request(app).get('/api/shares').expect(401);
  await request(app).get('/api/vaults/anything').expect(401);
  await request(app).get('/api/devices/me').expect(401);
});

test('anonymous requests are refused', async () => {
  const anon = request(app);
  await anon.get('/api/folders/root/children').expect(401);
  await anon.put('/api/upload?name=x.txt').send(Buffer.from('x')).expect(401);
  await anon.get(`/api/files/${aliceFile.id}/content`).expect(401);
  await anon.get('/api/sync/tree').expect(401);
  await anon.get('/api/admin/users').expect(403);
  await anon.get('/api/folders/root/children').set('Authorization', 'Bearer nc_forged').expect(401);
});

test('non-admins can’t reach the admin API', async () => {
  await bob.agent.get('/api/admin/users').expect(403);
  await bob.agent.put(`/api/admin/users/${bob.user.id}/quota`).send({ mode: 'unlimited' }).expect(403);
  await bob.agent.post('/api/admin/keys/master').send({ password: STRONG_PASSWORD }).expect(403);
});

test('state-changing requests from a foreign origin are blocked (CSRF)', async () => {
  const res = await alice.agent
    .post('/api/folders')
    .set('Origin', 'https://evil.example')
    .send({ name: 'csrf' })
    .expect(403);
  assert.equal(res.body.code, 'ORIGIN_NOT_ALLOWED');
});

test('quota: the admin limit is enforced on single and resumable uploads', async () => {
  await alice.agent.put(`/api/admin/users/${bob.user.id}/quota`).send({ mode: 'custom', bytes: '2KB' }).expect(200);
  const tooBig = await upload(bob.agent, 'big.bin', Buffer.alloc(4096)).expect(413);
  assert.equal(tooBig.body.code, 'QUOTA_EXCEEDED');
  const session = await bob.agent.post('/api/uploads').send({ name: 'big.bin', size: 4096 }).expect(413);
  assert.equal(session.body.code, 'QUOTA_EXCEEDED');

  // Without Content-Length (chunked transfer) the stream is cut at the limit.
  const { token } = (await bob.agent.post('/api/account/devices').send({ name: 'cli' }).expect(201)).body;
  const server = app.listen(0);
  try {
    const body = new ReadableStream({
      start(controller) {
        for (let i = 0; i < 4; i += 1) controller.enqueue(new Uint8Array(1024));
        controller.close();
      },
    });
    const response = await fetch(`http://127.0.0.1:${server.address().port}/api/upload?name=stream.bin`, {
      method: 'PUT',
      headers: { Authorization: `Bearer ${token}` },
      body,
      duplex: 'half',
    });
    assert.equal(response.status, 413);
    assert.equal((await response.json()).code, 'QUOTA_EXCEEDED');
  } finally {
    server.close();
  }
  assert.equal(fs.readdirSync(path.join(home, 'data', 'storage', 'tmp')).filter((f) => f.endsWith('.part')).length, 0);

  await upload(bob.agent, 'small.bin', Buffer.alloc(512)).expect(201);
  const usage = (await bob.agent.get('/api/account/usage').expect(200)).body;
  assert.equal(usage.quotaBytes, 2048);

  const users = (await alice.agent.get('/api/admin/users').expect(200)).body.users;
  const row = users.find((user) => user.id === bob.user.id);
  assert.equal(row.quotaMode, 'custom');

  await alice.agent.put(`/api/admin/users/${bob.user.id}/quota`).send({ mode: 'default' }).expect(200);
  await upload(bob.agent, 'big.bin', Buffer.alloc(4096)).expect(201);
});

test('names can’t escape their folder', async () => {
  for (const name of ['../etc/passwd', '..', 'a/b.txt', 'a\\b.txt']) {
    const res = await upload(alice.agent, name, 'x');
    assert.equal(res.status, 400, name);
  }
  await alice.agent.post('/api/folders').send({ name: '../up' }).expect(400);
});

test('active content is never served inline', async () => {
  const html = (await upload(alice.agent, 'page.html', '<script>alert(1)</script>').expect(201)).body;
  const res = await alice.agent.get(`/api/files/${html.id}/content`).expect(200);
  assert.match(res.headers['content-disposition'], /^attachment/);
  assert.match(res.headers['content-security-policy'], /sandbox/);
  assert.equal(res.headers['x-content-type-options'], 'nosniff');
});

test('files are encrypted on disk', () => {
  const blobRoot = path.join(home, 'data', 'storage', 'blobs');
  const files = fs.readdirSync(blobRoot, { recursive: true })
    .map((entry) => path.join(blobRoot, entry))
    .filter((entry) => fs.statSync(entry).isFile());
  assert.ok(files.length > 0);
  for (const file of files) {
    const bytes = fs.readFileSync(file);
    assert.equal(bytes.subarray(0, 4).toString('ascii'), 'NCB1');
    assert.equal(bytes.includes(Buffer.from('top secret')), false);
  }
});

test('master key export needs the admin password', async () => {
  await alice.agent.post('/api/admin/keys/master').send({ password: 'wrong' }).expect(401);
  const res = await alice.agent.post('/api/admin/keys/master').send({ password: STRONG_PASSWORD }).expect(200);
  assert.equal(res.body.masterKey, process.env.NEOCLOUD_MASTER_KEY);
});

test('registration is refused while sign-up is off', async () => {
  await alice.agent.put('/api/admin/access/signup').send({ allowSignup: false }).expect(200);
  const status = await request(app).get('/api/auth/status').expect(200);
  assert.equal(status.body.registrationOpen, false);
  const blocked = await request(app).post('/api/auth/register').send({
    username: 'carol', email: 'carol@example.com', password: STRONG_PASSWORD,
  }).expect(403);
  assert.equal(blocked.body.code, 'SIGNUP_DISABLED');
  await alice.agent.get('/api/admin/signup-keys').expect(404);

  await alice.agent.put('/api/admin/access/signup').send({ allowSignup: true }).expect(200);
  assert.equal((await request(app).get('/api/auth/status').expect(200)).body.registrationOpen, true);
  await request(app).post('/api/auth/register').send({
    username: 'carol', email: 'carol@example.com', password: STRONG_PASSWORD,
  }).expect(200);
});

test('weak passwords and duplicate names are refused', async () => {
  const weak = await request(app).post('/api/auth/register').send({
    username: 'dave', email: 'dave@example.com', password: 'password1',
  }).expect(400);
  assert.equal(weak.body.code, 'PASSWORD_WEAK');
  const taken = await request(app).post('/api/auth/register').send({
    username: alice.username.toUpperCase(), email: 'x@example.com', password: STRONG_PASSWORD,
  }).expect(409);
  assert.equal(taken.body.code, 'USERNAME_TAKEN');
});

test('deleting an account removes its files', async () => {
  await alice.agent.delete(`/api/admin/users/${alice.user.id}`).expect(403);
  await alice.agent.delete(`/api/admin/users/${bob.user.id}`).expect(200);
  assert.equal(fs.existsSync(path.join(home, 'data', 'storage', 'blobs', String(bob.user.id))), false);
  await bob.agent.get('/api/folders/root/children').expect(401);
});

test('favorites, opened tracking, search scopes and link filters: another account’s ids behave as missing', async () => {
  // Bob is gone by now (account deletion above); Carol plays the other account.
  const carol = await registerUser('carol');
  for (const id of [aliceFile.id, aliceFolder.id]) {
    await carol.agent.put(`/api/nodes/${id}/favorite`).expect(404);
    await carol.agent.delete(`/api/nodes/${id}/favorite`).expect(404);
    await carol.agent.post(`/api/nodes/${id}/opened`).expect(404);
  }
  await carol.agent.get(`/api/search?parentId=${aliceFolder.id}`).expect(404);
  await carol.agent.get(`/api/search?q=secret&parentId=${aliceFolder.id}`).expect(404);
  assert.deepEqual((await carol.agent.get(`/api/shares?nodeId=${aliceFolder.id}`).expect(200)).body.shares, []);
  const aliceShare = (await alice.agent.post('/api/shares').send({ nodeId: aliceFolder.id }).expect(201)).body;
  await carol.agent.patch(`/api/shares/${aliceShare.id}`).send({ password: 'take over now' }).expect(404);
  assert.equal((await alice.agent.get(`/api/shares?nodeId=${aliceFolder.id}`).expect(200)).body.shares[0].hasPassword, false);

  // Carol's storage pages never mention Alice's files.
  const breakdown = await carol.agent.get('/api/storage/breakdown').expect(200);
  assert.ok(!JSON.stringify(breakdown.body).includes('secret.txt'));
  await upload(carol.agent, 'same.txt', 'top secret content of alice').expect(201);
  const duplicates = await carol.agent.get('/api/storage/duplicates').expect(200);
  assert.deepEqual(duplicates.body.groups, []);

  for (const path of ['/api/favorites', '/api/recent', '/api/storage/breakdown', '/api/storage/duplicates', '/api/search?type=image']) {
    await request(app).get(path).expect(401);
  }
  await request(app).put(`/api/nodes/${aliceFile.id}/favorite`).expect(401);
  await request(app).post(`/api/nodes/${aliceFile.id}/opened`).expect(401);
  await request(app).patch(`/api/shares/${aliceShare.id}`).send({}).expect(401);
});

test('landing pages load no third-party fonts', async () => {
  for (const name of ['index.html', '404.html', 'share.html', 'css/styles.css', 'css/neocloud.css', 'css/fonts.css']) {
    const text = fs.readFileSync(path.join(__dirname, '..', '..', 'landing', name), 'utf8');
    assert.ok(!/fonts\.(googleapis|gstatic)\.com/.test(text), name);
  }
  const font = await request(app).get('/fonts/inter-latin.woff2').expect(200);
  assert.equal(font.body.subarray(0, 4).toString('latin1'), 'wOF2');
  await request(app).get('/fonts/OFL.txt').expect(200);
  const page = await request(app).get('/s/anything').set('Accept', 'text/html').expect(200);
  assert.match(page.text, /href="\/css\/fonts\.css"/);
  assert.match(page.text, /You never need a password for your NeoCloud account to open a shared link/);
});

test('imports: session only, scoped to the account, /test rate-limited', async () => {
  await request(app).get('/api/imports').expect(401);
  await request(app).post('/api/imports').send({}).expect(401);
  // Another account can't point an import at Alice's folder.
  const mallory = await registerUser('mallory');
  await mallory.agent.post('/api/imports').send({
    source: 'webdav', url: 'http://127.0.0.1:9/dav/', username: 'x', password: 'y', targetFolderId: aliceFolder.id,
  }).expect(404);
  let last;
  for (let attempt = 0; attempt < 21; attempt += 1) {
    last = await mallory.agent.post('/api/imports/test').send({ source: 'webdav', url: 'not a url' });
  }
  assert.equal(last.status, 429);
});
