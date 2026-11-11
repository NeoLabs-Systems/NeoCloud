'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const sharp = require('sharp');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let alice;
let bob;
let vault;

function keyParams() {
  return {
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  };
}

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
});

test('creates a vault folder with its wrapped key', async () => {
  const params = keyParams();
  const res = await alice.agent.post('/api/vaults').send({ parentId: 'root', name: 'Tresor', ...params }).expect(201);
  vault = res.body;
  assert.ok(vault.id);
  assert.equal(vault.salt, params.salt);
  assert.equal(vault.iterations, 600000);
  assert.equal(vault.wrappedKey, params.wrappedKey);
  assert.equal(vault.folder.id, vault.folderId);
  assert.equal(vault.folder.vaultId, vault.id);

  const fetched = await alice.agent.get(`/api/vaults/${vault.id}`).expect(200);
  assert.deepEqual(
    { id: fetched.body.id, folderId: fetched.body.folderId, salt: fetched.body.salt, iterations: fetched.body.iterations, wrappedKey: fetched.body.wrappedKey },
    { id: vault.id, folderId: vault.folderId, ...params },
  );
  const listing = await alice.agent.get('/api/folders/root/children').expect(200);
  assert.equal(listing.body.items.find((item) => item.id === vault.folderId).vaultId, vault.id);
  const plain = await alice.agent.post('/api/folders').send({ name: 'Plain' }).expect(201);
  assert.equal(plain.body.vaultId, null);
});

test('vault parameters are validated and vaults are private', async () => {
  const bad = [
    { salt: crypto.randomBytes(8).toString('base64') },
    { wrappedKey: crypto.randomBytes(32).toString('base64') },
    { iterations: 1000 },
    { iterations: '600000.5' },
    { iterations: 599999 },
    { salt: 'not base64!' },
  ];
  for (const override of bad) {
    const res = await alice.agent.post('/api/vaults').send({ name: 'Bad', ...keyParams(), ...override }).expect(400);
    assert.equal(res.body.code, 'VAULT_PARAMS_INVALID');
  }
  await bob.agent.get(`/api/vaults/${vault.id}`).expect(404);
  await bob.agent.put(`/api/vaults/${vault.id}/key`).send(keyParams()).expect(404);
  await bob.agent.post('/api/vaults').send({ parentId: vault.folderId, name: 'x', ...keyParams() }).expect(404);
  const nested = await alice.agent.post('/api/vaults').send({ parentId: vault.folderId, name: 'Inner', ...keyParams() }).expect(409);
  assert.equal(nested.body.code, 'VAULT_BOUNDARY');
});

test('the passphrase change replaces the wrapped key', async () => {
  const params = { ...keyParams(), iterations: 600000 };
  const res = await alice.agent.put(`/api/vaults/${vault.id}/key`).send(params).expect(200);
  assert.equal(res.body.wrappedKey, params.wrappedKey);
  assert.equal(res.body.iterations, 600000);
  await alice.agent.put(`/api/vaults/${vault.id}/key`).send({ ...params, iterations: 5 }).expect(400);
  assert.equal((await alice.agent.get(`/api/vaults/${vault.id}`).expect(200)).body.salt, params.salt);
});

test('vaults made before the 600 000 floor keep working; re-wrapping needs the floor', async () => {
  db.prepare('UPDATE vaults SET iterations = 310000 WHERE id = ?').run(vault.id);
  assert.equal((await alice.agent.get(`/api/vaults/${vault.id}`).expect(200)).body.iterations, 310000);
  const weak = await alice.agent.put(`/api/vaults/${vault.id}/key`).send({ ...keyParams(), iterations: 310000 }).expect(400);
  assert.equal(weak.body.code, 'VAULT_PARAMS_INVALID');
  const strong = await alice.agent.put(`/api/vaults/${vault.id}/key`).send({ ...keyParams(), iterations: 600000 }).expect(200);
  assert.equal(strong.body.iterations, 600000);
});

