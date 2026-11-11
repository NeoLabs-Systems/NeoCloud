'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let alice;
let bob;
let shared;
let sub;
let inSub;
let inShared;
let outside;
let secret;

function binary(res, callback) {
  const chunks = [];
  res.on('data', (chunk) => chunks.push(chunk));
  res.on('end', () => callback(null, Buffer.concat(chunks)));
}

function vaultBody(parentId, name) {
  return {
    parentId,
    name,
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  };
}

async function createShare(agent, body) {
  return (await agent.post('/api/shares').send(body).expect(201)).body;
}

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
  shared = (await alice.agent.post('/api/folders').send({ name: 'Shared' }).expect(201)).body;
  sub = (await alice.agent.post('/api/folders').send({ parentId: shared.id, name: 'Sub' }).expect(201)).body;
  inShared = (await upload(alice.agent, 'readme.txt', 'shared readme', { parentId: shared.id }).expect(201)).body;
  inSub = (await upload(alice.agent, 'a.txt', 'alpha content', { parentId: sub.id }).expect(201)).body;
  outside = (await alice.agent.post('/api/folders').send({ name: 'Outside' }).expect(201)).body;
  secret = (await upload(alice.agent, 'secret.txt', 'not for the link', { parentId: outside.id }).expect(201)).body;
});

test('owner creates, lists and revokes links', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id });
  assert.ok(share.token.length >= 30);
  assert.match(share.url, new RegExp(`/s/${share.token}$`));
  assert.equal(share.nodeId, shared.id);
  assert.equal(share.nodeType, 'folder');
  assert.equal(share.hasPassword, false);
  assert.equal(share.allowUpload, false);

  const listed = await alice.agent.get('/api/shares').expect(200);
  assert.ok(listed.body.shares.some((item) => item.id === share.id));
  assert.equal((await bob.agent.get('/api/shares').expect(200)).body.shares.length, 0);

  // Another account's link and node behave as missing.
  await bob.agent.delete(`/api/shares/${share.id}`).expect(404);
  await bob.agent.post('/api/shares').send({ nodeId: shared.id }).expect(404);
  await request(app).post('/api/shares').send({ nodeId: shared.id }).expect(401);

  await alice.agent.delete(`/api/shares/${share.id}`).expect(200);
  const revoked = await request(app).get(`/api/public/shares/${share.token}`).expect(410);
  assert.equal(revoked.body.code, 'SHARE_REVOKED');
  assert.ok(!(await alice.agent.get('/api/shares').expect(200)).body.shares.some((item) => item.id === share.id));
});

test('invalid link requests are refused', async () => {
  const root = await alice.agent.get('/api/folders/root/children').expect(200);
  const rootShare = await alice.agent.post('/api/shares').send({ nodeId: root.body.folder.id }).expect(400);
  assert.equal(rootShare.body.code, 'ROOT_IMMUTABLE');
  const past = await alice.agent.post('/api/shares').send({ nodeId: shared.id, expiresAt: '2000-01-01T00:00:00Z' }).expect(400);
  assert.equal(past.body.code, 'INVALID_EXPIRY');
  await alice.agent.post('/api/shares').send({ nodeId: shared.id, expiresAt: 'soon' }).expect(400);
  const fileUpload = await alice.agent.post('/api/shares').send({ nodeId: inShared.id, allowUpload: true }).expect(400);
  assert.equal(fileUpload.body.code, 'NOT_A_FOLDER');
  await request(app).get('/api/public/shares/does-not-exist').expect(404);
});

