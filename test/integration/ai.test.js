'use strict';

// AI foundation (docs/ai.md): admin providers, the model catalog, visibility
// and grants, per-role model resolution, BYOK, the SSRF guard, NeoRouter's
// device sign-in, the facade with usage and budgets, the admin switch and
// which tokens reach these routes. Providers are fakes on loopback.

const test = require('node:test');
const assert = require('node:assert/strict');
const { app, cleanup, registerUser, request } = require('../helpers/app');
const { startFakeProvider, startFakeRouter } = require('../helpers/fake_ai');
const db = require('../../server/db/database');
const ai = require('../../server/services/ai/client');
const { createDeviceToken } = require('../../server/services/devices/tokens');

const SERVER_KEY = 'sk-server-secret-key-1234';
const MEMBER_KEY = 'sk-member-own-key-9876';

let provider;
let router;
let admin;
let member;
let outsider;

test.before(async () => {
  provider = await startFakeProvider();
  router = await startFakeRouter();
  provider.state.keys.add(SERVER_KEY);
  provider.state.keys.add(MEMBER_KEY);
  process.env.NEOCLOUD_AI_REFERENCE_URL = provider.referenceUrl;
  admin = await registerUser('aiadmin'); // the first account is the admin
  member = await registerUser('aimember');
  outsider = await registerUser('aioutsider');
});

test.after(() => {
  provider.close();
  router.close();
  cleanup();
});

function bearerFor(account, platform) {
  return `Bearer ${createDeviceToken(account.user.id, { name: `Client ${platform}`, platform }).token}`;
}

async function memberModels() {
  return (await member.agent.get('/api/settings/meta/models').expect(200)).body.models;
}

async function codeOf(promise) {
  try {
    await promise;
  } catch (error) {
    return error.code;
  }
  return null;
}

async function waitForSignIn(agent, id) {
  for (let i = 0; i < 40; i += 1) {
    const res = await agent.get(`/api/neorouter/connect/${id}`).expect(200);
    if (res.body.status !== 'pending') return res.body;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error('sign-in did not finish');
}

test('AI is off until the admin enables it, and status asks no provider then', async () => {
  const status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.enabled, false);
  assert.equal(status.available, false);
  assert.equal(status.optedIn, false);
  assert.deepEqual(Object.keys(status.roles), ['text', 'smart', 'vision', 'embedding', 'transcription']);
  assert.equal(status.roles.text.ready, false);
  assert.deepEqual(status.budget, { used: 0, limit: null });
  assert.deepEqual((await admin.agent.get('/api/admin/ai').expect(200)).body, { enabled: false, monthlyTokens: null });
});

test('admin providers are set in the environment and only ever shown masked', async () => {
  await member.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY', value: 'x' }).expect(403);
  const unknown = await admin.agent.put('/api/admin/providers').send({ key: 'PATH', value: 'x' }).expect(400);
  assert.equal(unknown.body.code, 'UNKNOWN_PROVIDER');
  const badUrl = await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OLLAMA_URL', value: 'ftp://x' }).expect(400);
  assert.equal(badUrl.body.code, 'INVALID_URL');

  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY', value: SERVER_KEY }).expect(200);
  // An admin's address may be on the LAN or loopback.
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL', value: provider.baseUrl }).expect(200);
  const listed = await admin.agent.get('/api/admin/providers').expect(200);
  assert.ok(!JSON.stringify(listed.body).includes(SERVER_KEY));
  const key = listed.body.providers.find((entry) => entry.key === 'NEOCLOUD_OPENAI_COMPATIBLE_API_KEY');
  assert.deepEqual(key, { key: key.key, label: key.label, type: 'key', configured: true, hint: 'sk-s••••1234' });
  const url = listed.body.providers.find((entry) => entry.key === 'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL');
  assert.equal(url.hint, provider.baseUrl);

  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_ANTHROPIC_API_KEY', value: 'sk-ant-temporary' }).expect(200);
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_ANTHROPIC_API_KEY', value: '' }).expect(200);
  const cleared = (await admin.agent.get('/api/admin/providers').expect(200)).body.providers;
  assert.equal(cleared.find((entry) => entry.key === 'NEOCLOUD_ANTHROPIC_API_KEY').configured, false);
});