test('everything created inside a vault inherits it', async () => {
  const folder = await alice.agent.post('/api/folders').send({ parentId: vault.folderId, name: 'ZW5jLWRpcg' }).expect(201);
  assert.equal(folder.body.vaultId, vault.id);
  const deep = await alice.agent.post('/api/folders').send({ parentId: folder.body.id, path: 'YQ/Yg' }).expect(201);
  assert.equal(deep.body.vaultId, vault.id);

  const single = await upload(alice.agent, 'ZW5jLWZpbGU', 'NCV1 single', { parentId: folder.body.id }).expect(201);
  assert.equal(single.body.vaultId, vault.id);

  const data = crypto.randomBytes(1000);
  const session = await alice.agent.post('/api/uploads').send({ parentId: vault.folderId, name: 'cmVzdW1hYmxl', size: data.length }).expect(201);
  await alice.agent.put(`/api/uploads/${session.body.id}/parts/0`).set('Content-Type', 'application/octet-stream').send(data).expect(200);
  const resumable = await alice.agent.post(`/api/uploads/${session.body.id}/complete`).send({}).expect(201);
  assert.equal(resumable.body.vaultId, vault.id);

  const copy = await alice.agent.post(`/api/nodes/${folder.body.id}/copy`).send({ parentId: vault.folderId }).expect(201);
  assert.equal(copy.body.vaultId, vault.id);
  const copied = await alice.agent.get(`/api/folders/${copy.body.id}/children`).expect(200);
  assert.ok(copied.body.items.length > 0);
  assert.ok(copied.body.items.every((item) => item.vaultId === vault.id));

  const changes = await alice.agent.get('/api/sync/changes?cursor=0').expect(200);
  const change = changes.body.changes.find((item) => item.nodeId === single.body.id);
  assert.equal(change.node.vaultId, vault.id);
  const tree = await alice.agent.get('/api/sync/tree').expect(200);
  assert.equal(tree.body.nodes.find((node) => node.id === single.body.id).vaultId, vault.id);
});

test('nothing moves or copies across the vault boundary', async () => {
  const plainFile = (await upload(alice.agent, 'plain.txt', 'plain text').expect(201)).body;
  const plainFolder = (await alice.agent.post('/api/folders').send({ name: 'Elsewhere' }).expect(201)).body;
  const cipher = (await upload(alice.agent, 'Y2lwaGVy', 'NCV1 x', { parentId: vault.folderId }).expect(201)).body;

  const checks = [
    alice.agent.patch(`/api/nodes/${plainFile.id}`).send({ parentId: vault.folderId }),
    alice.agent.patch(`/api/nodes/${cipher.id}`).send({ parentId: plainFolder.id }),
    alice.agent.patch(`/api/nodes/${cipher.id}`).send({ parentId: 'root' }),
    alice.agent.post(`/api/nodes/${plainFile.id}/copy`).send({ parentId: vault.folderId }),
    alice.agent.post(`/api/nodes/${cipher.id}/copy`).send({ parentId: plainFolder.id }),
    alice.agent.post(`/api/nodes/${vault.folderId}/copy`).send({ parentId: plainFolder.id }),
    alice.agent.post(`/api/nodes/${vault.folderId}/copy`).send({}),
  ];
  for (const pending of checks) {
    const res = await pending.expect(409);
    assert.equal(res.body.code, 'VAULT_BOUNDARY');
  }

  // Within the vault, and the vault folder as a whole, things move freely.
  const inner = (await alice.agent.post('/api/folders').send({ parentId: vault.folderId, name: 'aW5uZXI' }).expect(201)).body;
  await alice.agent.patch(`/api/nodes/${cipher.id}`).send({ parentId: inner.id }).expect(200);
  await alice.agent.patch(`/api/nodes/${vault.folderId}`).send({ parentId: plainFolder.id }).expect(200);
  await alice.agent.patch(`/api/nodes/${vault.folderId}`).send({ parentId: 'root', name: 'Tresor 2' }).expect(200);
  const moved = await alice.agent.get(`/api/nodes/${cipher.id}`).expect(200);
  assert.equal(moved.body.node.vaultId, vault.id);
});

