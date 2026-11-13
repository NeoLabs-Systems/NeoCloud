'use strict';

// AI phase 2 (docs/ai.md): jobs on upload and on request, the extraction →
// rename suggestion, accept / reject / undo with conflicts, auto-sort (rules,
// then embeddings, then the model), rule proposals, tags in nodes and search,
// summaries per etag, auto-apply, the budget pause, AI off, and the
// ai:suggestions event. The provider is a fake on loopback.

const test = require('node:test');
const assert = require('node:assert/strict');
const sharp = require('sharp');
const { cleanup, registerUser, upload } = require('../helpers/app');
const { startFakeProvider } = require('../helpers/fake_ai');
const db = require('../../server/db/database');
const { attachSocketServer } = require('../../server/services/events');
const { runAiJobsUntilIdle } = require('../../server/services/ai/organize/worker');

const SERVER_KEY = 'sk-organize-key-5555';

let provider;
let admin;
let member;
const events = [];

// What the fake model answers: the extraction for the file, the sorting
// pick, or a summary.
let extraction = {};
let sortChoice = () => ({ choice: 0, reason: 'none fits', confidence: 0.3 });

function promptText(body) {
  return body.messages.map((message) => (typeof message.content === 'string'
    ? message.content
    : message.content.map((part) => part.text || '').join(' '))).join('\n');
}

function kindOf(body) {
  const text = promptText(body);
  if (text.includes('report facts about it')) return 'extract';
  if (text.includes('You decide where one file belongs')) return 'sort';
  if (text.includes('Summarize this file')) return 'summary';
  return 'other';
}

function reply(body) {
  const kind = kindOf(body);
  if (kind === 'extract') return JSON.stringify(typeof extraction === 'function' ? extraction(body) : extraction);
  // The sorting call ends with the file and its folders, after the few-shot examples.
  if (kind === 'sort') return JSON.stringify(sortChoice(String(body.messages.at(-1).content)));
  if (kind === 'summary') return `Summary ${provider.state.chats.length}`;
  return undefined;
}

function calls(kind) {
  return provider.state.chats.filter((chat) => kindOf(chat.body) === kind).length;
}

const TELEKOM = {
  date: '2026-03-12',
  issuer: 'Telekom',
  subject: 'Mobilfunk März',
  type: 'invoice',
  amount: 39.99,
  currency: 'EUR',
  tags: ['telefon', 'rechnung'],
  language: 'de',
  summary: 'Mobile phone bill for March.',
  caption: null,
  confidence: { date: 0.95, issuer: 0.95, subject: 0.9, type: 0.95, amount: 0.9, tags: 0.9 },
  overall: 0.93,
};
const TELEKOM_TEXT = 'Telekom Deutschland GmbH\nRechnung vom 12.03.2026\nMobilfunk März\nBetrag 39,99 EUR';

async function settings(agent, patch) {
  await agent.put('/api/settings').send(patch).expect(200);
}

async function features(flags) {
  await settings(member.agent, { ai_features: flags });
}

async function folder(agent, parentId, name) {
  return (await agent.post('/api/folders').send({ parentId, name }).expect(201)).body;
}

async function put(agent, name, content, parentId = 'root', conflict) {
  return (await upload(agent, name, content, { parentId, conflict }).expect(201)).body;
}

async function suggestionsOf(agent, query = '') {
  return (await agent.get(`/api/ai/suggestions${query}`).expect(200)).body;
}

async function request(agent, body) {
  return (await agent.post('/api/ai/jobs').send(body).expect(202)).body;
}

async function node(agent, id) {
  return (await agent.get(`/api/nodes/${id}`).expect(200)).body.node;
}

function jobsOf(userId) {
  return db.prepare('SELECT * FROM ai_jobs WHERE user_id = ? ORDER BY id').all(userId);
}