test('a folder link lists and serves only its own subtree', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id });
  const visitor = request(app);
  const base = `/api/public/shares/${share.token}`;

  const top = await visitor.get(base).expect(200);
  assert.equal(top.body.share.name, 'Shared');
  assert.equal(top.body.share.type, 'folder');
  assert.deepEqual(top.body.path.map((part) => part.name), ['Shared']);
  assert.deepEqual(top.body.items.map((item) => item.name), ['Sub', 'readme.txt']);
  assert.equal(top.body.items[0].parentId, undefined);

  const inner = await visitor.get(`${base}?folderId=${sub.id}`).expect(200);
  assert.deepEqual(inner.body.path.map((part) => part.name), ['Shared', 'Sub']);
  assert.deepEqual(inner.body.items.map((item) => item.name), ['a.txt']);

  const content = await visitor.get(`${base}/files/${inSub.id}/content`).expect(200);
  assert.equal(content.text, 'alpha content');
  assert.match(content.headers['content-security-policy'], /sandbox/);
  const partial = await visitor.get(`${base}/files/${inSub.id}/content`).set('Range', 'bytes=6-').expect(206);
  assert.equal(partial.text, 'content');

  // Ancestry: nothing outside the shared folder, nothing of other accounts.
  const bobFile = (await upload(bob.agent, 'bob.txt', 'bob').expect(201)).body;
  const aliceRoot = (await alice.agent.get('/api/folders/root/children').expect(200)).body.folder.id;
  for (const id of [outside.id, aliceRoot, bobFile.id, 'root', 'not-a-uuid']) {
    await visitor.get(`${base}?folderId=${encodeURIComponent(id)}`).expect(404);
    await visitor.get(`${base}/zip?folderId=${encodeURIComponent(id)}`).expect(404);
  }
  for (const id of [secret.id, bobFile.id]) {
    await visitor.get(`${base}/files/${id}/content`).expect(404);
  }
  await visitor.get(`${base}/files/${sub.id}/content`).expect(400);

  const zip = await visitor.get(`${base}/zip`).buffer(true).parse(binary).expect(200);
  assert.equal(zip.headers['content-type'], 'application/zip');
  const text = zip.body.toString('latin1');
  assert.ok(text.includes('Shared/Sub/a.txt'));
  assert.ok(text.includes('Shared/readme.txt'));
  assert.ok(!text.includes('secret.txt'));

  const subZip = await visitor.get(`${base}/zip?folderId=${sub.id}`).buffer(true).parse(binary).expect(200);
  assert.ok(subZip.body.toString('latin1').includes('Sub/a.txt'));

  const listed = (await alice.agent.get('/api/shares').expect(200)).body.shares.find((item) => item.id === share.id);
  // The two ZIPs count; inline views of a file do not.
  assert.equal(listed.downloadCount, 2);
});

test('trashed nodes disappear from the link', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id });
  const base = `/api/public/shares/${share.token}`;
  await alice.agent.delete(`/api/nodes/${sub.id}`).expect(200);
  await request(app).get(`${base}?folderId=${sub.id}`).expect(404);
  await request(app).get(`${base}/files/${inSub.id}/content`).expect(404);
  const top = await request(app).get(base).expect(200);
  assert.deepEqual(top.body.items.map((item) => item.name), ['readme.txt']);
  await alice.agent.post(`/api/trash/${sub.id}/restore`).expect(200);

  await alice.agent.delete(`/api/nodes/${shared.id}`).expect(200);
  await request(app).get(base).expect(404);
  await request(app).get(`${base}/zip`).expect(404);
  await alice.agent.post(`/api/trash/${shared.id}/restore`).expect(200);
  await request(app).get(base).expect(200);
});

test('a file link serves just that file', async () => {
  const share = await createShare(alice.agent, { nodeId: inShared.id });
  const base = `/api/public/shares/${share.token}`;
  const meta = await request(app).get(base).expect(200);
  assert.equal(meta.body.share.type, 'file');
  assert.equal(meta.body.node.name, 'readme.txt');
  assert.deepEqual(meta.body.items, []);
  const content = await request(app).get(`${base}/files/${inShared.id}/content?download=1`).expect(200);
  assert.equal(content.text, 'shared readme');
  assert.match(content.headers['content-disposition'], /^attachment/);
  await request(app).get(`${base}/files/${inSub.id}/content`).expect(404);
  await request(app).get(`${base}?folderId=${shared.id}`).expect(404);
});

