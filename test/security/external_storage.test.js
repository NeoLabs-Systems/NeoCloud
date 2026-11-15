'use strict';

// Cloud storage is the server connecting to hosts a person typed, with their
// credentials: it must not reach the server's own network, follow redirects
// with a password, leak secrets, leave the folder it was pointed at, or let
// one account use another's connection.

const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('http');

process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'true';
const { cleanup, registerUser } = require('../helpers/app');
const db = require('../../server/db/database');
const { createFakeWebdav } = require('../helpers/fake_webdav');
const { guardedLookup, resolveAllowed } = require('../../server/services/external_storage/network');
const { normalizePath } = require('../../server/services/external_storage/paths');
const { setUserDisabled } = require('../../server/services/admin/users');

const dav = createFakeWebdav();
let davUrl;
let admin;
let owner;
let other;
let mount;
const secretText = 'p4ss-never-leaves';

test.before(async () => {
  admin = await registerUser('admin');
  owner = await registerUser('owner');
  other = await registerUser('other');
  davUrl = await dav.start();
  dav.files.set('inside.txt', Buffer.from('inside'));
  mount = (await owner.agent.post('/api/external-storage').send({
    name: 'NAS', provider: 'webdav', config: { url: davUrl, username: dav.username, password: dav.password },
  }).expect(201)).body;
});

test.after(async () => {
  await dav.stop();
  cleanup();
});

const davConfig = (url) => ({ provider: 'webdav', name: 'x', config: { url, username: 'u', password: secretText } });

test('private and metadata addresses are refused unless the server allows private networks', async () => {
  process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'false';
  try {
    assert.equal(db.prepare('SELECT is_admin FROM users WHERE id = ?').get(owner.user.id).is_admin, 0);
    for (const url of [
      davUrl, 'http://127.0.0.1:1/', 'http://localhost/', 'http://[::1]/', 'http://[::ffff:127.0.0.1]/', 'http://10.0.0.5/',
      'http://192.168.1.2/', 'http://172.16.0.1/', 'http://169.254.169.254/latest/meta-data/', 'http://0.0.0.0/', 'http://100.100.100.200/',
    ]) {
      const res = await owner.agent.post('/api/external-storage/test').send(davConfig(url)).expect(200);
      assert.equal(res.body.ok, false, url);
      assert.equal(res.body.code, 'EXTERNAL_HOST_BLOCKED', url);
    }
    const s3 = await owner.agent.post('/api/external-storage/test').send({
      provider: 's3', name: 'x', config: { endpoint: 'http://127.0.0.1:9', bucket: 'bkt', accessKeyId: 'a', secretAccessKey: 's' },
    }).expect(200);
    assert.equal(s3.body.code, 'EXTERNAL_HOST_BLOCKED');
    const sftp = await owner.agent.post('/api/external-storage/test').send({
      provider: 'sftp', name: 'x', config: { host: '10.1.2.3', username: 'u', password: 'p' },
    }).expect(200);
    assert.equal(sftp.body.code, 'EXTERNAL_HOST_BLOCKED');
    const refused = await owner.agent.post('/api/external-storage').send(davConfig('http://127.0.0.1:1/')).expect(422);
    assert.equal(refused.body.code, 'EXTERNAL_TEST_FAILED');
    assert.equal((await owner.agent.get('/api/external-storage').expect(200)).body.items.length, 1);
    // The existing connection to a private address is refused when used, too.
    const listing = await owner.agent.get(`/api/external-storage/${mount.id}/list`);
    assert.equal(listing.status, 403);
    assert.equal(listing.body.code, 'EXTERNAL_HOST_BLOCKED');
  } finally {
    process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'true';
  }
});

test('the socket lookup refuses a name that resolves to a private address, and metadata stays refused when private is allowed', async () => {
  process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'false';
  try {
    const lookup = guardedLookup(owner.user.id);
    const err = await new Promise((resolve) => lookup('localhost', { all: false }, (e) => resolve(e)));
    assert.equal(err.code, 'EXTERNAL_HOST_BLOCKED');
  } finally {
    process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE = 'true';
  }
  await assert.rejects(resolveAllowed('169.254.169.254', owner.user.id), { code: 'EXTERNAL_HOST_BLOCKED' });
  await assert.rejects(resolveAllowed('::ffff:169.254.169.254', owner.user.id), { code: 'EXTERNAL_HOST_BLOCKED' });
  await resolveAllowed('127.0.0.1', owner.user.id);
});

