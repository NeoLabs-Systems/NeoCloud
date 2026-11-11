'use strict';

// A backup target is storage someone else may be able to write: a bucket, a
// NAS, an SFTP or WebDAV host. Whatever it hands back must not reach outside
// BLOB_DIR, replace the database with something that is not one, or stall
// the server.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const http = require('http');
const os = require('os');
const path = require('path');
const { Readable } = require('stream');
const { pipeline } = require('stream/promises');

process.env.NEOCLOUD_OFFICE_PREVIEW = 'false';
// Restore refuses while something answers /api/health on PORT.
process.env.PORT = '1';
const { cleanup, home, registerUser, upload } = require('../helpers/app');
const Database = require('better-sqlite3');
const db = require('../../server/db/database');
const runner = require('../../server/services/backup/runner');
const { restoreSnapshot } = require('../../server/services/backup/restore');
const { verifyTarget } = require('../../server/services/backup/verify');
const { createWebdavAdapter } = require('../../server/services/backup/adapters/webdav');
const { getBackupKey } = require('../../server/services/crypto/keys');
const { createEncryptStream } = require('../../server/services/crypto/stream_cipher');

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-backup-hardening-'));
let owner;

test.before(async () => {
  owner = await registerUser('backup');
  await upload(owner.agent, 'a.txt', 'some content').expect(201);
});

test.after(() => {
  cleanup();
  fs.rmSync(scratch, { recursive: true, force: true });
});

async function localTarget(name) {
  const folder = path.join(scratch, name);
  const target = (await owner.agent.post('/api/admin/backups/targets').send({
    name,
    type: 'local',
    config: { path: folder },
    retention: 5,
  }).expect(201)).body;
  return { id: target.id, folder };
}

function usersCount() {
  return db.prepare('SELECT COUNT(*) AS n FROM users').get().n;
}

function writeSnapshot(folder, name, manifest, databaseObject) {
  const dir = path.join(folder, 'snapshots', name);
  fs.mkdirSync(dir, { recursive: true });
  if (databaseObject) fs.writeFileSync(path.join(dir, 'neocloud.db.enc'), databaseObject);
  fs.writeFileSync(path.join(dir, 'manifest.json'), JSON.stringify(manifest));
}

async function encryptForSnapshot(plaintext, name) {
  const file = path.join(scratch, `plain-${name}`);
  await pipeline(Readable.from(plaintext.length ? [plaintext] : []), createEncryptStream(getBackupKey(), runner.snapshotIdFor(name)), fs.createWriteStream(file));
  return fs.readFileSync(file);
}

test('restore, verify and prune refuse manifest entries that are not <user id>/<blob id>', async () => {
  const { id, folder } = await localTarget('traversal');
  const run = await runner.runBackup(id);
  assert.equal(run.status, 'ok', run.error);
  const manifestFile = path.join(folder, 'snapshots', run.snapshot, 'manifest.json');
  const original = JSON.parse(fs.readFileSync(manifestFile, 'utf8'));
  const [good] = original.blobs;
  const blobId = good.split('/')[1];

  const bad = [
    '../..canary',
    `1/..\\..\\..\\canary-${blobId}`,
    `0/${blobId}`,
    `01/${blobId}`,
    `1/${blobId}/extra`,
    `9007199254740993/${blobId}`,
    `1/${blobId.toUpperCase()}`,
    42,
  ];
  for (const entry of bad) {
    fs.writeFileSync(manifestFile, JSON.stringify({ ...original, blobs: [good, entry] }));
    // Serve the bytes too, where the local target allows such a key at all.
    if (typeof entry === 'string' && !entry.includes('\\')) {
      try {
        fs.mkdirSync(path.dirname(path.join(folder, 'blobs', entry)), { recursive: true });
        fs.writeFileSync(path.join(folder, 'blobs', entry), 'planted');
      } catch {
        // Outside the target folder: nothing to plant.
      }
    }
    await assert.rejects(restoreSnapshot(id, run.snapshot), /manifest is not valid/, String(entry));
    const verification = await verifyTarget(id);
    assert.equal(verification.ok, false, String(entry));
    assert.match(verification.error, /manifest is not valid/);
  }
  assert.equal(fs.readdirSync(path.join(home, 'data')).some((name) => name.includes('canary')), false);
  assert.equal(fs.existsSync(path.join(home, 'data', 'storage', 'canary')), false);

  // Prune reads every kept manifest; a bad one fails the run instead of
  // deleting blobs it cannot account for.
  const blobsBefore = fs.readdirSync(path.join(folder, 'blobs'), { recursive: true }).length;
  await new Promise((resolve) => setTimeout(resolve, 1100));
  const second = await runner.runBackup(id);
  assert.equal(second.status, 'failed');
  assert.match(second.error, /manifest is not valid/);
  assert.equal(fs.readdirSync(path.join(folder, 'blobs'), { recursive: true }).length, blobsBefore);

  // Snapshot names come from the target too; restore takes only plain ones.
  await assert.rejects(restoreSnapshot(id, '../traversal'), /not a snapshot name/);
});

