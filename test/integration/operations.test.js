'use strict';

// Server operations: health, disk-full refusals, backup ordering, interrupted
// runs, scheduled retries, verification, local snapshots and restore guards.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

// Restore refuses while something answers /api/health on PORT; keep a dev
// server on the default port out of it.
process.env.PORT = '1';
const { app, cleanup, home, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const runner = require('../../server/services/backup/runner');
const { restoreSnapshot } = require('../../server/services/backup/restore');
const { verifyTarget } = require('../../server/services/backup/verify');
const { checkSchedules, isDue } = require('../../server/services/backup/scheduler');
const { listLocalSnapshots, restoreLocalSnapshot } = require('../../server/services/backup/local_snapshots');
const { PID_FILE } = require('../../runtime/paths');

const backupDir = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-ops-backup-'));

test.after(() => {
  cleanup();
  fs.rmSync(backupDir, { recursive: true, force: true });
});

let admin;
let member;

test.before(async () => {
  admin = await registerUser('opsadmin');
  member = await registerUser('opsmember');
});

function readManifest(snapshot) {
  return JSON.parse(fs.readFileSync(path.join(backupDir, 'snapshots', snapshot, 'manifest.json'), 'utf8'));
}

async function withStatfs(freeBytes, action) {
  const original = fs.statfsSync;
  fs.statfsSync = () => ({ bsize: 4096, blocks: 1e9, bavail: Math.floor(freeBytes / 4096) });
  try {
    return await action();
  } finally {
    fs.statfsSync = original;
  }
}

test('GET /api/health reports the database and storage, 503 when degraded', async () => {
  const ok = await request(app).get('/api/health').expect(200);
  assert.deepEqual(ok.body, { status: 'ok', db: 'ok', storage: 'ok' });

  const original = fs.accessSync;
  fs.accessSync = () => {
    throw Object.assign(new Error('read-only file system'), { code: 'EROFS' });
  };
  try {
    const degraded = await request(app).get('/api/health').expect(503);
    assert.deepEqual(degraded.body, { status: 'degraded', db: 'ok', storage: 'error' });
  } finally {
    fs.accessSync = original;
  }
});

test('admin health reports cached checks instead of running integrity_check', async () => {
  const health = await admin.agent.get('/api/admin/health').expect(200);
  assert.equal(health.body.database, 'pending');
  assert.equal(health.body.damagedBlobs, 0);
  assert.equal(typeof health.body.minFreeDiskBytes, 'number');
});

test('uploads are refused with 507 DISK_FULL below NEOCLOUD_MIN_FREE_DISK', async () => {
  const folder = (await member.agent.post('/api/folders').send({ parentId: 'root', name: 'drop' }).expect(201)).body;
  const share = (await member.agent.post('/api/shares').send({ nodeId: folder.id, mode: 'view_upload' }).expect(201)).body;
  const session = (await member.agent.post('/api/uploads').send({ parentId: 'root', name: 'big.bin', size: 5 }).expect(201)).body;

  await withStatfs(512 * 1024 * 1024, async () => {
    // A non-admin still gets the real reason: the message is written for people.
    const single = await upload(member.agent, 'full.txt', 'no room').expect(507);
    assert.equal(single.body.code, 'DISK_FULL');
    assert.match(single.body.error, /disk space/);

    const part = await member.agent.put(`/api/uploads/${session.id}/parts/0`)
      .set('Content-Type', 'application/octet-stream').send(Buffer.from('12345')).expect(507);
    assert.equal(part.body.code, 'DISK_FULL');

    const visitor = await request(app).put(`/api/public/shares/${share.token}/upload?name=guest.txt`)
      .set('Content-Type', 'application/octet-stream').send(Buffer.from('guest')).expect(507);
    assert.equal(visitor.body.code, 'DISK_FULL');
  });

  // The floor is configurable, and nothing was written meanwhile.
  process.env.NEOCLOUD_MIN_FREE_DISK = '100MB';
  try {
    await withStatfs(512 * 1024 * 1024, () => upload(member.agent, 'fits.txt', 'room enough').expect(201));
  } finally {
    delete process.env.NEOCLOUD_MIN_FREE_DISK;
  }
  const names = (await member.agent.get('/api/folders/root/children').expect(200)).body.items.map((node) => node.name);
  assert.ok(names.includes('fits.txt'));
  assert.ok(!names.includes('full.txt'));
});

let target;

test('a backup uploads what its database snapshot references, nothing written later', async () => {
  target = (await admin.agent.post('/api/admin/backups/targets').send({
    name: 'Ops disk',
    type: 'local',
    config: { path: backupDir },
    retention: 5,
  }).expect(201)).body;
  await upload(admin.agent, 'before.txt', 'in the snapshot').expect(201);

  // A file uploaded right after the database copy is taken.
  let late;
  const originalBackup = db.backup;
  db.backup = async function backupThenUpload(...args) {
    const result = await originalBackup.apply(this, args);
    db.backup = originalBackup;
    late = (await upload(admin.agent, 'after.txt', 'written after the snapshot').expect(201)).body;
    return result;
  };
  let run;
  try {
    run = await runner.runBackup(target.id);
  } finally {
    db.backup = originalBackup;
  }
  assert.equal(run.status, 'ok', run.error);

  const lateBlob = db.prepare('SELECT id, user_id FROM blobs WHERE id = (SELECT blob_id FROM nodes WHERE id = ?)').get(late.id);
  const manifest = readManifest(run.snapshot);
  assert.equal(manifest.format, 2);
  assert.equal(manifest.schemaVersion, db.pragma('user_version', { simple: true }));
  assert.equal(manifest.missingBlobs, 0);
  assert.ok(!manifest.blobs.includes(`${lateBlob.user_id}/${lateBlob.id}`));
  assert.equal(fs.existsSync(path.join(backupDir, 'blobs', String(lateBlob.user_id), lateBlob.id)), false);
  // Every blob the snapshot lists is on the target.
  for (const blob of manifest.blobs) assert.ok(fs.existsSync(path.join(backupDir, 'blobs', blob)), blob);
  assert.equal(run.blobsUploaded, manifest.blobs.length);
});

test('runs left running by a crash are marked interrupted', () => {
  const stuck = db.prepare("INSERT INTO backup_runs (target_id, status, snapshot) VALUES (?, 'running', 'x')").run(target.id);
  assert.equal(runner.markInterruptedRuns(), 1);
  const row = db.prepare('SELECT status, error, finished_at FROM backup_runs WHERE id = ?').get(stuck.lastInsertRowid);
  assert.equal(row.status, 'failed');
  assert.equal(row.error, 'interrupted');
  assert.ok(row.finished_at);
  assert.equal(runner.markInterruptedRuns(), 0);
});

test('a failed scheduled run is retried with backoff, three times per slot', () => {
  const scheduled = db.prepare(`
    INSERT INTO backup_targets (name, type, config_enc, schedule, hour, retention)
    SELECT 'retry', type, config_enc, 'daily', 3, 1 FROM backup_targets WHERE id = ?
  `).run(target.id);
  const row = db.prepare('SELECT * FROM backup_targets WHERE id = ?').get(scheduled.lastInsertRowid);
  const now = new Date();
  now.setHours(12, 0, 0, 0);
  const sqlTime = (date) => date.toISOString().slice(0, 19).replace('T', ' ');
  const minutesAgo = (minutes) => sqlTime(new Date(now - minutes * 60 * 1000));
  const fail = (finishedMinutesAgo) => db.prepare(`
    INSERT INTO backup_runs (target_id, status, started_at, finished_at, error) VALUES (?, 'failed', ?, ?, 'boom')
  `).run(row.id, minutesAgo(finishedMinutesAgo + 1), minutesAgo(finishedMinutesAgo));

  assert.equal(isDue(row, now), true);
  fail(5);
  assert.equal(isDue(row, now), false, 'waits 10 minutes after the first failure');
  db.prepare('UPDATE backup_runs SET finished_at = ? WHERE id = (SELECT MAX(id) FROM backup_runs)').run(minutesAgo(11));
  assert.equal(isDue(row, now), true);
  fail(20);
  assert.equal(isDue(row, now), false, 'then 30 minutes');
  fail(100);
  fail(100);
  assert.equal(isDue(row, now), false, 'gives up after three retries until the next slot');
  db.prepare('DELETE FROM backup_targets WHERE id = ?').run(row.id);
});

test('verify checks the newest snapshot and is listed with the target', async () => {
  const verified = await admin.agent.post(`/api/admin/backups/targets/${target.id}/verify`).expect(200);
  assert.equal(verified.body.ok, true, verified.body.error);
  assert.equal(verified.body.database, 'ok');
  assert.equal(verified.body.missingBlobs, 0);
  assert.ok(verified.body.blobsChecked > 0);

  const listed = (await admin.agent.get('/api/admin/backups').expect(200)).body.targets.find((item) => item.id === target.id);
  assert.equal(listed.lastVerification.ok, true);
  assert.equal(listed.lastVerification.snapshot, verified.body.snapshot);

  // A blob gone from the target fails the check.
  const [blob] = readManifest(verified.body.snapshot).blobs;
  const remote = path.join(backupDir, 'blobs', blob);
  const saved = fs.readFileSync(remote);
  fs.rmSync(remote);
  const broken = await verifyTarget(target.id);
  assert.equal(broken.ok, false);
  assert.equal(broken.missingBlobs, 1);
  fs.writeFileSync(remote, saved);

  await member.agent.post(`/api/admin/backups/targets/${target.id}/verify`).expect(403);
});

test('restore refuses while the server runs, and snapshots from a newer schema', async () => {
  const [snapshot] = (await runner.snapshotsOf(target.id)).snapshots;

  fs.writeFileSync(PID_FILE, String(process.ppid));
  try {
    await assert.rejects(restoreSnapshot(target.id, snapshot), (err) => err.code === 'SERVER_RUNNING');
  } finally {
    fs.rmSync(PID_FILE, { force: true });
  }

  const manifestPath = path.join(backupDir, 'snapshots', snapshot, 'manifest.json');
  const original = fs.readFileSync(manifestPath, 'utf8');
  fs.writeFileSync(manifestPath, JSON.stringify({ ...JSON.parse(original), schemaVersion: 999 }));
  try {
    await assert.rejects(restoreSnapshot(target.id, snapshot), /newer NeoCloud/);
  } finally {
    fs.writeFileSync(manifestPath, original);
  }
  // Nothing local changed.
  assert.equal(db.open, true);
  assert.equal(fs.readdirSync(path.join(home, 'data')).some((name) => name.includes('before-restore')), false);
});

test('without a scheduled target, a daily encrypted database snapshot is kept locally and restorable', async () => {
  db.prepare('DELETE FROM backup_targets').run();
  await checkSchedules();
  const [daily] = listLocalSnapshots('daily');
  assert.ok(daily, 'a daily snapshot was taken');
  const bytes = fs.readFileSync(daily.file);
  assert.equal(bytes.subarray(0, 4).toString(), 'NCB1');
  assert.equal(bytes.includes(Buffer.from('SQLite format')), false);
  // Not again within the day.
  await checkSchedules();
  assert.equal(listLocalSnapshots('daily').length, 1);

  const usersBefore = db.prepare('SELECT username FROM users ORDER BY id').all();
  // Closes the database handle: must stay the last test in this file.
  const { previous } = await restoreLocalSnapshot(daily.file);
  assert.ok(previous && fs.existsSync(previous));
  const Database = require('better-sqlite3');
  const restored = new Database(path.join(home, 'data', 'neocloud.db'), { readonly: true });
  assert.deepEqual(restored.prepare('SELECT username FROM users ORDER BY id').all(), usersBefore);
  assert.equal(restored.prepare('SELECT COUNT(*) AS n FROM backup_targets').get().n, 0);
  restored.close();
});
