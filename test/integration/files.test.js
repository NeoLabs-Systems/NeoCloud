'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { cleanup, registerUser, upload } = require('../helpers/app');

test.after(cleanup);

let owner;

test.before(async () => {
  owner = await registerUser('files');
});

async function rootItems(agent) {
  const res = await agent.get('/api/folders/root/children').expect(200);
  return res.body.items;
}

test('the first account is admin and starts with an empty root', async () => {
  assert.equal(owner.user.isAdmin, true);
  assert.deepEqual(await rootItems(owner.agent), []);
});

test('uploads, lists and downloads a file', async () => {
  const res = await upload(owner.agent, 'hello.txt', 'hello neocloud').expect(201);
  assert.equal(res.body.name, 'hello.txt');
  assert.equal(res.body.size, 14);
  assert.equal(res.body.mime, 'text/plain');
  assert.equal(res.body.etag, crypto.createHash('sha256').update('hello neocloud').digest('hex'));

  const download = await owner.agent.get(`/api/files/${res.body.id}/content`).expect(200);
  assert.equal(download.text, 'hello neocloud');
  assert.match(download.headers['content-security-policy'], /sandbox/);
  assert.equal(download.headers['accept-ranges'], 'bytes');

  const partial = await owner.agent.get(`/api/files/${res.body.id}/content`).set('Range', 'bytes=6-').expect(206);
  assert.equal(partial.text, 'neocloud');
  assert.equal(partial.headers['content-range'], 'bytes 6-13/14');

  await owner.agent.get(`/api/files/${res.body.id}/content`).set('If-None-Match', `"${res.body.etag}"`).expect(304);
  await owner.agent.get(`/api/files/${res.body.id}/content`).set('Range', 'bytes=99-').expect(416);
});

test('name conflicts: fail, rename, replace with versions', async () => {
  await upload(owner.agent, 'doc.md', 'v1').expect(201);
  const clash = await upload(owner.agent, 'DOC.md', 'v2').expect(409);
  assert.equal(clash.body.code, 'NAME_EXISTS');

  const renamed = await upload(owner.agent, 'doc.md', 'copy', { conflict: 'rename' }).expect(201);
  assert.equal(renamed.body.name, 'doc (1).md');

  const replaced = await upload(owner.agent, 'doc.md', 'v2', { conflict: 'replace' }).expect(201);
  assert.equal(replaced.body.version, 2);

  const versions = await owner.agent.get(`/api/files/${replaced.body.id}/versions`).expect(200);
  assert.equal(versions.body.versions.length, 1);
  assert.equal(versions.body.versions[0].version, 1);

  const old = await owner.agent.get(`/api/files/${replaced.body.id}/content?version=1`).expect(200);
  assert.equal(old.text, 'v1');

  const restored = await owner.agent.post(`/api/files/${replaced.body.id}/versions/1/restore`).expect(200);
  assert.equal(restored.body.version, 3);
  const current = await owner.agent.get(`/api/files/${replaced.body.id}/content`).expect(200);
  assert.equal(current.text, 'v1');
});

test('replace with a stale base etag is refused', async () => {
  const first = await upload(owner.agent, 'sync.txt', 'one').expect(201);
  await owner.agent
    .put('/api/upload?name=sync.txt&conflict=replace')
    .set('If-Match', '"not-the-current-etag"')
    .send(Buffer.from('two'))
    .expect(412);
  await owner.agent
    .put('/api/upload?name=sync.txt&conflict=replace')
    .set('If-Match', `"${first.body.etag}"`)
    .send(Buffer.from('two'))
    .expect(201);
});

