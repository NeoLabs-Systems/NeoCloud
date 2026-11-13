'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let alice;
let bob;

function keyParams() {
  return {
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  };
}

async function folder(agent, name, parentId = 'root') {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

async function vaultBeside(agent, source) {
  return (await agent.post('/api/vaults').send({
    parentId: source.parentId,
    name: source.name,
    conflict: 'rename',
    ...keyParams(),
  }).expect(201)).body;
}

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
});

test('the check counts the folder and finds nothing in the way', async () => {
  const source = await folder(alice.agent, 'Taxes');
  const inner = await folder(alice.agent, '2026', source.id);
  await upload(alice.agent, 'a.txt', 'hello', { parentId: source.id }).expect(201);
  await upload(alice.agent, 'b.bin', Buffer.alloc(70000), { parentId: inner.id }).expect(201);

  const check = (await alice.agent.get(`/api/vaults/conversions/${source.id}`).expect(200)).body;
  assert.equal(check.folderId, source.id);
  assert.equal(check.name, 'Taxes');
  assert.equal(check.files, 2);
  assert.equal(check.folders, 1);
  assert.equal(check.bytes, 70005);
  // 5 bytes: header + one tag; 70 000 bytes: header + two tags.
  assert.equal(check.vaultBytes, (12 + 16 + 5) + (12 + 32 + 70000));
  assert.deepEqual(check.problems, []);
  assert.ok(Number.isInteger(check.cursor));

  await bob.agent.get(`/api/vaults/conversions/${source.id}`).expect(404);
});

test('a vault beside the folder takes a free name with conflict rename', async () => {
  const source = await folder(alice.agent, 'Clash');
  await alice.agent.post('/api/vaults').send({ parentId: 'root', name: 'Clash', ...keyParams() }).expect(409);
  const vault = await vaultBeside(alice.agent, source);
  assert.equal(vault.folder.name, 'Clash (1)');
  await alice.agent.post('/api/vaults').send({ parentId: 'root', name: 'X', conflict: 'replace', ...keyParams() }).expect(400);
});

test('shared folders, links, vaults inside and vault contents are refused', async () => {
  const team = await folder(alice.agent, 'Team');
  const below = await folder(alice.agent, 'Below', team.id);
  await alice.agent.post(`/api/nodes/${team.id}/members`).send({ recipient: bob.username, permission: 'read' }).expect(201);
  assert.deepEqual((await alice.agent.get(`/api/vaults/conversions/${team.id}`).expect(200)).body.problems, ['SHARED']);
  // Shared from a folder above counts too.
  assert.deepEqual((await alice.agent.get(`/api/vaults/conversions/${below.id}`).expect(200)).body.problems, ['SHARED']);

  const linked = await folder(alice.agent, 'Linked');
  const file = (await upload(alice.agent, 'deep.txt', 'x', { parentId: linked.id }).expect(201)).body;
  await alice.agent.post('/api/shares').send({ nodeId: file.id }).expect(201);
  assert.deepEqual((await alice.agent.get(`/api/vaults/conversions/${linked.id}`).expect(200)).body.problems, ['PUBLIC_LINK']);

  const holder = await folder(alice.agent, 'Holder');
  const inside = (await alice.agent.post('/api/vaults').send({ parentId: holder.id, name: 'V', ...keyParams() }).expect(201)).body;
  assert.deepEqual((await alice.agent.get(`/api/vaults/conversions/${holder.id}`).expect(200)).body.problems, ['CONTAINS_VAULT']);
  assert.ok((await alice.agent.get(`/api/vaults/conversions/${inside.folderId}`).expect(200)).body.problems.includes('IN_VAULT'));
  assert.ok((await alice.agent.get('/api/vaults/conversions/root').expect(200)).body.problems.includes('ROOT'));

  const vault = await vaultBeside(alice.agent, team);
  const check = (await alice.agent.get(`/api/vaults/conversions/${team.id}`).expect(200)).body;
  const res = await alice.agent.post(`/api/vaults/${vault.id}/replace`).send({ folderId: team.id, cursor: check.cursor }).expect(409);
  assert.equal(res.body.code, 'VAULT_CONVERT_REFUSED');
});

