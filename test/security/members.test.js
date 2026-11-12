'use strict';

// Sharing with other accounts: who may add whom, and above all what a
// member can and cannot reach through the file and transfer routes.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { attachSocketServer } = require('../../server/services/events');

test.after(cleanup);

let owner; // first account: also the admin
let reader;
let writer;
let stranger;
let shared; // owner's folder, shared with reader (read) and writer (write)
let sharedFile;
let inner; // a folder inside `shared`
let privateFolder; // owner's folder next to it, shared with nobody
let privateFile;
let readerShareId;
let writerShareId;

function vaultKeys() {
  return {
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  };
}

function usedBytes(userId) {
  return db.prepare('SELECT used_bytes FROM users WHERE id = ?').get(userId).used_bytes;
}

function share(agent, nodeId, recipient, permission = 'read') {
  return agent.post(`/api/nodes/${nodeId}/members`).send({ recipient, permission });
}

test.before(async () => {
  owner = await registerUser('owner');
  reader = await registerUser('reader');
  writer = await registerUser('writer');
  stranger = await registerUser('stranger');
  shared = (await owner.agent.post('/api/folders').send({ name: 'Team' }).expect(201)).body;
  inner = (await owner.agent.post('/api/folders').send({ parentId: shared.id, name: 'Inner' }).expect(201)).body;
  sharedFile = (await upload(owner.agent, 'plan.txt', 'the shared plan', { parentId: shared.id }).expect(201)).body;
  privateFolder = (await owner.agent.post('/api/folders').send({ name: 'Private' }).expect(201)).body;
  privateFile = (await upload(owner.agent, 'diary.txt', 'private words', { parentId: privateFolder.id }).expect(201)).body;
  readerShareId = (await share(owner.agent, shared.id, reader.username.toUpperCase()).expect(201)).body.id;
  writerShareId = (await share(owner.agent, shared.id, `${writer.username}@EXAMPLE.com`, 'write').expect(201)).body.id;
});

test('members are listed with their display name and what the owner typed, nothing more', async () => {
  const { members } = (await owner.agent.get(`/api/nodes/${shared.id}/members`).expect(200)).body;
  assert.equal(members.length, 2);
  const byId = new Map(members.map((member) => [member.id, member]));
  assert.equal(byId.get(readerShareId).recipient, reader.username.toUpperCase());
  assert.equal(byId.get(readerShareId).permission, 'read');
  assert.equal(byId.get(writerShareId).permission, 'write');
  assert.equal(byId.get(writerShareId).expiresAt, null);
  // Added by username: the member's email never appears.
  assert.equal(JSON.stringify(byId.get(readerShareId)).includes('@'), false);
  assert.equal(Object.hasOwn(byId.get(readerShareId), 'email'), false);
  assert.equal(Object.hasOwn(byId.get(readerShareId), 'granteeId'), false);
});

test('the recipient lookup is exact and enumeration-safe', async () => {
  const folder = (await owner.agent.post('/api/folders').send({ name: 'Probe' }).expect(201)).body;
  const unknown = await share(owner.agent, folder.id, 'nobody-here').expect(404);
  const unknownMail = await share(owner.agent, folder.id, 'nobody@example.com').expect(404);
  const self = await share(owner.agent, folder.id, owner.username).expect(404);
  const selfMail = await share(owner.agent, folder.id, `${owner.username}@example.com`).expect(404);
  // A prefix or a partial address is no match.
  const prefix = await share(owner.agent, folder.id, reader.username.slice(0, -1)).expect(404);
  const partial = await share(owner.agent, folder.id, `${reader.username}@example`).expect(404);
  for (const res of [unknown, unknownMail, self, selfMail, prefix, partial]) {
    assert.deepEqual(res.body, unknown.body);
    assert.equal(res.body.code, 'RECIPIENT_NOT_FOUND');
  }
  // The same account twice is a conflict only the owner can see.
  await share(owner.agent, shared.id, reader.username).expect(409);
  await share(owner.agent, folder.id, '').expect(400);
  await owner.agent.post(`/api/nodes/${folder.id}/members`).send({ recipient: { $ne: '' } }).expect(400);
  await share(owner.agent, folder.id, reader.username, 'admin').expect(400);
});