test('the catalog lists every kind with provider-scoped ids and vision support', async () => {
  const res = await admin.agent.get('/api/admin/models').expect(200);
  const byId = Object.fromEntries(res.body.models.map((model) => [model.id, model]));
  assert.equal(byId['openai-compatible::fake-chat'].kind, 'chat');
  assert.equal(byId['openai-compatible::fake-chat'].modelId, 'fake-chat');
  assert.equal(byId['openai-compatible::fake-chat'].supportsVision, false);
  assert.equal(byId['openai-compatible::fake-chat'].priceTier, 'cheap');
  assert.equal(byId['openai-compatible::fake-vision'].supportsVision, true);
  assert.equal(byId['openai-compatible::fake-big'].priceTier, 'expensive');
  assert.equal(byId['openai-compatible::fake-embed'].kind, 'embedding');
  assert.deepEqual(res.body.disabledModels, []);
  assert.equal(res.body.autoEnableNewModels, true);
  await member.agent.get('/api/admin/models').expect(403);

  const providers = (await member.agent.get('/api/settings/meta/ai-providers').expect(200)).body.providers;
  const compatible = providers.find((entry) => entry.id === 'openai-compatible');
  assert.equal(compatible.available, true);
  assert.equal(compatible.modelCount, 4);
  // The operator's address is not shown to people.
  assert.equal(compatible.baseUrl, '');
  assert.equal(providers.find((entry) => entry.id === 'ollama').available, false);
});

test('disabled models are off for everyone except accounts they are granted to', async () => {
  const set = await admin.agent.put('/api/admin/models/config')
    .send({ disabledModels: ['openai-compatible::fake-big'], autoEnableNewModels: true }).expect(200);
  assert.deepEqual(set.body.disabledModels, ['openai-compatible::fake-big']);
  await admin.agent.put('/api/admin/models/config').send({ disabledModels: 'nope' }).expect(400);

  let big = (await memberModels()).find((model) => model.id === 'openai-compatible::fake-big');
  assert.equal(big.available, false);

  const unknown = await admin.agent.put(`/api/admin/users/${member.user.id}/allowed-models`).send({ modelIds: ['nope::nope'] }).expect(400);
  assert.equal(unknown.body.code, 'UNKNOWN_MODEL');
  await admin.agent.put(`/api/admin/users/${member.user.id}/allowed-models`).send({ modelIds: ['openai-compatible::fake-big'] }).expect(200);
  assert.deepEqual((await admin.agent.get(`/api/admin/users/${member.user.id}/allowed-models`).expect(200)).body,
    { modelIds: ['openai-compatible::fake-big'], monthlyTokens: null });
  big = (await memberModels()).find((model) => model.id === 'openai-compatible::fake-big');
  assert.equal(big.available, true);
  assert.equal(big.adminGranted, true);
  const forOutsider = (await outsider.agent.get('/api/settings/meta/models').expect(200)).body.models;
  assert.equal(forOutsider.find((model) => model.id === 'openai-compatible::fake-big').available, false);
});