test('the check reports a quota without room for the encrypted copy', async () => {
  const source = await folder(alice.agent, 'Big');
  await upload(alice.agent, 'big.bin', Buffer.alloc(4096), { parentId: source.id }).expect(201);
  const used = db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(alice.user.id).used_bytes;
  db.prepare('UPDATE users SET quota_bytes = ? WHERE id = ?').run(used + 100, alice.user.id);
  try {
    const check = (await alice.agent.get(`/api/vaults/conversions/${source.id}`).expect(200)).body;
    assert.deepEqual(check.problems, ['QUOTA']);
    assert.equal(check.freeBytes, 100);
  } finally {
    db.prepare('UPDATE users SET quota_bytes = NULL WHERE id = ?').run(alice.user.id);
  }
});

test('replace trashes the folder and gives the vault its name and place', async () => {
  const parent = await folder(alice.agent, 'Home');
  const source = await folder(alice.agent, 'Private', parent.id);
  await upload(alice.agent, 'note.txt', 'secret', { parentId: source.id }).expect(201);
  const check = (await alice.agent.get(`/api/vaults/conversions/${source.id}`).expect(200)).body;
  const vault = await vaultBeside(alice.agent, source);
  assert.equal(vault.folder.name, 'Private (1)');

  await bob.agent.post(`/api/vaults/${vault.id}/replace`).send({ folderId: source.id, cursor: check.cursor }).expect(404);
  await alice.agent.post(`/api/vaults/${vault.id}/replace`).send({ folderId: source.id }).expect(400);

  const res = await alice.agent.post(`/api/vaults/${vault.id}/replace`).send({ folderId: source.id, cursor: check.cursor }).expect(200);
  assert.equal(res.body.trashedId, source.id);
  assert.equal(res.body.folder.id, vault.folderId);
  assert.equal(res.body.folder.name, 'Private');
  assert.equal(res.body.folder.parentId, parent.id);
  assert.equal(res.body.folder.vaultId, vault.id);

  const trash = (await alice.agent.get('/api/trash').expect(200)).body.items;
  assert.ok(trash.some((item) => item.id === source.id));
  const listing = (await alice.agent.get(`/api/folders/${parent.id}/children`).expect(200)).body.items;
  assert.deepEqual(listing.map((item) => item.name), ['Private']);
});

test('replace is refused when the folder changed after the check', async () => {
  const source = await folder(alice.agent, 'Moving');
  const check = (await alice.agent.get(`/api/vaults/conversions/${source.id}`).expect(200)).body;
  const vault = await vaultBeside(alice.agent, source);
  await upload(alice.agent, 'late.txt', 'added later', { parentId: source.id }).expect(201);

  const res = await alice.agent.post(`/api/vaults/${vault.id}/replace`).send({ folderId: source.id, cursor: check.cursor }).expect(409);
  assert.equal(res.body.code, 'VAULT_SOURCE_CHANGED');
  const again = (await alice.agent.get(`/api/vaults/conversions/${source.id}`).expect(200)).body;
  await alice.agent.post(`/api/vaults/${vault.id}/replace`).send({ folderId: source.id, cursor: again.cursor }).expect(200);
});

test('a vault inside the folder can’t replace it', async () => {
  const source = await folder(alice.agent, 'Outer');
  const inside = (await alice.agent.post('/api/vaults').send({ parentId: source.id, name: 'V', ...keyParams() }).expect(201)).body;
  const check = (await alice.agent.get(`/api/vaults/conversions/${source.id}`).expect(200)).body;
  const res = await alice.agent.post(`/api/vaults/${inside.id}/replace`).send({ folderId: source.id, cursor: check.cursor }).expect(409);
  assert.equal(res.body.code, 'VAULT_BOUNDARY');
});