test('there is no way to list or search accounts', async () => {
  await reader.agent.get('/api/admin/users').expect(403);
  await reader.agent.get('/api/users').expect(404);
  await reader.agent.get('/api/shared-with-me?q=owner').expect(200);
});

test('adding members is rate-limited per account', async () => {
  const prober = await registerUser('prober');
  const folder = (await prober.agent.post('/api/folders').send({ name: 'Probe' }).expect(201)).body;
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await share(prober.agent, folder.id, `guess${attempt}`).expect(404);
  }
  const limited = await share(prober.agent, folder.id, reader.username).expect(429);
  assert.equal(limited.body.code, 'RATE_LIMITED');
  // Another account's budget is its own.
  const folderOfStranger = (await stranger.agent.post('/api/folders').send({ name: 'S' }).expect(201)).body;
  await share(stranger.agent, folderOfStranger.id, 'nobody-again').expect(404);
});

test('root, vault folders, vault contents and other accounts’ nodes can’t be shared; files only read', async () => {
  const root = (await owner.agent.get('/api/folders/root/children').expect(200)).body.folder;
  assert.equal((await share(owner.agent, root.id, reader.username).expect(400)).body.code, 'ROOT_IMMUTABLE');
  const vault = (await owner.agent.post('/api/vaults').send({ parentId: shared.id, name: 'Vault', ...vaultKeys() }).expect(201)).body;
  assert.equal((await share(owner.agent, vault.folderId, reader.username).expect(409)).body.code, 'VAULT_NOT_SHAREABLE');
  assert.equal((await share(owner.agent, sharedFile.id, reader.username, 'write').expect(400)).body.code, 'WRITE_NEEDS_FOLDER');
  // Someone else's node behaves as missing.
  await share(stranger.agent, privateFolder.id, reader.username).expect(404);
});

test('a non-member is refused everywhere, exactly as for a missing node', async () => {
  const agent = stranger.agent;
  await agent.get(`/api/folders/${shared.id}/children`).expect(404);
  await agent.get(`/api/nodes/${shared.id}`).expect(404);
  await agent.get(`/api/files/${sharedFile.id}/content`).expect(404);
  await agent.get(`/api/files/${sharedFile.id}/thumbnail`).expect(404);
  await agent.get(`/api/files/${sharedFile.id}/preview`).expect(404);
  await agent.get(`/api/download/zip?ids=${shared.id}`).expect(404);
  await upload(agent, 'x.txt', 'x', { parentId: shared.id }).expect(404);
  await agent.post('/api/uploads').send({ parentId: shared.id, name: 'x.bin', size: 1 }).expect(404);
  await agent.post('/api/folders').send({ parentId: shared.id, name: 'x' }).expect(404);
  await agent.patch(`/api/nodes/${sharedFile.id}`).send({ name: 'x.txt' }).expect(404);
  await agent.post(`/api/nodes/${sharedFile.id}/copy`).send({}).expect(404);
  await agent.delete(`/api/nodes/${sharedFile.id}`).expect(404);
  await agent.get(`/api/nodes/${shared.id}/members`).expect(404);
  const missing = await agent.get(`/api/nodes/${crypto.randomUUID()}`).expect(404);
  const foreign = await agent.get(`/api/nodes/${shared.id}`).expect(404);
  assert.deepEqual(foreign.body, missing.body);
  const { shares } = (await agent.get('/api/shared-with-me').expect(200)).body;
  assert.equal(shares.length, 0);
});