test.before(async () => {
  provider = await startFakeProvider();
  provider.state.keys.add(SERVER_KEY);
  provider.state.reply = reply;
  process.env.NEOCLOUD_AI_REFERENCE_URL = provider.referenceUrl;
  attachSocketServer({ to: (room) => ({ emit: (event, payload) => events.push({ room, event, payload }) }) });
  admin = await registerUser('orgadmin');
  member = await registerUser('orgmember');
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY', value: SERVER_KEY }).expect(200);
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL', value: provider.baseUrl }).expect(200);
  // The worker starts following the journal from here.
  await runAiJobsUntilIdle();
});

test.after(() => {
  attachSocketServer(null);
  provider.close();
  cleanup();
});

test('with AI off nothing is queued, even with "on upload" on', async () => {
  await settings(member.agent, { ai_opt_in: true, ai_features: { onUpload: true } });
  await put(member.agent, 'scan0001.txt', TELEKOM_TEXT);
  await runAiJobsUntilIdle();
  assert.equal(jobsOf(member.user.id).length, 0);
  const refused = await member.agent.post('/api/ai/jobs').send({ folderId: 'root' }).expect(403);
  assert.equal(refused.body.code, 'AI_DISABLED');
  assert.equal(provider.state.chats.length, 0);
  await admin.agent.put('/api/admin/ai').send({ enabled: true }).expect(200);
  // Not opted in: refused as well.
  await settings(member.agent, { ai_opt_in: false });
  assert.equal((await member.agent.post('/api/ai/jobs').send({ folderId: 'root' }).expect(403)).body.code, 'AI_NOT_OPTED_IN');
  await settings(member.agent, { ai_opt_in: true });
});

test('an upload with a meaningless name becomes a rename suggestion; a good name is left alone', async () => {
  await features({ onUpload: true, tags: false, folders: false });
  extraction = TELEKOM;
  events.length = 0;
  const scan = await put(member.agent, 'scan0002.txt', TELEKOM_TEXT);
  const good = await put(member.agent, 'Holiday plan.txt', 'Packing list for the holiday');
  await runAiJobsUntilIdle();

  const jobs = jobsOf(member.user.id);
  assert.deepEqual(jobs.map((job) => [job.node_id, job.source, job.status]), [[scan.id, 'upload', 'done'], [good.id, 'upload', 'done']]);
  // One extraction call, for the scan only.
  assert.equal(calls('extract'), 1);
  const listed = await suggestionsOf(member.agent);
  assert.equal(listed.total, 1);
  const [suggestion] = listed.items;
  assert.equal(suggestion.kind, 'rename');
  assert.equal(suggestion.from, 'scan0002.txt');
  assert.equal(suggestion.to, '2026-03-12 Telekom – Mobilfunk März.txt');
  assert.equal(suggestion.tier, 'high');
  assert.equal(suggestion.status, 'pending');
  assert.equal(suggestion.model, 'openai-compatible::fake-chat');
  assert.equal(suggestion.node.id, scan.id);
  assert.match(suggestion.reason, /invoice from Telekom, date from content/);
  // The extracted text reached the model, the metadata is on the node.
  assert.ok(provider.state.chats.at(-1).body.messages.some((message) => String(message.content).includes('Rechnung vom 12.03.2026')));
  const scanNode = await node(member.agent, scan.id);
  assert.equal(scanNode.aiMeta.issuer, 'Telekom');
  assert.equal(scanNode.aiMeta.dateSource, 'content');
  assert.equal(scanNode.aiMeta.model, 'openai-compatible::fake-chat');
  assert.deepEqual(scanNode.aiTags, []);
  // Metadata only in the usage log, under the feature's name.
  const usage = db.prepare("SELECT feature FROM ai_usage WHERE user_id = ? AND feature = 'title'").all(member.user.id);
  assert.equal(usage.length, 1);
  // Open apps were told.
  const told = events.filter((entry) => entry.event === 'ai:suggestions' && entry.room === `user:${member.user.id}`);
  assert.ok(told.length > 0);
  assert.deepEqual(told.at(-1).payload.pending, { rename: 1, move: 0, tags: 0, total: 1 });
  const counts = (await member.agent.get('/api/ai/suggestions/counts').expect(200)).body;
  assert.equal(counts.pending.total, 1);
  assert.equal(counts.high, 1);
});

