'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const http = require('http');
const os = require('os');
const path = require('path');
const { Readable } = require('stream');

process.env.NEOCLOUD_OFFICE_PREVIEW = 'false';
// Restore refuses while something answers /api/health on PORT; keep a dev
// server on the default port out of it.
process.env.PORT = '1';
const { cleanup, home, registerUser, upload } = require('../helpers/app');
const sharp = require('sharp');
const db = require('../../server/db/database');
const runner = require('../../server/services/backup/runner');
const { restoreSnapshot } = require('../../server/services/backup/restore');
const { isDue } = require('../../server/services/backup/scheduler');
const { createWebdavAdapter } = require('../../server/services/backup/adapters/webdav');

const backupDir = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-backup-'));

test.after(() => {
  cleanup();
  fs.rmSync(backupDir, { recursive: true, force: true });
});

let owner;

test.before(async () => {
  owner = await registerUser('backup');
});

function binary(res, callback) {
  const chunks = [];
  res.on('data', (chunk) => chunks.push(chunk));
  res.on('end', () => callback(null, Buffer.concat(chunks)));
}

test('image thumbnails are rendered once, cached and served with an etag', async () => {
  const png = await sharp({ create: { width: 1600, height: 900, channels: 3, background: '#705331' } }).png().toBuffer();
  const file = (await upload(owner.agent, 'photo.png', png).expect(201)).body;

  const first = await owner.agent.get(`/api/files/${file.id}/thumbnail?size=256`).buffer(true).parse(binary).expect(200);
  assert.equal(first.headers['content-type'], 'image/webp');
  const meta = await sharp(first.body).metadata();
  assert.equal(meta.width, 256);
  assert.equal(meta.height, 144);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM previews').get().n, 1);

  await owner.agent.get(`/api/files/${file.id}/thumbnail?size=256`).set('If-None-Match', first.headers.etag).expect(304);
  await owner.agent.get(`/api/files/${file.id}/thumbnail?size=999`).expect(400);

  // Cached thumbnails are encrypted like blobs.
  const previewDir = path.join(home, 'data', 'storage', 'previews');
  const stored = fs.readdirSync(previewDir, { recursive: true }).map((p) => path.join(previewDir, p)).filter((p) => fs.statSync(p).isFile());
  assert.equal(fs.readFileSync(stored[0]).subarray(0, 4).toString(), 'NCB1');
});

test('files without a renderer say so', async () => {
  const text = (await upload(owner.agent, 'notes.txt', 'hello').expect(201)).body;
  const thumb = await owner.agent.get(`/api/files/${text.id}/thumbnail`).expect(415);
  assert.equal(thumb.body.code, 'PREVIEW_UNAVAILABLE');
  const docx = (await upload(owner.agent, 'brief.docx', 'not really a docx').expect(201)).body;
  const preview = await owner.agent.get(`/api/files/${docx.id}/preview`).expect(415);
  assert.equal(preview.body.code, 'PREVIEW_UNAVAILABLE');
  const caps = await owner.agent.get('/api/capabilities').expect(200);
  assert.deepEqual(caps.body, { imageThumbnails: true, officePreview: false, fullTextSearch: false });
});

test('backup targets keep their secrets to themselves', async () => {
  const created = await owner.agent.post('/api/admin/backups/targets').send({
    name: 'NAS',
    type: 'sftp',
    config: { host: 'nas.local', username: 'neo', password: 'hunter2-secret', path: '/backups' },
  }).expect(201);
  assert.equal(created.body.config.passwordSet, true);
  assert.equal(JSON.stringify(created.body).includes('hunter2'), false);
  const updated = await owner.agent.put(`/api/admin/backups/targets/${created.body.id}`).send({ config: { host: 'nas2.local', username: 'neo', path: '/backups' } }).expect(200);
  assert.equal(updated.body.config.passwordSet, true);
  assert.equal(updated.body.config.host, 'nas2.local');
  await owner.agent.post('/api/admin/backups/targets').send({ name: 'x', type: 'local', config: { path: 'relative/path' } }).expect(400);
  await owner.agent.delete(`/api/admin/backups/targets/${created.body.id}`).expect(200);
});

