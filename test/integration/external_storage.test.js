'use strict';

// Per-account cloud storage: connect S3 and WebDAV (in-process fakes), browse,
// read with ranges, upload, make folders, rename, delete, copy to and from
// NeoCloud, read-only mounts, and what happens when the account goes away.

const test = require('node:test');
const assert = require('node:assert/strict');

process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'true';
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { createFakeS3 } = require('../helpers/fake_s3');
const { createFakeWebdav } = require('../helpers/fake_webdav');
const { exportAccount } = require('../../server/services/account/export');
const { eraseAccount } = require('../../server/services/account/erasure');

const s3 = createFakeS3();
const dav = createFakeWebdav();
let owner;
let s3Url;
let davUrl;
let s3Mount;
let davMount;

test.before(async () => {
  owner = await registerUser('ext');
  s3.createBucket('photos');
  s3Url = await s3.start();
  davUrl = await dav.start();
  s3.objects.set('photos/files/hello.txt', { body: Buffer.from('hello from s3'), storageClass: 'STANDARD', restore: null });
  s3.objects.set('photos/files/sub/deep.txt', { body: Buffer.from('deep'), storageClass: 'STANDARD', restore: null });
  s3.objects.set('photos/other/outside.txt', { body: Buffer.from('outside the prefix'), storageClass: 'STANDARD', restore: null });
  dav.files.set('note.txt', Buffer.from('0123456789'));
  dav.folders.add('docs');
  dav.files.set('docs/a.md', Buffer.from('# a'));
});

test.after(async () => {
  await s3.stop();
  await dav.stop();
  cleanup();
});

const s3Body = (extra = {}) => ({
  name: 'My bucket',
  provider: 's3',
  config: { endpoint: s3Url, region: 'us-east-1', bucket: 'photos', prefix: 'files', accessKeyId: 'AKIATEST', secretAccessKey: 'super-secret-key' },
  ...extra,
});
const davBody = (extra = {}) => ({
  name: 'My NAS', provider: 'webdav', config: { url: davUrl, username: dav.username, password: dav.password }, ...extra,
});

test('connecting tests the credentials first and saves nothing when they fail', async () => {
  const bad = await owner.agent.post('/api/external-storage').send(davBody({ config: { url: davUrl, username: dav.username, password: 'wrong' } })).expect(422);
  assert.equal(bad.body.code, 'EXTERNAL_TEST_FAILED');
  assert.equal((await owner.agent.get('/api/external-storage').expect(200)).body.items.length, 0);

  const dry = await owner.agent.post('/api/external-storage/test').send(davBody()).expect(200);
  assert.deepEqual(dry.body, { ok: true, items: 2 });
  const refused = await owner.agent.post('/api/external-storage/test').send(davBody({ config: { url: davUrl, username: 'x', password: 'y' } })).expect(200);
  assert.equal(refused.body.ok, false);
});

test('S3: connect, browse inside the prefix, ranged download', async () => {
  s3Mount = (await owner.agent.post('/api/external-storage').send(s3Body()).expect(201)).body;
  assert.equal(s3Mount.config.bucket, 'photos');
  assert.equal(s3Mount.config.secretAccessKeySet, true);

  const root = (await owner.agent.get(`/api/external-storage/${s3Mount.id}/list`).expect(200)).body;
  assert.deepEqual(root.items.map((item) => [item.name, item.type]), [['sub', 'folder'], ['hello.txt', 'file']]);
  assert.equal(root.items[1].size, 13);
  const sub = (await owner.agent.get(`/api/external-storage/${s3Mount.id}/list`).query({ path: 'sub' }).expect(200)).body;
  assert.deepEqual(sub.items.map((item) => item.path), ['sub/deep.txt']);

  const whole = await owner.agent.get(`/api/external-storage/${s3Mount.id}/file`).query({ path: 'hello.txt' }).buffer(true).parse((res, cb) => {
    const chunks = [];
    res.on('data', (c) => chunks.push(c));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  }).expect(200);
  assert.equal(whole.body.toString(), 'hello from s3');
  assert.match(whole.headers['content-disposition'], /^attachment/);
  const part = await owner.agent.get(`/api/external-storage/${s3Mount.id}/file`).query({ path: 'hello.txt' }).set('Range', 'bytes=6-9').buffer(true).parse((res, cb) => {
    const chunks = [];
    res.on('data', (c) => chunks.push(c));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  }).expect(206);
  assert.equal(part.body.toString(), 'from');
  assert.equal(part.headers['content-range'], 'bytes 6-9/13');
});

