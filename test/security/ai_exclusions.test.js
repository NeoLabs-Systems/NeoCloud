'use strict';

// What AI may never read: vaults, folders their owner excluded (and all
// below them), and anything of another account. The exclusion switch is the
// owner's alone.

const crypto = require('crypto');
const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const { assertAiAllowedForNode } = require('../../server/services/ai/exclusions');
const { createDeviceToken } = require('../../server/services/devices/tokens');

test.after(cleanup);

let alice;
let bob;

test.before(async () => {
  alice = await registerUser('aiguarda');
  bob = await registerUser('aiguardb');
});

function guardCode(userId, nodeId) {
  try {
    assertAiAllowedForNode(userId, nodeId);
    return null;
  } catch (error) {
    return error.code;
  }
}

async function folder(agent, parentId, name) {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

test('vault content and excluded folders are refused, other files pass', async () => {
  const docs = await folder(alice.agent, 'root', 'Docs');
  const sub = await folder(alice.agent, docs.id, 'Sub');
  const file = (await upload(alice.agent, 'note.txt', 'hello', { parentId: sub.id }).expect(201)).body;
  assert.equal(guardCode(alice.user.id, file.id), null);
  assert.equal(guardCode(alice.user.id, sub.id), null);

  const marked = await alice.agent.put(`/api/ai/folders/${docs.id}/excluded`).send({ excluded: true }).expect(200);
  assert.equal(marked.body.node.aiExcluded, true);
  assert.equal((await alice.agent.get(`/api/nodes/${docs.id}`).expect(200)).body.node.aiExcluded, true);
  assert.equal(guardCode(alice.user.id, docs.id), 'AI_EXCLUDED');
  assert.equal(guardCode(alice.user.id, file.id), 'AI_EXCLUDED');
  await alice.agent.put(`/api/ai/folders/${docs.id}/excluded`).send({ excluded: false }).expect(200);
  assert.equal(guardCode(alice.user.id, file.id), null);

  const vault = (await alice.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Tresor',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  const sealed = (await upload(alice.agent, 'b3JwaGFu', 'NCV1 x', { parentId: vault.folderId }).expect(201)).body;
  assert.equal(guardCode(alice.user.id, vault.folderId), 'AI_VAULT');
  assert.equal(guardCode(alice.user.id, sealed.id), 'AI_VAULT');

  await alice.agent.delete(`/api/nodes/${file.id}`).expect(200);
  assert.equal(guardCode(alice.user.id, file.id), 'NOT_FOUND');
});

test('another account’s node is missing, for the guard and for the switch', async () => {
  const mine = await folder(alice.agent, 'root', 'Private');
  assert.equal(guardCode(bob.user.id, mine.id), 'NOT_FOUND');
  assert.equal(guardCode(bob.user.id, 'no-such-node'), 'NOT_FOUND');
  await bob.agent.put(`/api/ai/folders/${mine.id}/excluded`).send({ excluded: true }).expect(404);
  assert.equal((await alice.agent.get(`/api/nodes/${mine.id}`).expect(200)).body.node.aiExcluded, false);
});

test('the switch takes a folder of one’s own, a boolean, and not the root', async () => {
  const file = (await upload(alice.agent, 'plain.txt', 'x').expect(201)).body;
  const notFolder = await alice.agent.put(`/api/ai/folders/${file.id}/excluded`).send({ excluded: true }).expect(400);
  assert.equal(notFolder.body.code, 'NOT_A_FOLDER');
  const root = await alice.agent.put('/api/ai/folders/root/excluded').send({ excluded: true }).expect(400);
  assert.equal(root.body.code, 'ROOT_FOLDER');
  const target = await folder(alice.agent, 'root', 'Target');
  await alice.agent.put(`/api/ai/folders/${target.id}/excluded`).send({ excluded: 'yes' }).expect(400);
  await request(app).put(`/api/ai/folders/${target.id}/excluded`).send({ excluded: true }).expect(401);
  const { token } = createDeviceToken(alice.user.id, { name: 'Desktop', platform: 'lite' });
  await request(app).put(`/api/ai/folders/${target.id}/excluded`).set('Authorization', `Bearer ${token}`)
    .send({ excluded: true }).expect(403);
});
