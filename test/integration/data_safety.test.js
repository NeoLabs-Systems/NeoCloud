'use strict';

// Regression tests for ways the server could lose or damage data: a commit
// that removed its own blob, a master key replaced or missing, migrations
// without a way back, backup runs of two processes pruning each other, and
// uploads expiring while they still move.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const fs = require('fs');
const os = require('os');
const path = require('path');

process.env.NEOCLOUD_OFFICE_PREVIEW = 'false';
// Restore refuses while something answers /api/health on PORT.
process.env.PORT = '1';
const { cleanup, home, registerUser, upload } = require('../helpers/app');
const Database = require('better-sqlite3');
const db = require('../../server/db/database');
const paths = require('../../runtime/paths');
const keys = require('../../server/services/crypto/keys');
const runner = require('../../server/services/backup/runner');
const { VERSIONED_MIGRATIONS, latestSchemaVersion, runVersionedMigrations } = require('../../lib/schema_migrations');
const { blobPath } = require('../../server/services/storage/blob_store');

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-data-safety-'));
let owner;

test.before(async () => {
  owner = await registerUser('safety');
});

test.after(() => {
  cleanup();
  fs.rmSync(scratch, { recursive: true, force: true });
});

function blobOf(nodeId) {
  return db.prepare('SELECT b.id, b.user_id, b.size FROM nodes n JOIN blobs b ON b.id = n.blob_id WHERE n.id = ?').get(nodeId);
}

test('a failure after the commit never deletes the blob just committed', async () => {
  const first = await upload(owner.agent, 'report.txt', 'version one').expect(201);
  // No versions kept: the replace orphans the old blob, whose previews are
  // removed after the commit -- and that removal fails here.
  process.env.NEOCLOUD_VERSION_RETENTION = '0';
  const originalRm = fs.promises.rm;
  fs.promises.rm = async (target, options) => {
    if (String(target).includes(`${path.sep}previews${path.sep}`)) throw Object.assign(new Error('busy'), { code: 'EBUSY' });
    return originalRm(target, options);
  };
  let second;
  try {
    second = await upload(owner.agent, 'report.txt', 'version two', { conflict: 'replace' });
  } finally {
    fs.promises.rm = originalRm;
    delete process.env.NEOCLOUD_VERSION_RETENTION;
  }
  assert.ok([200, 201].includes(second.status), JSON.stringify(second.body));
  assert.equal(second.body.id, first.body.id);
  const blob = blobOf(first.body.id);
  assert.ok(fs.existsSync(blobPath(blob.user_id, blob.id)), 'the new content is on disk');
  const download = await owner.agent.get(`/api/files/${first.body.id}/content`).expect(200);
  assert.equal(download.text, 'version two');
});

test('a missing master key is never replaced by a new one while data exists', () => {
  const envFile = path.join(scratch, 'missing-key.env');
  const blobDir = path.join(scratch, 'blobs-with-data');
  fs.mkdirSync(path.join(blobDir, '1'), { recursive: true });
  fs.writeFileSync(envFile, 'SESSION_SECRET=abc123abc123abc123\n');
  const env = {};
  assert.throws(
    () => paths.ensureSecureRuntimeEnv({ envFile, env, logger: null, dataPaths: { blobDir, databaseFile: path.join(scratch, 'none.db') } }),
    /NEOCLOUD_MASTER_KEY is missing.*already holds encrypted data/s,
  );
  assert.equal(env.NEOCLOUD_MASTER_KEY, undefined);
  assert.doesNotMatch(fs.readFileSync(envFile, 'utf8'), /NEOCLOUD_MASTER_KEY/);

  // The live database has an account: same answer without any blob.
  const emptyBlobs = path.join(scratch, 'blobs-empty');
  fs.mkdirSync(emptyBlobs);
  assert.equal(paths.hasEncryptedData({ blobDir: emptyBlobs, databaseFile: paths.DATABASE_FILE }), true);

  // A fresh install (no accounts, no blobs) gets its key.
  const fresh = {};
  paths.ensureSecureRuntimeEnv({ envFile, env: fresh, logger: null, dataPaths: { blobDir: emptyBlobs, databaseFile: path.join(scratch, 'none.db') } });
  assert.ok(paths.isValidMasterKey(fresh.NEOCLOUD_MASTER_KEY));
});