test('accept renames through the file operations, undo puts the name back, conflicts answer 409', async () => {
  const { items: [suggestion] } = await suggestionsOf(member.agent);
  const cursor = db.prepare('SELECT MAX(seq) AS seq FROM changes').get().seq;
  const accepted = (await member.agent.post(`/api/ai/suggestions/${suggestion.id}/accept`).send({}).expect(200)).body.suggestion;
  assert.equal(accepted.status, 'accepted');
  assert.equal(accepted.node.name, '2026-03-12 Telekom – Mobilfunk März.txt');
  assert.equal(db.prepare("SELECT COUNT(*) AS count FROM changes WHERE seq > ? AND op = 'rename'").get(cursor).count, 1);
  await member.agent.post(`/api/ai/suggestions/${suggestion.id}/accept`).send({}).expect(409);

  const undone = (await member.agent.post(`/api/ai/suggestions/${suggestion.id}/undo`).expect(200)).body.suggestion;
  assert.equal(undone.status, 'undone');
  assert.equal(undone.node.name, 'scan0002.txt');
  await member.agent.post(`/api/ai/suggestions/${suggestion.id}/undo`).expect(409);

  // Accepted with an edited name, then renamed by hand: undo can't put it back.
  await request(member.agent, { nodeIds: [suggestion.nodeId], kinds: ['names'] });
  await runAiJobsUntilIdle();
  const { items: [again] } = await suggestionsOf(member.agent);
  const edited = (await member.agent.post(`/api/ai/suggestions/${again.id}/accept`).send({ value: 'Telekom März.txt' }).expect(200)).body.suggestion;
  assert.equal(edited.to, 'Telekom März.txt');
  assert.equal(edited.node.name, 'Telekom März.txt');
  await member.agent.patch(`/api/nodes/${again.nodeId}`).send({ name: 'Mine.txt' }).expect(200);
  assert.equal((await member.agent.post(`/api/ai/suggestions/${again.id}/undo`).expect(409)).body.code, 'AI_UNDO_CONFLICT');

  // A suggestion whose file changed turns stale; accepting it answers 409.
  await request(member.agent, { nodeIds: [again.nodeId], kinds: ['names'] });
  await runAiJobsUntilIdle();
  const { items: [third] } = await suggestionsOf(member.agent);
  await put(member.agent, 'Mine.txt', `${TELEKOM_TEXT}\nnew page`, 'root', 'replace');
  assert.equal((await member.agent.post(`/api/ai/suggestions/${third.id}/accept`).send({}).expect(409)).body.code, 'AI_SUGGESTION_STALE');
  assert.equal((await suggestionsOf(member.agent, '?status=stale')).items[0].id, third.id);

  // Reject.
  await request(member.agent, { nodeIds: [again.nodeId], kinds: ['names'] });
  await runAiJobsUntilIdle();
  const { items: [fourth] } = await suggestionsOf(member.agent);
  assert.equal((await member.agent.post(`/api/ai/suggestions/${fourth.id}/reject`).expect(200)).body.suggestion.status, 'rejected');
  assert.equal((await suggestionsOf(member.agent)).total, 0);
  assert.equal((await node(member.agent, again.nodeId)).name, 'Mine.txt');
});

test('low confidence is stored but hidden; accept-all-high takes only high ones', async () => {
  const low = await put(member.agent, 'download (1).txt', 'Some letter text');
  const high = await put(member.agent, 'download (2).txt', TELEKOM_TEXT);
  extraction = (body) => (promptText(body).includes('Some letter text')
    ? { ...TELEKOM, issuer: 'Somebody', subject: 'Letter', overall: 0.3 }
    : TELEKOM);
  await request(member.agent, { nodeIds: [low.id, high.id], kinds: ['names'] });
  await runAiJobsUntilIdle();
  const visible = await suggestionsOf(member.agent);
  assert.deepEqual(visible.items.map((item) => item.nodeId), [high.id]);
  assert.equal(visible.counts.hiddenLow, 1);
  const all = await suggestionsOf(member.agent, '?tier=all');
  assert.equal(all.total, 2);
  assert.equal(all.items.find((item) => item.nodeId === low.id).tier, 'low');
  const result = (await member.agent.post('/api/ai/suggestions/accept-high').send({}).expect(200)).body;
  assert.deepEqual(result, { accepted: 1, failed: [] });
  // The collision with the earlier rename was resolved in the folder.
  assert.equal((await node(member.agent, high.id)).name, '2026-03-12 Telekom – Mobilfunk März.txt');
  assert.equal((await node(member.agent, low.id)).name, 'download (1).txt');
  extraction = TELEKOM;
});