test('schedules come due once per slot', () => {
  const target = { id: 999999, enabled: 1, schedule: 'daily', hour: 3, weekday: 0 };
  assert.equal(isDue(target, new Date()), true);
  assert.equal(isDue({ ...target, schedule: 'manual' }), false);
  assert.equal(isDue({ ...target, enabled: 0 }), false);
});

test('local backup: incremental, encrypted, pruned and restorable', async () => {
  const target = (await owner.agent.post('/api/admin/backups/targets').send({
    name: 'Disk',
    type: 'local',
    config: { path: backupDir },
    retention: 1,
  }).expect(201)).body;
  await owner.agent.post(`/api/admin/backups/targets/${target.id}/test`).expect(200);

  const first = await runner.runBackup(target.id);
  assert.equal(first.status, 'ok', first.error);
  const blobCount = db.prepare('SELECT COUNT(*) AS n FROM blobs').get().n;
  assert.equal(first.blobsUploaded, blobCount);

  const snapshotDir = path.join(backupDir, 'snapshots', first.snapshot);
  const dbObject = fs.readFileSync(path.join(snapshotDir, 'neocloud.db.enc'));
  assert.equal(dbObject.subarray(0, 4).toString(), 'NCB1');
  assert.equal(dbObject.includes(Buffer.from('SQLite format')), false);
  assert.equal(dbObject.includes(Buffer.from(owner.username)), false);

  // Second run uploads only the new file; retention 1 drops the first snapshot.
  const extra = (await upload(owner.agent, 'later.txt', 'added after the first backup').expect(201)).body;
  await new Promise((resolve) => setTimeout(resolve, 1100));
  const second = await runner.runBackup(target.id);
  assert.equal(second.status, 'ok', second.error);
  assert.equal(second.blobsUploaded, 1);
  assert.equal(fs.existsSync(snapshotDir), false);
  assert.deepEqual((await runner.snapshotsOf(target.id)).snapshots, [second.snapshot]);

  // A file deleted for good disappears from the target after the next run.
  await owner.agent.delete(`/api/nodes/${extra.id}`).expect(200);
  await owner.agent.delete(`/api/trash/${extra.id}`).expect(200);
  await new Promise((resolve) => setTimeout(resolve, 1100));
  const third = await runner.runBackup(target.id);
  assert.equal(third.status, 'ok', third.error);
  const remoteBlobs = fs.readdirSync(path.join(backupDir, 'blobs'), { recursive: true }).filter((p) => p.includes('/') || p.includes('\\'));
  assert.equal(remoteBlobs.filter((p) => !fs.statSync(path.join(backupDir, 'blobs', p)).isDirectory()).length, blobCount);

  const runs = await owner.agent.get('/api/admin/backups').expect(200);
  assert.equal(runs.body.targets[0].lastRun.status, 'ok');

  // Restore: wipe the local database and blobs, then bring them back.
  const blobDir = path.join(home, 'data', 'storage', 'blobs');
  const usersBefore = db.prepare('SELECT username FROM users ORDER BY id').all();
  fs.rmSync(blobDir, { recursive: true, force: true });
  await restoreSnapshot(target.id, third.snapshot);
  const Database = require('better-sqlite3');
  const restored = new Database(path.join(home, 'data', 'neocloud.db'), { readonly: true });
  assert.deepEqual(restored.prepare('SELECT username FROM users ORDER BY id').all(), usersBefore);
  const blobRows = restored.prepare('SELECT id, user_id FROM blobs').all();
  for (const blob of blobRows) {
    assert.ok(fs.existsSync(path.join(blobDir, String(blob.user_id), blob.id.slice(0, 2), blob.id)));
  }
  restored.close();
});

