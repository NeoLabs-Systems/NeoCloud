'use strict';

// Full-text search: the admin endpoints are admin-only, and content search
// never crosses accounts, even with another account's folder as scope.

const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const { runIndexerUntilIdle } = require('../../server/services/search_index/indexer');

test.after(cleanup);

let admin;
let member;
let folder;

test.before(async () => {
  admin = await registerUser('ftsadmin');
  member = await registerUser('ftsmember');
  await admin.agent.put('/api/admin/config').send({ fullTextSearch: true }).expect(200);
  folder = (await admin.agent.post('/api/folders').send({ parentId: 'root', name: 'Mine' }).expect(201)).body;
  await upload(admin.agent, 'plan.txt', 'confidential durian plan', { parentId: folder.id }).expect(201);
  await runIndexerUntilIdle();
});

test('the search index switches and status are for admins only', async () => {
  await member.agent.get('/api/admin/config').expect(403);
  await member.agent.put('/api/admin/config').send({ fullTextSearch: false }).expect(403);
  await member.agent.get('/api/admin/search-index').expect(403);
  await member.agent.post('/api/admin/search-index/rebuild').expect(403);
  const anonymous = await request(app).get('/api/admin/search-index');
  assert.ok([401, 403].includes(anonymous.status));
  assert.equal((await admin.agent.get('/api/admin/config').expect(200)).body.fullTextSearch, true);
});

test('content search stays inside the account', async () => {
  const own = await admin.agent.get('/api/search?content=1&q=durian').expect(200);
  assert.deepEqual(own.body.items.map((item) => item.name), ['plan.txt']);
  const theirs = await member.agent.get('/api/search?content=1&q=durian').expect(200);
  assert.deepEqual(theirs.body.items, []);
  // Another account's folder as the scope behaves like a missing one.
  await member.agent.get(`/api/search?content=1&q=durian&parentId=${folder.id}`).expect(404);
  await request(app).get('/api/search?content=1&q=durian').expect(401);
});