test('camera photos get tags but no new name unless asked; images go to the vision model', async () => {
  await features({ tags: true, folders: false });
  const png = await sharp({ create: { width: 8, height: 8, channels: 3, background: '#336699' } }).png().toBuffer();
  const photo = await put(member.agent, 'IMG_2041.png', png);
  extraction = { ...TELEKOM, type: 'photo', subject: 'Blue square', tags: ['blau'] };
  const before = provider.state.chats.length;
  await request(member.agent, { folderId: 'root', kinds: ['names', 'tags'] });
  await runAiJobsUntilIdle();
  const visionCall = provider.state.chats.slice(before).find((chat) => chat.body.model === 'fake-vision');
  assert.ok(visionCall, 'the image went to the vision model');
  const parts = visionCall.body.messages.at(-1).content;
  assert.ok(parts.some((part) => part.type === 'image_url' && part.image_url.url.startsWith('data:image/jpeg;base64,')));
  const forPhoto = (await suggestionsOf(member.agent, `?nodeId=${photo.id}`)).items;
  // No EXIF camera here, so the meaningless name is renamed; tags suggested too.
  assert.deepEqual(forPhoto.map((item) => item.kind).sort(), ['rename', 'tags']);
  const tags = forPhoto.find((item) => item.kind === 'tags');
  assert.deepEqual(tags.from, []);
  assert.deepEqual(tags.to, ['blau']);
  await member.agent.post(`/api/ai/suggestions/${tags.id}/accept`).send({ value: ['blau', 'Quadrat!'] }).expect(200);
  const tagged = await node(member.agent, photo.id);
  assert.deepEqual(tagged.aiTags, ['blau', 'quadrat']);
  // Tags are searchable like names.
  const found = (await member.agent.get('/api/search?q=quadrat').expect(200)).body.items;
  assert.deepEqual(found.map((item) => item.id), [photo.id]);
  await member.agent.post(`/api/ai/suggestions/${forPhoto.find((item) => item.kind === 'rename').id}/reject`).expect(200);
  await member.agent.post('/api/ai/suggestions/accept-high').send({}).expect(200);
});