// A minimal WebDAV server over a temp folder, enough to exercise the adapter.
function startWebdavServer(root) {
  const server = http.createServer(async (req, res) => {
    if (req.headers.authorization !== `Basic ${Buffer.from('dav:pw').toString('base64')}`) {
      res.writeHead(401).end();
      return;
    }
    const url = new URL(req.url, 'http://x');
    const target = path.join(root, decodeURIComponent(url.pathname));
    try {
      if (req.method === 'MKCOL') {
        if (fs.existsSync(target)) return res.writeHead(405).end();
        fs.mkdirSync(target);
        return res.writeHead(201).end();
      }
      if (req.method === 'PUT') {
        const chunks = [];
        for await (const chunk of req) chunks.push(chunk);
        fs.writeFileSync(target, Buffer.concat(chunks));
        return res.writeHead(201).end();
      }
      if (req.method === 'MOVE') {
        fs.renameSync(target, path.join(root, decodeURIComponent(new URL(req.headers.destination).pathname)));
        return res.writeHead(201).end();
      }
      if (req.method === 'GET') {
        if (!fs.existsSync(target)) return res.writeHead(404).end();
        return res.writeHead(200).end(fs.readFileSync(target));
      }
      if (req.method === 'DELETE') {
        fs.rmSync(target, { recursive: true, force: true });
        return res.writeHead(204).end();
      }
      if (req.method === 'PROPFIND') {
        if (!fs.existsSync(target)) return res.writeHead(404).end();
        const entries = req.headers.depth === '0' || !fs.statSync(target).isDirectory() ? [] : fs.readdirSync(target, { withFileTypes: true });
        const self = `<d:response><d:href>${url.pathname}</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>`;
        const items = entries.map((entry) => {
          const href = `${url.pathname.replace(/\/$/, '')}/${encodeURIComponent(entry.name)}${entry.isDirectory() ? '/' : ''}`;
          const type = entry.isDirectory() ? '<d:collection/>' : '';
          return `<d:response><d:href>${href}</d:href><d:propstat><d:prop><d:resourcetype>${type}</d:resourcetype></d:prop></d:propstat></d:response>`;
        }).join('');
        res.writeHead(207, { 'Content-Type': 'application/xml' });
        return res.end(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">${self}${items}</d:multistatus>`);
      }
      res.writeHead(405).end();
    } catch {
      res.writeHead(500).end();
    }
  });
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(server)));
}

test('the WebDAV adapter writes, lists, reads and deletes', async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-dav-'));
  fs.mkdirSync(path.join(root, 'share'));
  const server = await startWebdavServer(root);
  try {
    const adapter = createWebdavAdapter({ url: `http://127.0.0.1:${server.address().port}/share/backup`, username: 'dav', password: 'pw' });
    await adapter.open();
    await adapter.put('blobs/1/abc', () => Readable.from([Buffer.from('one')]));
    await adapter.put('snapshots/s1/manifest.json', () => Readable.from(['{}']));
    assert.deepEqual((await adapter.list('blobs/')).sort(), ['blobs/1/abc']);
    assert.deepEqual((await adapter.list('')).sort(), ['blobs/1/abc', 'snapshots/s1/manifest.json']);
    const chunks = [];
    for await (const chunk of await adapter.get('blobs/1/abc')) chunks.push(chunk);
    assert.equal(Buffer.concat(chunks).toString(), 'one');
    await adapter.delete('blobs/1/abc');
    await adapter.deletePrefix('snapshots/s1');
    assert.deepEqual(await adapter.list(''), []);
    await assert.rejects(createWebdavAdapter({ url: `http://127.0.0.1:${server.address().port}/share/x`, username: 'dav', password: 'nope' }).open());
  } finally {
    server.close();
    fs.rmSync(root, { recursive: true, force: true });
  }
});
