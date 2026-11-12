'use strict';

// The restore drill, end to end with real processes, exactly as
// docs/operations.md describes it: a server on a throwaway NEOCLOUD_HOME with
// a generated master key, two accounts with a mix of files (small and a
// resumable one over 8 MiB, folders, a vault, versions, trash, a folder
// shared with the other account, a public link), a backup to a local folder
// and a verify; then the server is stopped, the data folder wiped (the .env
// with the master key kept), and `neocloud backup restore --from` brings it
// back. Every file's SHA-256, the versions, trash, shares, vault and users
// must match. Also: a wrong or missing master key stops the server and a
// restore without destroying anything, and a missing or damaged blob is
// reported without bringing the server down.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const net = require('net');
const os = require('os');
const path = require('path');
const { spawn, spawnSync } = require('child_process');
const request = require('supertest');
const Database = require('better-sqlite3');

const APP_DIR = path.resolve(__dirname, '..', '..');
const PASSWORD = 'Plum-Orbit-73!quartz';
const PART_SIZE = 8 * 1024 * 1024;

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-drill-'));
const home = path.join(root, 'home');
const dataDir = path.join(home, 'data');
const envFile = path.join(home, '.env');
const backupDir = path.join(root, 'backup');
const masterKey = crypto.randomBytes(32).toString('hex');
let port;
let server = null;

test.after(async () => {
  await stopServer().catch(() => {});
  fs.rmSync(root, { recursive: true, force: true });
});

function freePort() {
  return new Promise((resolve, reject) => {
    const probe = net.createServer();
    probe.once('error', reject);
    probe.listen(0, '127.0.0.1', () => {
      const { port: found } = probe.address();
      probe.close(() => resolve(found));
    });
  });
}

function writeEnv(key) {
  const lines = [`SESSION_SECRET=${'s'.repeat(64)}`, `PORT=${port}`];
  if (key !== null) lines.push(`NEOCLOUD_MASTER_KEY=${key}`);
  fs.writeFileSync(envFile, `${lines.join('\n')}\n`, { mode: 0o600 });
}

// Only what a service manager would pass; secrets come from the .env.
function childEnv() {
  return { PATH: process.env.PATH, HOME: root, NEOCLOUD_HOME: home, PORT: String(port), NODE_ENV: 'test' };
}

function baseUrl() {
  return `http://127.0.0.1:${port}`;
}

async function startServer() {
  const child = spawn(process.execPath, ['server/index.js'], { cwd: APP_DIR, env: childEnv(), stdio: ['ignore', 'pipe', 'pipe'] });
  const output = [];
  child.stdout.on('data', (chunk) => output.push(chunk));
  child.stderr.on('data', (chunk) => output.push(chunk));
  const exited = new Promise((resolve) => child.once('exit', (code) => resolve(code)));
  child.exited = exited;
  child.output = () => Buffer.concat(output).toString('utf8');
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const code = await Promise.race([exited, new Promise((resolve) => setTimeout(() => resolve(undefined), 200))]);
    if (code !== undefined) return { exitCode: code, output: child.output() };
    try {
      const res = await fetch(`${baseUrl()}/api/health`);
      if (res.ok) {
        server = child;
        return { exitCode: null, output: child.output() };
      }
    } catch {
      // Not listening yet.
    }
  }
  child.kill('SIGKILL');
  throw new Error(`Server did not come up:\n${child.output()}`);
}

async function stopServer() {
  if (!server) return;
  const child = server;
  server = null;
  child.kill('SIGTERM');
  const code = await child.exited;
  assert.equal(code, 0, child.output());
}

function cli(...args) {
  const result = spawnSync(process.execPath, ['bin/neocloud.js', ...args], { cwd: APP_DIR, env: childEnv(), encoding: 'utf8' });
  return { status: result.status, output: `${result.stdout}${result.stderr}` };
}

function sha256(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex');
}