test('roles resolve to the person’s choice, else the best fit, else AI_MODELS_UNAVAILABLE', async () => {
  await admin.agent.put('/api/admin/ai').send({ enabled: true }).expect(200);
  const invalid = await member.agent.put('/api/settings').send({ ai_model_text: 'not a model' }).expect(400);
  assert.equal(invalid.body.code, 'INVALID_SETTING_VALUE');
  await member.agent.put('/api/settings').send({ ai_features: { autoTitle: 'yes' } }).expect(400);
  await member.agent.put('/api/settings').send({ ai_naming_template: 'a/b' }).expect(400);
  const saved = await member.agent.put('/api/settings')
    .send({ ai_opt_in: true, ai_features: { autoTitle: true }, ai_naming_template: '{date} {subject}' }).expect(200);
  assert.equal(saved.body.settings.ai_opt_in, true);
  assert.equal(saved.body.settings.ai_model_text, 'auto');

  let status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.available, true);
  assert.equal(status.optedIn, true);
  assert.deepEqual(status.roles.text, { ready: true, model: 'openai-compatible::fake-chat', source: 'auto', requestedUnavailable: false });
  assert.equal(status.roles.smart.model, 'openai-compatible::fake-big'); // granted, and the quality pick
  assert.equal(status.roles.vision.model, 'openai-compatible::fake-vision');
  assert.equal(status.roles.embedding.model, 'openai-compatible::fake-embed');
  assert.equal(status.roles.transcription.ready, false);

  await member.agent.put('/api/settings').send({ ai_model_text: 'openai-compatible::fake-vision' }).expect(200);
  status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.deepEqual(status.roles.text, { ready: true, model: 'openai-compatible::fake-vision', source: 'user', requestedUnavailable: false });

  // Without the grant the smart choice falls back.
  await member.agent.put('/api/settings').send({ ai_model_smart: 'openai-compatible::fake-big' }).expect(200);
  await admin.agent.put(`/api/admin/users/${member.user.id}/allowed-models`).send({ modelIds: null }).expect(200);
  status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.roles.smart.model, 'openai-compatible::fake-vision');
  assert.equal(status.roles.smart.requestedUnavailable, true);

  // No vision model left: the vision role has nothing.
  await admin.agent.put('/api/admin/models/config')
    .send({ disabledModels: ['openai-compatible::fake-big', 'openai-compatible::fake-vision'] }).expect(200);
  status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.roles.vision.ready, false);
  const code = await codeOf(ai.vision({ userId: member.user.id, feature: 'test', image: { data: Buffer.from('png'), mimeType: 'image/png' } }));
  assert.equal(code, 'AI_MODELS_UNAVAILABLE');
  await admin.agent.put('/api/admin/models/config').send({ disabledModels: [] }).expect(200);
  await member.agent.put('/api/settings').send({ ai_model_text: 'auto', ai_model_smart: 'auto' }).expect(200);
});