test('a well-formed but different master key is refused at start', async () => {
  await upload(owner.agent, 'needs-a-key.txt', 'x').expect(201);
  keys.assertMasterKeyMatches();
  const original = process.env.NEOCLOUD_MASTER_KEY;
  process.env.NEOCLOUD_MASTER_KEY = crypto.randomBytes(32).toString('hex');
  try {
    assert.throws(() => keys.assertMasterKeyMatches(), /does not match the key this data was encrypted with/);
  } finally {
    process.env.NEOCLOUD_MASTER_KEY = original;
  }
  keys.assertMasterKeyMatches();
});

function databaseAtVersion(file, version) {
  const database = new Database(file);
  for (const migration of VERSIONED_MIGRATIONS.filter((m) => m.version <= version)) {
    migration.up(database);
    database.pragma(`user_version = ${migration.version}`);
  }
  return database;
}

test('a pending migration first copies the database to data/backups, keeping the last 3', () => {
  const snapshotDir = path.join(scratch, 'pre-migration');
  fs.mkdirSync(snapshotDir);
  const from = latestSchemaVersion() - 1;
  // Three older copies; the oldest goes.
  const old = new Date(Date.now() - 60 * 60 * 1000);
  for (const name of ['pre-migration-v1-a.db', 'pre-migration-v2-b.db', 'pre-migration-v3-c.db']) {
    fs.writeFileSync(path.join(snapshotDir, name), 'x');
    fs.utimesSync(path.join(snapshotDir, name), old, old);
  }
  fs.utimesSync(path.join(snapshotDir, 'pre-migration-v1-a.db'), new Date(old - 1000), new Date(old - 1000));
  fs.writeFileSync(path.join(snapshotDir, 'unrelated.db'), 'keep');

  const database = databaseAtVersion(path.join(scratch, 'old.db'), from);
  try {
    database.prepare("INSERT INTO users (username, password) VALUES ('kept', 'x')").run();
    runVersionedMigrations(database, { snapshotDir });
    assert.equal(database.pragma('user_version', { simple: true }), latestSchemaVersion());
  } finally {
    database.close();
  }
  const copies = fs.readdirSync(snapshotDir).filter((name) => name.startsWith('pre-migration-')).sort();
  assert.equal(copies.length, 3);
  assert.ok(!copies.includes('pre-migration-v1-a.db'));
  assert.ok(fs.existsSync(path.join(snapshotDir, 'unrelated.db')));
  const fresh = copies.find((name) => name.startsWith(`pre-migration-v${from}-`));
  assert.ok(fresh, copies.join(', '));
  const copy = new Database(path.join(snapshotDir, fresh), { readonly: true });
  try {
    assert.equal(copy.pragma('user_version', { simple: true }), from);
    assert.equal(copy.prepare('SELECT username FROM users').get().username, 'kept');
  } finally {
    copy.close();
  }
  if (process.platform !== 'win32') assert.equal(fs.statSync(path.join(snapshotDir, fresh)).mode & 0o777, 0o600);

  // Up to date, or a brand-new database: nothing to copy.
  const before = fs.readdirSync(snapshotDir).length;
  const empty = new Database(':memory:');
  runVersionedMigrations(empty, { snapshotDir });
  runVersionedMigrations(empty, { snapshotDir });
  empty.close();
  assert.equal(fs.readdirSync(snapshotDir).length, before);
});

test('no migration runs when the copy before it fails', () => {
  const blocked = path.join(scratch, 'not-a-folder');
  fs.writeFileSync(blocked, 'a file where the backups folder should be');
  const from = latestSchemaVersion() - 1;
  const database = databaseAtVersion(path.join(scratch, 'blocked.db'), from);
  try {
    assert.throws(() => runVersionedMigrations(database, { snapshotDir: blocked }), /Nothing was changed/);
    assert.equal(database.pragma('user_version', { simple: true }), from);
  } finally {
    database.close();
  }
});