test('auto-sort: a rule beats embeddings, a clear embedding beats the model, and the model only sees allowed folders', async () => {
  await features({ folders: true, names: false, tags: false, newFolders: false });
  const finance = await folder(member.agent, 'root', 'Finance');
  const telekom = await folder(member.agent, finance.id, 'Telekom');
  await folder(member.agent, 'root', 'Photos');
  const excluded = await folder(member.agent, 'root', 'Private');
  await member.agent.put(`/api/ai/folders/${excluded.id}/excluded`).send({ excluded: true }).expect(200);
  const shared = await folder(member.agent, 'root', 'Team');
  await member.agent.post(`/api/nodes/${shared.id}/members`).send({ recipient: admin.username, permission: 'write' }).expect(201);
  await member.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Tresor',
    salt: Buffer.alloc(16, 1).toString('base64'),
    iterations: 600000,
    wrappedKey: Buffer.alloc(60, 2).toString('base64'),
  }).expect(201);
  const bill = await put(member.agent, 'bill.txt', TELEKOM_TEXT);
  extraction = TELEKOM;

  // 1. A rule: no embedding or model call for the decision.
  const rule = (await member.agent.post('/api/ai/rules').send({
    name: 'Telekom bills',
    match: { issuer: 'telekom', type: 'invoice' },
    targetFolderId: telekom.id,
  }).expect(201)).body.rule;
  assert.equal(rule.targetPath, '/Finance/Telekom');
  const embedsBefore = provider.state.embeddings.length;
  const sortsBefore = calls('sort');
  await request(member.agent, { nodeIds: [bill.id], kinds: ['folders'] });
  await runAiJobsUntilIdle();
  let [move] = (await suggestionsOf(member.agent, '?kind=move')).items;
  assert.equal(move.source, 'rule');
  assert.equal(move.reason, 'Rule: Telekom bills');
  assert.deepEqual(move.to, { id: telekom.id, path: '/Finance/Telekom' });
  assert.deepEqual(move.from, { id: bill.parentId, path: '/' });
  assert.equal(move.tier, 'high');
  assert.equal(provider.state.embeddings.length, embedsBefore);
  assert.equal(calls('sort'), sortsBefore);
  await member.agent.post(`/api/ai/suggestions/${move.id}/reject`).expect(200);
  await member.agent.delete(`/api/ai/rules/${rule.id}`).expect(200);

  // 2. A clear embedding match decides without the model.
  provider.state.vector = (text) => {
    if (text.startsWith('Folder /Finance/Telekom')) return [1, 0, 0];
    if (text.startsWith('Folder ')) return [0, 1, 0];
    return [1, 0.05, 0];
  };
  await request(member.agent, { nodeIds: [bill.id], kinds: ['folders'] });
  await runAiJobsUntilIdle();
  [move] = (await suggestionsOf(member.agent, '?kind=move')).items;
  assert.equal(move.source, 'ai');
  assert.equal(move.to.id, telekom.id);
  assert.equal(move.model, 'openai-compatible::fake-embed');
  assert.equal(calls('sort'), sortsBefore);
  const profiled = provider.state.embeddings.flatMap((call) => call.body.input).filter((text) => text.startsWith('Folder '));
  assert.ok(profiled.every((text) => !/Private|Tresor|Team/.test(text.split('\n')[0])), 'no excluded, vault or shared folder');
  await member.agent.post(`/api/ai/suggestions/${move.id}/reject`).expect(200);

  // 3. No clear match: the model picks among the candidates. Folder profiles
  // are cached; only the file's vector is new.
  provider.state.vector = () => [0.7, 0.7, 0];
  let offered = '';
  sortChoice = (text) => {
    offered = text;
    const line = text.split('\n').find((entry) => entry.endsWith('/Finance/Telekom'));
    return { choice: Number(line.split('.')[0]), reason: 'Phone bills live here', confidence: 0.7 };
  };
  await request(member.agent, { nodeIds: [bill.id], kinds: ['folders'] });
  await runAiJobsUntilIdle();
  [move] = (await suggestionsOf(member.agent, '?kind=move')).items;
  assert.equal(calls('sort'), sortsBefore + 1);
  assert.equal(move.to.id, telekom.id);
  assert.equal(move.tier, 'medium');
  assert.equal(move.reason, 'Phone bills live here');
  assert.doesNotMatch(offered, /Private|Tresor|Team/);
  // A document is never offered the photo tree.
  assert.doesNotMatch(offered, /\/Photos/);
  assert.match(offered, /^\d+\. \/Finance$/m);
  // Accepting into a vault or across a share border is refused.
  const vaultFolder = db.prepare("SELECT id FROM nodes WHERE user_id = ? AND name = 'Tresor'").get(member.user.id).id;
  assert.equal((await member.agent.post(`/api/ai/suggestions/${move.id}/accept`).send({ value: vaultFolder }).expect(409)).body.code, 'AI_TARGET_NOT_ALLOWED');
  assert.equal((await member.agent.post(`/api/ai/suggestions/${move.id}/accept`).send({ value: excluded.id }).expect(409)).body.code, 'AI_TARGET_NOT_ALLOWED');
  assert.equal((await member.agent.post(`/api/ai/suggestions/${move.id}/accept`).send({ value: shared.id }).expect(409)).body.code, 'SHARE_BOUNDARY');
  const done = (await member.agent.post(`/api/ai/suggestions/${move.id}/accept`).send({}).expect(200)).body.suggestion;
  assert.equal(done.node.parentId, telekom.id);
  const back = (await member.agent.post(`/api/ai/suggestions/${move.id}/undo`).expect(200)).body.suggestion;
  assert.equal(back.node.parentId, bill.parentId);
  // A rule can't point into a vault or an excluded folder.
  await member.agent.post('/api/ai/rules').send({ name: 'x', match: { type: 'invoice' }, targetFolderId: vaultFolder }).expect(409);
  await member.agent.post('/api/ai/rules').send({ name: 'x', match: {}, targetFolderId: telekom.id }).expect(400);
});