test('the facade chats, answers JSON, reads images and embeds, recording usage without content', async () => {
  const userId = member.user.id;
  const secret = 'secret prompt words about the tax return';
  const text = await ai.chat({ userId, feature: 'test_chat', messages: [{ role: 'user', content: secret }] });
  assert.equal(text.content, 'Hello from fake-chat');
  assert.equal(text.model, 'openai-compatible::fake-chat');
  assert.equal(provider.state.chats.at(-1).key, SERVER_KEY);

  const json = await ai.chat({ userId, feature: 'test_json', role: 'smart', json: true, messages: [{ role: 'user', content: secret }] });
  assert.deepEqual(json.data, { title: 'Invoice March' });
  assert.equal(provider.state.chats.at(-1).body.response_format.type, 'json_object');
  assert.equal(provider.state.chats.at(-1).body.model, 'fake-big');

  const seen = await ai.vision({ userId, feature: 'test_vision', image: { data: Buffer.from('fake image'), mimeType: 'image/png' }, prompt: secret });
  assert.equal(seen.model, 'openai-compatible::fake-vision');
  const parts = provider.state.chats.at(-1).body.messages.at(-1).content;
  assert.equal(parts[1].type, 'image_url');
  assert.ok(parts[1].image_url.url.startsWith('data:image/png;base64,'));

  const embedded = await ai.embed({ userId, feature: 'test_embed', texts: [secret, 'second'] });
  assert.equal(embedded.vectors.length, 2);
  assert.equal(embedded.model, 'openai-compatible::fake-embed');

  // A transient failure is retried on the same model.
  provider.state.failNext = 1;
  const retried = await ai.chat({ userId, feature: 'test_retry', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(retried.content, 'Hello from fake-chat');

  const rows = db.prepare('SELECT * FROM ai_usage WHERE user_id = ? ORDER BY id').all(userId);
  assert.deepEqual(rows.map((row) => row.feature), ['test_chat', 'test_json', 'test_vision', 'test_embed', 'test_retry']);
  assert.equal(rows[0].total_tokens, 15);
  assert.equal(rows[3].total_tokens, 4);
  assert.ok(rows.every((row) => row.ok === 1 && row.provider === 'openai-compatible'));
  assert.ok(!JSON.stringify(rows).includes('secret prompt'));

  const status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.budget.used, 64);
  const usage = (await admin.agent.get('/api/admin/ai/usage').expect(200)).body;
  assert.equal(usage.users.find((entry) => entry.userId === userId).tokens, 64);
  assert.ok(usage.features.some((entry) => entry.feature === 'test_json'));
  await member.agent.get('/api/admin/ai/usage').expect(403);
});

test('a used-up monthly budget blocks further calls', async () => {
  const userId = member.user.id;
  await admin.agent.put('/api/admin/ai').send({ monthlyTokens: -5 }).expect(400);
  assert.equal((await admin.agent.put('/api/admin/ai').send({ monthlyTokens: 1000 }).expect(200)).body.monthlyTokens, 1000);
  assert.equal((await member.agent.get('/api/ai/status').expect(200)).body.budget.limit, 1000);

  await admin.agent.put(`/api/admin/users/${userId}/ai-budget`).send({ monthlyTokens: 50 }).expect(200);
  // The override shows in the grants answer and the admin's user list.
  assert.equal((await admin.agent.get(`/api/admin/users/${userId}/allowed-models`).expect(200)).body.monthlyTokens, 50);
  const listed = (await admin.agent.get('/api/admin/users').expect(200)).body.users;
  assert.equal(listed.find((entry) => entry.id === userId).aiMonthlyTokens, 50);
  assert.equal(listed.find((entry) => entry.id === outsider.user.id).aiMonthlyTokens, null);
  assert.equal(await codeOf(ai.chat({ userId, feature: 'test', messages: [{ role: 'user', content: 'hi' }] })), 'AI_BUDGET_EXCEEDED');
  // 0 exempts the account from the default; null follows it again.
  await admin.agent.put(`/api/admin/users/${userId}/ai-budget`).send({ monthlyTokens: 0 }).expect(200);
  assert.equal((await admin.agent.get(`/api/admin/users/${userId}/allowed-models`).expect(200)).body.monthlyTokens, 0);
  assert.equal((await member.agent.get('/api/ai/status').expect(200)).body.budget.limit, null);
  await admin.agent.put(`/api/admin/users/${userId}/ai-budget`).send({ monthlyTokens: null }).expect(200);
  assert.equal((await admin.agent.get(`/api/admin/users/${userId}/allowed-models`).expect(200)).body.monthlyTokens, null);
  await admin.agent.put('/api/admin/users/99999/ai-budget').send({ monthlyTokens: 5 }).expect(404);
  await admin.agent.put('/api/admin/ai').send({ monthlyTokens: null }).expect(200);
  await ai.chat({ userId, feature: 'test', messages: [{ role: 'user', content: 'hi' }] });
});

test('with AI off, or not opted in, nothing runs', async () => {
  assert.equal(await codeOf(ai.chat({ userId: outsider.user.id, feature: 'test', messages: [{ role: 'user', content: 'hi' }] })), 'AI_NOT_OPTED_IN');
  await admin.agent.put('/api/admin/ai').send({ enabled: 'yes' }).expect(400);
  await admin.agent.put('/api/admin/ai').send({ enabled: false }).expect(200);
  const before = provider.state.chats.length;
  assert.equal(await codeOf(ai.chat({ userId: member.user.id, feature: 'test', messages: [{ role: 'user', content: 'hi' }] })), 'AI_DISABLED');
  assert.equal(await codeOf(ai.embed({ userId: member.user.id, feature: 'test', texts: ['x'] })), 'AI_DISABLED');
  assert.equal(provider.state.chats.length, before);
  const status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.available, false);
  assert.equal(status.roles.text.ready, false);
  await admin.agent.put('/api/admin/ai').send({ enabled: true }).expect(200);
});

