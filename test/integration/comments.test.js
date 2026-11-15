'use strict';

// Comments on files and folders: add, list, edit, delete, who may do what,
// and that they follow the node.

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { eraseAccount } = require('../../server/services/account/erasure');
const { exportAccount } = require('../../server/services/account/export');

test.after(cleanup);

let owner;
let member;
let file;
let folder;

const url = (id = file.id) => `/api/files/${id}/comments`;

test.before(async () => {
  owner = await registerUser('owner');
  member = await registerUser('member');
  folder = (await owner.agent.post('/api/folders').send({ name: 'Shared' }).expect(201)).body;
  file = (await upload(owner.agent, 'plan.txt', 'plan', { parentId: folder.id }).expect(201)).body;
  await owner.agent.post(`/api/nodes/${folder.id}/members`).send({ recipient: member.username }).expect(201);
});

test('comments are added, listed newest first and stored sealed', async () => {
  const first = (await owner.agent.post(url()).send({ body: '  First note ' }).expect(201)).body;
  assert.equal(first.body, 'First note');
  assert.equal(first.mine, true);
  await member.agent.post(url()).send({ body: 'Looks good\nthanks' }).expect(201);
  const list = (await owner.agent.get(url()).expect(200)).body;
  assert.deepEqual(list.comments.map((comment) => comment.body), ['Looks good\nthanks', 'First note']);
  assert.equal(list.comments[0].mine, false);
  assert.equal(list.comments[0].author, member.username);
  assert.equal(list.nextCursor, null);
  const stored = db.prepare('SELECT body FROM node_comments').all().map((row) => row.body).join('');
  assert.ok(!stored.includes('First note'));
});

test('a read-only member can comment, the comment shows as activity', async () => {
  const page = (await owner.agent.get(`/api/activity?nodeId=${file.id}`).expect(200)).body;
  assert.ok(page.items.some((item) => item.kind === 'comment' && item.actor.name === member.username));
});

test('bodies are validated', async () => {
  for (const body of ['', '   ', 5, null, 'a'.repeat(2001), 'bell\u0007']) {
    await owner.agent.post(url()).send({ body }).expect(400);
  }
  await owner.agent.post(url()).send({ body: 'a'.repeat(2000) }).expect(201);
  const html = (await owner.agent.post(url()).send({ body: '<b>x</b>' }).expect(201)).body;
  assert.equal(html.body, '<b>x</b>', 'kept as plain text, never interpreted');
});

test('only the author edits; the author and the file owner delete', async () => {
  const mine = (await member.agent.post(url()).send({ body: 'mine' }).expect(201)).body;
  const edited = (await member.agent.patch(`${url()}/${mine.id}`).send({ body: 'mine, edited' }).expect(200)).body;
  assert.equal(edited.body, 'mine, edited');
  assert.ok(edited.editedAt);
  const theirs = (await owner.agent.post(url()).send({ body: 'owner note' }).expect(201)).body;
  await member.agent.patch(`${url()}/${theirs.id}`).send({ body: 'x' }).expect(403);
  await member.agent.delete(`${url()}/${theirs.id}`).expect(403);
  await owner.agent.patch(`${url()}/${mine.id}`).send({ body: 'x' }).expect(403);
  await owner.agent.delete(`${url()}/${mine.id}`).expect(200);
  await member.agent.delete(`${url()}/${theirs.id}`).expect(403);
  await owner.agent.delete(`${url()}/${theirs.id}`).expect(200);
  await owner.agent.delete(`${url()}/${theirs.id}`).expect(404);
});

test('comments follow the node through rename and move, and work on folders', async () => {
  const before = (await owner.agent.get(url()).expect(200)).body.comments.length;
  await owner.agent.patch(`/api/nodes/${file.id}`).send({ name: 'renamed.txt', parentId: 'root' }).expect(200);
  assert.equal((await owner.agent.get(url()).expect(200)).body.comments.length, before);
  await member.agent.get(url()).expect(404);
  await owner.agent.post(url(folder.id)).send({ body: 'about the folder' }).expect(201);
  await member.agent.get(url(folder.id)).expect(200);
});

test('listing is cut by cursor', async () => {
  const page = (await owner.agent.get(`${url()}?limit=2`).expect(200)).body;
  assert.equal(page.comments.length, 2);
  assert.ok(page.nextCursor);
  const next = (await owner.agent.get(`${url()}?limit=100&cursor=${page.nextCursor}`).expect(200)).body;
  assert.ok(next.comments.every((comment) => comment.id < page.nextCursor));
});

test('comments go with a node deleted for good and with an erased account', async () => {
  const doomed = (await upload(owner.agent, 'doomed.txt', 'x').expect(201)).body;
  await owner.agent.post(url(doomed.id)).send({ body: 'bye' }).expect(201);
  await owner.agent.delete(`/api/nodes/${doomed.id}`).expect(200);
  await owner.agent.delete(`/api/trash/${doomed.id}`).expect(200);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM node_comments WHERE node_id = ?').get(doomed.id).n, 0);

  const exported = exportAccount(owner.user.id);
  assert.ok(exported.activity.comments.some((comment) => comment.body === 'about the folder' && comment.onMyFile));

  await member.agent.post(url(folder.id)).send({ body: 'from the member' }).expect(201);
  await eraseAccount(member.user.id, { actorUserId: member.user.id });
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM node_comments WHERE user_id = ?').get(member.user.id).n, 0);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM activity WHERE actor_id = ?').get(member.user.id).n, 0);
});