test('password links need the unlock cookie, scoped to the link', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id, password: 'open sesame' });
  assert.equal(share.hasPassword, true);
  const other = await createShare(alice.agent, { nodeId: sub.id, password: 'open sesame' });
  const base = `/api/public/shares/${share.token}`;
  const visitor = request.agent(app);

  const locked = await visitor.get(base).expect(401);
  assert.equal(locked.body.code, 'SHARE_PASSWORD_REQUIRED');
  await visitor.get(`${base}/files/${inShared.id}/content`).expect(401);
  await visitor.get(`${base}/zip`).expect(401);

  const wrong = await visitor.post(`${base}/unlock`).send({ password: 'nope' }).expect(401);
  assert.equal(wrong.body.code, 'SHARE_PASSWORD_INVALID');

  const unlocked = await visitor.post(`${base}/unlock`).send({ password: 'open sesame' }).expect(200);
  const cookie = unlocked.headers['set-cookie'][0];
  assert.match(cookie, new RegExp(`Path=/api/public/shares/${share.token}`));
  assert.match(cookie, /HttpOnly/);
  assert.match(cookie, /Max-Age=3600/);

  await visitor.get(base).expect(200);
  const content = await visitor.get(`${base}/files/${inShared.id}/content`).expect(200);
  assert.equal(content.text, 'shared readme');

  // The cookie of one link opens nothing else, and can't be forged.
  await visitor.get(`/api/public/shares/${other.token}`).expect(401);
  const value = cookie.split(';')[0].split('=')[1];
  await request(app).get(`/api/public/shares/${other.token}`).set('Cookie', `nc_share_unlock=${value}`).expect(401);
  const [expires] = value.split('.');
  await request(app).get(base).set('Cookie', `nc_share_unlock=${expires}.forged`).expect(401);
  await request(app).get(base).set('Cookie', `nc_share_unlock=${Number(expires) + 1e9}.${value.split('.')[1]}`).expect(401);
  await request(app).get(base).set('Cookie', `nc_share_unlock=${value}`).expect(200);
});

test('expired links answer 410', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id, expiresAt: new Date(Date.now() + 3600e3).toISOString() });
  assert.ok(share.expiresAt);
  await request(app).get(`/api/public/shares/${share.token}`).expect(200);
  db.prepare("UPDATE shares SET expires_at = datetime('now', '-1 minute') WHERE id = ?").run(share.id);
  const expired = await request(app).get(`/api/public/shares/${share.token}`).expect(410);
  assert.equal(expired.body.code, 'SHARE_EXPIRED');
  await request(app).get(`/api/public/shares/${share.token}/files/${inShared.id}/content`).expect(410);
  await request(app).post(`/api/public/shares/${share.token}/unlock`).send({}).expect(410);
  const listed = (await alice.agent.get('/api/shares').expect(200)).body.shares.find((item) => item.id === share.id);
  assert.equal(listed.expired, true);
});