test('a manifest over the size cap is refused before it is parsed', async () => {
  const { id, folder } = await localTarget('oversize');
  const dir = path.join(folder, 'snapshots', '2099-01-01T00-00-00Z');
  fs.mkdirSync(dir, { recursive: true });
  const fd = fs.openSync(path.join(dir, 'manifest.json'), 'w');
  // Sparse 65 MB file: no disk needed.
  fs.ftruncateSync(fd, 65 * 1024 * 1024);
  fs.closeSync(fd);
  const verification = await verifyTarget(id);
  assert.equal(verification.ok, false);
  assert.match(verification.error, /larger than/);
});

test('a forged snapshot with segment size 0 neither verifies nor restores', async () => {
  const { id, folder } = await localTarget('zero-segment');
  const header = Buffer.alloc(20);
  header.write('NCB1', 0, 'ascii');
  header.writeUInt8(1, 4);
  const name = '2099-01-01T00-00-00Z';
  writeSnapshot(folder, name, { format: 2, version: 'x', schemaVersion: 1, databaseSize: 1, blobs: [] }, header);

  const verification = await verifyTarget(id);
  assert.equal(verification.snapshot, name);
  assert.equal(verification.ok, false);
  const users = usersCount();
  await assert.rejects(restoreSnapshot(id, name), /could not be decrypted/);
  assert.equal(usersCount(), users);
});

test('a decryptable snapshot that is not a NeoCloud database is refused', async () => {
  const { id, folder } = await localTarget('not-a-database');

  // Empty: authenticates, decrypts to 0 bytes.
  const empty = '2099-01-01T00-00-00Z';
  writeSnapshot(folder, empty, { format: 2, version: 'x', schemaVersion: 1, databaseSize: 0, blobs: [] }, await encryptForSnapshot(Buffer.alloc(0), empty));
  let verification = await verifyTarget(id);
  assert.equal(verification.ok, false);
  assert.match(verification.error, /empty/);
  await assert.rejects(restoreSnapshot(id, empty), /empty/);

  // A valid SQLite file without NeoCloud's schema.
  const foreignFile = path.join(scratch, 'foreign.db');
  const foreign = new Database(foreignFile);
  foreign.exec('CREATE TABLE notes (text TEXT)');
  foreign.close();
  const plain = fs.readFileSync(foreignFile);
  const other = '2099-01-02T00-00-00Z';
  writeSnapshot(folder, other, { format: 2, version: 'x', schemaVersion: 1, databaseSize: plain.length, blobs: [] }, await encryptForSnapshot(plain, other));
  verification = await verifyTarget(id);
  assert.equal(verification.snapshot, other);
  assert.equal(verification.ok, false);
  assert.match(verification.error, /not a NeoCloud database/);
  await assert.rejects(restoreSnapshot(id, other), /not a NeoCloud database/);
  assert.ok(usersCount() >= 1);
});

function davServer(handler) {
  const server = http.createServer(handler);
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(server)));
}

function multistatus(...hrefs) {
  const items = hrefs.map((href) => `<d:response><d:href>${href}</d:href><d:propstat><d:prop><d:resourcetype/></d:prop></d:propstat></d:response>`);
  return `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">${items.join('')}</d:multistatus>`;
}

test('WebDAV target: unsafe names, redirects, huge and adversarial listings are refused', async () => {
  let answer = () => {};
  const server = await davServer((req, res) => answer(req, res));
  const adapter = createWebdavAdapter({ url: `http://127.0.0.1:${server.address().port}/b/` });
  try {
    for (const name of ['%2F..%2F..%2Fetc', '..%5C..%5Cx', 'a%2Fb']) {
      answer = (req, res) => res.writeHead(207).end(multistatus('/b/blobs/', `/b/blobs/${name}`));
      await assert.rejects(adapter.list('blobs/'), /unsafe name/, name);
    }

    // Entries that are not direct children of the folder are ignored.
    answer = (req, res) => res.writeHead(207).end(multistatus('/b/blobs/', '/b/blobs/1', '/elsewhere/2', '/b/3'));
    assert.deepEqual(await adapter.list('blobs/'), ['blobs/1']);

    // Endless nesting stops.
    answer = (req, res) => {
      const self = new URL(req.url, 'http://x').pathname;
      res.writeHead(207).end(multistatus(self, `${self}a/`).replace('<d:resourcetype/></d:prop></d:propstat></d:response></d:multistatus>', '<d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>'));
    };
    await assert.rejects(adapter.list('blobs/'), /nested too deeply/);

    let redirected = false;
    answer = (req, res) => {
      if (req.url.startsWith('/elsewhere')) {
        redirected = true;
        return res.writeHead(207).end(multistatus());
      }
      return res.writeHead(301, { Location: '/elsewhere/' }).end();
    };
    await assert.rejects(adapter.list('blobs/'), /answered 301/);
    assert.equal(redirected, false);

    answer = (req, res) => {
      res.writeHead(207);
      const chunk = Buffer.alloc(1024 * 1024, 'x');
      const pump = () => {
        while (res.write(chunk)) { /* fill the socket */ }
      };
      res.on('drain', pump);
      res.on('error', () => {});
      pump();
    };
    await assert.rejects(adapter.list('blobs/'), /larger than/);

    answer = (req, res) => res.writeHead(207).end(`<d:multistatus xmlns:d="DAV:">${'<d:response '.repeat(100000)}`);
    const started = Date.now();
    await assert.rejects(adapter.list('blobs/'), /valid WebDAV XML/);
    assert.ok(Date.now() - started < 1000, 'took too long');
  } finally {
    server.closeAllConnections();
    server.close();
  }
});