test('S3: upload, folder, rename, delete', async () => {
  const id = s3Mount.id;
  const base = `/api/external-storage/${id}`;
  await owner.agent.post(`${base}/folder`).send({ path: 'new' }).expect(201);
  await owner.agent.post(`${base}/folder`).send({ path: 'new' }).expect(409);
  const put = await owner.agent.put(`${base}/file`).query({ path: 'new/up.txt' }).set('Content-Type', 'application/octet-stream').send(Buffer.from('uploaded')).expect(201);
  assert.equal(put.body.path, 'new/up.txt');
  assert.equal(s3.objects.get('photos/files/new/up.txt').body.toString(), 'uploaded');
  await owner.agent.put(`${base}/file`).query({ path: 'new/up.txt' }).set('Content-Type', 'application/octet-stream').send(Buffer.from('again')).expect(409);
  const renamed = await owner.agent.put(`${base}/file`).query({ path: 'new/up.txt', conflict: 'rename' }).set('Content-Type', 'application/octet-stream').send(Buffer.from('again')).expect(201);
  assert.equal(renamed.body.name, 'up (1).txt');

  await owner.agent.post(`${base}/move`).send({ from: 'new/up.txt', to: 'new/moved.txt' }).expect(200);
  assert.ok(s3.objects.has('photos/files/new/moved.txt'));
  assert.ok(!s3.objects.has('photos/files/new/up.txt'));
  await owner.agent.post(`${base}/move`).send({ from: 'new', to: 'renamed' }).expect(200);
  assert.ok(s3.objects.has('photos/files/renamed/moved.txt'));

  await owner.agent.delete(`${base}/entry`).query({ path: 'renamed' }).expect(200);
  assert.ok(![...s3.objects.keys()].some((key) => key.startsWith('photos/files/renamed')));
  assert.ok(s3.objects.has('photos/other/outside.txt'), 'nothing outside the prefix is touched');
  await owner.agent.delete(`${base}/entry`).query({ path: 'nothing-here' }).expect(404);
});

test('WebDAV: connect, browse, ranged read, write, rename, delete', async () => {
  davMount = (await owner.agent.post('/api/external-storage').send(davBody()).expect(201)).body;
  const base = `/api/external-storage/${davMount.id}`;
  const root = (await owner.agent.get(`${base}/list`).expect(200)).body;
  assert.deepEqual(root.items.map((item) => item.name), ['docs', 'note.txt']);

  const part = await owner.agent.get(`${base}/file`).query({ path: 'note.txt' }).set('Range', 'bytes=2-4').buffer(true).parse((res, cb) => {
    const chunks = [];
    res.on('data', (c) => chunks.push(c));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  }).expect(206);
  assert.equal(part.body.toString(), '234');

  await owner.agent.put(`${base}/file`).query({ path: 'docs/new.txt' }).set('Content-Type', 'application/octet-stream').send(Buffer.from('dav upload')).expect(201);
  assert.equal(dav.files.get('docs/new.txt').toString(), 'dav upload');
  assert.ok(dav.requests.some((r) => r.method === 'PUT' && r.contentLength === '10'), 'the size is announced');
  await owner.agent.post(`${base}/folder`).send({ path: 'docs/sub' }).expect(201);
  await owner.agent.post(`${base}/move`).send({ from: 'docs/new.txt', to: 'docs/sub/renamed.txt' }).expect(200);
  assert.ok(dav.files.has('docs/sub/renamed.txt'));
  await owner.agent.post(`${base}/move`).send({ from: 'docs/a.md', to: 'note.txt' }).expect(409);
  await owner.agent.delete(`${base}/entry`).query({ path: 'docs/sub' }).expect(200);
  assert.ok(!dav.folders.has('docs/sub'));
});