test('a read member can browse and download, from the share down only', async () => {
  const listing = (await reader.agent.get(`/api/folders/${shared.id}/children`).expect(200)).body;
  assert.deepEqual(listing.path, [{ id: shared.id, name: 'Team' }]);
  assert.equal(listing.folder.parentId, null, 'the owner’s folder above stays hidden');
  const names = listing.items.map((item) => item.name).sort();
  assert.deepEqual(names, ['Inner', 'plan.txt'], 'the vault inside stays the owner’s');
  assert.equal(listing.total, 2);
  assert.ok(listing.items.every((item) => item.favorite === false));
  assert.ok(listing.items.every((item) => !Object.hasOwn(item, 'shared')), 'who else it is shared with stays hidden');
  const ownerView = (await owner.agent.get('/api/folders/root/children').expect(200)).body.items;
  assert.equal(ownerView.find((item) => item.id === shared.id).shared, true);
  assert.equal(ownerView.find((item) => item.id === privateFolder.id).shared, false);

  const nested = (await reader.agent.get(`/api/nodes/${inner.id}`).expect(200)).body;
  assert.deepEqual(nested.path.map((crumb) => crumb.name), ['Team', 'Inner']);

  assert.equal((await reader.agent.get(`/api/files/${sharedFile.id}/content`).expect(200)).text, 'the shared plan');
  const range = await reader.agent.get(`/api/files/${sharedFile.id}/content`).set('Range', 'bytes=4-9').expect(206);
  assert.equal(range.text, 'shared');
  const zip = await reader.agent.get(`/api/download/zip?ids=${shared.id}`)
    .buffer(true)
    .parse((res, callback) => {
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => callback(null, Buffer.concat(chunks)));
    })
    .expect(200);
  assert.equal(zip.headers['content-type'], 'application/zip');
  assert.ok(zip.body.includes(Buffer.from('Team/plan.txt')));
  assert.equal(zip.body.includes(Buffer.from('Vault/')), false, 'no vault in the archive');
});

test('a read member can’t write anything', async () => {
  const refused = async (res) => assert.equal((await res.expect(403)).body.code, 'SHARE_READ_ONLY');
  await refused(upload(reader.agent, 'new.txt', 'x', { parentId: shared.id }));
  await refused(reader.agent.post('/api/uploads').send({ parentId: shared.id, name: 'x.bin', size: 1 }));
  await refused(reader.agent.post('/api/folders').send({ parentId: shared.id, name: 'New' }));
  await refused(reader.agent.post('/api/folders').send({ parentId: shared.id, path: 'a/b' }));
  await refused(reader.agent.patch(`/api/nodes/${sharedFile.id}`).send({ name: 'renamed.txt' }));
  await refused(reader.agent.patch(`/api/nodes/${inner.id}`).send({ parentId: shared.id }));
  await refused(reader.agent.delete(`/api/nodes/${sharedFile.id}`));
  await refused(reader.agent.post(`/api/nodes/${sharedFile.id}/copy`).send({ parentId: inner.id }));
  const own = (await upload(reader.agent, 'mine.txt', 'mine').expect(201)).body;
  await refused(reader.agent.post(`/api/nodes/${own.id}/copy`).send({ parentId: shared.id }));
  assert.equal((await owner.agent.get(`/api/files/${sharedFile.id}/content`).expect(200)).text, 'the shared plan');
});

