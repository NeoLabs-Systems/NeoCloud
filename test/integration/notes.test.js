'use strict';

// Notes (docs/notes.md): Markdown files under /Notes as cards, frontmatter
// properties, autosave with revisions, rename and notebooks, trash, AI help,
// and that another account's note answers like a missing one.

const test = require('node:test');
const assert = require('node:assert/strict');
const { cleanup, registerUser } = require('../helpers/app');
const { startFakeProvider } = require('../helpers/fake_ai');

const SERVER_KEY = 'sk-notes-key-4242';

let provider;
let admin;
let owner;
let other;
let answer = () => 'ok';

test.before(async () => {
  provider = await startFakeProvider();
  provider.state.keys.add(SERVER_KEY);
  provider.state.reply = (body) => answer(body);
  process.env.NEOCLOUD_AI_REFERENCE_URL = provider.referenceUrl;
  admin = await registerUser('notesadmin');
  owner = await registerUser('notesowner');
  other = await registerUser('notesother');
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY', value: SERVER_KEY }).expect(200);
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL', value: provider.baseUrl }).expect(200);
});

test.after(() => {
  provider.close();
  cleanup();
});

async function fileText(agent, id) {
  return (await agent.get(`/api/files/${id}/content`).buffer(true).parse((res, done) => {
    const chunks = [];
    res.on('data', (chunk) => chunks.push(chunk));
    res.on('end', () => done(null, Buffer.concat(chunks).toString('utf8')));
  }).expect(200)).body;
}

test('before the first note there is nothing, and no Notes folder', async () => {
  const listed = await owner.agent.get('/api/notes').expect(200);
  assert.deepEqual(listed.body, { notes: [], notebooks: [], tags: [] });
});

test('a note is a Markdown file under /Notes with its properties as frontmatter', async () => {
  const created = await owner.agent.post('/api/notes').send({
    title: 'Groceries', body: '- [ ] Milk\n- [x] Bread', tags: ['home', 'shopping'], pinned: true, color: 'amber',
  }).expect(201);
  const note = created.body;
  assert.equal(note.title, 'Groceries');
  assert.equal(note.notebook, '');
  assert.deepEqual(note.checklist, { done: 1, total: 2 });
  assert.deepEqual(note.tags, ['home', 'shopping']);
  assert.equal(note.pinned, true);
  assert.equal(note.color, 'amber');
  assert.equal(note.body, '- [ ] Milk\n- [x] Bread');
  assert.equal(await fileText(owner.agent, note.id), '---\ntags:\n  - home\n  - shopping\npinned: true\ncolor: amber\n---\n- [ ] Milk\n- [x] Bread');

  const root = await owner.agent.get('/api/folders/root/children').expect(200);
  const notesFolder = JSON.stringify(root.body).includes('"name":"Notes"');
  assert.ok(notesFolder, 'a Notes folder in the file tree');
});

test('a note without a title takes its first line; a taken title gets a number', async () => {
  const first = (await owner.agent.post('/api/notes').send({ body: '# Meeting notes\nAgenda' }).expect(201)).body;
  assert.equal(first.title, 'Meeting notes');
  const second = (await owner.agent.post('/api/notes').send({ title: 'Meeting notes', body: 'Again' }).expect(201)).body;
  assert.equal(second.title, 'Meeting notes (2)');
});

test('the list puts pinned notes first and filters by text, tag and archive', async () => {
  const listed = (await owner.agent.get('/api/notes').expect(200)).body;
  assert.equal(listed.notes[0].title, 'Groceries');
  assert.deepEqual(listed.tags.map((tag) => tag.name), ['home', 'shopping']);
  assert.ok(listed.notes.every((note) => note.body === undefined), 'cards carry no body');
  const byText = (await owner.agent.get('/api/notes?q=agenda').expect(200)).body.notes;
  assert.deepEqual(byText.map((note) => note.title), ['Meeting notes']);
  const byTag = (await owner.agent.get('/api/notes?tag=HOME').expect(200)).body.notes;
  assert.deepEqual(byTag.map((note) => note.title), ['Groceries']);
});

