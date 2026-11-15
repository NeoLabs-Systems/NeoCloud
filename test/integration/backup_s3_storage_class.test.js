'use strict';

// S3 targets with a storage class, against an in-process fake S3: uploads in
// the configured class, runs and verify that never read archived objects,
// Glacier restores that ask first and finish on a second call, and moving
// existing objects to the class.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { Readable } = require('stream');

process.env.NEOCLOUD_OFFICE_PREVIEW = 'false';
// Restore refuses while something answers /api/health on PORT.
process.env.PORT = '1';
const { cleanup, registerUser, upload } = require('../helpers/app');
const { createFakeS3 } = require('../helpers/fake_s3');
const { LOCAL_BACKUP_DIR } = require('../../runtime/paths');
const runner = require('../../server/services/backup/runner');
const { restoreSnapshot } = require('../../server/services/backup/restore');
const { verifyTarget } = require('../../server/services/backup/verify');
const { migrateStorageClass } = require('../../server/services/backup/storage_class');
const { createS3Adapter } = require('../../server/services/backup/adapters/s3');

const fake = createFakeS3();
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-s3-class-'));
let endpoint;
let owner;

test.before(async () => {
  endpoint = await fake.start();
  fake.createBucket('backups');
  owner = await registerUser('s3class');
  await upload(owner.agent, 'a.txt', 'first file').expect(201);
  await upload(owner.agent, 'b.txt', 'second file').expect(201);
});

test.after(async () => {
  await fake.stop();
  cleanup();
  fs.rmSync(scratch, { recursive: true, force: true });
});

function s3Config(prefix, storageClass) {
  return {
    endpoint,
    region: 'eu-central-1',
    bucket: 'backups',
    accessKeyId: 'test',
    secretAccessKey: 'test-secret',
    prefix,
    forcePathStyle: true,
    ...(storageClass ? { storageClass } : {}),
  };
}

async function s3Target(prefix, storageClass) {
  return (await owner.agent.post('/api/admin/backups/targets').send({
    name: prefix,
    type: 's3',
    config: s3Config(prefix, storageClass),
    retention: 5,
  }).expect(201)).body;
}

function objectsUnder(prefix) {
  return [...fake.objects.entries()].filter(([id]) => id.startsWith(`backups/${prefix}/`));
}

function requestsSince(mark) {
  return fake.requests.slice(mark);
}

test('storageClass is validated and kept with the target', async () => {
  const bad = await owner.agent.post('/api/admin/backups/targets').send({
    name: 'bad', type: 's3', config: s3Config('bad', 'COLD'),
  });
  assert.equal(bad.status, 400);
  assert.equal(bad.body.code, 'INVALID_VALUE');
  const target = await s3Target('lower', 'glacier');
  assert.equal(target.config.storageClass, 'GLACIER');
  assert.equal(target.storageMigration, null);
});

test('a run uploads in the configured class and never reads the target', async () => {
  const target = await s3Target('glacier', 'GLACIER');
  const mark = fake.requests.length;
  const run = await runner.runBackup(target.id);
  assert.equal(run.status, 'ok', run.error);
  const objects = objectsUnder('glacier');
  assert.ok(objects.length >= 4, 'blobs, database and manifest');
  for (const [id, object] of objects) assert.equal(object.storageClass, 'GLACIER', id);
  const requests = requestsSince(mark);
  assert.ok(requests.some((r) => r.method === 'PUT' && r.storageClass === 'GLACIER'));
  assert.deepEqual(requests.filter((r) => r.method === 'GET' && r.key), [], 'no object GETs during a run');
});

test('runs keep working with archived objects, with and without the local manifest copies', async () => {
  const target = await s3Target('archived', 'GLACIER');
  assert.equal((await runner.runBackup(target.id)).status, 'ok');
  await upload(owner.agent, 'c.txt', 'third file').expect(201);
  let mark = fake.requests.length;
  const second = await runner.runBackup(target.id);
  assert.equal(second.status, 'ok', second.error);
  assert.equal(second.blobsUploaded, 1);
  assert.deepEqual(requestsSince(mark).filter((r) => r.method === 'GET' && r.key), []);

  // A server without its manifest copies (say, reinstalled) still backs up
  // and only skips pruning blobs.
  fs.rmSync(path.join(LOCAL_BACKUP_DIR, 'manifests', String(target.id)), { recursive: true, force: true });
  mark = fake.requests.length;
  const third = await runner.runBackup(target.id);
  assert.equal(third.status, 'ok', third.error);
  assert.deepEqual(requestsSince(mark).filter((r) => r.method === 'GET' && r.key), []);
});