test('uploads into a link: only when allowed, never overwriting, within the owner’s quota', async () => {
  const closed = await createShare(alice.agent, { nodeId: shared.id });
  const refused = await request(app)
    .put(`/api/public/shares/${closed.token}/upload?name=drop.txt`)
    .set('Content-Type', 'application/octet-stream')
    .send(Buffer.from('x'))
    .expect(403);
  assert.equal(refused.body.code, 'UPLOAD_NOT_ALLOWED');

  const open = await createShare(alice.agent, { nodeId: shared.id, allowUpload: true });
  const base = `/api/public/shares/${open.token}`;
  const put = (name, content, folderId) => request(app)
    .put(`${base}/upload?${new URLSearchParams({ name, ...(folderId ? { folderId } : {}) })}`)
    .set('Content-Type', 'application/json')
    .send(Buffer.from(content));

  const first = await put('readme.txt', 'from a visitor').expect(201);
  assert.equal(first.body.name, 'readme (1).txt');
  const original = await alice.agent.get(`/api/files/${inShared.id}/content`).expect(200);
  assert.equal(original.text, 'shared readme');

  const nested = await put('into-sub.txt', 'nested', sub.id).expect(201);
  const subItems = (await alice.agent.get(`/api/folders/${sub.id}/children`).expect(200)).body.items;
  assert.ok(subItems.some((item) => item.id === nested.body.id));
  await put('escape.txt', 'x', outside.id).expect(404);
  const meta = await request(app).get(base).expect(200);
  assert.equal(meta.body.share.allowUpload, true);

  // Bob's link counts against Bob's quota.
  const bobFolder = (await bob.agent.post('/api/folders').send({ name: 'Inbox' }).expect(201)).body;
  const bobShare = await createShare(bob.agent, { nodeId: bobFolder.id, allowUpload: true });
  await alice.agent.put(`/api/admin/users/${bob.user.id}/quota`).send({ mode: 'custom', bytes: 1024 }).expect(200);
  const tooBig = await request(app)
    .put(`/api/public/shares/${bobShare.token}/upload?name=big.bin`)
    .set('Content-Type', 'application/octet-stream')
    .send(crypto.randomBytes(4096))
    .expect(413);
  assert.equal(tooBig.body.code, 'QUOTA_EXCEEDED');
  await request(app)
    .put(`/api/public/shares/${bobShare.token}/upload?name=small.bin`)
    .set('Content-Type', 'application/octet-stream')
    .send(crypto.randomBytes(100))
    .expect(201);
  await alice.agent.put(`/api/admin/users/${bob.user.id}/quota`).send({ mode: 'default' }).expect(200);
});

test('vaults are never shared, not even inside a shared folder', async () => {
  const vault = (await alice.agent.post('/api/vaults').send(vaultBody(shared.id, 'Locked')).expect(201)).body;
  const refused = await alice.agent.post('/api/shares').send({ nodeId: vault.folderId }).expect(409);
  assert.equal(refused.body.code, 'VAULT_NOT_SHAREABLE');
  const cipher = (await upload(alice.agent, 'Y2lwaGVy', 'NCV1 ciphertext', { parentId: vault.folderId }).expect(201)).body;
  await alice.agent.post('/api/shares').send({ nodeId: cipher.id }).expect(409);

  const share = await createShare(alice.agent, { nodeId: shared.id });
  const base = `/api/public/shares/${share.token}`;
  const top = await request(app).get(base).expect(200);
  assert.ok(!top.body.items.some((item) => item.name === 'Locked'));
  await request(app).get(`${base}?folderId=${vault.folderId}`).expect(404);
  await request(app).get(`${base}/zip?folderId=${vault.folderId}`).expect(404);
  await request(app).get(`${base}/files/${cipher.id}/content`).expect(404);
  const zip = await request(app).get(`${base}/zip`).buffer(true).parse(binary).expect(200);
  const text = zip.body.toString('latin1');
  assert.ok(!text.includes('Locked'));
  assert.ok(!text.includes('Y2lwaGVy'));
});

test('the share page is served at /s/:token without a referrer', async () => {
  const res = await request(app).get('/s/some-token').set('Accept', 'text/html').expect(200);
  assert.match(res.headers['content-type'], /text\/html/);
  assert.equal(res.headers['referrer-policy'], 'no-referrer');
  assert.match(res.text, /\/js\/share\.js/);
  assert.ok(!/<script>(?!<\/script>)/.test(res.text), 'no inline script');
});