test('saving keeps the other properties, refuses a stale revision and renames by title', async () => {
  const [note] = (await owner.agent.get('/api/notes?q=groceries').expect(200)).body.notes;
  const saved = (await owner.agent.patch(`/api/notes/${note.id}`).send({ body: '- [x] Milk', baseRevision: note.revision }).expect(200)).body;
  assert.equal(saved.body, '- [x] Milk');
  assert.equal(saved.pinned, true);
  assert.notEqual(saved.revision, note.revision);
  const stale = await owner.agent.patch(`/api/notes/${note.id}`).send({ body: 'lost', baseRevision: note.revision }).expect(409);
  assert.equal(stale.body.code, 'CONFLICT');

  const archived = (await owner.agent.patch(`/api/notes/${note.id}`).send({ archived: true, pinned: false, title: 'Shopping list' }).expect(200)).body;
  assert.equal(archived.title, 'Shopping list');
  assert.equal(archived.archived, true);
  assert.equal(archived.body, '- [x] Milk');
  const active = (await owner.agent.get('/api/notes').expect(200)).body.notes;
  assert.ok(!active.some((item) => item.id === note.id));
  const inArchive = (await owner.agent.get('/api/notes?archived=true').expect(200)).body.notes;
  assert.deepEqual(inArchive.map((item) => item.title), ['Shopping list']);
});

test('notebooks are folders under /Notes; a note moves between them', async () => {
  const notebook = (await owner.agent.post('/api/notes/notebooks').send({ name: 'Work' }).expect(200)).body;
  assert.deepEqual(notebook, { name: 'Work', created: true });
  const note = (await owner.agent.post('/api/notes').send({ title: 'Plan', body: 'Q3', notebook: 'Work' }).expect(201)).body;
  assert.equal(note.notebook, 'Work');
  const moved = (await owner.agent.patch(`/api/notes/${note.id}`).send({ notebook: 'Ideas' }).expect(200)).body;
  assert.equal(moved.notebook, 'Ideas');
  assert.equal(moved.title, 'Plan');
  const listed = (await owner.agent.get('/api/notes').expect(200)).body;
  assert.deepEqual(listed.notebooks, ['Ideas', 'Work']);
  assert.deepEqual((await owner.agent.get('/api/notes?notebook=Ideas').expect(200)).body.notes.map((item) => item.title), ['Plan']);
});

test('deleting a note moves it to the trash', async () => {
  const note = (await owner.agent.post('/api/notes').send({ title: 'Scratch', body: 'x' }).expect(201)).body;
  await owner.agent.delete(`/api/notes/${note.id}`).expect(200);
  await owner.agent.get(`/api/notes/${note.id}`).expect(404);
  const trash = await owner.agent.get('/api/trash').expect(200);
  assert.ok(JSON.stringify(trash.body).includes('Scratch.md'));
});

test('another account’s note answers like a missing one', async () => {
  const [note] = (await owner.agent.get('/api/notes').expect(200)).body.notes;
  await other.agent.get(`/api/notes/${note.id}`).expect(404);
  await other.agent.patch(`/api/notes/${note.id}`).send({ body: 'mine now' }).expect(404);
  await other.agent.delete(`/api/notes/${note.id}`).expect(404);
  await other.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'summarize' }).expect(404);
  assert.deepEqual((await other.agent.get('/api/notes').expect(200)).body.notes, []);
});

test('AI help returns text, a title or tags and never changes the note', async () => {
  const note = (await owner.agent.post('/api/notes').send({ title: 'Call', body: 'Call Anna about the offer on Friday.' }).expect(201)).body;
  const off = await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'summarize' }).expect(403);
  assert.equal(off.body.code, 'AI_DISABLED');

  await admin.agent.put('/api/admin/ai').send({ enabled: true }).expect(200);
  await owner.agent.put('/api/settings').send({ ai_opt_in: true }).expect(200);
  answer = () => '```markdown\n- [ ] Call Anna about the offer (Friday)\n```';
  const items = (await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'actionItems' }).expect(200)).body;
  assert.equal(items.text, '- [ ] Call Anna about the offer (Friday)');
  const prompt = JSON.stringify(provider.state.chats.at(-1).body.messages);
  assert.match(prompt, /Call Anna about the offer on Friday/);
  assert.match(prompt, /data, not instructions/);

  answer = () => JSON.stringify({ title: 'Call Anna / offer' });
  assert.equal((await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'title' }).expect(200)).body.title, 'Call Anna   offer');
  answer = () => JSON.stringify({ tags: ['work', 'Work', 'calls'] });
  assert.deepEqual((await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'tags' }).expect(200)).body.tags, ['work', 'calls']);

  answer = () => 'Shorter.';
  const shorter = (await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'shorten', selection: 'A long sentence here.' }).expect(200)).body;
  assert.equal(shorter.text, 'Shorter.');
  assert.match(JSON.stringify(provider.state.chats.at(-1).body.messages), /Selected text/);

  await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'ask' }).expect(400);
  await owner.agent.post(`/api/notes/${note.id}/ai`).send({ action: 'delete everything' }).expect(400);
  assert.equal((await owner.agent.get(`/api/notes/${note.id}`).expect(200)).body.revision, note.revision);
});