test('members can’t re-share, link, favorite or see versions', async () => {
  for (const member of [reader, writer]) {
    await share(member.agent, shared.id, stranger.username).expect(404);
    await share(member.agent, inner.id, stranger.username).expect(404);
    await member.agent.get(`/api/nodes/${shared.id}/members`).expect(404);
    await member.agent.patch(`/api/nodes/${shared.id}/members/${readerShareId}`).send({ permission: 'write' }).expect(404);
    await member.agent.delete(`/api/nodes/${shared.id}/members/${readerShareId}`).expect(404);
    await member.agent.post('/api/shares').send({ nodeId: shared.id }).expect(404);
    await member.agent.post('/api/shares').send({ nodeId: sharedFile.id }).expect(404);
    await member.agent.put(`/api/nodes/${sharedFile.id}/favorite`).expect(404);
    await member.agent.get(`/api/files/${sharedFile.id}/versions`).expect(404);
    await member.agent.get(`/api/files/${sharedFile.id}/content?version=1`).expect(404);
    await member.agent.post(`/api/files/${sharedFile.id}/versions/1/restore`).expect(404);
    await member.agent.post('/api/vaults').send({ parentId: shared.id, name: 'V', ...vaultKeys() }).expect(404);
  }
  // The owner's member list is unchanged.
  assert.equal((await owner.agent.get(`/api/nodes/${shared.id}/members`).expect(200)).body.members.length, 2);
});

test('a member learns nothing about the owner’s other files', async () => {
  const root = (await owner.agent.get('/api/folders/root/children').expect(200)).body.folder;
  for (const member of [reader, writer]) {
    await member.agent.get(`/api/folders/${root.id}/children`).expect(404);
    await member.agent.get(`/api/nodes/${privateFolder.id}`).expect(404);
    await member.agent.get(`/api/files/${privateFile.id}/content`).expect(404);
    await member.agent.get(`/api/download/zip?ids=${privateFile.id}`).expect(404);
    await member.agent.get(`/api/download/zip?ids=${shared.id},${privateFile.id}`).expect(404);
    const mine = (await upload(member.agent, `zip-${member.username}.txt`, 'mine').expect(201)).body;
    const mixed = await member.agent.get(`/api/download/zip?ids=${shared.id},${mine.id}`).expect(400);
    assert.equal(mixed.body.code, 'ZIP_MIXED_OWNERS');
    const vault = db.prepare('SELECT folder_id FROM vaults WHERE user_id = ?').get(owner.user.id);
    await member.agent.get(`/api/folders/${vault.folder_id}/children`).expect(404);
    await member.agent.get(`/api/nodes/${vault.folder_id}`).expect(404);
    // The member's own search, recent files and sync tree stay the member's.
    const search = JSON.stringify((await member.agent.get('/api/search?q=plan').expect(200)).body);
    assert.equal(search.includes(sharedFile.id), false);
    const tree = JSON.stringify((await member.agent.get('/api/sync/tree').expect(200)).body);
    assert.equal(tree.includes(shared.id), false);
    assert.equal(tree.includes(privateFile.id), false);
    const recent = JSON.stringify((await member.agent.get('/api/recent').expect(200)).body);
    assert.equal(recent.includes(sharedFile.id), false);
  }
});

test('shared-with-me lists the share roots with owner name and permission', async () => {
  const { shares } = (await reader.agent.get('/api/shared-with-me').expect(200)).body;
  assert.equal(shares.length, 1);
  assert.equal(shares[0].id, readerShareId);
  assert.equal(shares[0].permission, 'read');
  assert.equal(shares[0].ownerName, owner.username);
  assert.equal(shares[0].node.id, shared.id);
  assert.equal(shares[0].node.parentId, null);
  assert.equal(shares[0].node.type, 'folder');
  assert.equal((await writer.agent.get('/api/shared-with-me').expect(200)).body.shares[0].permission, 'write');
});