test('a STANDARD target whose objects a lifecycle rule archived still runs', async () => {
  const target = await s3Target('lifecycle');
  assert.equal((await runner.runBackup(target.id)).status, 'ok');
  fake.setStorageClass((id) => id.startsWith('backups/lifecycle/'), 'GLACIER');
  const run = await runner.runBackup(target.id);
  assert.equal(run.status, 'ok', run.error);
  const fresh = objectsUnder('lifecycle').filter(([id]) => id.includes(run.snapshot));
  assert.ok(fresh.every(([, object]) => object.storageClass === 'STANDARD'));
});

test('verify checks an archived target without reading it', async () => {
  const target = await s3Target('verify', 'GLACIER');
  assert.equal((await runner.runBackup(target.id)).status, 'ok');
  const mark = fake.requests.length;
  const result = await verifyTarget(target.id);
  assert.equal(result.ok, true, result.error);
  assert.equal(result.mode, 'metadata');
  assert.equal(result.database, 'archived');
  assert.ok(result.blobsChecked >= 2);
  assert.equal(result.missingBlobs, 0);
  assert.deepEqual(requestsSince(mark).filter((r) => r.method === 'GET' && r.key), []);

  // A missing blob is still a failure.
  const blob = objectsUnder('verify').find(([id]) => id.includes('/blobs/'))[0];
  fake.objects.delete(blob);
  const broken = await verifyTarget(target.id);
  assert.equal(broken.ok, false);
  assert.equal(broken.missingBlobs, 1);
});