test('folders: nested create, rename, move, copy and no moving into itself', async () => {
  const deep = await owner.agent.post('/api/folders').send({ path: 'Fotos/2026/Mai' }).expect(201);
  const again = await owner.agent.post('/api/folders').send({ path: 'Fotos/2026/Mai' }).expect(201);
  assert.equal(again.body.id, deep.body.id);

  const node = await owner.agent.get(`/api/nodes/${deep.body.id}`).expect(200);
  assert.deepEqual(node.body.path.map((p) => p.name), ['', 'Fotos', '2026', 'Mai']);
  const fotosId = node.body.path[1].id;

  await upload(owner.agent, 'beach.jpg', 'jpeg bytes', { parentId: deep.body.id }).expect(201);

  const moveIntoSelf = await owner.agent.patch(`/api/nodes/${fotosId}`).send({ parentId: deep.body.id }).expect(400);
  assert.equal(moveIntoSelf.body.code, 'MOVE_INTO_SELF');

  const renamed = await owner.agent.patch(`/api/nodes/${deep.body.id}`).send({ name: 'May' }).expect(200);
  assert.equal(renamed.body.name, 'May');

  const copy = await owner.agent.post(`/api/nodes/${fotosId}/copy`).send({}).expect(201);
  assert.equal(copy.body.name, 'Fotos (1)');
  const copied = await owner.agent.get(`/api/folders/${copy.body.id}/children`).expect(200);
  assert.equal(copied.body.items[0].name, '2026');

  const moved = await owner.agent.patch(`/api/nodes/${copy.body.id}`).send({ parentId: fotosId }).expect(200);
  assert.equal(moved.body.parentId, fotosId);

  const search = await owner.agent.get('/api/search?q=beach').expect(200);
  assert.equal(search.body.items.length, 2);
});

test('copies share storage; trash, restore and purge keep usage right', async () => {
  const before = (await owner.agent.get('/api/account/usage').expect(200)).body.usedBytes;
  const file = await upload(owner.agent, 'big.bin', crypto.randomBytes(5000)).expect(201);
  const afterUpload = (await owner.agent.get('/api/account/usage')).body.usedBytes;
  assert.equal(afterUpload, before + 5000);

  const copy = await owner.agent.post(`/api/nodes/${file.body.id}/copy`).send({}).expect(201);
  assert.equal((await owner.agent.get('/api/account/usage')).body.usedBytes, afterUpload);

  await owner.agent.delete(`/api/nodes/${file.body.id}`).expect(200);
  await owner.agent.get(`/api/files/${file.body.id}/content`).expect(404);
  const trash = await owner.agent.get('/api/trash').expect(200);
  assert.ok(trash.body.items.some((item) => item.id === file.body.id));

  await owner.agent.post(`/api/trash/${file.body.id}/restore`).expect(200);
  await owner.agent.get(`/api/files/${file.body.id}/content`).expect(200);

  await owner.agent.delete(`/api/nodes/${file.body.id}`).expect(200);
  await owner.agent.delete(`/api/trash/${file.body.id}`).expect(200);
  // The copy still holds the blob.
  assert.equal((await owner.agent.get('/api/account/usage')).body.usedBytes, afterUpload);

  await owner.agent.delete(`/api/nodes/${copy.body.id}`).expect(200);
  await owner.agent.delete('/api/trash').expect(200);
  assert.equal((await owner.agent.get('/api/account/usage')).body.usedBytes, before);
});

test('trashing a folder hides its contents and restore brings them back', async () => {
  const folder = await owner.agent.post('/api/folders').send({ name: 'Projekt' }).expect(201);
  const inner = await upload(owner.agent, 'plan.txt', 'plan', { parentId: folder.body.id }).expect(201);
  await owner.agent.delete(`/api/nodes/${folder.body.id}`).expect(200);
  await owner.agent.get(`/api/files/${inner.body.id}/content`).expect(404);
  const search = await owner.agent.get('/api/search?q=plan.txt').expect(200);
  assert.equal(search.body.items.length, 0);
  await owner.agent.post(`/api/trash/${folder.body.id}/restore`).expect(200);
  await owner.agent.get(`/api/files/${inner.body.id}/content`).expect(200);
});