function fileOf(...parts) {
  return fs.readFileSync(path.join(...parts));
}

function binary(req) {
  return req.buffer(true).parse((res, callback) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => callback(null, Buffer.concat(chunks)));
    res.on('error', callback);
  });
}

async function signIn(username) {
  const agent = request.agent(baseUrl());
  await agent.post('/api/auth/login').send({ username, password: PASSWORD }).expect(200);
  return agent;
}

async function uploadFile(agent, parentId, name, content, conflict) {
  const query = new URLSearchParams({ parentId, name, ...(conflict ? { conflict } : {}) });
  const res = await agent.put(`/api/upload?${query}`).set('Content-Type', 'application/octet-stream').send(content);
  assert.ok([200, 201].includes(res.status), `${name}: ${res.status} ${JSON.stringify(res.body)}`);
  return res.body;
}

async function resumableUpload(agent, parentId, name, content) {
  const session = (await agent.post('/api/uploads').send({ parentId, name, size: content.length, sha256: sha256(content) }).expect(201)).body;
  assert.equal(session.partCount, Math.ceil(content.length / PART_SIZE));
  // Out of order on purpose.
  for (const index of [...Array(session.partCount).keys()].reverse()) {
    await agent.put(`/api/uploads/${session.id}/parts/${index}`).set('Content-Type', 'application/octet-stream')
      .send(content.subarray(index * session.partSize, (index + 1) * session.partSize)).expect(200);
  }
  return (await agent.post(`/api/uploads/${session.id}/complete`).send({}).expect(201)).body;
}