test('modes, notes and passwords are validated', async () => {
  const legacy = await createShare(alice.agent, { nodeId: shared.id, allowUpload: true });
  assert.equal(legacy.mode, 'view_upload');
  assert.equal(legacy.allowUpload, true);
  const plain = await createShare(alice.agent, { nodeId: inShared.id });
  assert.equal(plain.mode, 'view');
  assert.equal(plain.note, null);
  assert.equal(plain.maxDownloads, null);
  assert.equal(plain.exhausted, false);

  const cases = [
    [{ nodeId: shared.id, mode: 'everything' }, 'INVALID_MODE'],
    [{ nodeId: inShared.id, mode: 'upload_only' }, 'NOT_A_FOLDER'],
    [{ nodeId: inShared.id, mode: 'view_upload' }, 'NOT_A_FOLDER'],
    [{ nodeId: shared.id, password: 'seven77' }, 'PASSWORD_TOO_SHORT'],
    [{ nodeId: shared.id, note: 'x'.repeat(501) }, 'NOTE_TOO_LONG'],
    [{ nodeId: shared.id, maxDownloads: 0 }, 'INVALID_MAX_DOWNLOADS'],
    [{ nodeId: shared.id, maxDownloads: 2.5 }, 'INVALID_MAX_DOWNLOADS'],
    [{ nodeId: shared.id, maxUploadBytes: -5 }, 'INVALID_SIZE'],
  ];
  for (const [body, code] of cases) {
    const res = await alice.agent.post('/api/shares').send(body).expect(400);
    assert.equal(res.body.code, code, JSON.stringify(body));
  }
});

test('public meta names the sharer, the host and the note', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id, note: '  Photos from Saturday  ' });
  assert.equal(share.note, 'Photos from Saturday');
  const meta = await request(app).get(`/api/public/shares/${share.token}`).expect(200);
  assert.equal(meta.body.share.sharedBy, alice.username);
  assert.equal(meta.body.share.note, 'Photos from Saturday');
  assert.equal(meta.body.share.mode, 'view');
  assert.match(meta.body.share.host, /^127\.0\.0\.1(:\d+)?$/);
});

test('an upload-only link takes files but shows and serves nothing', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id, mode: 'upload_only' });
  assert.equal(share.mode, 'upload_only');
  assert.equal(share.allowUpload, true);
  const base = `/api/public/shares/${share.token}`;

  const meta = await request(app).get(base).expect(200);
  assert.equal(meta.body.share.mode, 'upload_only');
  assert.equal(meta.body.share.allowUpload, true);
  assert.deepEqual(meta.body.items, []);
  assert.deepEqual(meta.body.path.map((part) => part.name), ['Shared']);
  assert.equal(meta.body.node.name, 'Shared');

  for (const path of [`?folderId=${sub.id}`, `/files/${inShared.id}/content`, `/files/${inShared.id}/content?download=1`, '/zip']) {
    const res = await request(app).get(`${base}${path}`).expect(403);
    assert.equal(res.body.code, 'SHARE_UPLOAD_ONLY', path);
  }

  const sent = await request(app)
    .put(`${base}/upload?name=readme.txt`)
    .set('Content-Type', 'application/octet-stream')
    .send(Buffer.from('dropped in'))
    .expect(201);
  // The visitor learns nothing about what was already there.
  assert.deepEqual(sent.body, { name: 'readme.txt', size: 10 });
  const intoSub = await request(app)
    .put(`${base}/upload?name=x.txt&folderId=${sub.id}`)
    .set('Content-Type', 'application/octet-stream')
    .send(Buffer.from('x'))
    .expect(403);
  assert.equal(intoSub.body.code, 'SHARE_UPLOAD_ONLY');

  const items = (await alice.agent.get(`/api/folders/${shared.id}/children`).expect(200)).body.items;
  const stored = items.filter((item) => /^readme \(\d+\)\.txt$/.test(item.name));
  assert.ok(stored.length >= 1);
  const original = await alice.agent.get(`/api/files/${inShared.id}/content`).expect(200);
  assert.equal(original.text, 'shared readme');
});