test('three accepted moves of the same kind make a rule proposal', async () => {
  const telekom = db.prepare("SELECT id FROM nodes WHERE user_id = ? AND name = 'Telekom'").get(member.user.id).id;
  provider.state.vector = (text) => {
    if (text.startsWith('Folder /Finance/Telekom')) return [1, 0, 0];
    if (text.startsWith('Folder ')) return [0, 1, 0];
    return [1, 0.05, 0];
  };
  for (let index = 0; index < 3; index += 1) {
    const file = await put(member.agent, `bill-${index}.txt`, `${TELEKOM_TEXT}\n${index}`);
    await request(member.agent, { nodeIds: [file.id], kinds: ['folders'] });
    await runAiJobsUntilIdle();
    const moves = (await suggestionsOf(member.agent, '?kind=move')).items;
    assert.deepEqual(moves.map((item) => [item.nodeId, item.status]), [[file.id, 'pending']]);
    const [move] = moves;
    await member.agent.post(`/api/ai/suggestions/${move.id}/accept`).send({}).expect(200);
    const proposals = (await member.agent.get('/api/ai/rules/proposals').expect(200)).body.proposals;
    assert.equal(proposals.length, index === 2 ? 1 : 0);
  }
  assert.equal((await member.agent.get('/api/ai/suggestions/counts').expect(200)).body.proposals, 1);
  const [proposal] = (await member.agent.get('/api/ai/rules/proposals').expect(200)).body.proposals;
  assert.deepEqual(
    { type: proposal.type, issuer: proposal.issuer, targetFolderId: proposal.targetFolderId, accepts: proposal.accepts },
    { type: 'invoice', issuer: 'Telekom', targetFolderId: telekom, accepts: 3 },
  );
  const rule = (await member.agent.post(`/api/ai/rules/proposals/${proposal.id}`).send({}).expect(201)).body.rule;
  assert.deepEqual(rule.match, { type: 'invoice', issuer: 'Telekom', extension: null, namePattern: null });
  assert.equal(rule.targetFolderId, telekom);
  assert.equal((await member.agent.get('/api/ai/rules/proposals').expect(200)).body.proposals.length, 0);
  const updated = (await member.agent.patch(`/api/ai/rules/${rule.id}`).send({ enabled: false }).expect(200)).body.rule;
  assert.equal(updated.enabled, false);
  assert.equal((await member.agent.get('/api/ai/rules').expect(200)).body.rules.length, 1);
});

test('auto-apply applies high-confidence suggestions at once, still undoable', async () => {
  await features({ names: true, folders: false, tags: false, autoApplyHighConfidence: true });
  const file = await put(member.agent, 'document (7).txt', TELEKOM_TEXT.replace('März', 'April'));
  extraction = { ...TELEKOM, subject: 'Mobilfunk April' };
  await request(member.agent, { nodeIds: [file.id] });
  await runAiJobsUntilIdle();
  const [applied] = (await suggestionsOf(member.agent, '?status=applied')).items;
  assert.equal(applied.nodeId, file.id);
  assert.equal((await node(member.agent, file.id)).name, '2026-03-12 Telekom – Mobilfunk April.txt');
  await member.agent.post(`/api/ai/suggestions/${applied.id}/undo`).expect(200);
  assert.equal((await node(member.agent, file.id)).name, 'document (7).txt');
  await features({ names: true, folders: false, tags: false });
  extraction = TELEKOM;
});