test('a write member’s changes land in the owner’s tree and on the owner’s quota', async () => {
  const ownerBefore = usedBytes(owner.user.id);
  const writerBefore = usedBytes(writer.user.id);
  const content = 'written by a member';
  const file = (await upload(writer.agent, 'note.txt', content, { parentId: shared.id }).expect(201)).body;
  assert.equal(file.parentId, shared.id);
  assert.equal(usedBytes(owner.user.id), ownerBefore + content.length);
  assert.equal(usedBytes(writer.user.id), writerBefore);
  const row = db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(file.id);
  assert.equal(row.user_id, owner.user.id);
  assert.equal((await owner.agent.get(`/api/files/${file.id}/content`).expect(200)).text, content);

  const folder = (await writer.agent.post('/api/folders').send({ parentId: shared.id, name: 'Drafts' }).expect(201)).body;
  const deep = (await writer.agent.post('/api/folders').send({ parentId: folder.id, path: 'a/b' }).expect(201)).body;
  assert.equal(deep.name, 'b');
  const renamed = (await writer.agent.patch(`/api/nodes/${file.id}`).send({ name: 'note-2.txt' }).expect(200)).body;
  assert.equal(renamed.name, 'note-2.txt');
  const moved = (await writer.agent.patch(`/api/nodes/${file.id}`).send({ parentId: folder.id }).expect(200)).body;
  assert.equal(moved.parentId, folder.id);
  const copy = (await writer.agent.post(`/api/nodes/${file.id}/copy`).send({ parentId: inner.id }).expect(201)).body;
  assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(copy.id).user_id, owner.user.id);
  // Replacing a file keeps a version, still on the owner's side.
  await upload(writer.agent, 'note-2.txt', 'second', { parentId: folder.id, conflict: 'replace' }).expect(201);
  assert.equal((await owner.agent.get(`/api/files/${file.id}/versions`).expect(200)).body.versions.length, 1);

  await writer.agent.delete(`/api/nodes/${copy.id}`).expect(200);
  await writer.agent.get(`/api/nodes/${copy.id}`).expect(404);
  const restored = (await writer.agent.post(`/api/trash/${copy.id}/restore`).expect(200)).body;
  assert.equal(restored.parentId, inner.id);
  // Deleting for good is the owner's.
  await writer.agent.delete(`/api/nodes/${copy.id}`).expect(200);
  await writer.agent.delete(`/api/trash/${copy.id}`).expect(404);
  await writer.agent.delete('/api/trash').expect(200);
  assert.ok(db.prepare('SELECT 1 FROM nodes WHERE id = ?').get(copy.id), 'emptying the member’s trash leaves the owner’s');
  const { items } = (await owner.agent.get('/api/trash').expect(200)).body;
  assert.ok(items.some((item) => item.id === copy.id));
});

test('the shared folder itself can’t be renamed, moved or deleted by a member', async () => {
  for (const request of [
    writer.agent.patch(`/api/nodes/${shared.id}`).send({ name: 'Mine now' }),
    writer.agent.patch(`/api/nodes/${shared.id}`).send({ parentId: inner.id }),
    writer.agent.delete(`/api/nodes/${shared.id}`),
  ]) {
    assert.equal((await request.expect(403)).body.code, 'SHARE_ROOT_IMMUTABLE');
  }
  assert.equal((await owner.agent.get(`/api/nodes/${shared.id}`).expect(200)).body.node.name, 'Team');
});

test('parent ids outside the share are refused (no path traversal)', async () => {
  const root = (await owner.agent.get('/api/folders/root/children').expect(200)).body.folder;
  for (const parentId of [root.id, privateFolder.id, 'root']) {
    const res = await upload(writer.agent, 'escape.txt', 'x', { parentId });
    // `root` is the member's own root; the owner's folders are missing.
    if (parentId === 'root') {
      assert.equal(res.status, 201);
      assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(res.body.id).user_id, writer.user.id);
      continue;
    }
    assert.equal(res.status, 404);
    await writer.agent.post('/api/folders').send({ parentId, name: 'escape' }).expect(404);
    await writer.agent.post('/api/uploads').send({ parentId, name: 'escape.bin', size: 1 }).expect(404);
    await writer.agent.patch(`/api/nodes/${sharedFile.id}`).send({ parentId }).expect(404);
    await writer.agent.post(`/api/nodes/${sharedFile.id}/copy`).send({ parentId }).expect(404);
  }
  // Restoring something the owner trashed outside the share is refused too.
  const outside = (await upload(owner.agent, 'outside.txt', 'x', { parentId: privateFolder.id }).expect(201)).body;
  await owner.agent.delete(`/api/nodes/${outside.id}`).expect(200);
  await writer.agent.post(`/api/trash/${outside.id}/restore`).expect(404);
  // Path segments can't climb out of the folder either.
  await writer.agent.post('/api/folders').send({ parentId: shared.id, path: '../escape' }).expect(400);
  const names = (await owner.agent.get('/api/folders/root/children').expect(200)).body.items.map((item) => item.name);
  assert.equal(names.includes('escape.txt'), false);
  assert.equal(names.includes('escape'), false);
  assert.equal((await owner.agent.get(`/api/nodes/${sharedFile.id}`).expect(200)).body.node.parentId, shared.id);
});

