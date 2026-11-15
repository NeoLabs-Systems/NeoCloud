'use strict';

// The activity feed: what is logged, how bursts group, who sees what, and
// the 90-day pruning.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { pruneActivity } = require('../../server/services/activity/record');
const { exportAccount } = require('../../server/services/account/export');

test.after(cleanup);

let owner;
let writer;
let reader;
let outsider;
let team;

const feed = async (who, query = '') => (await who.agent.get(`/api/activity${query}`).expect(200)).body;
const kinds = (page) => page.items.map((item) => item.kind);

test.before(async () => {
  owner = await registerUser('owner');
  writer = await registerUser('writer');
  reader = await registerUser('reader');
  outsider = await registerUser('outsider');
  team = (await owner.agent.post('/api/folders').send({ name: 'Team' }).expect(201)).body;
  await owner.agent.post(`/api/nodes/${team.id}/members`).send({ recipient: writer.username, permission: 'write' }).expect(201);
  await owner.agent.post(`/api/nodes/${team.id}/members`).send({ recipient: reader.username }).expect(201);
});

test('a burst of uploads by one account into one folder is a single entry with a count', async () => {
  for (const name of ['a.txt', 'b.txt', 'c.txt', 'd.txt']) await upload(writer.agent, name, name, { parentId: team.id }).expect(201);
  const page = await feed(owner, `?nodeId=${team.id}`);
  const uploads = page.items.filter((item) => item.kind === 'upload');
  assert.equal(uploads.length, 1);
  assert.equal(uploads[0].count, 4);
  assert.equal(uploads[0].actor.self, false);
  assert.deepEqual(uploads[0].names, ['a.txt', 'b.txt', 'c.txt']);
});

test('entries name their folder where the viewer can see it', async () => {
  const mine = (await feed(owner, '?scope=mine')).items.find((item) => item.kind === 'upload');
  assert.equal(mine.folder, 'Team');
  const rootShare = (await feed(reader, `?nodeId=${team.id}`)).items.find((item) => item.kind === 'share');
  assert.equal(rootShare.folder, undefined, 'nothing about what sits above the share');
});

test('the actor of a member write is the member; the owner sees it in their own tree', async () => {
  const page = await feed(owner, '?scope=mine');
  const entry = page.items.find((item) => item.kind === 'upload');
  assert.ok(entry);
  assert.equal(entry.actor.name, writer.username);
  const own = (await feed(writer, '?scope=mine')).items;
  assert.equal(own.filter((item) => item.kind === 'upload').length, 0, 'the write landed in the owner’s tree');
  const shared = (await feed(writer, '?scope=shared')).items.find((item) => item.kind === 'upload');
  assert.equal(shared.actor.self, true);
});

test('rename, move, delete, restore, new version and folders are logged', async () => {
  const file = (await upload(owner.agent, 'Contract.pdf', 'v1', { parentId: team.id }).expect(201)).body;
  await owner.agent.patch(`/api/nodes/${file.id}`).send({ name: 'Contract final.pdf' }).expect(200);
  const inner = (await owner.agent.post('/api/folders').send({ parentId: team.id, name: 'Inner' }).expect(201)).body;
  await owner.agent.patch(`/api/nodes/${file.id}`).send({ parentId: inner.id }).expect(200);
  await upload(owner.agent, 'Contract final.pdf', 'v2', { parentId: inner.id, conflict: 'replace' }).expect(201);
  await owner.agent.delete(`/api/nodes/${file.id}`).expect(200);
  await owner.agent.post(`/api/trash/${file.id}/restore`).expect(200);
  const page = await feed(owner, `?nodeId=${file.id}`);
  assert.deepEqual(kinds(page), ['restore', 'trash', 'version', 'move', 'rename', 'upload']);
  assert.equal(page.items.find((item) => item.kind === 'rename').from, 'Contract.pdf');
});