test('BYOK keys are saved encrypted, tested, used for that account only and removed', async () => {
  // The operator points OpenAI at the fake but sets no server key.
  await admin.agent.put('/api/admin/providers').send({ key: 'NEOCLOUD_OPENAI_BASE_URL', value: provider.baseUrl }).expect(200);
  const list = (await member.agent.get('/api/settings/byok').expect(200)).body.providers;
  assert.ok(list.some((entry) => entry.id === 'openai'));
  assert.ok(!list.some((entry) => entry.id === 'neorouter'));
  await member.agent.put('/api/settings/byok/neorouter').send({ apiKey: 'x' }).expect(404);
  const missing = await member.agent.put('/api/settings/byok/openai').send({}).expect(400);
  assert.equal(missing.body.code, 'API_KEY_REQUIRED');

  const rejected = (await member.agent.post('/api/settings/byok/openai/test').send({ apiKey: 'sk-wrong' }).expect(200)).body;
  assert.equal(rejected.ok, false);

  assert.deepEqual((await member.agent.put('/api/settings/byok/openai').send({ apiKey: MEMBER_KEY }).expect(200)).body,
    { success: true, configured: true });
  const stored = db.prepare('SELECT key, value FROM user_settings WHERE user_id = ?').all(member.user.id);
  assert.ok(stored.some((row) => row.key === 'ai_provider_api_keys'));
  assert.ok(!JSON.stringify(stored).includes(MEMBER_KEY));
  assert.ok(!JSON.stringify((await member.agent.get('/api/settings').expect(200)).body).includes('ai_provider'));

  const single = (await member.agent.get('/api/settings/byok/openai').expect(200)).body.provider;
  assert.equal(single.configured, true);
  assert.ok(!JSON.stringify(single).includes(MEMBER_KEY));

  const tested = (await member.agent.post('/api/settings/byok/openai/test').send({}).expect(200)).body;
  assert.equal(tested.ok, true);
  assert.match(tested.message, /found 4 model/);
  assert.equal(provider.state.modelListKeys.at(-1), MEMBER_KEY);

  const mine = (await memberModels()).find((model) => model.id === 'openai::fake-chat');
  assert.equal(mine.isByok, true);
  const theirs = (await outsider.agent.get('/api/settings/meta/models').expect(200)).body.models;
  assert.ok(!theirs.some((model) => model.provider === 'openai'));

  await member.agent.put('/api/settings').send({ ai_model_text: 'openai::fake-chat' }).expect(200);
  await ai.chat({ userId: member.user.id, feature: 'test_byok', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(provider.state.chats.at(-1).key, MEMBER_KEY);

  assert.deepEqual((await member.agent.delete('/api/settings/byok/openai').expect(200)).body, { success: true, configured: false });
  assert.equal((await member.agent.get('/api/settings/byok/openai').expect(200)).body.provider.configured, false);
  assert.equal((await member.agent.get('/api/ai/status').expect(200)).body.roles.text.requestedUnavailable, true);
  await member.agent.put('/api/settings').send({ ai_model_text: 'auto' }).expect(200);
});

test('a person’s base URL may not reach the server’s own network', async () => {
  for (const baseUrl of ['http://127.0.0.1:9/v1', 'http://169.254.169.254/latest', 'http://[::1]:8080', 'http://router.local/v1']) {
    const res = await member.agent.put('/api/settings/byok/openai-compatible').send({ apiKey: 'sk-x', baseUrl }).expect(400);
    assert.equal(res.body.code, 'URL_NOT_ALLOWED', baseUrl);
  }
  const scheme = await member.agent.put('/api/settings/byok/openai-compatible').send({ apiKey: 'sk-x', baseUrl: 'file:///etc/passwd' }).expect(400);
  assert.equal(scheme.body.code, 'INVALID_URL');
  const ollama = await member.agent.put('/api/settings/byok/ollama').send({ baseUrl: 'http://localhost:11434' }).expect(400);
  assert.equal(ollama.body.code, 'URL_NOT_ALLOWED');
  const probe = await member.agent.post('/api/settings/byok/openai-compatible/test').send({ apiKey: 'sk-x', baseUrl: 'http://10.0.0.1/v1' }).expect(400);
  assert.equal(probe.body.code, 'URL_NOT_ALLOWED');
  // The address the admin configured is not the person's to abuse: it is allowed.
  await member.agent.put('/api/settings/byok/openai-compatible').send({ apiKey: MEMBER_KEY, baseUrl: provider.baseUrl }).expect(200);
  await member.agent.delete('/api/settings/byok/openai-compatible').expect(200);
});

test('NeoRouter connects for the server and for a person through the device flow', async () => {
  const denied = await member.agent.post('/api/neorouter/connect').send({ scope: 'server', address: router.origin }).expect(403);
  assert.equal(denied.body.code, 'ADMIN_REQUIRED');
  await admin.agent.post('/api/neorouter/connect').send({ scope: 'everyone', address: router.origin }).expect(400);

  const started = (await admin.agent.post('/api/neorouter/connect').send({ scope: 'server', address: router.origin }).expect(200)).body;
  assert.equal(started.userCode, 'ABCD-EFGH');
  assert.ok(started.url.startsWith(router.origin));
  assert.equal(router.state.deviceRequests.at(-1).client_id, 'neocloud');
  assert.match(router.state.deviceRequests.at(-1).client_name, /^NeoCloud \(/);
  assert.equal(router.state.deviceRequests.at(-1).scope, 'chat embedding transcription image');
  await member.agent.get(`/api/neorouter/connect/${started.id}`).expect(404);
  assert.deepEqual(await waitForSignIn(admin.agent, started.id), { status: 'completed', error: null });
  await admin.agent.get(`/api/neorouter/connect/${started.id}`).expect(404); // handed out once

  const adminView = (await admin.agent.get('/api/neorouter').expect(200)).body;
  assert.deepEqual(adminView.roles, ['chat', 'embeddings', 'transcription', 'images']);
  assert.equal(adminView.server.connected, true);
  assert.equal(adminView.server.address, router.origin);
  assert.deepEqual(adminView.server.roles, ['chat', 'embeddings', 'transcription', 'images']);
  assert.deepEqual(adminView.server.offer.roles, { chat: 1, embeddings: 1, transcription: 0, images: 0 });
  const memberView = (await member.agent.get('/api/neorouter').expect(200)).body;
  assert.deepEqual(memberView.server, { connected: true });
  assert.equal(memberView.user.connected, false);

  // Automatic roles now go to NeoRouter's profiles.
  let status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.roles.text.model, 'neorouter::auto:fast');
  assert.equal(status.roles.text.source, 'neorouter');
  assert.equal(status.roles.smart.model, 'neorouter::auto:smart');
  assert.equal(status.roles.vision.model, 'neorouter::auto:default');
  assert.equal(status.roles.embedding.model, 'neorouter::auto:default');
  assert.equal(status.roles.transcription.model, 'neorouter::auto:default');
  const models = await memberModels();
  assert.equal(models.find((model) => model.id === 'neorouter::router-chat').supportsVision, true);

  const embedded = await ai.embed({ userId: member.user.id, feature: 'test_router', texts: ['x'] });
  assert.equal(embedded.provider, 'neorouter');
  assert.equal(router.state.embeddings.at(-1).body.model, 'auto');
  await ai.chat({ userId: member.user.id, feature: 'test_router', messages: [{ role: 'user', content: 'hi' }] });
  assert.equal(router.state.chats.at(-1).body.model, 'auto:fast');

  // Embeddings switched off at the router: they go back to the other providers.
  assert.deepEqual((await admin.agent.put('/api/neorouter/server/roles').send({ roles: ['chat'] }).expect(200)).body, { roles: ['chat'] });
  status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.roles.embedding.model, 'openai-compatible::fake-embed');
  await admin.agent.put('/api/neorouter/server/roles').send({ roles: 'chat' }).expect(400);

  // A person's own NeoRouter: another private address is refused, the
  // server's own router is fine.
  const blocked = await member.agent.post('/api/neorouter/connect').send({ scope: 'user', address: 'http://192.168.1.50:3333' }).expect(400);
  assert.equal(blocked.body.code, 'URL_NOT_ALLOWED');
  const own = (await member.agent.post('/api/neorouter/connect').send({ scope: 'user', address: router.origin }).expect(200)).body;
  assert.equal((await waitForSignIn(member.agent, own.id)).status, 'completed');
  const userKey = router.state.issued.at(-1);
  assert.ok(!JSON.stringify(db.prepare('SELECT value FROM user_settings WHERE user_id = ?').all(member.user.id)).includes(userKey));
  const mine = (await member.agent.get('/api/neorouter').expect(200)).body.user;
  assert.equal(mine.connected, true);
  assert.deepEqual(mine.roles, ['chat', 'embeddings', 'transcription', 'images']);
  await ai.embed({ userId: member.user.id, feature: 'test_router', texts: ['x'] });
  assert.equal(router.state.embeddings.at(-1).key, userKey);

  assert.deepEqual((await member.agent.put('/api/neorouter/user/roles').send({ roles: ['embeddings', 'bogus'] }).expect(200)).body, { roles: ['embeddings'] });
  status = (await member.agent.get('/api/ai/status').expect(200)).body;
  assert.equal(status.roles.text.model, 'openai-compatible::fake-chat');

  await member.agent.delete('/api/neorouter/user').expect(200);
  assert.equal((await member.agent.get('/api/neorouter').expect(200)).body.user.connected, false);
  await member.agent.delete('/api/neorouter/server').expect(403);
  await admin.agent.delete('/api/neorouter/server').expect(200);
  assert.equal((await admin.agent.get('/api/neorouter').expect(200)).body.server.connected, false);
  assert.equal(process.env.NEOROUTER_API_KEY, undefined);
});

test('the phone app reaches the AI routes like a session; desktop tokens do not', async () => {
  const phone = bearerFor(member, 'android');
  await request(app).get('/api/ai/status').set('Authorization', phone).expect(200);
  await request(app).get('/api/settings/meta/models').set('Authorization', phone).expect(200);
  await request(app).get('/api/settings/byok').set('Authorization', phone).expect(200);
  await request(app).get('/api/neorouter').set('Authorization', phone).expect(200);
  await request(app).get('/api/admin/ai').set('Authorization', bearerFor(admin, 'ios')).expect(200);
  await request(app).get('/api/admin/ai').set('Authorization', phone).expect(403);

  const desktop = bearerFor(member, 'lite');
  for (const path of ['/api/ai/status', '/api/settings/meta/models', '/api/settings/byok', '/api/neorouter']) {
    const res = await request(app).get(path).set('Authorization', desktop).expect(403);
    assert.equal(res.body.code, 'PHONE_REQUIRED', path);
  }
  await request(app).get('/api/ai/status').expect(401);
});