test('download limits count real downloads and end the link with 410', async () => {
  const share = await createShare(alice.agent, { nodeId: inShared.id, maxDownloads: 2 });
  const base = `/api/public/shares/${share.token}`;
  const file = `${base}/files/${inShared.id}/content`;
  await request(app).get(file).expect(200);
  await request(app).get(file).expect(200);
  await request(app).head(`${file}?download=1`).expect(200);
  const counted = () => db.prepare('SELECT download_count FROM shares WHERE id = ?').get(share.id).download_count;
  assert.equal(counted(), 0, 'inline views and HEAD are not downloads');

  await request(app).get(`${file}?download=1`).expect(200);
  // The visitor keeps the cookie its counted download sets.
  const visitor = request.agent(app);
  await visitor.get(`${file}?download=1`).set('Range', 'bytes=0-3').expect(206);
  assert.equal(counted(), 2);
  // A download in progress may finish after the limit is reached.
  const rest = await visitor.get(`${file}?download=1`).set('Range', 'bytes=4-').expect(206);
  assert.equal(rest.text, 'ed readme');
  assert.equal(counted(), 2);
  // Ranges that include byte 0 are new downloads, however they are spelled,
  // and a later start without that download's cookie is one too.
  for (const range of ['bytes=-1000000', 'bytes=00-', 'bytes=0-']) {
    const res = await visitor.get(`${file}?download=1`).set('Range', range).expect(410);
    assert.equal(res.body.code, 'SHARE_EXHAUSTED');
  }
  await request(app).get(`${file}?download=1`).set('Range', 'bytes=1-').expect(410);
  assert.equal(counted(), 2);

  for (const path of [`${file}?download=1`, file, base]) {
    const res = await request(app).get(path).expect(410);
    assert.equal(res.body.code, 'SHARE_EXHAUSTED');
  }
  const listed = (await alice.agent.get('/api/shares').expect(200)).body.shares.find((item) => item.id === share.id);
  assert.equal(listed.exhausted, true);
  assert.equal(listed.downloadCount, 2);
  assert.ok(!(await alice.agent.get(`/api/shares?nodeId=${inShared.id}`).expect(200)).body.shares.some((item) => item.id === share.id));

  // ZIPs count too.
  const folderShare = await createShare(alice.agent, { nodeId: sub.id, maxDownloads: 1 });
  await request(app).get(`/api/public/shares/${folderShare.token}/zip`).expect(200);
  const zip = await request(app).get(`/api/public/shares/${folderShare.token}/zip`).expect(410);
  assert.equal(zip.body.code, 'SHARE_EXHAUSTED');

  // Raising the limit opens the link again.
  const raised = await alice.agent.patch(`/api/shares/${share.id}`).send({ maxDownloads: null }).expect(200);
  assert.equal(raised.body.exhausted, false);
  await request(app).get(base).expect(200);
});

