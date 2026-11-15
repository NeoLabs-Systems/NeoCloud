'use strict';

// Comments and the activity feed against other accounts' ids: a node that is
// not yours behaves like a missing one, vault nodes never take part, and
// nobody reaches either through a public link.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let owner;
let member;
let stranger;
let file;
let folder;
let cipher;

test.before(async () => {
  owner = await registerUser('owner');
  member = await registerUser('member');
  stranger = await registerUser('stranger');
  folder = (await owner.agent.post('/api/folders').send({ name: 'Team' }).expect(201)).body;
  file = (await upload(owner.agent, 'plan.txt', 'plan', { parentId: folder.id }).expect(201)).body;
  await owner.agent.post(`/api/nodes/${folder.id}/members`).send({ recipient: member.username }).expect(201);
  const vault = (await owner.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Safe',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  cipher = (await upload(owner.agent, 'c2VjcmV0', 'NCV1 x', { parentId: vault.folderId }).expect(201)).body;
  await owner.agent.post(`/api/files/${file.id}/comments`).send({ body: 'private to the team' }).expect(201);
});

test('another account’s node ids answer 404 on every comment route', async () => {
  const { id: commentId } = db.prepare('SELECT id FROM node_comments LIMIT 1').get();
  await stranger.agent.get(`/api/files/${file.id}/comments`).expect(404);
  await stranger.agent.post(`/api/files/${file.id}/comments`).send({ body: 'hi' }).expect(404);
  await stranger.agent.patch(`/api/files/${file.id}/comments/${commentId}`).send({ body: 'hi' }).expect(404);
  await stranger.agent.delete(`/api/files/${file.id}/comments/${commentId}`).expect(404);
  await stranger.agent.get('/api/files/no-such-node/comments').expect(404);
  await stranger.agent.get('/api/files/root/comments').expect(404);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM node_comments').get().n, 1);
});

test('a comment is reached only through its own node', async () => {
  const { id: commentId } = db.prepare('SELECT id FROM node_comments LIMIT 1').get();
  const other = (await upload(owner.agent, 'other.txt', 'o').expect(201)).body;
  await owner.agent.patch(`/api/files/${other.id}/comments/${commentId}`).send({ body: 'x' }).expect(404);
  await owner.agent.delete(`/api/files/${other.id}/comments/${commentId}`).expect(404);
});

test('vault nodes and the root take no comments and show no activity', async () => {
  await owner.agent.get(`/api/files/${cipher.id}/comments`).expect(404);
  await owner.agent.post(`/api/files/${cipher.id}/comments`).send({ body: 'x' }).expect(404);
  await owner.agent.get('/api/files/root/comments').expect(404);
  await owner.agent.get(`/api/activity?nodeId=${cipher.id}`).expect(404);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM activity WHERE node_id = ?').get(cipher.id).n, 0);
});

test('the activity feed of another account’s node is a 404 too', async () => {
  await stranger.agent.get(`/api/activity?nodeId=${file.id}`).expect(404);
  await stranger.agent.get(`/api/activity?nodeId=${folder.id}`).expect(404);
  assert.deepEqual((await stranger.agent.get('/api/activity').expect(200)).body.items, []);
});

test('members lose comments and activity when access ends, trash hides the node', async () => {
  await member.agent.get(`/api/files/${file.id}/comments`).expect(200);
  await member.agent.get(`/api/activity?nodeId=${file.id}`).expect(200);
  db.prepare("UPDATE user_shares SET expires_at = datetime('now', '-1 minute')").run();
  await member.agent.get(`/api/files/${file.id}/comments`).expect(404);
  await member.agent.get(`/api/activity?nodeId=${file.id}`).expect(404);
  assert.deepEqual((await member.agent.get('/api/activity').expect(200)).body.items, []);
  db.prepare('UPDATE user_shares SET expires_at = NULL').run();
  await owner.agent.delete(`/api/nodes/${file.id}`).expect(200);
  await owner.agent.get(`/api/files/${file.id}/comments`).expect(404);
  await member.agent.get(`/api/files/${file.id}/comments`).expect(404);
});

test('share-link visitors and signed-out callers get nothing', async () => {
  const visible = (await upload(owner.agent, 'link.txt', 'l').expect(201)).body;
  await owner.agent.post(`/api/files/${visible.id}/comments`).send({ body: 'not for visitors' }).expect(201);
  const link = (await owner.agent.post('/api/shares').send({ nodeId: visible.id }).expect(201)).body;
  const token = link.token;
  const visitor = request(app);
  await visitor.get(`/api/files/${visible.id}/comments`).expect(401);
  await visitor.post(`/api/files/${visible.id}/comments`).send({ body: 'x' }).expect(401);
  await visitor.get('/api/activity').expect(401);
  const page = (await visitor.get(`/api/public/shares/${token}`).expect(200)).body;
  assert.ok(!JSON.stringify(page).includes('not for visitors'));
  await visitor.get(`/api/public/shares/${token}/comments`).expect(404);
  await visitor.get(`/api/public/shares/${token}/files/${visible.id}/comments`).expect(404);
});