test('moves never cross the share’s border; copies out are charged to the member', async () => {
  const own = (await writer.agent.post('/api/folders').send({ name: 'Own' }).expect(201)).body;
  const ownFile = (await upload(writer.agent, 'own.txt', 'own bytes').expect(201)).body;
  const boundary = async (res) => assert.equal((await res.expect(409)).body.code, 'SHARE_BOUNDARY');
  await boundary(writer.agent.patch(`/api/nodes/${sharedFile.id}`).send({ parentId: own.id }));
  await boundary(writer.agent.patch(`/api/nodes/${ownFile.id}`).send({ parentId: shared.id }));
  await boundary(writer.agent.patch(`/api/nodes/${inner.id}`).send({ parentId: 'root' }));

  // A copy out of the share is a new file of the member's, on their quota.
  const ownerBefore = usedBytes(owner.user.id);
  const writerBefore = usedBytes(writer.user.id);
  const out = (await writer.agent.post(`/api/nodes/${sharedFile.id}/copy`).send({ parentId: own.id }).expect(201)).body;
  assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(out.id).user_id, writer.user.id);
  assert.equal(usedBytes(writer.user.id), writerBefore + 'the shared plan'.length);
  assert.equal(usedBytes(owner.user.id), ownerBefore);
  assert.equal((await writer.agent.get(`/api/files/${out.id}/content`).expect(200)).text, 'the shared plan');
  await owner.agent.get(`/api/files/${out.id}/content`).expect(404);

  // A folder copied out comes with its contents, minus any vault.
  const folderCopy = (await writer.agent.post(`/api/nodes/${shared.id}/copy`).send({ parentId: own.id }).expect(201)).body;
  const copied = (await writer.agent.get(`/api/folders/${folderCopy.id}/children`).expect(200)).body.items.map((item) => item.name);
  assert.ok(copied.includes('plan.txt'));
  assert.equal(copied.includes('Vault'), false);

  // A copy into the share is the owner's, on the owner's quota.
  const ownerMid = usedBytes(owner.user.id);
  const writerMid = usedBytes(writer.user.id);
  const into = (await writer.agent.post(`/api/nodes/${ownFile.id}/copy`).send({ parentId: shared.id }).expect(201)).body;
  assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(into.id).user_id, owner.user.id);
  assert.equal(usedBytes(owner.user.id), ownerMid + 'own bytes'.length);
  assert.equal(usedBytes(writer.user.id), writerMid);
  assert.equal((await owner.agent.get(`/api/files/${into.id}/content`).expect(200)).text, 'own bytes');
});