test('a WebDAV server that redirects is not followed and gets no second request', async () => {
  const hits = [];
  const target = http.createServer((req, res) => {
    hits.push(req.headers.authorization || 'none');
    res.writeHead(207);
    res.end();
  });
  await new Promise((resolve) => target.listen(0, '127.0.0.1', resolve));
  dav.redirectTo(`http://127.0.0.1:${target.address().port}/stolen/`);
  try {
    const res = await owner.agent.post('/api/external-storage/test').send({
      provider: 'webdav', name: 'x', config: { url: davUrl, username: dav.username, password: dav.password },
    }).expect(200);
    assert.equal(res.body.ok, false);
    assert.equal(res.body.code, 'EXTERNAL_REDIRECT');
    assert.deepEqual(hits, []);
  } finally {
    dav.redirectTo(null);
    await new Promise((resolve) => target.close(resolve));
  }
});

test('paths are normalised and anything that climbs, or is not a path, is refused', async () => {
  assert.equal(normalizePath('/a//b/'), 'a/b');
  assert.equal(normalizePath(''), '');
  assert.equal(normalizePath(undefined), '');
  for (const bad of ['..', '../x', 'a/../..', 'a/./b', 'a\\..\\b', 'a\u0000b', 'a/../../etc', ['a'], 'x'.repeat(3000)]) {
    assert.throws(() => normalizePath(bad), { code: 'INVALID_PATH' }, JSON.stringify(bad));
  }
  const base = `/api/external-storage/${mount.id}`;
  for (const bad of ['../outside.txt', 'a/../../outside.txt', '..']) {
    assert.equal((await owner.agent.get(`${base}/list`).query({ path: bad })).body.code, 'INVALID_PATH');
    assert.equal((await owner.agent.get(`${base}/file`).query({ path: bad })).body.code, 'INVALID_PATH');
    assert.equal((await owner.agent.put(`${base}/file`).query({ path: bad }).set('Content-Type', 'application/octet-stream').send(Buffer.from('x'))).body.code, 'INVALID_PATH');
    assert.equal((await owner.agent.post(`${base}/folder`).send({ path: bad })).body.code, 'INVALID_PATH');
    assert.equal((await owner.agent.post(`${base}/move`).send({ from: 'inside.txt', to: bad })).body.code, 'INVALID_PATH');
    assert.equal((await owner.agent.delete(`${base}/entry`).query({ path: bad })).body.code, 'INVALID_PATH');
  }
  assert.ok(dav.files.has('inside.txt'));
  // The root itself can be neither deleted nor moved, and nothing moves into itself.
  assert.equal((await owner.agent.delete(`${base}/entry`).query({ path: '' })).status, 400);
  assert.equal((await owner.agent.post(`${base}/move`).send({ from: 'inside.txt', to: 'inside.txt/x' })).status, 400);
});

test('secrets are never returned and never stored in the clear', async () => {
  const created = await owner.agent.post('/api/external-storage').send({
    name: 'Bucket', provider: 's3', config: { bucket: 'bkt', accessKeyId: 'AKIAEXAMPLE', secretAccessKey: secretText, region: 'eu-central-1' },
  });
  // The connection test fails (nothing answers), so nothing is stored; read what an edit returns instead.
  assert.ok(!JSON.stringify(created.body).includes(secretText));
  const all = JSON.stringify((await owner.agent.get('/api/external-storage').expect(200)).body);
  assert.ok(!all.includes(dav.password));
  assert.ok(all.includes('"passwordSet":true'));
  const edited = JSON.stringify((await owner.agent.patch(`/api/external-storage/${mount.id}`).send({ name: 'NAS', config: { url: davUrl, username: dav.username, password: '' } }).expect(200)).body);
  assert.ok(!edited.includes(dav.password));
  const row = db.prepare('SELECT * FROM external_mounts WHERE id = ?').get(mount.id);
  assert.ok(!JSON.stringify(row).includes(dav.password));
  assert.ok(!row.config_enc.includes(davUrl));
  // A row moved to another account cannot be opened: the key is bound to the account and the mount.
  db.prepare('UPDATE external_mounts SET user_id = ? WHERE id = ?').run(other.user.id, mount.id);
  try {
    assert.throws(() => require('../../server/services/external_storage/mounts').readConfig(db.prepare('SELECT * FROM external_mounts WHERE id = ?').get(mount.id)));
  } finally {
    db.prepare('UPDATE external_mounts SET user_id = ? WHERE id = ?').run(owner.user.id, mount.id);
  }
});

