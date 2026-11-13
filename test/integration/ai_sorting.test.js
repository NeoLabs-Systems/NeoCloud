'use strict';

// Sorting restraint and consent (docs/ai.md › Auto-sort): nothing is created
// or moved before the person accepts; a new folder needs three files of one
// job, is never high-tier (so never auto-applied or bulk-accepted), is made
// only on accept in the same step as the move, and goes again on undo; an
// existing folder in another language is reused; photos only go into the
// existing photo tree; files in the person's own folders stay in them;
// empty AI folders can be removed; file types can be left out of AI.

const test = require('node:test');
const assert = require('node:assert/strict');
const sharp = require('sharp');
const { cleanup, registerUser, upload } = require('../helpers/app');
const { startFakeProvider } = require('../helpers/fake_ai');
const db = require('../../server/db/database');
const { runAiJobsUntilIdle } = require('../../server/services/ai/organize/worker');

const SERVER_KEY = 'sk-sorting-key-7777';

let provider;
let admin;
let member;
let extraction = {};
let sortChoice = () => ({ choice: 0, confidence: 0.9, reason: 'keep' });

function promptText(body) {
  return body.messages.map((message) => (typeof message.content === 'string'
    ? message.content
    : message.content.map((part) => part.text || '').join(' '))).join('\n');
}

function kindOf(body) {
  const text = promptText(body);
  if (text.includes('report facts about it')) return 'extract';
  if (text.includes('You decide where one file belongs')) return 'sort';
  return 'other';
}

function sortCalls() {
  return provider.state.chats.filter((chat) => kindOf(chat.body) === 'sort').length;
}

// Folder profiles and file texts all look alike (no embedding decides);
// bare folder names each get their own direction (no two are "similar").
const nameDimensions = new Map();
function vector(text) {
  const values = new Array(64).fill(0);
  if (text.startsWith('Folder ') || text.includes('\n')) {
    values[0] = 1;
    values[1] = 1;
    return values;
  }
  if (!nameDimensions.has(text)) nameDimensions.set(text, 2 + nameDimensions.size);
  values[nameDimensions.get(text)] = 1;
  return values;
}

const POLICY = {
  date: '2026-02-01',
  issuer: 'Allianz',
  subject: 'Hausrat',
  type: 'contract',
  amount: null,
  currency: null,
  tags: [],
  language: 'de',
  summary: 'Insurance policy.',
  caption: null,
  confidence: { date: 0.9, issuer: 0.9, subject: 0.9, type: 0.9, amount: 0.9, tags: 0.9 },
  overall: 0.9,
};

async function features(flags) {
  await member.agent.put('/api/settings').send({ ai_features: flags }).expect(200);
}

