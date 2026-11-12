'use strict';

// Regressions from the stability pass: inputs that used to answer 500 (or
// crash the process), races between concurrent requests, and cleanup.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const http = require('http');
const path = require('path');
const sharp = require('sharp');
const { app, cleanup, home, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { blobPath } = require('../../server/services/storage/blob_store');
const { markInterruptedRuns } = require('../../server/services/backup/runner');
const { purgeEndedSessions } = require('../../server/services/account/sessions');

test.after(cleanup);

let admin;
let member;

test.before(async () => {
  admin = await registerUser('admin');
  member = await registerUser('member');
});

function vaultParams() {
  return {
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  };
}

async function liveChildren(agent, parentId) {
  return (await agent.get(`/api/folders/${parentId}/children`).expect(200)).body.items;
}

test('a thumbnail of an image whose blob is missing answers 415 instead of ending the process', async () => {
  const png = await sharp({ create: { width: 64, height: 64, channels: 3, background: '#336699' } }).png().toBuffer();
  const file = (await upload(member.agent, 'gone.png', png).expect(201)).body;
  const { blob_id: blobId } = db.prepare('SELECT blob_id FROM nodes WHERE id = ?').get(file.id);
  fs.rmSync(blobPath(member.user.id, blobId));
  const res = await member.agent.get(`/api/files/${file.id}/thumbnail`).expect(415);
  assert.equal(res.body.code, 'PREVIEW_UNAVAILABLE');
  // Still serving.
  await request(app).get('/api/health').expect(200);
});

test('copying a folder leaves a vault inside it behind', async () => {
  const outer = (await member.agent.post('/api/folders').send({ name: 'Outer' }).expect(201)).body;
  await upload(member.agent, 'plain.txt', 'plain', { parentId: outer.id }).expect(201);
  const vault = (await member.agent.post('/api/vaults').send({ parentId: outer.id, name: 'Secret', ...vaultParams() }).expect(201)).body;
  await upload(member.agent, 'cipher.bin', 'ciphertext', { parentId: vault.folderId }).expect(201);

  const copy = (await member.agent.post(`/api/nodes/${outer.id}/copy`).send({ parentId: 'root' }).expect(201)).body;
  const names = (await liveChildren(member.agent, copy.id)).map((item) => item.name);
  assert.deepEqual(names, ['plain.txt']);
  const leaked = db.prepare(`
    SELECT COUNT(*) AS count FROM nodes WHERE user_id = ? AND name IN ('Secret', 'cipher.bin') AND vault_id IS NULL
  `).get(member.user.id).count;
  assert.equal(leaked, 0);
});

test('settings refuse inherited object keys with 400', async () => {
  for (const key of ['constructor', 'toString', 'hasOwnProperty']) {
    const res = await member.agent.put('/api/settings').send({ [key]: 1 }).expect(400);
    assert.equal(res.body.code, 'UNKNOWN_SETTING');
  }
});

test('paging numbers SQLite cannot bind are clamped, not a 500', async () => {
  await member.agent.get('/api/sync/changes?limit=1.5&cursor=1e30').expect(200);
  await member.agent.get('/api/sync/changes?cursor=-4&limit=abc').expect(200);
  await member.agent.get('/api/folders/root/children?offset=99999999999999999999').expect(200);
});

test('HEAD on a public folder ZIP does not use up a download', async () => {
  const folder = (await member.agent.post('/api/folders').send({ name: 'Zipped' }).expect(201)).body;
  await upload(member.agent, 'a.txt', 'a', { parentId: folder.id }).expect(201);
  const share = (await member.agent.post('/api/shares').send({ nodeId: folder.id, maxDownloads: 1 }).expect(201)).body;
  const head = await request(app).head(`/api/public/shares/${share.token}/zip`).expect(200);
  assert.equal(head.headers['content-type'], 'application/zip');
  await request(app).head(`/api/public/shares/${share.token}/zip`).expect(200);
  await request(app).get(`/api/public/shares/${share.token}/zip`).expect(200);
  const exhausted = await request(app).get(`/api/public/shares/${share.token}/zip`).expect(410);
  assert.equal(exhausted.body.code, 'SHARE_EXHAUSTED');
});

async function readyUpload(agent, name, data) {
  const session = (await agent.post('/api/uploads').send({ name, size: data.length }).expect(201)).body;
  await agent.put(`/api/uploads/${session.id}/parts/0`).set('Content-Type', 'application/octet-stream').send(data).expect(200);
  return session;
}

test('completing a resumable upload twice at once gives one file', async () => {
  const data = crypto.randomBytes(256 * 1024);
  const session = await readyUpload(member.agent, 'twice.bin', data);
  const results = await Promise.all([
    member.agent.post(`/api/uploads/${session.id}/complete`).send({}),
    member.agent.post(`/api/uploads/${session.id}/complete`).send({}),
  ]);
  const statuses = results.map((res) => res.status).sort();
  assert.equal(statuses[0], 201);
  assert.ok([404, 409].includes(statuses[1]), `second complete answered ${statuses[1]}`);
  assert.ok(['UPLOAD_COMPLETING', 'UPLOAD_NOT_FOUND'].includes(results.find((res) => res.status !== 201).body.code));
  const copies = (await liveChildren(member.agent, 'root')).filter((item) => item.name.startsWith('twice'));
  assert.equal(copies.length, 1);
  assert.equal(fs.existsSync(path.join(home, 'data', 'storage', 'tmp', 'parts', session.id)), false);
});

test('a part that arrives after its upload was cancelled is refused and not kept', async () => {
  const data = crypto.randomBytes(1024);
  const session = (await member.agent.post('/api/uploads').send({ name: 'late.bin', size: data.length }).expect(201)).body;
  const { token } = (await member.agent.post('/api/account/devices').send({ name: 'cli' }).expect(201)).body;
  const server = app.listen(0);
  try {
    const response = new Promise((resolve, reject) => {
      const req = http.request({
        host: '127.0.0.1',
        port: server.address().port,
        method: 'PUT',
        path: `/api/uploads/${session.id}/parts/0`,
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/octet-stream', 'Content-Length': String(data.length) },
      }, (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => resolve({ status: res.statusCode, body: JSON.parse(Buffer.concat(chunks).toString('utf8')) }));
      });
      req.on('error', reject);
      req.write(data.subarray(0, 10));
      // The part is on its way when the upload is cancelled.
      setTimeout(async () => {
        await member.agent.delete(`/api/uploads/${session.id}`).expect(200);
        req.end(data.subarray(10));
      }, 100);
    });
    const res = await response;
    assert.equal(res.status, 404);
    assert.equal(res.body.code, 'UPLOAD_NOT_FOUND');
  } finally {
    server.close();
  }
  assert.equal(db.prepare('SELECT COUNT(*) AS count FROM upload_parts WHERE upload_id = ?').get(session.id).count, 0);
  assert.equal(fs.existsSync(path.join(home, 'data', 'storage', 'tmp', 'parts', session.id, '0')), false);
});