test('links are edited in place and listed per node', async () => {
  const share = await createShare(alice.agent, { nodeId: shared.id });
  const future = new Date(Date.now() + 7 * 864e5).toISOString();
  const edited = await alice.agent.patch(`/api/shares/${share.id}`).send({
    mode: 'view_upload',
    note: 'Hello',
    expiresAt: future,
    maxDownloads: 5,
    maxUploadBytes: 1024,
  }).expect(200);
  assert.equal(edited.body.token, share.token);
  assert.ok(edited.body.url.endsWith(`/s/${share.token}`));
  assert.equal(edited.body.mode, 'view_upload');
  assert.equal(edited.body.note, 'Hello');
  assert.equal(edited.body.maxDownloads, 5);
  assert.equal(edited.body.maxUploadBytes, 1024);
  assert.ok(edited.body.expiresAt);

  // A password set later locks visitors out until they unlock again.
  const visitor = request.agent(app);
  await visitor.get(`/api/public/shares/${share.token}`).expect(200);
  const locked = await alice.agent.patch(`/api/shares/${share.id}`).send({ password: 'long enough' }).expect(200);
  assert.equal(locked.body.hasPassword, true);
  await visitor.get(`/api/public/shares/${share.token}`).expect(401);
  await visitor.post(`/api/public/shares/${share.token}/unlock`).send({ password: 'long enough' }).expect(200);
  await visitor.get(`/api/public/shares/${share.token}`).expect(200);
  const changed = await alice.agent.patch(`/api/shares/${share.id}`).send({ password: 'another one' }).expect(200);
  assert.equal(changed.body.hasPassword, true);
  await visitor.get(`/api/public/shares/${share.token}`).expect(401);

  const opened = await alice.agent.patch(`/api/shares/${share.id}`).send({ password: null, expiresAt: null, note: null }).expect(200);
  assert.equal(opened.body.hasPassword, false);
  assert.equal(opened.body.expiresAt, null);
  assert.equal(opened.body.note, null);
  assert.equal(opened.body.mode, 'view_upload');
  await visitor.get(`/api/public/shares/${share.token}`).expect(200);

  assert.equal((await alice.agent.patch(`/api/shares/${share.id}`).send({ password: 'short' }).expect(400)).body.code, 'PASSWORD_TOO_SHORT');
  assert.equal((await alice.agent.patch(`/api/shares/${share.id}`).send({ expiresAt: '2001-01-01' }).expect(400)).body.code, 'INVALID_EXPIRY');
  const fileShare = await createShare(alice.agent, { nodeId: inShared.id });
  assert.equal((await alice.agent.patch(`/api/shares/${fileShare.id}`).send({ mode: 'upload_only' }).expect(400)).body.code, 'NOT_A_FOLDER');
  await bob.agent.patch(`/api/shares/${share.id}`).send({ note: 'mine now' }).expect(404);
  await alice.agent.patch('/api/shares/abc').send({}).expect(400);

  const forNode = await alice.agent.get(`/api/shares?nodeId=${shared.id}`).expect(200);
  assert.ok(forNode.body.shares.length >= 1);
  assert.ok(forNode.body.shares.every((item) => item.nodeId === shared.id && !item.expired && !item.exhausted));
  assert.ok(forNode.body.shares.some((item) => item.id === share.id));
  assert.deepEqual((await bob.agent.get(`/api/shares?nodeId=${shared.id}`).expect(200)).body.shares, []);

  await alice.agent.delete(`/api/shares/${share.id}`).expect(200);
  await alice.agent.patch(`/api/shares/${share.id}`).send({ note: 'x' }).expect(404);
  assert.ok(!(await alice.agent.get(`/api/shares?nodeId=${shared.id}`).expect(200)).body.shares.some((item) => item.id === share.id));
});

test('a link caps what visitors may upload through it', async () => {
  const share = await createShare(alice.agent, { nodeId: sub.id, mode: 'view_upload', maxUploadBytes: 10 });
  const put = (name, bytes) => request(app)
    .put(`/api/public/shares/${share.token}/upload?name=${name}`)
    .set('Content-Type', 'application/octet-stream')
    .send(Buffer.alloc(bytes, 1));
  await put('eight.bin', 8).expect(201);
  const over = await put('five.bin', 5).expect(413);
  assert.equal(over.body.code, 'SHARE_UPLOAD_LIMIT');
  await put('two.bin', 2).expect(201);
  assert.equal((await put('one.bin', 1).expect(413)).body.code, 'SHARE_UPLOAD_LIMIT');
  const listed = (await alice.agent.get('/api/shares').expect(200)).body.shares.find((item) => item.id === share.id);
  assert.equal(listed.uploadedBytes, 10);
});

// A raw request whose body is sent by hand, so a test can hold it half-sent.
function openRawUpload(server, path, headers) {
  const http = require('http');
  const { port } = server.address();
  const req = http.request({ port, path, method: 'PUT', headers: { 'Content-Type': 'application/octet-stream', ...headers } });
  const response = new Promise((resolve, reject) => {
    req.on('response', (res) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve({ status: res.statusCode, body: JSON.parse(Buffer.concat(chunks).toString() || '{}') }));
    });
    req.on('error', reject);
  });
  return { req, response };
}

async function waitFor(check) {
  for (let i = 0; i < 200 && !check(); i += 1) await new Promise((resolve) => setTimeout(resolve, 10));
  assert.ok(check());
}