async function folder(parentId, name) {
  return (await member.agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

async function put(name, content, parentId = 'root') {
  return (await upload(member.agent, name, content, { parentId }).expect(201)).body;
}

async function requestFolders(nodeIds) {
  await member.agent.post('/api/ai/jobs').send({ nodeIds, kinds: ['folders'] }).expect(202);
  await runAiJobsUntilIdle();
}

async function moves(query = '') {
  return (await member.agent.get(`/api/ai/suggestions?kind=move&tier=all${query}`).expect(200)).body.items;
}

function foldersNamed(name) {
  return db.prepare("SELECT * FROM nodes WHERE user_id = ? AND type = 'folder' AND name = ? AND trashed_at IS NULL")
    .all(member.user.id, name);
}

async function rejectAll() {
  for (const item of await moves()) await member.agent.post(`/api/ai/suggestions/${item.id}/reject`).expect(200);
}

test.before(async () => {
  provider = await startFakeProvider();
  provider.state.keys.add(SERVER_KEY);
  provider.state.vector = vector;
  provider.state.reply = (body) => {
    const kind = kindOf(body);
    if (kind === 'extract') return JSON.stringify(typeof extraction === 'function' ? extraction(body) : extraction);
    if (kind === 'sort') return JSON.stringify(sortChoice(String(body.messages.at(-1).content)));
    return undefined;
  };
  process.env.NEOCLOUD_AI_REFERENCE_URL = provider.referenceUrl;
  admin = await registerUser('sortadmin');
  member = await registerUser('sortmember');
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY', value: SERVER_KEY }).expect(200);
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL', value: provider.baseUrl }).expect(200);
  await admin.agent.put('/api/admin/ai').send({ enabled: true }).expect(200);
  await member.agent.put('/api/settings').send({ ai_opt_in: true }).expect(200);
  await runAiJobsUntilIdle();
});

test.after(() => {
  provider.close();
  cleanup();
});

test('new folders are off by default, and nothing is suggested into one then', async () => {
  const { settings } = (await member.agent.get('/api/settings').expect(200)).body;
  assert.deepEqual(settings.ai_features, {});
  await folder('root', 'Fotos');
  await folder('root', 'Arbeit');
  extraction = POLICY;
  sortChoice = () => ({ choice: 0, newFolder: 'Insurances', parent: 0, confidence: 0.9, reason: 'new' });
  const files = [await put('p1.txt', 'Allianz Police 1'), await put('p2.txt', 'Allianz Police 2'), await put('p3.txt', 'Allianz Police 3')];
  await requestFolders(files.map((file) => file.id));
  assert.equal((await moves()).length, 0);
  assert.equal(db.prepare("SELECT COUNT(*) AS count FROM nodes WHERE user_id = ? AND type = 'folder'").get(member.user.id).count, 3);
});

test('a new folder needs three files of one job, is never high, and is created only on accept', async () => {
  await features({ folders: true, names: false, tags: false, newFolders: true });
  const files = [await put('q1.txt', 'Allianz Police A'), await put('q2.txt', 'Allianz Police B'), await put('q3.txt', 'Allianz Police C')];
  const lone = await put('q4.txt', 'Allianz Police D');

  // One file on its own: held, nothing to see.
  await requestFolders([lone.id]);
  assert.equal((await moves()).length, 0);

  await requestFolders(files.map((file) => file.id));
  const listed = await moves();
  assert.deepEqual(listed.map((item) => item.nodeId).sort(), files.map((file) => file.id).sort());
  for (const item of listed) {
    // In the person's language (their folders are German), and generic.
    assert.equal(item.to.name, 'Versicherungen');
    assert.equal(item.to.id, null);
    assert.equal(item.tier, 'medium');
  }
  assert.equal(foldersNamed('Versicherungen').length, 0, 'suggesting creates nothing');

  // Accept-all-high takes none of them.
  assert.deepEqual((await member.agent.post('/api/ai/suggestions/accept-high').send({}).expect(200)).body, { accepted: 0, failed: [] });
  assert.equal(foldersNamed('Versicherungen').length, 0);

  const [first, second, third] = listed;
  const accepted = (await member.agent.post(`/api/ai/suggestions/${first.id}/accept`).send({}).expect(200)).body.suggestion;
  const [created] = foldersNamed('Versicherungen');
  assert.equal(created.created_by_ai, 1);
  assert.equal(accepted.node.parentId, created.id);
  await member.agent.post(`/api/ai/suggestions/${second.id}/accept`).send({}).expect(200);
  assert.equal(foldersNamed('Versicherungen').length, 1, 'the second file reuses the folder');

  // Undo both: the folder AI made goes with the last file.
  await member.agent.post(`/api/ai/suggestions/${second.id}/undo`).expect(200);
  assert.equal(foldersNamed('Versicherungen').length, 1);
  await member.agent.post(`/api/ai/suggestions/${first.id}/undo`).expect(200);
  assert.equal(foldersNamed('Versicherungen').length, 0);

  // Switched off: the pending one can't create anything any more.
  await features({ folders: true, names: false, tags: false, newFolders: false });
  const refused = await member.agent.post(`/api/ai/suggestions/${third.id}/accept`).send({}).expect(409);
  assert.equal(refused.body.code, 'AI_NEW_FOLDERS_OFF');
  assert.equal(foldersNamed('Versicherungen').length, 0);
  assert.equal((await moves('&status=stale')).some((item) => item.id === third.id), true);
});

test('a failing move leaves no new folder behind', async () => {
  await features({ folders: true, names: false, tags: false, newFolders: true });
  const files = [await put('r1.txt', 'Allianz R1'), await put('r2.txt', 'Allianz R2'), await put('r3.txt', 'Allianz R3')];
  await requestFolders(files.map((file) => file.id));
  const [item] = await moves();
  // A file now has the folder's name: creating the folder fails, nothing is moved or made.
  await put('Versicherungen', 'x');
  await member.agent.post(`/api/ai/suggestions/${item.id}/accept`).send({}).expect(409);
  assert.equal(foldersNamed('Versicherungen').length, 0);
  assert.equal(db.prepare('SELECT parent_id FROM nodes WHERE id = ?').get(item.nodeId).parent_id, files[0].parentId);
  await rejectAll();
});

test('auto-apply never creates a folder', async () => {
  await features({ folders: true, names: false, tags: false, newFolders: true, autoApplyHighConfidence: true });
  sortChoice = () => ({ choice: 0, newFolder: 'Policen', parent: 0, confidence: 0.99, reason: 'new' });
  const files = [await put('s1.txt', 'Allianz S1'), await put('s2.txt', 'Allianz S2'), await put('s3.txt', 'Allianz S3')];
  await requestFolders(files.map((file) => file.id));
  assert.equal((await moves('&status=applied')).length, 0);
  assert.equal((await moves()).length, 3);
  assert.equal(foldersNamed('Policen').length, 0);
  await rejectAll();
  await features({ folders: true, names: false, tags: false, newFolders: true });
});

test('an existing folder in another language is used instead of a new one', async () => {
  const bills = await folder('root', 'Rechnungen');
  extraction = { ...POLICY, type: 'invoice', issuer: 'Telekom', subject: 'Mobilfunk' };
  sortChoice = () => ({ choice: 0, newFolder: 'Invoices', parent: 0, confidence: 0.8, reason: 'bills' });
  const bill = await put('t1.txt', 'Telekom Rechnung');
  await requestFolders([bill.id]);
  const [item] = await moves();
  assert.deepEqual(item.to, { id: bills.id, path: '/Rechnungen' });
  assert.equal(foldersNamed('Invoices').length, 0);
  await rejectAll();
});

test('low confidence and "keep it here" give no suggestion', async () => {
  const doc = await put('u1.txt', 'Telekom Rechnung 2');
  sortChoice = (text) => {
    const line = text.split('\n').find((entry) => entry.endsWith('/Rechnungen'));
    return { choice: Number(line.split('.')[0]), confidence: 0.4, reason: 'maybe' };
  };
  await requestFolders([doc.id]);
  assert.equal((await moves()).length, 0);
  sortChoice = () => ({ choice: 0, newFolder: null, confidence: 0.9, reason: 'keep' });
  await requestFolders([doc.id]);
  assert.equal((await moves()).length, 0);
});

test('photos go only into the existing photo tree, by year when the person sorts that way', async () => {
  const fotos = foldersNamed('Fotos')[0];
  const year = await folder(fotos.id, '2026');
  const arbeit = foldersNamed('Arbeit')[0];
  const png = await sharp({ create: { width: 8, height: 8, channels: 3, background: '#88aa22' } }).png().toBuffer();
  const loose = await put('IMG_0001.png', png);
  const kept = await put('IMG_0002.png', png, arbeit.id);
  extraction = { ...POLICY, type: 'photo', issuer: null, subject: 'Wiese', date: '2026-05-01', caption: 'A meadow' };
  sortChoice = () => ({ choice: 0, newFolder: 'Wiese im Mai', parent: 0, confidence: 0.99, reason: 'per photo' });
  const before = sortCalls();
  await requestFolders([loose.id, kept.id]);
  const listed = await moves();
  assert.deepEqual(listed.map((item) => [item.nodeId, item.to.id]), [[loose.id, year.id]]);
  assert.equal(sortCalls(), before, 'no model call for photos');
  assert.equal(foldersNamed('Wiese im Mai').length, 0);
  await rejectAll();
});

test('a file in a folder the person made is not moved out of it', async () => {
  const project = await folder('root', 'Projekt X');
  const file = await put('v1.txt', 'Telekom Rechnung 3', project.id);
  extraction = { ...POLICY, type: 'invoice', issuer: 'Telekom' };
  await features({ folders: true, names: false, tags: false, newFolders: false });
  let offered = null;
  sortChoice = (text) => {
    offered = text;
    return { choice: 0, confidence: 0.9, reason: 'keep' };
  };
  await requestFolders([file.id]);
  assert.equal((await moves()).length, 0);
  // Only its own subtree could be offered; with no subfolders the model isn't asked.
  assert.equal(offered, null);
});

test('empty folders AI made, and recent empty top-level folders, can be listed and removed', async () => {
  await features({ folders: true, names: false, tags: false, newFolders: true });
  extraction = POLICY;
  sortChoice = () => ({ choice: 0, newFolder: 'Verträge', parent: 0, confidence: 0.8, reason: 'contracts' });
  const files = [await put('w1.txt', 'Vertrag 1'), await put('w2.txt', 'Vertrag 2'), await put('w3.txt', 'Vertrag 3')];
  await requestFolders(files.map((file) => file.id));
  const [item] = await moves();
  await member.agent.post(`/api/ai/suggestions/${item.id}/accept`).send({}).expect(200);
  const [aiFolder] = foldersNamed('Verträge');
  // The person moves the file out again by hand: the AI folder is left empty.
  await member.agent.patch(`/api/nodes/${item.nodeId}`).send({ parentId: files[0].parentId }).expect(200);
  const manual = await folder('root', 'Leer');

  const listed = (await member.agent.get('/api/ai/cleanup/folders').expect(200)).body;
  assert.deepEqual(listed.aiCreated.map((entry) => [entry.id, entry.name, entry.path]), [[aiFolder.id, 'Verträge', '/Verträge']]);
  const recent = listed.recent.map((entry) => entry.id);
  assert.ok(recent.includes(manual.id));
  assert.ok(!recent.includes(aiFolder.id));
  const fotos = foldersNamed('Fotos')[0];
  assert.ok(!recent.includes(fotos.id), 'a folder with something in it is never listed');

  const result = (await member.agent.post('/api/ai/cleanup/folders').send({ ids: [aiFolder.id, manual.id, fotos.id] }).expect(200)).body;
  assert.equal(result.removed, 2);
  assert.deepEqual(result.failed, [{ id: fotos.id, code: 'NOT_EMPTY_OR_NOT_ALLOWED' }]);
  assert.equal(foldersNamed('Verträge').length, 0);
  assert.equal(foldersNamed('Fotos').length, 1);
  await member.agent.post('/api/ai/cleanup/folders').send({ ids: [] }).expect(400);
  await rejectAll();
});

test('file types left out of AI are never queued, read or summarized', async () => {
  const status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.ok(status.fileTypes.find((category) => category.id === 'images').extensions.includes('heic'));
  await member.agent.put('/api/settings').send({ ai_skip_types: { categories: ['nope'] } }).expect(400);
  await member.agent.put('/api/settings').send({ ai_skip_types: { extensions: ['TXT'] } }).expect(400);
  await member.agent.put('/api/settings').send({ ai_skip_types: { categories: ['images'], extensions: ['md'] } }).expect(200);
  const box = await folder('root', 'Kiste');
  const note = await put('note.md', '# Notes', box.id);
  const text = await put('scan0001.txt', 'Allianz Brief', box.id);
  const png = await put('IMG_0009.png', await sharp({ create: { width: 4, height: 4, channels: 3, background: '#000' } }).png().toBuffer(), box.id);
  await member.agent.post('/api/ai/jobs').send({ folderId: box.id, kinds: ['folders'] }).expect(202);
  const queued = db.prepare('SELECT node_id FROM ai_jobs WHERE user_id = ? AND status = ?').all(member.user.id, 'pending').map((row) => row.node_id);
  assert.ok(queued.includes(text.id));
  assert.ok(!queued.includes(note.id));
  assert.ok(!queued.includes(png.id));
  await runAiJobsUntilIdle();
  const summary = await member.agent.post(`/api/ai/files/${note.id}/summary`).send({}).expect(422);
  assert.equal(summary.body.code, 'AI_TYPE_SKIPPED');
  // Asked for by name, it is skipped all the same.
  await member.agent.post('/api/ai/jobs').send({ nodeIds: [png.id], kinds: ['folders'] }).expect(202);
  await runAiJobsUntilIdle();
  const job = db.prepare('SELECT status, error_code FROM ai_jobs WHERE node_id = ? ORDER BY id DESC').get(png.id);
  assert.deepEqual({ ...job }, { status: 'skipped', error_code: 'AI_TYPE_SKIPPED' });
});