test('admin flag and password reset take only well-typed values', async () => {
  const id = member.user.id;
  for (const body of [{}, { isAdmin: 'true' }, { isAdmin: 1 }]) {
    const res = await admin.agent.put(`/api/admin/users/${id}/admin`).send(body).expect(400);
    assert.equal(res.body.code, 'INVALID_VALUE');
  }
  await admin.agent.put(`/api/admin/users/${id}/admin`).send({ isAdmin: true }).expect(200);
  // A malformed request must not have demoted the other admin.
  await admin.agent.put(`/api/admin/users/${id}/admin`).send({}).expect(400);
  assert.equal(db.prepare('SELECT is_admin FROM users WHERE id = ?').get(id).is_admin, 1);
  await admin.agent.put(`/api/admin/users/${id}/admin`).send({ isAdmin: false }).expect(200);

  for (const password of [{}, ['Plum-Orbit-73!quartz'], 12345678901]) {
    const res = await admin.agent.post(`/api/admin/users/${id}/password`).send({ password }).expect(400);
    assert.equal(res.body.code, 'MISSING_FIELDS');
  }
  const change = await member.agent.put('/api/account/password').send({ currentPassword: 'x', newPassword: { a: 1 } }).expect(400);
  assert.equal(change.body.code, 'MISSING_FIELDS');
  const register = await request(app).post('/api/auth/register')
    .send({ username: 'objpass', email: 'objpass@example.com', password: { toString: 'x' } }).expect(400);
  assert.equal(register.body.code, 'MISSING_FIELDS');
});