test('parallel uploads cannot go past a link\'s upload cap', async () => {
  const share = await createShare(alice.agent, { nodeId: sub.id, mode: 'view_upload', maxUploadBytes: 1000 });
  const uploaded = () => db.prepare('SELECT uploaded_bytes FROM shares WHERE id = ?').get(share.id).uploaded_bytes;
  const path = (name) => `/api/public/shares/${share.token}/upload?name=${name}`;
  const server = app.listen(0);
  try {
    // The first upload reserves its size before its body arrives ...
    const first = openRawUpload(server, path('first.bin'), { 'Content-Length': '900' });
    first.req.write(Buffer.alloc(1, 1));
    await waitFor(() => uploaded() === 900);
    // ... so a parallel one that would go past the cap is refused up front.
    const second = await request(app).put(path('second.bin')).set('Content-Type', 'application/octet-stream')
      .send(Buffer.alloc(900, 2)).expect(413);
    assert.equal(second.body.code, 'SHARE_UPLOAD_LIMIT');
    first.req.end(Buffer.alloc(899, 1));
    assert.equal((await first.response).status, 201);
    assert.equal(uploaded(), 900);

    // A capped link needs the size up front.
    const chunked = openRawUpload(server, path('chunked.bin'), { 'Transfer-Encoding': 'chunked' });
    chunked.req.end(Buffer.alloc(10, 3));
    const refused = await chunked.response;
    assert.equal(refused.status, 411);
    assert.equal(refused.body.code, 'LENGTH_REQUIRED');

    // A failed upload gives its reservation back.
    await request(app).put(path('broken.bin')).set('Content-Type', 'application/octet-stream')
      .set('X-Content-SHA256', '0'.repeat(64)).send(Buffer.alloc(50, 4)).expect(422);
    assert.equal(uploaded(), 900);
    await request(app).put(path('last.bin')).set('Content-Type', 'application/octet-stream')
      .send(Buffer.alloc(100, 5)).expect(201);
    assert.equal(uploaded(), 1000);
  } finally {
    server.close();
  }
});

test('wrong passwords slow a link down, then lock it for 30 minutes', async () => {
  const share = await createShare(alice.agent, { nodeId: sub.id, password: 'correct horse' });
  const base = `/api/public/shares/${share.token}`;
  const guess = (password) => request(app).post(`${base}/unlock`).send({ password });
  const failures = () => db.prepare('SELECT COUNT(*) AS n FROM share_unlock_failures WHERE share_id = ?').get(share.id).n;
  const age = (ms) => db.prepare('UPDATE share_unlock_failures SET failed_at_ms = failed_at_ms - ? WHERE share_id = ?').run(ms, share.id);

  for (let i = 0; i < 5; i += 1) {
    assert.equal((await guess(`wrong ${i}`).expect(401)).body.code, 'SHARE_PASSWORD_INVALID');
  }
  const slowed = await guess('wrong 5').expect(429);
  assert.equal(slowed.body.code, 'SHARE_LOCKED');
  assert.ok(slowed.body.retryAfter >= 1 && slowed.body.retryAfter <= 2);
  assert.equal(slowed.headers['retry-after'], String(slowed.body.retryAfter));
  assert.equal(failures(), 5, 'a refused try is not a guess');

  // After the delay the next guess goes through; ten in the window lock it.
  age(60e3);
  await guess('wrong 6').expect(401);
  const insert = db.prepare('INSERT INTO share_unlock_failures (share_id, failed_at_ms) VALUES (?, ?)');
  for (let i = 0; i < 4; i += 1) insert.run(share.id, Date.now() - 60e3);
  const locked = await guess('correct horse').expect(429);
  assert.equal(locked.body.code, 'SHARE_LOCKED');
  assert.ok(locked.body.retryAfter > 25 * 60, `retryAfter ${locked.body.retryAfter}`);

  // Another link is not affected.
  const other = await createShare(alice.agent, { nodeId: sub.id, password: 'correct horse' });
  await request(app).post(`/api/public/shares/${other.token}/unlock`).send({ password: 'correct horse' }).expect(200);

  age(31 * 60e3);
  await guess('correct horse').expect(200);
  assert.equal(failures(), 0);
});