async function createFolder(agent, parentId, name) {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

async function download(agent, url) {
  return binary(agent.get(url).expect(200)).then((res) => res.body);
}

/**
 * Everything the drill compares, as seen through the API: per account every
 * live file's path, size, version and SHA-256 of a download, each kept
 * version's SHA-256, the trash, share links (and a public download through
 * each), members, what is shared with the account, vaults and the account
 * list.
 */
async function capture(accounts, { vaultId }) {
  const state = {};
  for (const [name, agent] of Object.entries(accounts)) {
    const tree = (await agent.get('/api/sync/tree').expect(200)).body;
    const files = {};
    for (const node of tree.nodes.filter((n) => n.type === 'file')) {
      const entry = { size: node.size, version: node.version, etag: node.etag, sha256: sha256(await download(agent, `/api/files/${node.id}/content`)) };
      const { versions } = (await agent.get(`/api/files/${node.id}/versions`).expect(200)).body;
      entry.versions = [];
      for (const kept of versions) {
        entry.versions.push({ version: kept.version, size: kept.size, sha256: sha256(await download(agent, `/api/files/${node.id}/content?version=${kept.version}`)) });
      }
      files[node.path] = entry;
    }
    const folders = tree.nodes.filter((n) => n.type === 'folder').map((n) => `${n.path}${n.vaultId ? ' (vault)' : ''}`).sort();
    const trash = (await agent.get('/api/trash').expect(200)).body.items.map((item) => ({ name: item.name, size: item.size, originalPath: item.originalPath }));
    const shares = [];
    for (const link of (await agent.get('/api/shares').expect(200)).body.shares) {
      const pub = (await request(baseUrl()).get(`/api/public/shares/${link.token}`).expect(200)).body;
      const zip = await binary(request(baseUrl()).get(`/api/public/shares/${link.token}/zip`).expect(200)).then((res) => res.body);
      shares.push({ token: link.token, mode: link.mode, nodeName: pub.node.name, items: pub.items.map((i) => i.name).sort(), zipBytes: zip.length > 0 });
    }
    const members = {};
    for (const node of tree.nodes.filter((n) => n.shared)) {
      members[node.path] = (await agent.get(`/api/nodes/${node.id}/members`).expect(200)).body.members.map((m) => ({ recipient: m.recipient, permission: m.permission }));
    }
    const sharedWithMe = [];
    for (const share of (await agent.get('/api/shared-with-me').expect(200)).body.shares) {
      const listing = (await agent.get(`/api/folders/${share.node.id}/children`).expect(200)).body.items;
      const contents = {};
      for (const item of listing.filter((i) => i.type === 'file')) contents[item.name] = sha256(await download(agent, `/api/files/${item.id}/content`));
      sharedWithMe.push({ ownerName: share.ownerName, permission: share.permission, name: share.node.name, contents });
    }
    state[name] = { files, folders, trash, shares, members, sharedWithMe };
  }
  const vault = (await accounts.alice.get(`/api/vaults/${vaultId}`).expect(200)).body;
  state.vault = { salt: vault.salt, iterations: vault.iterations, wrappedKey: vault.wrappedKey, folderId: vault.folderId };
  state.users = (await accounts.alice.get('/api/admin/users').expect(200)).body.users
    .map((u) => ({ username: u.username, isAdmin: u.isAdmin, usedBytes: u.usedBytes, fileCount: u.fileCount }))
    .sort((a, b) => a.username.localeCompare(b.username));
  return state;
}

function databaseFingerprint() {
  return sha256(fileOf(dataDir, 'neocloud.db'));
}

test('restore drill: back up, wipe, restore, compare; wrong keys and damaged blobs', async (t) => {
  port = await freePort();
  fs.mkdirSync(home, { recursive: true, mode: 0o700 });
  writeEnv(masterKey);
  const started = await startServer();
  assert.equal(started.exitCode, null, started.output);

  // Two throwaway accounts; the first one administers the install.
  const alice = request.agent(baseUrl());
  await alice.post('/api/auth/register').send({ username: 'drillalice', email: 'drillalice@example.com', password: PASSWORD }).expect(200);
  const bob = request.agent(baseUrl());
  await bob.post('/api/auth/register').send({ username: 'drillbob', email: 'drillbob@example.com', password: PASSWORD }).expect(200);

  const docs = await createFolder(alice, 'root', 'Docs');
  const deep = await createFolder(alice, docs.id, 'Deep');
  await uploadFile(alice, 'root', 'hello.txt', Buffer.from('hello, drill\n'));
  await uploadFile(alice, deep.id, 'empty.bin', Buffer.alloc(0));
  await uploadFile(alice, docs.id, 'photo.jpg', crypto.randomBytes(300 * 1024));
  const big = await resumableUpload(alice, docs.id, 'video.mp4', crypto.randomBytes(PART_SIZE + 1234567));
  // Three overwrites: version 4 current, three kept.
  await uploadFile(alice, 'root', 'notes.md', Buffer.from('v1'));
  for (const text of ['v2 longer', 'v3 even longer', 'v4 final']) await uploadFile(alice, 'root', 'notes.md', Buffer.from(text), 'replace');
  const doomed = await uploadFile(alice, docs.id, 'old-draft.txt', Buffer.from('trashed but restorable'));
  const doomedSha = sha256(Buffer.from('trashed but restorable'));
  await alice.delete(`/api/nodes/${doomed.id}`).expect(200);
  const shared = await createFolder(alice, 'root', 'Shared');
  await uploadFile(alice, shared.id, 'plan.pdf', crypto.randomBytes(70 * 1024));
  await alice.post(`/api/nodes/${shared.id}/members`).send({ recipient: 'drillbob', permission: 'write' }).expect(201);
  await alice.post('/api/shares').send({ nodeId: shared.id }).expect(201);
  const vault = (await alice.post('/api/vaults').send({
    parentId: 'root',
    name: 'Tresor',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  // Vault contents are ciphertext made on the device.
  await uploadFile(alice, vault.folderId, 'c2VjcmV0.bin', crypto.randomBytes(5000));
  await uploadFile(bob, 'root', 'bob.txt', Buffer.from('bob was here'));
  // Bob writes into the folder Alice shared with him.
  await uploadFile(bob, shared.id, 'from-bob.txt', Buffer.from('a member upload'));

  const before = await capture({ alice, bob }, { vaultId: vault.id });
  assert.equal(before.alice.files['notes.md'].version, 4);
  assert.equal(before.alice.files['notes.md'].versions.length, 3);
  assert.equal(before.alice.files['Docs/video.mp4'].size, PART_SIZE + 1234567);
  assert.deepEqual(before.alice.trash.map((item) => item.name), ['old-draft.txt']);
  assert.equal(before.bob.sharedWithMe[0].contents['from-bob.txt'], sha256(Buffer.from('a member upload')));

  await t.test('backup to a local folder, then verify', async () => {
    const target = (await alice.post('/api/admin/backups/targets').send({
      name: 'USB disk', type: 'local', config: { path: backupDir }, retention: 3,
    }).expect(201)).body;
    await alice.post(`/api/admin/backups/targets/${target.id}/run`).expect(202);
    let run;
    for (let tries = 0; tries < 100; tries += 1) {
      run = (await alice.get('/api/admin/backups').expect(200)).body.runs.find((r) => r.targetId === target.id);
      if (run && run.status !== 'running') break;
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    assert.equal(run.status, 'ok', run.error);
    // The CLI verify while the server runs (read-only).
    const verify = cli('backup', 'verify', String(target.id));
    assert.equal(verify.status, 0, verify.output);
    assert.match(verify.output, /database ok, all \d+ blob\(s\) present/);
  });

  await stopServer();
  const intact = databaseFingerprint();

  await t.test('a wrong master key stops the server and a restore, and changes nothing', async () => {
    writeEnv(crypto.randomBytes(32).toString('hex'));
    const refused = await startServer();
    assert.equal(refused.exitCode, 1, refused.output);
    assert.match(refused.output, /NEOCLOUD_MASTER_KEY does not match/);

    const [snapshot] = cli('backup', 'snapshots', '--from', backupDir).output.match(/\d{4}-\d{2}-\d{2}T[\d-]+Z/) || [];
    assert.ok(snapshot);
    const restore = cli('backup', 'restore', '--from', backupDir, snapshot);
    assert.notEqual(restore.status, 0, restore.output);
    assert.match(restore.output, /could not be decrypted\. Is NEOCLOUD_MASTER_KEY the one this backup was made with\?/);
    assert.equal(databaseFingerprint(), intact);
    assert.equal(fs.readdirSync(dataDir).some((name) => name.includes('before-restore')), false);
  });

  await t.test('a missing master key stops the server instead of creating a new one', async () => {
    writeEnv(null);
    const refused = await startServer();
    assert.equal(refused.exitCode, 1, refused.output);
    assert.match(refused.output, /NEOCLOUD_MASTER_KEY is missing/);
    assert.doesNotMatch(fs.readFileSync(envFile, 'utf8'), /NEOCLOUD_MASTER_KEY/);
    assert.equal(databaseFingerprint(), intact);
    writeEnv(masterKey);
  });

  await t.test('wipe the data folder, keep the .env, restore as documented', async () => {
    fs.rmSync(dataDir, { recursive: true, force: true });
    const listed = cli('backup', 'snapshots', '--from', backupDir);
    assert.equal(listed.status, 0, listed.output);
    const [snapshot] = listed.output.match(/\d{4}-\d{2}-\d{2}T[\d-]+Z/);
    const restore = cli('backup', 'restore', '--from', backupDir, snapshot);
    assert.equal(restore.status, 0, restore.output);
    assert.match(restore.output, /Downloaded \d+ blob\(s\)/);

    const again = await startServer();
    assert.equal(again.exitCode, null, again.output);
    const after = await capture({ alice: await signIn('drillalice'), bob: await signIn('drillbob') }, { vaultId: vault.id });
    assert.deepEqual(after, before);

    // The trashed file comes back with its content.
    const agent = await signIn('drillalice');
    const [trashed] = (await agent.get('/api/trash').expect(200)).body.items;
    await agent.post(`/api/trash/${trashed.id}/restore`).expect(200);
    assert.equal(sha256(await download(agent, `/api/files/${trashed.id}/content`)), doomedSha);
    await stopServer();
  });

  await t.test('a missing and a damaged blob are reported, the server stays up', async () => {
    const db = new Database(path.join(dataDir, 'neocloud.db'), { readonly: true });
    const blobOf = (name) => db.prepare(`
      SELECT b.id, b.user_id, b.size, n.id AS node_id FROM nodes n JOIN blobs b ON b.id = n.blob_id
      WHERE n.name = ? AND n.trashed_at IS NULL
    `).get(name);
    const missing = blobOf('hello.txt');
    const flippedStart = blobOf('photo.jpg');
    const flippedMiddle = blobOf('video.mp4');
    db.close();
    const blobFile = (blob) => path.join(dataDir, 'storage', 'blobs', String(blob.user_id), blob.id.slice(0, 2), blob.id);
    fs.rmSync(blobFile(missing));
    for (const [blob, offset] of [[flippedStart, 100], [flippedMiddle, Math.floor(flippedMiddle.size / 2)]]) {
      const handle = fs.openSync(blobFile(blob), 'r+');
      const byte = Buffer.alloc(1);
      fs.readSync(handle, byte, 0, 1, offset);
      byte[0] ^= 0x01;
      fs.writeSync(handle, byte, 0, 1, offset);
      fs.closeSync(handle);
    }

    // The hourly maintenance's blob scrub, run once by hand.
    const scrub = spawnSync(process.execPath, ['-e', `
      require('dotenv').config({ path: process.env.NEOCLOUD_HOME + '/.env', quiet: true });
      const scrub = require('./server/services/storage/blob_scrub');
      scrub.scrubSlice().then(() => console.log(JSON.stringify(scrub.getScrubStatus())));
    `], { cwd: APP_DIR, env: childEnv(), encoding: 'utf8' });
    assert.equal(scrub.status, 0, scrub.stderr);
    // The scrub reads the first and last segment of each blob: the missing
    // file and the damaged first segment. Damage in the middle of a large
    // file shows on download.
    assert.equal(JSON.parse(scrub.stdout.trim().split('\n').pop()).damaged, 2);

    const up = await startServer();
    assert.equal(up.exitCode, null, up.output);
    const agent = await signIn('drillalice');
    const health = (await agent.get('/api/admin/health').expect(200)).body;
    assert.equal(health.damagedBlobs, 2);

    // A missing or damaged start answers with an error, not a reset.
    const gone = await agent.get(`/api/files/${missing.node_id}/content`);
    assert.equal(gone.status, 500);
    assert.equal(gone.body.code, 'FILE_UNREADABLE');
    const broken = await agent.get(`/api/files/${flippedStart.node_id}/content`);
    assert.equal(broken.status, 500);
    assert.equal(broken.body.code, 'FILE_UNREADABLE');
    // Headers are out before the damaged segment is reached: the transfer is
    // cut short, never completed with altered bytes.
    let middle = null;
    try {
      middle = await binary(agent.get(`/api/files/${flippedMiddle.node_id}/content`));
    } catch {
      middle = null;
    }
    assert.ok(middle === null || middle.body.length < flippedMiddle.size, 'a damaged file is never delivered whole');
    // An undamaged range of it still plays.
    const head = await binary(agent.get(`/api/files/${flippedMiddle.node_id}/content`).set('Range', 'bytes=0-1023').expect(206));
    assert.equal(head.body.length, 1024);

    const alive = await fetch(`${baseUrl()}/api/health`);
    assert.equal(alive.status, 200);
    assert.equal(server.exitCode, null);
    await stopServer();
  });
});
