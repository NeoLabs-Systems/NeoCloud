'use strict';

// Every file and folder is a database row, and copies share their blobs, so
// node counts have limits of their own: per copy, per account, and a
// per-account rate limit on tree changes.

const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(() => {
  delete process.env.NEOCLOUD_MAX_COPY_NODES;
  delete process.env.NEOCLOUD_MAX_NODES;
  cleanup();
});

let alice;
let bob;
let token;

const nodeCount = (userId) => db.prepare('SELECT COUNT(*) AS n FROM nodes WHERE user_id = ?').get(userId).n;
const changeCount = (userId) => db.prepare('SELECT COUNT(*) AS n FROM changes WHERE user_id = ?').get(userId).n;
const counter = (userId) => db.prepare('SELECT node_count FROM users WHERE id = ?').get(userId).node_count;

async function folder(agent, parentId, name) {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
  token = (await alice.agent.post('/api/account/devices').send({ name: 'dav', platform: 'webdav' }).expect(201)).body.token;
});

test('a copy over NEOCLOUD_MAX_COPY_NODES is refused before anything is written', async () => {
  const top = await folder(alice.agent, 'root', 'Big');
  for (let i = 0; i < 3; i += 1) await folder(alice.agent, top.id, `sub${i}`);
  const inner = await folder(alice.agent, top.id, 'deep');
  await upload(alice.agent, 'f.txt', 'x', { parentId: inner.id }).expect(201);
  // Big + 3 subs + deep + f.txt = 6 nodes.
  process.env.NEOCLOUD_MAX_COPY_NODES = '5';
  const nodesBefore = nodeCount(alice.user.id);
  const changesBefore = changeCount(alice.user.id);
  const refused = await alice.agent.post(`/api/nodes/${top.id}/copy`).send({ parentId: 'root' }).expect(413);
  assert.equal(refused.body.code, 'COPY_TOO_LARGE');
  assert.equal(nodeCount(alice.user.id), nodesBefore);
  assert.equal(changeCount(alice.user.id), changesBefore);
  // The same over WebDAV.
  await request(app).copy(`/dav/files/${alice.username}/Big`).auth(alice.username, token)
    .set('Destination', `/dav/files/${alice.username}/Big2`).expect(413);
  assert.equal(nodeCount(alice.user.id), nodesBefore);

  process.env.NEOCLOUD_MAX_COPY_NODES = '6';
  await alice.agent.post(`/api/nodes/${top.id}/copy`).send({ parentId: 'root' }).expect(201);
  assert.equal(nodeCount(alice.user.id), nodesBefore + 6);
  delete process.env.NEOCLOUD_MAX_COPY_NODES;
});

test('NEOCLOUD_MAX_NODES caps an account on every path that creates nodes', async () => {
  assert.equal(counter(alice.user.id), nodeCount(alice.user.id), 'the counter follows the rows');
  const room = 2;
  process.env.NEOCLOUD_MAX_NODES = String(nodeCount(alice.user.id) + room);
  const a = await folder(alice.agent, 'root', 'one');
  await upload(alice.agent, 'two.txt', 'x').expect(201);
  const full = nodeCount(alice.user.id);

  const refusals = [
    await alice.agent.post('/api/folders').send({ parentId: 'root', name: 'three' }),
    await alice.agent.post('/api/folders').send({ parentId: 'root', path: 'p/q' }),
    await upload(alice.agent, 'three.txt', 'x'),
    await alice.agent.post(`/api/nodes/${a.id}/copy`).send({ parentId: 'root' }),
  ];
  for (const res of refusals) {
    assert.equal(res.status, 507, JSON.stringify(res.body));
    assert.equal(res.body.code, 'NODE_LIMIT');
  }
  await request(app).mkcol(`/dav/files/${alice.username}/viaDav`).auth(alice.username, token).expect(507);
  assert.equal(nodeCount(alice.user.id), full);

  // Another account is not affected by alice's count.
  process.env.NEOCLOUD_MAX_NODES = String(full);
  await folder(bob.agent, 'root', 'fine');

  // Deleting for good makes room again; the counter follows.
  await alice.agent.delete(`/api/nodes/${a.id}`).expect(200);
  await alice.agent.delete('/api/trash').expect(200);
  assert.equal(counter(alice.user.id), nodeCount(alice.user.id));
  await folder(alice.agent, 'root', 'again');
  delete process.env.NEOCLOUD_MAX_NODES;
});

test('tree changes are rate-limited per account', async () => {
  const carol = await registerUser('carol');
  for (let i = 0; i < 100; i += 1) await carol.agent.post('/api/nodes/missing/copy').send({}).expect(404);
  const limited = await carol.agent.post('/api/nodes/missing/copy').send({});
  assert.equal(limited.status, 429);
  assert.equal(limited.body.code, 'RATE_LIMITED');
  // Same address, other account: its own budget.
  await bob.agent.post('/api/nodes/missing/copy').send({}).expect(404);
});