test('a vault file isn’t restored outside its vault', async () => {
  const vault2 = (await alice.agent.post('/api/vaults').send({ name: 'Second', ...keyParams() }).expect(201)).body;
  const cipher = (await upload(alice.agent, 'b3JwaGFu', 'NCV1 y', { parentId: vault2.folderId }).expect(201)).body;
  await alice.agent.delete(`/api/nodes/${cipher.id}`).expect(200);
  await alice.agent.delete(`/api/nodes/${vault2.folderId}`).expect(200);
  const refused = await alice.agent.post(`/api/trash/${cipher.id}/restore`).expect(409);
  assert.equal(refused.body.code, 'VAULT_BOUNDARY');
  await alice.agent.post(`/api/trash/${vault2.folderId}/restore`).expect(200);
  const restored = await alice.agent.post(`/api/trash/${cipher.id}/restore`).expect(200);
  assert.equal(restored.body.parentId, vault2.folderId);
});

test('vault files get no previews and stay out of search', async () => {
  const png = await sharp({ create: { width: 8, height: 8, channels: 3, background: '#b07d2b' } }).png().toBuffer();
  const image = (await upload(alice.agent, 'findme.png', png, { parentId: vault.folderId }).expect(201)).body;
  await alice.agent.get(`/api/files/${image.id}/thumbnail?size=256`).expect(404);
  await alice.agent.get(`/api/files/${image.id}/preview`).expect(404);
  const doc = (await upload(alice.agent, 'findme.docx', 'NCV1 z', { parentId: vault.folderId }).expect(201)).body;
  await alice.agent.get(`/api/files/${doc.id}/preview`).expect(404);

  // The same image outside a vault does get a thumbnail.
  const outside = (await upload(alice.agent, 'findme-too.png', png).expect(201)).body;
  await alice.agent.get(`/api/files/${outside.id}/thumbnail?size=256`).expect(200);

  const found = await alice.agent.get('/api/search?q=findme').expect(200);
  assert.deepEqual(found.body.items.map((item) => item.id), [outside.id]);
  const recent = await alice.agent.get('/api/recent').expect(200);
  assert.ok(!recent.body.items.some((item) => item.vaultId));
  // Not even when opened, filtered by type, or searched below the vault.
  await alice.agent.post(`/api/nodes/${image.id}/opened`).expect(204);
  assert.ok(!(await alice.agent.get('/api/recent').expect(200)).body.items.some((item) => item.vaultId));
  assert.ok(!(await alice.agent.get('/api/search?type=image').expect(200)).body.items.some((item) => item.vaultId));
  assert.deepEqual((await alice.agent.get(`/api/search?parentId=${vault.folderId}`).expect(200)).body.items, []);
  // A favorite inside a vault stays out of the list; the vault folder itself is listed.
  await alice.agent.put(`/api/nodes/${image.id}/favorite`).expect(200);
  await alice.agent.put(`/api/nodes/${vault.folderId}/favorite`).expect(200);
  const favorites = (await alice.agent.get('/api/favorites').expect(200)).body.items.map((item) => item.id);
  assert.deepEqual(favorites, [vault.folderId]);
  const duplicates = (await alice.agent.get('/api/storage/duplicates').expect(200)).body.groups;
  assert.ok(!duplicates.some((group) => group.nodes.some((node) => node.vaultId)));

  // Content itself still downloads (the client decrypts it).
  const content = await alice.agent.get(`/api/files/${doc.id}/content`).expect(200);
  assert.equal(content.text, 'NCV1 z');
});