test('resumable upload: parts in any order, resume, checksum', async () => {
  const data = crypto.randomBytes(8 * 1024 * 1024 + 4321);
  const sha256 = crypto.createHash('sha256').update(data).digest('hex');
  const session = await owner.agent.post('/api/uploads').send({ name: 'video.mp4', size: data.length, sha256 }).expect(201);
  const { id, partSize, partCount } = session.body;
  assert.equal(partCount, 2);

  await owner.agent.put(`/api/uploads/${id}/parts/1`).set('Content-Type', 'application/octet-stream').send(data.subarray(partSize)).expect(200);
  const incomplete = await owner.agent.post(`/api/uploads/${id}/complete`).send({}).expect(409);
  assert.equal(incomplete.body.code, 'UPLOAD_INCOMPLETE');

  const status = await owner.agent.get(`/api/uploads/${id}`).expect(200);
  assert.deepEqual(status.body.receivedParts, [1]);

  const wrongSize = await owner.agent.put(`/api/uploads/${id}/parts/0`).set('Content-Type', 'application/octet-stream').send(data.subarray(0, 10)).expect(400);
  assert.equal(wrongSize.body.code, 'SIZE_MISMATCH');

  await owner.agent.put(`/api/uploads/${id}/parts/0`).set('Content-Type', 'application/octet-stream').send(data.subarray(0, partSize)).expect(200);
  const done = await owner.agent.post(`/api/uploads/${id}/complete`).send({}).expect(201);
  assert.equal(done.body.etag, sha256);
  assert.equal(done.body.mime, 'video/mp4');

  const tail = await owner.agent
    .get(`/api/files/${done.body.id}/content`)
    .set('Range', `bytes=${data.length - 100}-`)
    .buffer(true)
    .parse((res, callback) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => callback(null, Buffer.concat(chunks)));
    })
    .expect(206);
  assert.deepEqual(tail.body, data.subarray(data.length - 100));
  await owner.agent.get(`/api/uploads/${id}`).expect(404);
});

test('a damaged single upload is rejected by checksum', async () => {
  const res = await owner.agent
    .put('/api/upload?name=check.txt')
    .set('X-Content-SHA256', '0'.repeat(64))
    .send(Buffer.from('content'))
    .expect(422);
  assert.equal(res.body.code, 'CHECKSUM_MISMATCH');
});

test('a retried upload of the same content (answer lost) never leaves "name (1)"', async () => {
  const folder = await owner.agent.post('/api/folders').send({ name: 'Retry' }).expect(201);
  const content = Buffer.from('IMG_0042 bytes');
  const hash = crypto.createHash('sha256').update(content).digest('hex');
  const send = (body, sha256 = hash) => owner.agent
    .put(`/api/upload?parentId=${folder.body.id}&name=IMG_0042.jpg&conflict=rename`)
    .set('Content-Type', 'application/octet-stream')
    .set('X-Content-SHA256', sha256)
    .send(body);
  const first = await send(content).expect(201);
  const usage = async () => (await owner.agent.get('/api/account/usage').expect(200)).body.usedBytes;
  const used = await usage();
  const again = await send(content).expect(201);
  assert.equal(again.body.id, first.body.id);
  assert.equal(await usage(), used);

  // The same through a resumable upload.
  const session = await owner.agent.post('/api/uploads')
    .send({ parentId: folder.body.id, name: 'IMG_0042.jpg', size: content.length, conflict: 'rename', sha256: hash })
    .expect(201);
  await owner.agent.put(`/api/uploads/${session.body.id}/parts/0`).set('Content-Type', 'application/octet-stream').send(content).expect(200);
  const resumed = await owner.agent.post(`/api/uploads/${session.body.id}/complete`).send({}).expect(201);
  assert.equal(resumed.body.id, first.body.id);
  await owner.agent.get(`/api/uploads/${session.body.id}`).expect(404);

  // Other content under that name still gets a free name; so does the same
  // content when the client declared no hash (an explicit "keep both").
  const other = Buffer.from('IMG_0042 edited');
  const otherHash = crypto.createHash('sha256').update(other).digest('hex');
  assert.equal((await send(other, otherHash).expect(201)).body.name, 'IMG_0042 (1).jpg');
  const kept = await upload(owner.agent, 'IMG_0042.jpg', content, { parentId: folder.body.id, conflict: 'rename' }).expect(201);
  assert.equal(kept.body.name, 'IMG_0042 (2).jpg');
  const names = (await owner.agent.get(`/api/folders/${folder.body.id}/children`).expect(200)).body.items.map((item) => item.name).sort();
  assert.deepEqual(names, ['IMG_0042 (1).jpg', 'IMG_0042 (2).jpg', 'IMG_0042.jpg']);
});