test('a copy out of a share respects the member’s quota', async () => {
  const tight = await registerUser('tight');
  db.prepare('UPDATE users SET quota_bytes = 4 WHERE id = ?').run(tight.user.id);
  const grant = (await share(owner.agent, shared.id, tight.username).expect(201)).body;
  const res = await tight.agent.post(`/api/nodes/${sharedFile.id}/copy`).send({ parentId: 'root' }).expect(413);
  assert.equal(res.body.code, 'QUOTA_EXCEEDED');
  assert.equal(usedBytes(tight.user.id), 0);
  // A folder is checked as a whole before anything is written.
  const folderRes = await tight.agent.post(`/api/nodes/${shared.id}/copy`).send({ parentId: 'root' }).expect(413);
  assert.equal(folderRes.body.code, 'QUOTA_EXCEEDED');
  assert.equal((await tight.agent.get('/api/folders/root/children').expect(200)).body.total, 0);
  db.prepare('UPDATE users SET quota_bytes = NULL WHERE id = ?').run(tight.user.id);
  process.env.NEOCLOUD_MAX_COPY_NODES = '2';
  try {
    const tooMany = await tight.agent.post(`/api/nodes/${shared.id}/copy`).send({ parentId: 'root' }).expect(413);
    assert.equal(tooMany.body.code, 'COPY_TOO_LARGE');
  } finally {
    delete process.env.NEOCLOUD_MAX_COPY_NODES;
  }
  assert.equal((await tight.agent.get('/api/folders/root/children').expect(200)).body.total, 0);
  await owner.agent.delete(`/api/nodes/${shared.id}/members/${grant.id}`).expect(200);
});

test('a member’s resumable upload is the owner’s and stops when access ends', async () => {
  const session = (await writer.agent.post('/api/uploads').send({ parentId: shared.id, name: 'big.bin', size: 3 }).expect(201)).body;
  const row = db.prepare('SELECT user_id, created_by FROM upload_sessions WHERE id = ?').get(session.id);
  assert.equal(row.user_id, owner.user.id);
  assert.equal(row.created_by, writer.user.id);
  await stranger.agent.get(`/api/uploads/${session.id}`).expect(404);
  await reader.agent.put(`/api/uploads/${session.id}/parts/0`).send(Buffer.from('abc')).expect(404);
  await writer.agent.put(`/api/uploads/${session.id}/parts/0`).set('Content-Type', 'application/octet-stream').send(Buffer.from('abc')).expect(200);
  await writer.agent.get(`/api/uploads/${session.id}`).expect(200);

  // Read-only from now on: the upload can't be finished.
  await owner.agent.patch(`/api/nodes/${shared.id}/members/${writerShareId}`).send({ permission: 'read' }).expect(200);
  await writer.agent.post(`/api/uploads/${session.id}/complete`).send({}).expect(403);
  await owner.agent.patch(`/api/nodes/${shared.id}/members/${writerShareId}`).send({ permission: 'write' }).expect(200);
  const done = (await writer.agent.post(`/api/uploads/${session.id}/complete`).send({}).expect(201)).body;
  assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(done.id).user_id, owner.user.id);
});

test('members are told when a shared folder changes, others are not', async () => {
  const sent = [];
  attachSocketServer({
    to: (room) => ({ emit: (event, payload) => sent.push({ room, event, payload }) }),
    in: () => ({ disconnectSockets() {} }),
  });
  try {
    await upload(owner.agent, 'live.txt', 'x', { parentId: inner.id }).expect(201);
    await upload(owner.agent, 'quiet.txt', 'x', { parentId: privateFolder.id }).expect(201);
  } finally {
    attachSocketServer(null);
  }
  const changed = sent.filter((entry) => entry.event === 'files:changed');
  const rooms = (folderId) => changed.filter((entry) => entry.payload.folderIds.includes(folderId)).map((entry) => entry.room).sort();
  assert.deepEqual(rooms(inner.id), [`user:${owner.user.id}`, `user:${reader.user.id}`, `user:${writer.user.id}`].sort());
  assert.deepEqual(rooms(privateFolder.id), [`user:${owner.user.id}`]);
});

