'use strict';

// The fake Nextcloud listens on localhost; plain accounts may import from it here.
process.env.NEOCLOUD_IMPORT_ALLOW_PRIVATE = 'true';

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser } = require('../helpers/app');
const { createFakeNextcloud } = require('../helpers/fake_nextcloud');
const db = require('../../server/db/database');
const runner = require('../../server/services/import/runner');

let nextcloud;
let owner;

const ALICE_FILES = {
  'readme.txt': { content: 'hello from nextcloud', mtime: '2025-12-24 18:30:00' },
  'Fotos/Übersicht ü.txt': { content: 'umlauts', mtime: '2024-02-29 08:15:00', favorite: true },
  'Fotos/2026/日本/写真 #1.jpg': { content: 'jpeg bytes', mtime: '2026-01-02 03:04:05' },
  'Документы/отчёт 2026.md': { content: '# отчёт', mtime: '2023-06-01 10:00:00' },
  'Документы/a&b + c%.txt': { content: 'escaping', mtime: '2023-06-02 10:00:00' },
  // Decomposed (NFD) on the server, as macOS clients upload it.
  'Café 🎉.txt': { content: 'party', mtime: '2022-01-01 00:00:00' },
};

const CAROL_FILES = {
  'a.bin': { content: 'a'.repeat(4000), mtime: '2026-05-01 00:00:00' },
  'b.bin': { content: 'b'.repeat(4000), mtime: '2026-05-01 00:00:00' },
  'c.bin': { content: 'c'.repeat(4000), mtime: '2026-05-01 00:00:00' },
  'd.bin': { content: 'd'.repeat(4000), mtime: '2026-05-01 00:00:00' },
};

test.before(async () => {
  nextcloud = await createFakeNextcloud({
    alice: {
      password: 'alice-app-pass',
      files: ALICE_FILES,
      folders: {
        Fotos: { mtime: '2024-03-01 09:00:00', favorite: true },
        Leer: { mtime: '2021-07-07 07:07:07' },
      },
    },
    bob: {
      password: 'bob-app-pass',
      files: {
        'good.txt': { content: 'fine', mtime: '2026-01-01 00:00:00' },
        'blocked.exe': { content: 'quarantined', mtime: '2026-01-01 00:00:00' },
        'flaky.txt': { content: 'second try', mtime: '2026-01-01 00:00:00' },
      },
    },
    carol: { password: 'carol-app-pass', files: CAROL_FILES },
    dave: { password: 'dave-app-pass', files: { 'big.bin': { content: 'x'.repeat(5000), mtime: '2026-01-01 00:00:00' } } },
  });
  owner = await registerUser('importer');
});

test.after(async () => {
  await runner.stopImports();
  await nextcloud.close();
  cleanup();
});

function connection(username, extra = {}) {
  return { source: 'nextcloud', url: nextcloud.url, username, password: `${username}-app-pass`, ...extra };
}