test('uploads/check finds content in the folder or below it, never in the trash or elsewhere', async () => {
  const photos = await owner.agent.post('/api/folders').send({ name: 'Kamera' }).expect(201);
  const sorted = await owner.agent.post('/api/folders').send({ parentId: photos.body.id, path: '2026/Urlaub' }).expect(201);
  const outside = await owner.agent.post('/api/folders').send({ name: 'Anderswo' }).expect(201);
  const content = 'IMG_0001 bytes';
  const hash = crypto.createHash('sha256').update(content).digest('hex');
  const check = (body) => owner.agent.post('/api/uploads/check').send({ parentId: photos.body.id, ...body });

  assert.equal((await check({ sha256: hash, size: content.length }).expect(200)).body.exists, false);
  await upload(owner.agent, 'other.jpg', 'elsewhere', { parentId: outside.body.id }).expect(201);
  const elsewhere = crypto.createHash('sha256').update('elsewhere').digest('hex');
  assert.equal((await check({ sha256: elsewhere, size: 9 }).expect(200)).body.exists, false);

  const file = await upload(owner.agent, 'IMG_0001.jpg', content, { parentId: sorted.body.id }).expect(201);
  assert.equal((await check({ sha256: hash.toUpperCase(), size: content.length }).expect(200)).body.exists, true);
  assert.equal((await check({ sha256: hash, size: content.length + 1 }).expect(200)).body.exists, false);

  await owner.agent.delete(`/api/nodes/${sorted.body.id}`).expect(200);
  assert.equal((await check({ sha256: hash, size: content.length }).expect(200)).body.exists, false);

  assert.equal((await check({ sha256: 'xyz', size: 1 }).expect(400)).body.code, 'INVALID_HASH');
  assert.equal((await check({ size: 1 }).expect(400)).body.code, 'INVALID_HASH');
  assert.equal((await check({ sha256: hash, size: -1 }).expect(400)).body.code, 'INVALID_SIZE');
  // A file in the trash is gone like any other node; a missing id too.
  await owner.agent.post('/api/uploads/check').send({ parentId: file.body.id, sha256: hash, size: 1 }).expect(404);
  const loose = await upload(owner.agent, 'loose.jpg', 'x', { parentId: photos.body.id }).expect(201);
  const notFolder = await owner.agent.post('/api/uploads/check').send({ parentId: loose.body.id, sha256: hash, size: 1 }).expect(400);
  assert.equal(notFolder.body.code, 'NOT_A_FOLDER');
});

test('zip download contains folders and files', async () => {
  const folder = await owner.agent.post('/api/folders').send({ name: 'Archiv' }).expect(201);
  await upload(owner.agent, 'a.txt', 'aaa', { parentId: folder.body.id }).expect(201);
  await owner.agent.post('/api/folders').send({ parentId: folder.body.id, name: 'leer' }).expect(201);
  const res = await owner.agent
    .get(`/api/download/zip?ids=${folder.body.id}`)
    .buffer(true)
    .parse((stream, callback) => {
      const chunks = [];
      stream.on('data', (chunk) => chunks.push(chunk));
      stream.on('end', () => callback(null, Buffer.concat(chunks)));
    })
    .expect(200);
  assert.equal(res.headers['content-type'], 'application/zip');
  assert.match(res.headers['content-disposition'], /Archiv\.zip/);
  const text = res.body.toString('latin1');
  assert.ok(text.startsWith('PK'));
  assert.ok(text.includes('Archiv/a.txt'));
  assert.ok(text.includes('Archiv/leer/'));
});