test('a backup run another process is still doing keeps its target and its database copy', async () => {
  const folder = path.join(scratch, 'held-target');
  const target = (await owner.agent.post('/api/admin/backups/targets').send({
    name: 'held', type: 'local', config: { path: folder },
  }).expect(201)).body;
  // `neocloud backup run`, two days into a first backup, still beating.
  const name = '2026-01-01T00-00-00Z';
  db.prepare(`
    INSERT INTO backup_runs (target_id, status, snapshot, started_at, heartbeat_at)
    VALUES (?, 'running', ?, datetime('now', '-2 days'), datetime('now', '-20 seconds'))
  `).run(target.id, name);
  const tmp = paths.UPLOAD_TMP_DIR;
  const copy = path.join(tmp, `backup-${target.id}-${name}.db`);
  fs.writeFileSync(copy, 'plaintext copy in use');
  const old = new Date(Date.now() - 3 * 60 * 60 * 1000);
  fs.utimesSync(copy, old, old);

  const busy = await owner.agent.post(`/api/admin/backups/targets/${target.id}/run`).expect(409);
  assert.equal(busy.body.code, 'BACKUP_RUNNING');
  assert.equal(runner.markInterruptedRuns(), 0);
  assert.ok(fs.existsSync(copy), 'the copy of a live run is kept');

  // The process dies: its heartbeat stops.
  db.prepare("UPDATE backup_runs SET heartbeat_at = datetime('now', '-10 minutes') WHERE target_id = ?").run(target.id);
  assert.equal(runner.markInterruptedRuns(), 1);
  assert.equal(fs.existsSync(copy), false);
  const run = await runner.runBackup(target.id);
  assert.equal(run.status, 'ok', run.error);
  assert.equal(db.prepare('SELECT heartbeat_at FROM backup_runs WHERE id = ?').get(run.id).heartbeat_at !== null, true);
});

test('a resumable upload that keeps receiving parts does not expire', async () => {
  const data = crypto.randomBytes(1024);
  const session = (await owner.agent.post('/api/uploads').send({ name: 'slow.bin', size: data.length }).expect(201)).body;
  // Created a day ago: without a part it would expire within the minute.
  db.prepare("UPDATE upload_sessions SET expires_at = datetime('now', '+1 minute') WHERE id = ?").run(session.id);
  await owner.agent.put(`/api/uploads/${session.id}/parts/0`).set('Content-Type', 'application/octet-stream').send(data).expect(200);
  const { hours } = db.prepare("SELECT (julianday(expires_at) - julianday('now')) * 24 AS hours FROM upload_sessions WHERE id = ?").get(session.id);
  assert.ok(hours > 23.9, `expires in ${hours} h`);
  await owner.agent.post(`/api/uploads/${session.id}/complete`).send({}).expect(201);
});

// Last: a restore closes this process's database handle.
test('restore --from a backup folder works without a target row and replaces a damaged local blob', async () => {
  const content = crypto.randomBytes(200 * 1024);
  const file = await upload(owner.agent, 'precious.bin', content).expect(201);
  const folder = path.join(scratch, 'restore-from');
  const target = (await owner.agent.post('/api/admin/backups/targets').send({
    name: 'disk', type: 'local', config: { path: folder },
  }).expect(201)).body;
  const run = await runner.runBackup(target.id);
  assert.equal(run.status, 'ok', run.error);

  // The local copy is cut short (a damaged disk, an interrupted copy).
  const blob = blobOf(file.body.id);
  const local = blobPath(blob.user_id, blob.id);
  fs.truncateSync(local, 1000);

  const { restoreSnapshot } = require('../../server/services/backup/restore');
  const messages = [];
  await restoreSnapshot({ from: folder }, run.snapshot, { log: (line) => messages.push(line) });
  assert.ok(messages.some((line) => /Downloaded [1-9]\d* blob/.test(line)), messages.join('\n'));
  const { createDecryptStream } = require('../../server/services/crypto/stream_cipher');
  const restored = new Database(paths.DATABASE_FILE, { readonly: true });
  const wrapped = restored.prepare('SELECT wrapped_dek FROM users WHERE id = ?').get(blob.user_id).wrapped_dek;
  restored.close();
  const chunks = [];
  for await (const chunk of createDecryptStream(local, keys.unwrapDek(blob.user_id, wrapped), blob.id, blob.size)) chunks.push(chunk);
  assert.ok(Buffer.concat(chunks).equals(content));
  assert.ok(home);
});