test('another account’s connection is a 404 everywhere and stays untouched', async () => {
  const base = `/api/external-storage/${mount.id}`;
  const attempts = [
    other.agent.get(`${base}/list`),
    other.agent.get(`${base}/file`).query({ path: 'inside.txt' }),
    other.agent.put(`${base}/file`).query({ path: 'x.txt' }).set('Content-Type', 'application/octet-stream').send(Buffer.from('x')),
    other.agent.post(`${base}/folder`).send({ path: 'x' }),
    other.agent.post(`${base}/move`).send({ from: 'inside.txt', to: 'y.txt' }),
    other.agent.delete(`${base}/entry`).query({ path: 'inside.txt' }),
    other.agent.patch(base).send({ name: 'mine now' }),
    other.agent.delete(base),
    other.agent.post('/api/external-storage/test').send({ id: mount.id, provider: 'webdav', config: { url: davUrl, username: 'u' } }),
  ];
  for (const attempt of attempts) assert.equal((await attempt).status, 404);
  assert.deepEqual((await other.agent.get('/api/external-storage').expect(200)).body.items, []);
  assert.ok(dav.files.has('inside.txt'));
  assert.equal(db.prepare('SELECT name FROM external_mounts WHERE id = ?').get(mount.id).name, 'NAS');
});

test('connections need a session', async () => {
  const { request, app } = require('../helpers/app');
  await request(app).get('/api/external-storage').expect(401);
  await request(app).get(`/api/external-storage/${mount.id}/list`).expect(401);
});

test('uploads over the size limit are refused and leave nothing behind', async () => {
  process.env.NEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES = '5';
  try {
    const res = await owner.agent.put(`/api/external-storage/${mount.id}/file`).query({ path: 'big.bin' })
      .set('Content-Type', 'application/octet-stream').send(Buffer.alloc(100));
    assert.equal(res.status, 413);
    assert.ok(!dav.files.has('big.bin'));
  } finally {
    delete process.env.NEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES;
  }
});

test('files that can run script are never shown inline', async () => {
  dav.files.set('page.html', Buffer.from('<script>alert(1)</script>'));
  dav.files.set('pic.svg', Buffer.from('<svg onload="alert(1)"/>'));
  dav.files.set('pic.png', Buffer.from('png'));
  for (const name of ['page.html', 'pic.svg']) {
    const res = await owner.agent.get(`/api/external-storage/${mount.id}/file`).query({ path: name }).expect(200);
    assert.match(res.headers['content-disposition'], /^attachment/);
    assert.match(res.headers['content-security-policy'], /sandbox/);
    assert.equal(res.headers['x-content-type-options'], 'nosniff');
  }
  const picture = await owner.agent.get(`/api/external-storage/${mount.id}/file`).query({ path: 'pic.png' }).expect(200);
  assert.match(picture.headers['content-disposition'], /^inline/);
});

test('copying a vault file out, or into a vault, is refused', async () => {
  const { upload } = require('../helpers/app');
  const file = (await upload(owner.agent, 'v.txt', 'v').expect(201)).body;
  db.prepare("UPDATE nodes SET vault_id = 'fake-vault' WHERE id = ?").run(file.id);
  const res = await owner.agent.post(`/api/external-storage/${mount.id}/copy-in`).send({ nodeId: file.id, folder: '' });
  assert.ok([404, 409].includes(res.status), String(res.status));
  assert.ok(!dav.files.has('v.txt'));
});

test('disabling an account removes its connections', async () => {
  assert.ok(db.prepare('SELECT COUNT(*) AS n FROM external_mounts WHERE user_id = ?').get(owner.user.id).n > 0);
  setUserDisabled(owner.user.id, true, { actorUserId: admin.user.id });
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM external_mounts WHERE user_id = ?').get(owner.user.id).n, 0);
});