test('sync: tree snapshot, then changes from the cursor', async () => {
  const snapshot = await owner.agent.get('/api/sync/tree').expect(200);
  assert.ok(snapshot.body.nodes.length > 1);
  const { cursor } = snapshot.body;

  const file = await upload(owner.agent, 'synced.txt', 'x').expect(201);
  await owner.agent.patch(`/api/nodes/${file.body.id}`).send({ name: 'synced2.txt' }).expect(200);
  await owner.agent.delete(`/api/nodes/${file.body.id}`).expect(200);

  const changes = await owner.agent.get(`/api/sync/changes?cursor=${cursor}`).expect(200);
  assert.deepEqual(changes.body.changes.map((c) => c.op), ['create', 'rename', 'trash']);
  assert.equal(changes.body.changes[2].node.trashedAt !== undefined, true);
  const next = await owner.agent.get(`/api/sync/changes?cursor=${changes.body.cursor}`).expect(200);
  assert.equal(next.body.changes.length, 0);
});

test('sync: cursor=latest starts at the head of the journal', async () => {
  await upload(owner.agent, 'head.txt', 'x').expect(201);
  const head = await owner.agent.get('/api/sync/changes?cursor=latest').expect(200);
  assert.deepEqual(head.body.changes, []);
  assert.equal(head.body.hasMore, false);
  const snapshot = await owner.agent.get('/api/sync/tree').expect(200);
  assert.equal(head.body.cursor, snapshot.body.cursor);
  assert.ok(head.body.cursor > 0);

  const file = await upload(owner.agent, 'after-head.txt', 'x').expect(201);
  const after = await owner.agent.get(`/api/sync/changes?cursor=${head.body.cursor}`).expect(200);
  assert.deepEqual(after.body.changes.map((c) => c.nodeId), [file.body.id]);
});

test('desktop device tokens reach files and sync, not account or admin', async () => {
  const created = await owner.agent.post('/api/account/devices').send({ name: 'Laptop', platform: 'macos' }).expect(201);
  const { token, device } = created.body;
  assert.match(token, /^nc_/);

  const { request, app } = require('../helpers/app');
  await request(app).get('/api/folders/root/children').set('Authorization', `Bearer ${token}`).expect(200);
  await request(app).get('/api/sync/tree').set('Authorization', `Bearer ${token}`).expect(200);
  await request(app).put('/api/upload?name=from-device.txt').set('Authorization', `Bearer ${token}`).send(Buffer.from('d')).expect(201);
  await request(app).get('/api/account').set('Authorization', `Bearer ${token}`).expect(403);
  await request(app).get('/api/admin/users').set('Authorization', `Bearer ${token}`).expect(403);

  await owner.agent.delete(`/api/account/devices/${device.id}`).expect(200);
  await request(app).get('/api/folders/root/children').set('Authorization', `Bearer ${token}`).expect(401);
});

test('settings validate keys and values', async () => {
  const defaults = await owner.agent.get('/api/settings').expect(200);
  assert.equal(defaults.body.settings.theme, 'system');
  const updated = await owner.agent.put('/api/settings').send({ language: 'de', fileView: 'grid' }).expect(200);
  assert.equal(updated.body.settings.language, 'de');
  await owner.agent.put('/api/settings').send({ theme: 'neon' }).expect(400);
  await owner.agent.put('/api/settings').send({ evil: true }).expect(400);
});