test('copy between NeoCloud and a mount, in both directions', async () => {
  const file = (await upload(owner.agent, 'report.txt', 'quarterly numbers').expect(201)).body;
  const toMount = await owner.agent.post(`/api/external-storage/${davMount.id}/copy-in`).send({ nodeId: file.id, folder: 'docs' }).expect(201);
  assert.equal(toMount.body.path, 'docs/report.txt');
  assert.equal(dav.files.get('docs/report.txt').toString(), 'quarterly numbers');
  const second = await owner.agent.post(`/api/external-storage/${davMount.id}/copy-in`).send({ nodeId: file.id, folder: 'docs' }).expect(201);
  assert.equal(second.body.path, 'docs/report (1).txt');

  const out = await owner.agent.post(`/api/external-storage/${davMount.id}/copy-out`).send({ path: 'note.txt', parentId: 'root' }).expect(201);
  assert.equal(out.body.name, 'note.txt');
  const content = await owner.agent.get(`/api/files/${out.body.id}/content`).buffer(true).parse((res, cb) => {
    const chunks = [];
    res.on('data', (c) => chunks.push(c));
    res.on('end', () => cb(null, Buffer.concat(chunks)));
  }).expect(200);
  assert.equal(content.body.toString(), '0123456789');
  // The file stays at the provider, and external files never enter the tree.
  const listing = await owner.agent.get('/api/folders/root/children').expect(200);
  assert.deepEqual(listing.body.items.map((item) => item.name).sort(), ['note.txt', 'report.txt']);
  await owner.agent.post(`/api/external-storage/${davMount.id}/copy-out`).send({ path: 'docs', parentId: 'root' }).expect(400);
});

test('a read-only mount refuses every change but still reads', async () => {
  const id = davMount.id;
  const base = `/api/external-storage/${id}`;
  const patched = await owner.agent.patch(base).send({ readOnly: true }).expect(200);
  assert.equal(patched.body.readOnly, true);
  const attempts = [
    owner.agent.put(`${base}/file`).query({ path: 'x.txt' }).set('Content-Type', 'application/octet-stream').send(Buffer.from('x')),
    owner.agent.post(`${base}/folder`).send({ path: 'x' }),
    owner.agent.post(`${base}/move`).send({ from: 'note.txt', to: 'y.txt' }),
    owner.agent.delete(`${base}/entry`).query({ path: 'note.txt' }),
  ];
  for (const attempt of attempts) assert.equal((await attempt).body.code, 'EXTERNAL_READ_ONLY');
  await owner.agent.get(`${base}/list`).expect(200);
  assert.ok(dav.files.has('note.txt'));
  await owner.agent.patch(base).send({ readOnly: false }).expect(200);
});

test('editing keeps secrets that are left blank and tests changed settings', async () => {
  const edited = await owner.agent.patch(`/api/external-storage/${davMount.id}`).send({
    name: 'Renamed NAS', config: { url: davUrl, username: dav.username, password: '' },
  }).expect(200);
  assert.equal(edited.body.name, 'Renamed NAS');
  assert.equal(edited.body.config.passwordSet, true);
  await owner.agent.get(`/api/external-storage/${davMount.id}/list`).expect(200);
  await owner.agent.patch(`/api/external-storage/${davMount.id}`).send({ config: { url: davUrl, username: 'someone-else', password: '' } }).expect(422);
});

test('names are unique per account and the number of connections is capped', async () => {
  await owner.agent.post('/api/external-storage').send(davBody({ name: 'renamed nas' })).expect(409);
  for (let index = 0; index < 8; index += 1) await owner.agent.post('/api/external-storage').send(davBody({ name: `extra ${index}` })).expect(201);
  const over = await owner.agent.post('/api/external-storage').send(davBody({ name: 'one too many' })).expect(409);
  assert.equal(over.body.code, 'EXTERNAL_MOUNT_LIMIT');
});

test('the export lists connections without secrets; erasing the account removes them', async () => {
  const exported = JSON.stringify(exportAccount(owner.user.id).externalStorage);
  assert.ok(exported.includes('My bucket'));
  assert.ok(!exported.includes('super-secret-key'));
  assert.ok(!exported.includes(dav.password));
  assert.ok(db.prepare('SELECT COUNT(*) AS n FROM external_mounts WHERE user_id = ?').get(owner.user.id).n > 0);
  await eraseAccount(owner.user.id, { actorUserId: owner.user.id });
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM external_mounts').get().n, 0);
});