test('moving existing objects copies them in the new class, skips done and archived ones, resumes, and stays in the prefix', async () => {
  const target = await s3Target('move');
  assert.equal((await runner.runBackup(target.id)).status, 'ok');
  fake.objects.set('backups/elsewhere/keep.bin', { body: Buffer.from('not ours'), storageClass: 'STANDARD', restore: null });
  fake.objects.set('backups/move/notes.txt', { body: Buffer.from('not NeoCloud layout'), storageClass: 'STANDARD', restore: null });
  const [deepId] = objectsUnder('move').find(([id]) => id.includes('/blobs/'));
  fake.objects.get(deepId).storageClass = 'DEEP_ARCHIVE';
  const before = objectsUnder('move').length;

  await owner.agent.put(`/api/admin/backups/targets/${target.id}`).send({ config: s3Config('move', 'GLACIER') }).expect(200);
  const mark = fake.requests.length;
  await owner.agent.post(`/api/admin/backups/targets/${target.id}/storage-class/migrate`).send({}).expect(202);
  let migration;
  for (let i = 0; i < 100; i += 1) {
    const list = (await owner.agent.get('/api/admin/backups').expect(200)).body.targets;
    migration = list.find((t) => t.id === target.id).storageMigration;
    if (migration?.status !== 'running') break;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  assert.equal(migration.status, 'ok', migration.error);
  assert.equal(migration.storageClass, 'GLACIER');
  assert.equal(migration.objectsArchived, 1);
  assert.equal(migration.objectsDone, migration.objectsTotal);
  assert.equal(migration.bytesDone, migration.bytesTotal);

  const requests = requestsSince(mark);
  assert.deepEqual(requests.filter((r) => r.method === 'DELETE' || r.query === 'delete'), [], 'never deletes');
  assert.ok(requests.every((r) => !r.key || r.key.startsWith('move/')), 'only keys below the prefix');
  assert.equal(objectsUnder('move').length, before);
  assert.equal(fake.objects.get('backups/elsewhere/keep.bin').storageClass, 'STANDARD');
  assert.equal(fake.objects.get('backups/move/notes.txt').storageClass, 'STANDARD');
  assert.equal(fake.objects.get(deepId).storageClass, 'DEEP_ARCHIVE');
  for (const [id, object] of objectsUnder('move')) {
    if (id !== deepId && !id.endsWith('notes.txt')) assert.equal(object.storageClass, 'GLACIER', id);
  }

  // Interrupted half-way: a second run copies only what is left.
  const [leftId] = objectsUnder('move').find(([id]) => id.includes('/snapshots/'));
  fake.objects.get(leftId).storageClass = 'STANDARD';
  const resumed = await migrateStorageClass(target.id);
  assert.equal(resumed.status, 'ok', resumed.error);
  assert.equal(resumed.objectsDone, 1);
  assert.equal(resumed.objectsSkipped, migration.objectsDone - 1 + migration.objectsSkipped);
  assert.equal(resumed.objectsArchived, 1);
  assert.equal(fake.objects.get(leftId).storageClass, 'GLACIER');
});

test('objects above the single-copy limit move with a multipart copy', async () => {
  const body = Buffer.from('0123456789abcdefghij-multipart-copy');
  fake.objects.set('backups/big/blobs/1/large', { body, storageClass: 'STANDARD', restore: null });
  const adapter = createS3Adapter({ ...s3Config('big', 'GLACIER'), maxSingleCopy: 10, copyPartSize: 8 });
  const mark = fake.requests.length;
  await adapter.changeStorageClass('blobs/1/large', body.length);
  adapter.close();
  const copied = fake.objects.get('backups/big/blobs/1/large');
  assert.equal(copied.storageClass, 'GLACIER');
  assert.deepEqual(copied.body, body);
  assert.equal(requestsSince(mark).filter((r) => r.query.includes('partNumber')).length, Math.ceil(body.length / 8));
});

test('migration refuses targets that are not S3', async () => {
  const local = (await owner.agent.post('/api/admin/backups/targets').send({
    name: 'disk', type: 'local', config: { path: path.join(scratch, 'disk') },
  }).expect(201)).body;
  const res = await owner.agent.post(`/api/admin/backups/targets/${local.id}/storage-class/migrate`).send({});
  assert.equal(res.status, 400);
  assert.equal(res.body.code, 'BACKUP_NOT_S3');
});

test('restore asks Glacier first, then completes once the objects are restored (also --from)', async () => {
  const target = await s3Target('restore', 'GLACIER');
  const run = await runner.runBackup(target.id);
  assert.equal(run.status, 'ok', run.error);
  // Lose the local blobs so the restore has to fetch them.
  const { BLOB_DIR } = require('../../runtime/paths');
  fs.rmSync(BLOB_DIR, { recursive: true, force: true });

  // A new machine: a target JSON file, the cheaper Bulk tier.
  const spec = path.join(scratch, 'target.json');
  fs.writeFileSync(spec, JSON.stringify({ type: 's3', config: s3Config('restore', 'GLACIER') }));
  const first = await restoreSnapshot({ from: spec }, run.snapshot, { tier: 'Bulk' });
  assert.equal(first.waiting, true);
  assert.ok(first.requested >= 4, `requested ${first.requested}`);
  const archived = objectsUnder('restore').filter(([, o]) => o.restore === 'ongoing');
  assert.equal(archived.length, first.requested);
  assert.match(archived[0][1].restoreRequest, /<Days>7<\/Days>/);
  assert.match(archived[0][1].restoreRequest, /<Tier>Bulk<\/Tier>/);

  const again = await restoreSnapshot(target.id, run.snapshot);
  assert.equal(again.waiting, true);
  assert.equal(again.requested, 0);
  assert.equal(again.pending, first.requested);

  fake.finishRestores();
  const logs = [];
  const done = await restoreSnapshot(target.id, run.snapshot, { log: (line) => logs.push(line) });
  assert.deepEqual(done, { restored: true });
  assert.ok(logs.some((line) => /Downloaded [1-9]\d* blob/.test(line)), logs.join('\n'));
});

test('the test object and new uploads honour a STANDARD_IA class too', async () => {
  const adapter = createS3Adapter(s3Config('ia', 'STANDARD_IA'));
  await adapter.put('blobs/1/x', () => Readable.from(['content']));
  adapter.close();
  assert.equal(fake.objects.get('backups/ia/blobs/1/x').storageClass, 'STANDARD_IA');
});