async function waitFor(agent, id, statuses, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const res = await agent.get(`/api/imports/${id}`).expect(200);
    if (statuses.includes(res.body.status)) return res.body;
    if (Date.now() > deadline) throw new Error(`import stayed ${res.body.status}: ${JSON.stringify(res.body)}`);
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

async function waitUntil(check, timeoutMs = 10000) {
  const deadline = Date.now() + timeoutMs;
  while (!(await check())) {
    if (Date.now() > deadline) throw new Error('condition not reached');
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

function storedPassword(id) {
  return db.prepare('SELECT password_enc FROM import_jobs WHERE id = ?').get(id).password_enc;
}

async function childrenByName(agent, folderId) {
  const res = await agent.get(`/api/folders/${folderId}/children`).expect(200);
  return new Map(res.body.items.map((item) => [item.name, item]));
}

function gate() {
  let release;
  const hold = new Promise((resolve) => {
    release = resolve;
  });
  return { hold, release };
}

test('imports need a session', async () => {
  const { request, app } = require('../helpers/app');
  await request(app).get('/api/imports').expect(401);
  await request(app).post('/api/imports/test').send(connection('alice')).expect(401);
});

test('test: wrong password answers ok false, the right one counts the root', async () => {
  const refused = await owner.agent.post('/api/imports/test').send({ ...connection('alice'), password: 'wrong' }).expect(200);
  assert.equal(refused.body.ok, false);
  assert.equal(refused.body.code, 'REMOTE_AUTH_FAILED');
  assert.match(refused.body.error, /user name or app password/);

  const ok = await owner.agent.post('/api/imports/test').send(connection('alice')).expect(200);
  assert.equal(ok.body.ok, true);
  // readme, Café, Fotos, Документы, Leer
  assert.equal(ok.body.files, 5);
  const total = Object.values(ALICE_FILES).reduce((sum, file) => sum + Buffer.byteLength(file.content), 0);
  assert.equal(ok.body.bytes, total);

  // A moved install: redirects are followed with the credentials kept.
  const moved = await owner.agent.post('/api/imports/test').send(connection('alice', { url: `${nextcloud.url}/old/index.php/apps/files` })).expect(200);
  assert.equal(moved.body.ok, true);

  await owner.agent.post('/api/imports/test').send(connection('alice', { url: 'ftp://example.com' })).expect(400);
  await owner.agent.post('/api/imports/test').send(connection('alice', { sourcePath: '/a/../b' })).expect(400);
});

let aliceTarget;

test('imports a nested unicode tree with dates and favorites, then forgets the password', async () => {
  const folder = await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'From Nextcloud' }).expect(201);
  aliceTarget = folder.body.id;
  const created = await owner.agent.post('/api/imports').send(connection('alice', { targetFolderId: aliceTarget })).expect(201);
  assert.equal(created.body.targetPath, '/From Nextcloud');
  assert.ok(!('password' in created.body));

  const job = await waitFor(owner.agent, created.body.id, ['done', 'failed']);
  assert.equal(job.status, 'done', JSON.stringify(job.errors));
  assert.equal(job.totalFiles, 6);
  assert.equal(job.doneFiles, 6);
  assert.equal(job.skippedFiles, 0);
  assert.equal(job.failedFiles, 0);
  assert.equal(job.doneBytes, job.totalBytes);
  assert.ok(job.finishedAt);
  assert.equal(storedPassword(job.id), null);

  const top = await childrenByName(owner.agent, aliceTarget);
  assert.deepEqual([...top.keys()].sort(), ['Café 🎉.txt', 'Fotos', 'Leer', 'readme.txt', 'Документы'].sort());
  assert.equal(top.get('readme.txt').modifiedAt, '2025-12-24 18:30:00');
  assert.equal(top.get('Leer').type, 'folder');
  assert.equal(top.get('Leer').modifiedAt, '2021-07-07 07:07:07');
  assert.equal(top.get('Fotos').modifiedAt, '2024-03-01 09:00:00');
  assert.equal(top.get('Fotos').favorite, true);

  const readme = await owner.agent.get(`/api/files/${top.get('readme.txt').id}/content`).expect(200);
  assert.equal(readme.text, 'hello from nextcloud');

  const fotos = await childrenByName(owner.agent, top.get('Fotos').id);
  assert.equal(fotos.get('Übersicht ü.txt').favorite, true);
  assert.equal(fotos.get('Übersicht ü.txt').modifiedAt, '2024-02-29 08:15:00');
  const japan = await childrenByName(owner.agent, (await childrenByName(owner.agent, fotos.get('2026').id)).get('日本').id);
  const photo = await owner.agent.get(`/api/files/${japan.get('写真 #1.jpg').id}/content`).expect(200);
  assert.equal(photo.body.toString(), 'jpeg bytes');

  const docs = await childrenByName(owner.agent, top.get('Документы').id);
  assert.equal(docs.get('a&b + c%.txt').size, 8);
  assert.equal(docs.get('отчёт 2026.md').modifiedAt, '2023-06-01 10:00:00');

  const favorites = await owner.agent.get('/api/favorites').expect(200);
  assert.deepEqual(favorites.body.items.map((item) => item.name).sort(), ['Fotos', 'Übersicht ü.txt']);
});

test('running it again skips everything and downloads nothing', async () => {
  const before = new Map(nextcloud.gets);
  const created = await owner.agent.post('/api/imports').send(connection('alice', { targetFolderId: aliceTarget })).expect(201);
  const job = await waitFor(owner.agent, created.body.id, ['done', 'failed']);
  assert.equal(job.status, 'done');
  assert.equal(job.doneFiles, 6);
  assert.equal(job.skippedFiles, 6);
  assert.deepEqual(nextcloud.gets, before);
  const top = await childrenByName(owner.agent, aliceTarget);
  assert.equal(top.size, 5);
});

test('one bad file does not stop the job; a 503 is retried', async () => {
  nextcloud.behaviour['blocked.exe'] = 'forbidden';
  nextcloud.behaviour['flaky.txt'] = 'flaky';
  const created = await owner.agent.post('/api/imports').send(connection('bob')).expect(201);
  assert.equal(created.body.targetPath, '/Nextcloud');
  const job = await waitFor(owner.agent, created.body.id, ['done', 'failed']);
  assert.equal(job.status, 'done');
  assert.equal(job.doneFiles, 2);
  assert.equal(job.failedFiles, 1);
  assert.deepEqual(job.errors, [{ path: '/blocked.exe', message: 'The server denied access.' }]);
  assert.equal(nextcloud.gets.get('flaky.txt'), 2);
  const files = await childrenByName(owner.agent, job.targetFolderId);
  assert.deepEqual([...files.keys()].sort(), ['flaky.txt', 'good.txt']);
  assert.equal(storedPassword(job.id), null);
});

test('an import that cannot fit the quota fails up front', async () => {
  const user = await registerUser('quota');
  db.prepare('UPDATE users SET quota_bytes = 1000 WHERE id = ?').run(user.user.id);
  const created = await user.agent.post('/api/imports').send(connection('dave')).expect(201);
  const job = await waitFor(user.agent, created.body.id, ['done', 'failed']);
  assert.equal(job.status, 'failed');
  assert.match(job.errors[0].message, /Not enough storage/);
  assert.equal(nextcloud.gets.get('big.bin'), undefined);
  assert.equal(storedPassword(job.id), null);
});

test('cancel stops the download, wipes the password; only ended jobs can be deleted', async () => {
  const user = await registerUser('cancel');
  const { hold, release } = gate();
  nextcloud.behaviour['a.bin'] = { hold };
  nextcloud.behaviour['b.bin'] = { hold };
  const created = await user.agent.post('/api/imports').send(connection('carol')).expect(201);
  await waitUntil(() => nextcloud.gets.get('a.bin') >= 1 && nextcloud.gets.get('b.bin') >= 1);
  await user.agent.delete(`/api/imports/${created.body.id}`).expect(409);

  const cancelled = await user.agent.post(`/api/imports/${created.body.id}/cancel`).expect(200);
  assert.equal(cancelled.body.status, 'cancelled');
  assert.equal(storedPassword(created.body.id), null);
  release();
  await new Promise((resolve) => setTimeout(resolve, 100));
  const job = await waitFor(user.agent, created.body.id, ['cancelled']);
  assert.equal(job.doneFiles, 0);
  assert.equal(nextcloud.gets.get('c.bin'), undefined);

  // Without the password an ended import can't go on.
  const again = await user.agent.post(`/api/imports/${created.body.id}/resume`).expect(400);
  assert.equal(again.body.code, 'PASSWORD_REQUIRED');
  await user.agent.delete(`/api/imports/${created.body.id}`).expect(200);
  await user.agent.get(`/api/imports/${created.body.id}`).expect(404);
  delete nextcloud.behaviour['a.bin'];
  delete nextcloud.behaviour['b.bin'];
});

test('pause keeps the password and resume finishes the job', async () => {
  const user = await registerUser('pause');
  const { hold, release } = gate();
  nextcloud.behaviour['a.bin'] = { hold };
  const created = await user.agent.post('/api/imports').send(connection('carol')).expect(201);
  const startGets = nextcloud.gets.get('a.bin') || 0;
  await waitUntil(() => (nextcloud.gets.get('a.bin') || 0) > startGets);
  const paused = await user.agent.post(`/api/imports/${created.body.id}/pause`).expect(200);
  assert.equal(paused.body.status, 'paused');
  assert.ok(storedPassword(created.body.id));
  release();
  delete nextcloud.behaviour['a.bin'];

  await user.agent.post(`/api/imports/${created.body.id}/resume`).expect(200);
  const job = await waitFor(user.agent, created.body.id, ['done', 'failed']);
  assert.equal(job.status, 'done');
  assert.equal(job.doneFiles, 4);
  assert.equal(storedPassword(job.id), null);
});

test('a job interrupted by a restart resumes without fetching finished files again', async () => {
  const user = await registerUser('restart');
  const { hold, release } = gate();
  nextcloud.behaviour['c.bin'] = { hold };
  const before = Object.fromEntries(Object.keys(CAROL_FILES).map((name) => [name, nextcloud.gets.get(name) || 0]));
  const created = await user.agent.post('/api/imports').send(connection('carol')).expect(201);
  await waitUntil(async () => {
    const res = await user.agent.get(`/api/imports/${created.body.id}`);
    return res.body.doneFiles === 3 && nextcloud.gets.get('c.bin') > before['c.bin'];
  });

  await runner.stopImports();
  assert.equal(db.prepare('SELECT status FROM import_jobs WHERE id = ?').get(created.body.id).status, 'running');
  delete nextcloud.behaviour['c.bin'];
  release();

  runner.startImports();
  const job = await waitFor(user.agent, created.body.id, ['done', 'failed']);
  assert.equal(job.status, 'done');
  assert.equal(job.doneFiles, 4);
  for (const name of ['a.bin', 'b.bin', 'd.bin']) assert.equal(nextcloud.gets.get(name) - before[name], 1, name);
  assert.equal(nextcloud.gets.get('c.bin') - before['c.bin'], 2);
  const files = await childrenByName(user.agent, job.targetFolderId);
  const c = await user.agent.get(`/api/files/${files.get('c.bin').id}/content`).expect(200);
  assert.equal(c.body.toString(), CAROL_FILES['c.bin'].content);
});

test('another account cannot see or touch an import', async () => {
  const stranger = await registerUser('stranger');
  const { body } = await owner.agent.get('/api/imports').expect(200);
  assert.ok(body.items.length >= 3);
  const id = body.items[0].id;
  await stranger.agent.get(`/api/imports/${id}`).expect(404);
  await stranger.agent.post(`/api/imports/${id}/cancel`).expect(404);
  await stranger.agent.delete(`/api/imports/${id}`).expect(404);
  const theirs = await stranger.agent.get('/api/imports').expect(200);
  assert.deepEqual(theirs.body.items, []);
  await stranger.agent.post('/api/imports').send(connection('alice', { targetFolderId: aliceTarget })).expect(404);
});

test('imports from another NeoCloud over its Nextcloud-compatible WebDAV', async () => {
  const http = require('http');
  const { app, upload } = require('../helpers/app');
  const server = http.createServer(app);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  try {
    const source = await registerUser('davsource');
    const docs = await source.agent.post('/api/folders').send({ parentId: 'root', name: 'Docs ä' }).expect(201);
    const note = await upload(source.agent, 'note #1.txt', 'from the other neocloud', { parentId: docs.body.id }).expect(201);
    await source.agent.put(`/api/nodes/${note.body.id}/favorite`).expect(200);
    db.prepare("UPDATE nodes SET modified_at = '2024-04-04 04:04:04' WHERE id = ?").run(note.body.id);
    const device = await source.agent.post('/api/account/devices').send({ name: 'import', platform: 'webdav' }).expect(201);

    const target = await registerUser('davtarget');
    const body = {
      source: 'nextcloud',
      url: `http://127.0.0.1:${server.address().port}`,
      username: source.username,
      password: device.body.token,
    };
    const tested = await target.agent.post('/api/imports/test').send(body).expect(200);
    assert.equal(tested.body.ok, true, JSON.stringify(tested.body));
    const created = await target.agent.post('/api/imports').send(body).expect(201);
    const job = await waitFor(target.agent, created.body.id, ['done', 'failed']);
    assert.equal(job.status, 'done', JSON.stringify(job.errors));
    assert.equal(job.doneFiles, 1);

    const folder = (await childrenByName(target.agent, job.targetFolderId)).get('Docs ä');
    const file = (await childrenByName(target.agent, folder.id)).get('note #1.txt');
    assert.equal(file.modifiedAt, '2024-04-04 04:04:04');
    assert.equal(file.favorite, true);
    const content = await target.agent.get(`/api/files/${file.id}/content`).expect(200);
    assert.equal(content.text, 'from the other neocloud');
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});
