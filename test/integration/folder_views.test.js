'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { STRONG_PASSWORD, cleanup, registerUser } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

const view = (sort, dir = 'asc', viewMode = 'list') => ({ sort, dir, view: viewMode });
const strip = (views) => Object.fromEntries(Object.entries(views).map(([key, { sort, dir, view: mode }]) => [key, { sort, dir, view: mode }]));

test('folder views start empty, merge per entry and drop entries set to null', async () => {
  const { agent } = await registerUser('views');
  const initial = await agent.get('/api/settings').expect(200);
  assert.deepEqual(initial.body.settings.folder_views, {});

  await agent.put('/api/settings').send({ folder_views: { 'folder-a': view('size', 'desc', 'grid'), recent: view('modified', 'desc') } }).expect(200);
  // A second device sends only its own folder: the others stay.
  const merged = await agent.put('/api/settings').send({ folder_views: { 'folder-b': view('type') } }).expect(200);
  assert.deepEqual(strip(merged.body.settings.folder_views), {
    'folder-a': view('size', 'desc', 'grid'),
    recent: view('modified', 'desc'),
    'folder-b': view('type'),
  });
  assert.equal(typeof merged.body.settings.folder_views['folder-a'].at, 'number');

  const removed = await agent.put('/api/settings').send({ folder_views: { 'folder-a': null, search: view('relevance', 'desc', 'grid') } }).expect(200);
  assert.deepEqual(Object.keys(removed.body.settings.folder_views).sort(), ['folder-b', 'recent', 'search']);

  // The global defaults are separate and unchanged.
  assert.equal(removed.body.settings.sortField, 'name');
  assert.equal(removed.body.settings.fileView, 'list');
});

test('folder views refuse bad shapes, keys and values', async () => {
  const { agent } = await registerUser('views');
  const bad = [
    [],
    'name',
    { 'folder-a': { sort: 'name', dir: 'asc' } },
    { 'folder-a': { ...view('name'), extra: 1 } },
    { 'folder-a': view('owner') },
    { 'folder-a': view('name', 'up') },
    { 'folder-a': view('name', 'asc', 'tiles') },
    { '../etc': view('name') },
    { ['x'.repeat(65)]: view('name') },
    Object.fromEntries(Array.from({ length: 501 }, (_, i) => [`f${i}`, view('name')])),
  ];
  for (const folderViews of bad) {
    const res = await agent.put('/api/settings').send({ folder_views: folderViews }).expect(400);
    assert.equal(res.body.code, 'INVALID_SETTING_VALUE', JSON.stringify(folderViews).slice(0, 80));
  }
  const after = await agent.get('/api/settings').expect(200);
  assert.deepEqual(after.body.settings.folder_views, {});
});

test('at most 500 folders are kept; the oldest go first', async () => {
  const { agent, user } = await registerUser('views');
  const first = Object.fromEntries(Array.from({ length: 500 }, (_, i) => [`f${i}`, view('name')]));
  await agent.put('/api/settings').send({ folder_views: first }).expect(200);
  // Age every entry but f7, so f7 counts as recently used.
  const stored = JSON.parse(db.prepare("SELECT value FROM user_settings WHERE user_id = ? AND key = 'folder_views'").get(user.id).value);
  for (const [key, entry] of Object.entries(stored)) entry.at = key === 'f7' ? Date.now() : 1000 + Number(key.slice(1));
  db.prepare("UPDATE user_settings SET value = ? WHERE user_id = ? AND key = 'folder_views'").run(JSON.stringify(stored), user.id);

  const res = await agent.put('/api/settings').send({ folder_views: { fresh1: view('size'), fresh2: view('type') } }).expect(200);
  const keys = Object.keys(res.body.settings.folder_views);
  assert.equal(keys.length, 500);
  assert.ok(keys.includes('fresh1') && keys.includes('fresh2') && keys.includes('f7'));
  // f0 and f1 were the oldest.
  assert.ok(!keys.includes('f0') && !keys.includes('f1'));
  assert.ok(keys.includes('f2'));
});

test('each account has its own folder views; the export carries them and erasure removes them', async () => {
  const alice = await registerUser('views');
  const bob = await registerUser('views');
  await alice.agent.put('/api/settings').send({ folder_views: { shared: view('modified', 'desc') } }).expect(200);
  const bobs = await bob.agent.get('/api/settings').expect(200);
  assert.deepEqual(bobs.body.settings.folder_views, {});

  const exported = await alice.agent.post('/api/account/export').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  assert.deepEqual(strip(exported.body.settings.folder_views), { shared: view('modified', 'desc') });

  await alice.agent.post('/api/account/delete').send({ currentPassword: STRONG_PASSWORD, confirmUsername: alice.username }).expect(200);
  const left = db.prepare("SELECT COUNT(*) AS n FROM user_settings WHERE user_id = ? AND key = 'folder_views'").get(alice.user.id).n;
  assert.equal(left, 0);
});