test('members see the entries of the folders shared with them, not those of private ones', async () => {
  const secret = (await owner.agent.post('/api/folders').send({ name: 'Private' }).expect(201)).body;
  await upload(owner.agent, 'diary.txt', 'x', { parentId: secret.id }).expect(201);
  const page = await feed(reader, '?scope=shared');
  assert.ok(page.items.length > 0);
  assert.ok(page.items.every((item) => item.node.name !== 'diary.txt' && item.node.name !== 'Private'));
  assert.deepEqual((await feed(outsider)).items, []);
  await reader.agent.get(`/api/activity?nodeId=${secret.id}`).expect(404);
});

test('who a folder was shared with is for the owner and that member only', async () => {
  const mine = (await feed(owner, `?nodeId=${team.id}`)).items.filter((item) => item.kind === 'share');
  assert.equal(mine.length, 2);
  const readerSees = (await feed(reader, `?nodeId=${team.id}`)).items.filter((item) => item.kind === 'share');
  assert.equal(readerSees.length, 1);
  assert.equal(readerSees[0].member.self, true);
});

test('entries stop showing once the share ends or the node leaves it', async () => {
  const mover = (await upload(owner.agent, 'wander.txt', 'w', { parentId: team.id }).expect(201)).body;
  await owner.agent.patch(`/api/nodes/${mover.id}`).send({ name: 'wander2.txt' }).expect(200);
  assert.ok((await feed(reader, '?scope=shared')).items.some((item) => item.node.name === 'wander2.txt'));
  await owner.agent.patch(`/api/nodes/${mover.id}`).send({ parentId: 'root' }).expect(200);
  assert.ok((await feed(reader, '?scope=shared')).items.every((item) => item.node.id !== mover.id));
  const grants = (await owner.agent.get(`/api/nodes/${team.id}/members`).expect(200)).body.members;
  const readerGrant = grants.find((member) => member.recipient === reader.username);
  await owner.agent.delete(`/api/nodes/${team.id}/members/${readerGrant.id}`).expect(200);
  assert.deepEqual((await feed(reader)).items, []);
});

test('vault contents are never logged', async () => {
  const vault = (await owner.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Safe',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  const cipher = (await upload(owner.agent, 'SGlkZGVu', 'NCV1 x', { parentId: vault.folderId }).expect(201)).body;
  await owner.agent.patch(`/api/nodes/${cipher.id}`).send({ name: 'SGlkZGVuMg' }).expect(200);
  await owner.agent.delete(`/api/nodes/${cipher.id}`).expect(200);
  const rows = db.prepare('SELECT name FROM activity WHERE owner_id = ?').all(owner.user.id);
  assert.ok(rows.every((row) => !row.name.startsWith('SGlkZGVu')));
  await owner.agent.get(`/api/activity?nodeId=${cipher.id}`).expect(404);
});

test('pages are cut by cursor without gaps or repeats', async () => {
  for (let i = 0; i < 5; i += 1) await owner.agent.post('/api/folders').send({ name: `Paged ${i}` }).expect(201);
  const seen = [];
  let cursor = null;
  do {
    const page = await feed(owner, `?scope=mine&limit=4${cursor ? `&cursor=${cursor}` : ''}`);
    seen.push(...page.items.map((item) => item.id));
    cursor = page.nextCursor;
  } while (cursor);
  assert.equal(new Set(seen).size, seen.length);
  assert.equal(seen.length, db.prepare(`
    SELECT COUNT(*) AS n FROM activity a WHERE owner_id = ? AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = a.node_id AND n.vault_id IS NOT NULL)
  `).get(owner.user.id).n);
  await owner.agent.get('/api/activity?scope=nope').expect(400);
});

test('entries older than 90 days are pruned and the export carries the rest', async () => {
  db.prepare("UPDATE activity SET at = datetime('now', '-91 days') WHERE id = (SELECT MIN(id) FROM activity)").run();
  const before = db.prepare('SELECT COUNT(*) AS n FROM activity').get().n;
  assert.equal(pruneActivity(), 1);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM activity').get().n, before - 1);
  const exported = exportAccount(owner.user.id);
  assert.ok(exported.activity.entries.length > 0);
});