test('access ends the moment a member is removed or leaves', async () => {
  const guest = await registerUser('guest');
  const grant = (await share(owner.agent, shared.id, guest.username, 'write').expect(201)).body;
  await guest.agent.get(`/api/folders/${shared.id}/children`).expect(200);
  await owner.agent.delete(`/api/nodes/${shared.id}/members/${grant.id}`).expect(200);
  await guest.agent.get(`/api/folders/${shared.id}/children`).expect(404);
  await guest.agent.get(`/api/files/${sharedFile.id}/content`).expect(404);
  await upload(guest.agent, 'late.txt', 'x', { parentId: shared.id }).expect(404);
  assert.equal((await guest.agent.get('/api/shared-with-me').expect(200)).body.shares.length, 0);

  const again = (await share(owner.agent, shared.id, guest.username).expect(201)).body;
  await guest.agent.get(`/api/nodes/${inner.id}`).expect(200);
  await stranger.agent.delete(`/api/shared-with-me/${again.id}`).expect(404);
  await owner.agent.delete(`/api/shared-with-me/${again.id}`).expect(404);
  await guest.agent.delete(`/api/shared-with-me/${again.id}`).expect(200);
  await guest.agent.get(`/api/nodes/${inner.id}`).expect(404);
  assert.equal((await owner.agent.get(`/api/nodes/${shared.id}/members`).expect(200)).body.members
    .some((member) => member.id === again.id), false);
});

test('an expired share grants nothing', async () => {
  const late = await registerUser('late');
  const grant = (await owner.agent.post(`/api/nodes/${shared.id}/members`)
    .send({ recipient: late.username, permission: 'read', expiresAt: new Date(Date.now() + 3600e3).toISOString() })
    .expect(201)).body;
  assert.ok(grant.expiresAt);
  await late.agent.get(`/api/nodes/${shared.id}`).expect(200);
  db.prepare("UPDATE user_shares SET expires_at = datetime('now', '-1 minute') WHERE id = ?").run(grant.id);
  await late.agent.get(`/api/nodes/${shared.id}`).expect(404);
  assert.equal((await late.agent.get('/api/shared-with-me').expect(200)).body.shares.length, 0);
  const listed = (await owner.agent.get(`/api/nodes/${shared.id}/members`).expect(200)).body.members.find((m) => m.id === grant.id);
  assert.equal(listed.expired, true);
  await owner.agent.post(`/api/nodes/${shared.id}/members`)
    .send({ recipient: 'x', expiresAt: '2001-01-01T00:00:00Z' }).expect(400);
});

test('a share in the owner’s trash is hidden; deleted nodes and accounts cascade', async () => {
  const folder = (await owner.agent.post('/api/folders').send({ name: 'Short-lived' }).expect(201)).body;
  const grant = (await share(owner.agent, folder.id, reader.username).expect(201)).body;
  await reader.agent.get(`/api/nodes/${folder.id}`).expect(200);
  await owner.agent.delete(`/api/nodes/${folder.id}`).expect(200);
  await reader.agent.get(`/api/nodes/${folder.id}`).expect(404);
  assert.equal((await reader.agent.get('/api/shared-with-me').expect(200)).body.shares.some((s) => s.id === grant.id), false);
  await owner.agent.delete(`/api/trash/${folder.id}`).expect(200);
  assert.equal(db.prepare('SELECT 1 FROM user_shares WHERE id = ?').get(grant.id), undefined);

  // A deleted owner takes their shares along.
  const leaving = await registerUser('leaving');
  const theirs = (await leaving.agent.post('/api/folders').send({ name: 'Bye' }).expect(201)).body;
  const theirGrant = (await share(leaving.agent, theirs.id, reader.username).expect(201)).body;
  await owner.agent.delete(`/api/admin/users/${leaving.user.id}`).expect(200);
  assert.equal(db.prepare('SELECT 1 FROM user_shares WHERE id = ?').get(theirGrant.id), undefined);
  await reader.agent.get(`/api/nodes/${theirs.id}`).expect(404);

  // So does a deleted member.
  const member = await registerUser('member');
  const memberGrant = (await share(owner.agent, shared.id, member.username).expect(201)).body;
  await owner.agent.delete(`/api/admin/users/${member.user.id}`).expect(200);
  assert.equal(db.prepare('SELECT 1 FROM user_shares WHERE id = ?').get(memberGrant.id), undefined);
});