test('summaries are cached per content and labelled with the model', async () => {
  const file = await put(member.agent, 'contract.txt', 'Mietvertrag zwischen A und B, Beginn 1.1.2026.');
  const first = (await member.agent.post(`/api/ai/files/${file.id}/summary`).send({}).expect(200)).body;
  assert.equal(first.cached, false);
  assert.match(first.summary, /^Summary/);
  assert.equal(first.model, 'openai-compatible::fake-big');
  assert.equal(first.etag, file.etag);
  const chats = provider.state.chats.length;
  const second = (await member.agent.post(`/api/ai/files/${file.id}/summary`).send({}).expect(200)).body;
  assert.equal(second.cached, true);
  assert.equal(second.summary, first.summary);
  assert.equal(provider.state.chats.length, chats);
  const changed = await put(member.agent, 'contract.txt', 'Mietvertrag, geändert.', 'root', 'replace');
  const third = (await member.agent.post(`/api/ai/files/${file.id}/summary`).send({}).expect(200)).body;
  assert.equal(third.cached, false);
  assert.equal(third.etag, changed.etag);
  assert.equal((await member.agent.post(`/api/ai/files/${file.id}/summary`).send({ refresh: true }).expect(200)).body.cached, false);
  // Stored encrypted, never as text.
  const row = db.prepare('SELECT summary_enc FROM ai_summaries WHERE node_id = ?').get(file.id);
  assert.ok(!row.summary_enc.toString('utf8').includes('Summary'));
  assert.equal(db.prepare("SELECT COUNT(*) AS count FROM ai_usage WHERE user_id = ? AND feature = 'summary'").get(member.user.id).count, 3);
  await features({ summaries: false });
  assert.equal((await member.agent.post(`/api/ai/files/${file.id}/summary`).send({}).expect(403)).body.code, 'AI_FEATURE_OFF');
  await features({});
});

test('a used-up budget pauses the queue until there is budget again', async () => {
  await features({ names: true, tags: false, folders: false });
  await admin.agent.put(`/api/admin/users/${member.user.id}/ai-budget`).send({ monthlyTokens: 1 }).expect(200);
  const file = await put(member.agent, 'scan0099.txt', TELEKOM_TEXT);
  const { batchId, queued } = await request(member.agent, { nodeIds: [file.id] });
  assert.equal(queued, 1);
  const chats = provider.state.chats.length;
  await runAiJobsUntilIdle();
  assert.equal(provider.state.chats.length, chats);
  let status = (await member.agent.get('/api/ai/jobs').expect(200)).body;
  assert.equal(status.paused, 'AI_BUDGET_EXCEEDED');
  assert.equal(status.pending, 1);
  assert.equal(status.batches[0].id, batchId);
  assert.equal(status.batches[0].pending, 1);

  await admin.agent.put(`/api/admin/users/${member.user.id}/ai-budget`).send({ monthlyTokens: null }).expect(200);
  await runAiJobsUntilIdle();
  status = (await member.agent.get(`/api/ai/jobs?batchId=${batchId}`).expect(200)).body;
  assert.equal(status.paused, null);
  assert.deepEqual(status.batches.map((batch) => [batch.total, batch.done]), [[1, 1]]);
  await member.agent.get('/api/ai/jobs?batchId=nope').expect(404);
});

test('a provider failure retries later instead of failing the job', async () => {
  const file = await put(member.agent, 'scan0100.txt', 'Another bill text');
  provider.state.failNext = 10;
  await request(member.agent, { nodeIds: [file.id] });
  await runAiJobsUntilIdle();
  const [job] = jobsOf(member.user.id).filter((entry) => entry.node_id === file.id);
  assert.equal(job.status, 'pending');
  assert.equal(job.attempts, 1);
  assert.equal(job.error_code, 'AI_PROVIDER_ERROR');
  assert.ok(job.run_after > db.prepare("SELECT datetime('now') AS now").get().now);
  provider.state.failNext = 0;
});
