'use strict';

// Finding and organizing: search filters and ranking, folder paging,
// favorites, opened files, trash expiry and the storage breakdown.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let owner;
let other;
let docs;
let photos;
let deep;
const files = {};

async function folder(agent, name, parentId = 'root') {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

async function put(name, content, parentId = 'root') {
  return (await upload(owner.agent, name, content, { parentId }).expect(201)).body;
}

function setModified(id, value) {
  db.prepare('UPDATE nodes SET modified_at = ? WHERE id = ?').run(value, id);
}

function names(res) {
  return res.body.items.map((item) => item.name);
}

test.before(async () => {
  owner = await registerUser('org');
  other = await registerUser('other');
  docs = await folder(owner.agent, 'Docs');
  photos = await folder(owner.agent, 'Photos');
  deep = await folder(owner.agent, 'Taxes 2026', docs.id);
  files.report = await put('report.pdf', 'pdf bytes', docs.id);
  files.reportFinal = await put('report final.docx', 'docx bytes', deep.id);
  files.annual = await put('annual-report.txt', 'text', docs.id);
  files.myReport = await put('myreport.md', 'markdown');
  files.beach = await put('beach.jpg', crypto.randomBytes(3000), photos.id);
  files.raw = await put('IMG_0001.CR2', crypto.randomBytes(5000), photos.id);
  files.clip = await put('clip.mp4', crypto.randomBytes(2000), photos.id);
  files.backup = await put('backup.tar', crypto.randomBytes(100));
  setModified(files.report.id, '2026-01-10 10:00:00');
  setModified(files.reportFinal.id, '2026-06-01 10:00:00');
  setModified(files.annual.id, '2026-09-01 10:00:00');
  setModified(files.myReport.id, '2026-10-01 10:00:00');
});

test('search ranks exact names, then prefixes, then word starts, then the rest by recency', async () => {
  const res = await owner.agent.get('/api/search?q=report').expect(200);
  assert.deepEqual(names(res), ['report.pdf', 'report final.docx', 'annual-report.txt', 'myreport.md']);
  const top = res.body.items[0];
  assert.equal(top.path, '/Docs');
  assert.equal(res.body.items[1].path, '/Docs/Taxes 2026');
  assert.equal(res.body.items[3].path, '/');
  assert.equal(top.favorite, false);

  // Every word has to appear; case doesn't matter.
  const words = await owner.agent.get('/api/search?q=REPORT%20final').expect(200);
  assert.deepEqual(names(words), ['report final.docx']);
  // LIKE wildcards are literal.
  assert.deepEqual(names(await owner.agent.get('/api/search?q=%25').expect(200)), []);
  // Nothing to search for and no filter: nothing.
  assert.deepEqual(names(await owner.agent.get('/api/search?q=').expect(200)), []);
});

test('search filters by type, date, size and folder; q may be empty with a filter', async () => {
  const images = await owner.agent.get('/api/search?type=image').expect(200);
  assert.deepEqual(names(images).sort(), ['IMG_0001.CR2', 'beach.jpg']);
  const media = await owner.agent.get('/api/search?type=video,audio').expect(200);
  assert.deepEqual(names(media), ['clip.mp4']);
  assert.deepEqual(names(await owner.agent.get('/api/search?type=pdf').expect(200)), ['report.pdf']);
  assert.deepEqual(names(await owner.agent.get('/api/search?type=archive').expect(200)), ['backup.tar']);
  const documents = await owner.agent.get('/api/search?q=report&type=document').expect(200);
  assert.deepEqual(names(documents), ['report final.docx', 'annual-report.txt', 'myreport.md']);
  const folders = await owner.agent.get('/api/search?type=folder').expect(200);
  assert.deepEqual(names(folders).sort(), ['Docs', 'Photos', 'Taxes 2026']);

  const dated = await owner.agent.get('/api/search?q=report&after=2026-05-01&before=2026-09-15T00:00:00Z').expect(200);
  assert.deepEqual(names(dated), ['report final.docx', 'annual-report.txt']);

  const big = await owner.agent.get('/api/search?minSize=2500').expect(200);
  assert.deepEqual(names(big).sort(), ['IMG_0001.CR2', 'beach.jpg']);
  const small = await owner.agent.get('/api/search?type=image&maxSize=4000').expect(200);
  assert.deepEqual(names(small), ['beach.jpg']);

  const below = await owner.agent.get(`/api/search?q=report&parentId=${docs.id}`).expect(200);
  assert.deepEqual(names(below), ['report.pdf', 'report final.docx', 'annual-report.txt']);
  const scoped = await owner.agent.get(`/api/search?parentId=${deep.id}`).expect(200);
  assert.deepEqual(names(scoped), ['report final.docx']);

  for (const query of ['type=spreadsheet', 'after=yesterday', 'minSize=-1', 'maxSize=1.5']) {
    await owner.agent.get(`/api/search?${query}`).expect(400);
  }
  // Another account's folder is missing, not forbidden.
  await other.agent.get(`/api/search?parentId=${docs.id}`).expect(404);
  assert.deepEqual(names(await other.agent.get('/api/search?q=report').expect(200)), []);
});

test('search leaves out trashed subtrees', async () => {
  await owner.agent.delete(`/api/nodes/${deep.id}`).expect(200);
  const res = await owner.agent.get('/api/search?q=report').expect(200);
  assert.ok(!names(res).includes('report final.docx'));
  await owner.agent.get(`/api/search?parentId=${deep.id}`).expect(404);
  await owner.agent.post(`/api/trash/${deep.id}/restore`).expect(200);
});

test('folder listings page with a stable cursor for every sort', async () => {
  const box = await folder(owner.agent, 'Paging');
  await folder(owner.agent, 'b-folder', box.id);
  await folder(owner.agent, 'a-folder', box.id);
  const sizes = [5, 1, 3, 3, 9, 2, 3];
  for (const [index, size] of sizes.entries()) {
    const node = (await upload(owner.agent, `f${index}.txt`, 'x'.repeat(size), { parentId: box.id }).expect(201)).body;
    setModified(node.id, `2026-0${(index % 3) + 1}-01 00:00:00`);
  }
  for (const sort of ['name', 'size', 'modified', 'type']) {
    for (const order of ['asc', 'desc']) {
      const full = await owner.agent.get(`/api/folders/${box.id}/children?sort=${sort}&order=${order}`).expect(200);
      assert.equal(full.body.nextCursor, null);
      assert.deepEqual(full.body.items.slice(0, 2).map((item) => item.type), ['folder', 'folder']);
      const paged = [];
      let cursor = null;
      for (let page = 0; page < 10; page += 1) {
        const query = new URLSearchParams({ sort, order, limit: '3', ...(cursor ? { cursor } : {}) });
        const res = await owner.agent.get(`/api/folders/${box.id}/children?${query}`).expect(200);
        assert.equal(res.body.total, 9);
        paged.push(...res.body.items.map((item) => item.id));
        cursor = res.body.nextCursor;
        if (!cursor) break;
      }
      assert.deepEqual(paged, full.body.items.map((item) => item.id), `${sort} ${order}`);
    }
  }
  const first = await owner.agent.get(`/api/folders/${box.id}/children?limit=4&sort=size`).expect(200);
  // A cursor belongs to its sort; garbage is refused.
  const wrong = await owner.agent.get(`/api/folders/${box.id}/children?sort=name&cursor=${first.body.nextCursor}`).expect(400);
  assert.equal(wrong.body.code, 'INVALID_CURSOR');
  await owner.agent.get(`/api/folders/${box.id}/children?cursor=bm9wZQ`).expect(400);

  // Items added before the cursor position don't shift the next page.
  const next = await owner.agent.get(`/api/folders/${box.id}/children?limit=4&sort=size&cursor=${first.body.nextCursor}`).expect(200);
  await upload(owner.agent, 'zzz-small.txt', '', { parentId: box.id }).expect(201);
  const again = await owner.agent.get(`/api/folders/${box.id}/children?limit=4&sort=size&cursor=${first.body.nextCursor}`).expect(200);
  assert.deepEqual(again.body.items.map((item) => item.id), next.body.items.map((item) => item.id));
});

test('favorites: mark, list with paths, flag on nodes, unmark', async () => {
  const marked = await owner.agent.put(`/api/nodes/${files.beach.id}/favorite`).expect(200);
  assert.equal(marked.body.favorite, true);
  await owner.agent.put(`/api/nodes/${files.beach.id}/favorite`).expect(200);
  await owner.agent.put(`/api/nodes/${docs.id}/favorite`).expect(200);

  const list = await owner.agent.get('/api/favorites').expect(200);
  assert.deepEqual(names(list), ['Docs', 'beach.jpg']);
  assert.equal(list.body.items[1].path, '/Photos');
  assert.ok(list.body.items.every((item) => item.favorite === true));

  const listing = await owner.agent.get(`/api/folders/${photos.id}/children`).expect(200);
  const flags = Object.fromEntries(listing.body.items.map((item) => [item.name, item.favorite]));
  assert.deepEqual(flags, { 'IMG_0001.CR2': false, 'beach.jpg': true, 'clip.mp4': false });
  assert.equal((await owner.agent.get(`/api/nodes/${files.beach.id}`).expect(200)).body.node.favorite, true);
  const renamed = await owner.agent.patch(`/api/nodes/${files.beach.id}`).send({ name: 'beach day.jpg' }).expect(200);
  assert.equal(renamed.body.favorite, true);

  // Trashed favorites drop out of the list and come back on restore.
  await owner.agent.delete(`/api/nodes/${photos.id}`).expect(200);
  assert.deepEqual(names(await owner.agent.get('/api/favorites').expect(200)), ['Docs']);
  await owner.agent.post(`/api/trash/${photos.id}/restore`).expect(200);
  assert.equal((await owner.agent.get('/api/favorites').expect(200)).body.items.length, 2);

  const cleared = await owner.agent.delete(`/api/nodes/${docs.id}/favorite`).expect(200);
  assert.equal(cleared.body.favorite, false);
  assert.deepEqual(names(await owner.agent.get('/api/favorites').expect(200)), ['beach day.jpg']);

  const root = (await owner.agent.get('/api/folders/root/children').expect(200)).body.folder.id;
  assert.equal((await owner.agent.put(`/api/nodes/${root}/favorite`).expect(400)).body.code, 'ROOT_IMMUTABLE');
  // Favorites are per account.
  await other.agent.put(`/api/nodes/${files.clip.id}/favorite`).expect(404);
  await other.agent.delete(`/api/nodes/${files.beach.id}/favorite`).expect(404);
  assert.deepEqual((await other.agent.get('/api/favorites').expect(200)).body.items, []);
});

test('opened files lead the recent list, modified files fill it up', async () => {
  const before = await owner.agent.get('/api/recent?by=modified').expect(200);
  assert.ok(before.body.items.every((item) => item.type === 'file' && typeof item.path === 'string'));

  await owner.agent.post(`/api/nodes/${files.report.id}/opened`).expect(204);
  await owner.agent.post(`/api/nodes/${files.raw.id}/opened`).expect(204);
  await owner.agent.post(`/api/nodes/${files.report.id}/opened`).expect(204);
  const recent = await owner.agent.get('/api/recent').expect(200);
  assert.deepEqual(names(recent).slice(0, 2), ['report.pdf', 'IMG_0001.CR2']);
  assert.ok(recent.body.items[0].lastOpenedAt);
  assert.equal(recent.body.items[2].lastOpenedAt, null);
  assert.equal(new Set(recent.body.items.map((item) => item.id)).size, recent.body.items.length);
  assert.equal(recent.body.items.length, before.body.items.length);
  const row = db.prepare('SELECT open_count FROM node_opens WHERE node_id = ?').get(files.report.id);
  assert.equal(row.open_count, 2);

  const limited = await owner.agent.get('/api/recent?by=opened&limit=1').expect(200);
  assert.deepEqual(names(limited), ['report.pdf']);
  const modified = await owner.agent.get('/api/recent?by=modified').expect(200);
  assert.deepEqual(names(modified), names(before));
  assert.equal(modified.body.items.find((item) => item.id === files.report.id).lastOpenedAt !== null, true);
  await owner.agent.get('/api/recent?by=size').expect(400);

  await other.agent.post(`/api/nodes/${files.report.id}/opened`).expect(404);
  assert.deepEqual((await other.agent.get('/api/recent').expect(200)).body.items, []);
});

test('trash items say when retention removes them', async () => {
  const doomed = await put('doomed.txt', 'bye');
  await owner.agent.delete(`/api/nodes/${doomed.id}`).expect(200);
  db.prepare("UPDATE nodes SET trashed_at = '2026-11-01 08:00:00' WHERE id = ?").run(doomed.id);
  const trash = await owner.agent.get('/api/trash').expect(200);
  const item = trash.body.items.find((entry) => entry.id === doomed.id);
  assert.equal(item.expiresAt, '2026-12-01 08:00:00');
  assert.equal(item.originalPath, '/');

  process.env.NEOCLOUD_TRASH_RETENTION_DAYS = '0';
  try {
    const kept = await owner.agent.get('/api/trash').expect(200);
    assert.equal(kept.body.items.find((entry) => entry.id === doomed.id).expiresAt, null);
  } finally {
    delete process.env.NEOCLOUD_TRASH_RETENTION_DAYS;
  }
  await owner.agent.delete(`/api/trash/${doomed.id}`).expect(200);
});

test('the storage breakdown adds up', async () => {
  const res = await owner.agent.get('/api/storage/breakdown').expect(200);
  const body = res.body;
  for (const key of ['total', 'files', 'trash', 'versions']) assert.equal(typeof body[key], 'number');
  const live = db.prepare(`
    SELECT COALESCE(SUM(size), 0) AS bytes FROM nodes WHERE user_id = ? AND type = 'file' AND trashed_at IS NULL
  `).get(owner.user.id).bytes;
  assert.equal(body.files, live);
  assert.equal(body.byType.reduce((sum, entry) => sum + entry.bytes, 0), body.files);
  assert.equal(body.byFolder.reduce((sum, entry) => sum + entry.bytes, 0), body.files);
  const image = body.byType.find((entry) => entry.type === 'image');
  assert.equal(image.count, 2);
  assert.equal(image.bytes, 8000);
  assert.equal(body.byFolder[0].name, 'Photos');
  assert.equal(body.byFolder[0].bytes, 10000);
  assert.ok(body.byFolder.some((entry) => entry.name === ''));
  assert.equal(body.largest[0].name, 'IMG_0001.CR2');
  assert.equal(body.largest[0].path, '/Photos');
  assert.ok(body.largest.every((node, index) => index === 0 || body.largest[index - 1].size >= node.size));

  // Accounts are separate.
  const theirs = await other.agent.get('/api/storage/breakdown').expect(200);
  assert.equal(theirs.body.files, 0);
  assert.deepEqual(theirs.body.largest, []);
});

test('duplicates are grouped by content, copies free nothing', async () => {
  const content = crypto.randomBytes(1000);
  const one = await put('dup-a.bin', content);
  const two = await put('dup-b.bin', content, docs.id);
  const copy = (await owner.agent.post(`/api/nodes/${one.id}/copy`).send({}).expect(201)).body;
  await put('empty-1.txt', '');
  await put('empty-2.txt', '');

  const res = await owner.agent.get('/api/storage/duplicates').expect(200);
  const group = res.body.groups.find((entry) => entry.sha256 === one.etag);
  assert.ok(group);
  assert.equal(group.size, 1000);
  assert.deepEqual(group.nodes.map((node) => node.id).sort(), [one.id, two.id, copy.id].sort());
  assert.equal(group.nodes.find((node) => node.id === two.id).path, '/Docs');
  // Two separate blobs (the copy shares one): deleting all but one frees 1000.
  assert.equal(group.reclaimable, 1000);
  assert.equal(res.body.reclaimable, res.body.groups.reduce((sum, entry) => sum + entry.reclaimable, 0));
  assert.equal(res.body.groups[0].sha256, one.etag, 'biggest waste first');
  assert.ok(!res.body.groups.some((entry) => entry.size === 0));

  await owner.agent.delete(`/api/nodes/${two.id}`).expect(200);
  const after = await owner.agent.get('/api/storage/duplicates').expect(200);
  assert.equal(after.body.reclaimable, res.body.reclaimable - 1000);
  assert.equal(after.body.groups.find((entry) => entry.sha256 === one.etag).nodes.length, 2);
  assert.deepEqual((await other.agent.get('/api/storage/duplicates').expect(200)).body, { groups: [], reclaimable: 0 });
});
