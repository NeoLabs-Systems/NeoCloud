'use strict';

// The AI review queue and jobs never reach what they must not: vault files,
// folders excluded from AI, files only shared with the caller, and anything
// of another account (404, like a missing id). Desktop tokens don't reach
// the routes at all.

const crypto = require('crypto');
const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');
const { createSuggestion } = require('../../server/services/ai/organize/suggestions');
const { runAiJobsUntilIdle } = require('../../server/services/ai/organize/worker');

let owner;
let other;
let file;
let suggestion;
let excludedFile;
let sealed;
let sharedFile;

async function folder(agent, parentId, name) {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

test.before(async () => {
  owner = await registerUser('aisecowner'); // the admin
  other = await registerUser('aisecother');
  await owner.agent.put('/api/admin/ai').send({ enabled: true }).expect(200);
  for (const account of [owner, other]) {
    await account.agent.put('/api/settings').send({ ai_opt_in: true }).expect(200);
  }
  file = (await upload(owner.agent, 'scan0001.txt', 'hello').expect(201)).body;
  const row = db.prepare('SELECT * FROM nodes WHERE id = ?').get(file.id);
  suggestion = createSuggestion(owner.user.id, row, { kind: 'rename', from: row.name, to: 'Hello.txt', confidence: 0.9 });

  const excluded = await folder(owner.agent, 'root', 'Private');
  excludedFile = (await upload(owner.agent, 'scan0002.txt', 'secret', { parentId: excluded.id }).expect(201)).body;
  await owner.agent.put(`/api/ai/folders/${excluded.id}/excluded`).send({ excluded: true }).expect(200);

  const vault = (await owner.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Tresor',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  sealed = (await upload(owner.agent, 'b3JwaGFu', 'NCV1 x', { parentId: vault.folderId }).expect(201)).body;

  const shared = await folder(owner.agent, 'root', 'Shared');
  sharedFile = (await upload(owner.agent, 'scan0003.txt', 'team', { parentId: shared.id }).expect(201)).body;
  await owner.agent.post(`/api/nodes/${shared.id}/members`).send({ recipient: other.username, permission: 'write' }).expect(201);
});

test.after(cleanup);

test('vault and excluded files are refused, and a backfill skips them', async () => {
  const vault = await owner.agent.post('/api/ai/jobs').send({ nodeIds: [sealed.id] }).expect(403);
  assert.equal(vault.body.code, 'AI_VAULT');
  const excluded = await owner.agent.post('/api/ai/jobs').send({ nodeIds: [excludedFile.id] }).expect(403);
  assert.equal(excluded.body.code, 'AI_EXCLUDED');
  await owner.agent.post(`/api/ai/files/${sealed.id}/summary`).send({}).expect(403);
  await owner.agent.post(`/api/ai/files/${excludedFile.id}/summary`).send({}).expect(403);

  const { queued } = (await owner.agent.post('/api/ai/jobs').send({ folderId: 'root', kinds: ['names'] }).expect(202)).body;
  const queuedIds = db.prepare('SELECT node_id FROM ai_jobs WHERE user_id = ?').all(owner.user.id).map((row) => row.node_id);
  assert.equal(queued, queuedIds.length);
  assert.ok(queuedIds.includes(file.id));
  assert.ok(!queuedIds.includes(excludedFile.id));
  assert.ok(!queuedIds.includes(sealed.id));
  // The excluded folder can't be backfilled either.
  const excludedFolder = db.prepare('SELECT parent_id FROM nodes WHERE id = ?').get(excludedFile.id).parent_id;
  assert.equal((await owner.agent.post('/api/ai/jobs').send({ folderId: excludedFolder }).expect(403)).body.code, 'AI_EXCLUDED');
  db.prepare('DELETE FROM ai_jobs').run();
});

test('a file excluded after it was queued is skipped, not sent', async () => {
  const late = (await upload(owner.agent, 'scan0004.txt', 'later secret').expect(201)).body;
  await owner.agent.post('/api/ai/jobs').send({ nodeIds: [late.id], kinds: ['names'] }).expect(202);
  const box = await folder(owner.agent, 'root', 'Box');
  await owner.agent.patch(`/api/nodes/${late.id}`).send({ parentId: box.id }).expect(200);
  await owner.agent.put(`/api/ai/folders/${box.id}/excluded`).send({ excluded: true }).expect(200);
  await runAiJobsUntilIdle();
  const job = db.prepare('SELECT status, error_code FROM ai_jobs WHERE node_id = ?').get(late.id);
  assert.deepEqual({ ...job }, { status: 'skipped', error_code: 'AI_EXCLUDED' });
});

test('another account sees none of it: suggestions, jobs, summaries and rules answer 404', async () => {
  assert.equal((await other.agent.get('/api/ai/suggestions').expect(200)).body.total, 0);
  await other.agent.post(`/api/ai/suggestions/${suggestion.id}/accept`).send({}).expect(404);
  await other.agent.post(`/api/ai/suggestions/${suggestion.id}/reject`).expect(404);
  await other.agent.post(`/api/ai/suggestions/${suggestion.id}/undo`).expect(404);
  await other.agent.post('/api/ai/jobs').send({ nodeIds: [file.id] }).expect(404);
  await other.agent.post('/api/ai/jobs').send({ folderId: file.parentId }).expect(404);
  await other.agent.post(`/api/ai/files/${file.id}/summary`).send({}).expect(404);
  await other.agent.post('/api/ai/rules').send({ name: 'x', match: { type: 'invoice' }, targetFolderId: file.parentId }).expect(404);
  const mine = (await owner.agent.post('/api/ai/rules').send({ name: 'x', match: { type: 'invoice' }, targetFolderId: 'root' }).expect(201)).body.rule;
  await other.agent.patch(`/api/ai/rules/${mine.id}`).send({ enabled: false }).expect(404);
  await other.agent.delete(`/api/ai/rules/${mine.id}`).expect(404);
  assert.equal((await other.agent.get('/api/ai/rules').expect(200)).body.rules.length, 0);
  // The owner's suggestion is untouched.
  assert.equal(db.prepare('SELECT status FROM ai_suggestions WHERE id = ?').get(suggestion.id).status, 'pending');
});

test('files only shared with the caller are never processed for them', async () => {
  // The member can read and write it, but AI works on the owner's nodes only.
  await other.agent.get(`/api/nodes/${sharedFile.id}`).expect(200);
  await other.agent.post('/api/ai/jobs').send({ nodeIds: [sharedFile.id] }).expect(404);
  await other.agent.post(`/api/ai/files/${sharedFile.id}/summary`).send({}).expect(404);
  const sharedFolder = sharedFile.parentId;
  await other.agent.post('/api/ai/jobs').send({ folderId: sharedFolder }).expect(404);
  // And the owner's AI tags and metadata don't show to the member.
  db.prepare("INSERT INTO ai_node_meta (node_id, user_id, etag, tags_json) VALUES (?, ?, ?, '[\"team\"]')")
    .run(sharedFile.id, owner.user.id, sharedFile.etag);
  const seen = (await other.agent.get(`/api/nodes/${sharedFile.id}`).expect(200)).body.node;
  assert.equal(seen.aiTags, undefined);
  assert.equal(seen.aiMeta, undefined);
  assert.deepEqual((await owner.agent.get(`/api/nodes/${sharedFile.id}`).expect(200)).body.node.aiTags, ['team']);
});

test('desktop tokens do not reach the AI routes; the phone app does', async () => {
  const desktop = `Bearer ${createDeviceToken(owner.user.id, { name: 'Lite', platform: 'lite' }).token}`;
  const phone = `Bearer ${createDeviceToken(owner.user.id, { name: 'Phone', platform: 'android' }).token}`;
  for (const path of ['/api/ai/suggestions', '/api/ai/jobs', '/api/ai/rules']) {
    await request(app).get(path).set('Authorization', desktop).expect(403);
    await request(app).get(path).set('Authorization', phone).expect(200);
  }
  await request(app).post(`/api/ai/suggestions/${suggestion.id}/reject`).set('Authorization', desktop).expect(403);
});