test('backup targets: bad config, unknown target and a run held by another process', async () => {
  const nullConfig = await admin.agent.post('/api/admin/backups/targets').send({ name: 'x', type: 'local', config: null }).expect(400);
  assert.equal(typeof nullConfig.body.code, 'string');
  const listConfig = await admin.agent.post('/api/admin/backups/targets').send({ name: 'x', type: 'local', config: ['a'] }).expect(400);
  assert.equal(listConfig.body.code, 'BACKUP_CONFIG_INVALID');

  const missing = await admin.agent.post('/api/admin/backups/targets/999999/run').expect(404);
  assert.equal(missing.body.code, 'NOT_FOUND');
  await admin.agent.get('/api/admin/backups/targets/999999/snapshots').expect(404);

  const backupDir = path.join(home, 'stability-backups');
  const target = (await admin.agent.post('/api/admin/backups/targets').send({
    name: 'Disk', type: 'local', config: { path: backupDir },
  }).expect(201)).body;
  // `neocloud backup run` in another process holds the target.
  db.prepare("INSERT INTO backup_runs (target_id, status, snapshot) VALUES (?, 'running', 'cli')").run(target.id);
  const busy = await admin.agent.post(`/api/admin/backups/targets/${target.id}/run`).expect(409);
  assert.equal(busy.body.code, 'BACKUP_RUNNING');
  db.prepare("UPDATE backup_runs SET status = 'failed' WHERE target_id = ?").run(target.id);
  await admin.agent.post(`/api/admin/backups/targets/${target.id}/run`).expect(202);
});

test('startup removes database copies a killed backup left behind', () => {
  const tmp = path.join(home, 'data', 'storage', 'tmp');
  fs.mkdirSync(tmp, { recursive: true });
  const stale = path.join(tmp, 'backup-1-2020-01-01T00-00-00Z.db');
  const staleVerify = path.join(tmp, 'verify-1-1700000000000.enc.db');
  const fresh = path.join(tmp, 'backup-1-2099-01-01T00-00-00Z.db');
  const unrelated = path.join(tmp, 'keep-me.db');
  for (const file of [stale, staleVerify, fresh, unrelated]) fs.writeFileSync(file, 'x');
  const old = new Date(Date.now() - 2 * 60 * 60 * 1000);
  for (const file of [stale, staleVerify, unrelated]) fs.utimesSync(file, old, old);
  markInterruptedRuns();
  assert.equal(fs.existsSync(stale), false);
  assert.equal(fs.existsSync(staleVerify), false);
  assert.equal(fs.existsSync(fresh), true);
  assert.equal(fs.existsSync(unrelated), true);
});

test('malformed request bodies and upper-case API paths answer with a code', async () => {
  const broken = await member.agent.put('/api/settings').set('Content-Type', 'application/json').send('{"a":').expect(400);
  assert.equal(broken.body.code, 'INVALID_JSON');
  const huge = await member.agent.put('/api/settings').set('Content-Type', 'application/json')
    .send(JSON.stringify({ x: 'y'.repeat(1024 * 1024 + 10) })).expect(413);
  assert.equal(huge.body.code, 'BODY_TOO_LARGE');
  const upper = await request(app).get('/API/health').expect(404);
  assert.equal(upper.body.code, 'NOT_FOUND');
});

test('ended session records are forgotten after 30 days', () => {
  const insert = db.prepare(`
    INSERT INTO user_sessions (user_id, session_hash, revoked_at, expires_at) VALUES (?, ?, ?, ?)
  `);
  insert.run(member.user.id, 'old-revoked', '2020-01-01 00:00:00', null);
  insert.run(member.user.id, 'old-expired', null, '2020-01-01T00:00:00.000Z');
  insert.run(member.user.id, 'recent-revoked', new Date().toISOString(), null);
  purgeEndedSessions();
  const left = db.prepare("SELECT session_hash FROM user_sessions WHERE session_hash IN ('old-revoked', 'old-expired', 'recent-revoked')")
    .all().map((row) => row.session_hash);
  assert.deepEqual(left, ['recent-revoked']);
});
