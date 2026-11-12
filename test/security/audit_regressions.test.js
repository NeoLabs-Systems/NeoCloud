'use strict';

// Regression tests for the findings of the November 2026 security audit.

const test = require('node:test');
const assert = require('node:assert/strict');
const dns = require('node:dns');
const http = require('node:http');
const { STRONG_PASSWORD, app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { aiFetch, fetchResponseText } = require('../../server/services/ai/http');
const { matchingRule } = require('../../server/services/ai/organize/rules');

test.after(cleanup);

let admin;
test.before(async () => {
  admin = await registerUser('admin');
});

// --- WebDAV app passwords reach WebDAV only -------------------------------------------

test('a WebDAV app password is refused as a bearer token on the API', async () => {
  const alice = await registerUser('dav');
  const owner = await registerUser('davowner');
  const folder = await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'Shared' }).expect(201);
  await owner.agent.post(`/api/nodes/${folder.body.id}/members`).send({ recipient: alice.username }).expect(201);
  const file = await upload(alice.agent, 'notes.txt', 'hello').expect(201);
  const { token } = (await alice.agent.post('/api/account/devices').send({ name: 'Files app', platform: 'webdav' }).expect(201)).body;

  const bearer = (req) => req.set('Authorization', `Bearer ${token}`);
  for (const req of [
    request(app).get('/api/folders/root/children'),
    request(app).get(`/api/files/${file.body.id}/content`),
    request(app).post('/api/shares').send({ nodeId: file.body.id }),
    request(app).get('/api/shared-with-me'),
    request(app).get(`/api/folders/${folder.body.id}/children`),
    request(app).get('/api/sync/tree'),
    request(app).get('/api/devices/me'),
    request(app).get('/api/account'),
  ]) {
    await bearer(req).expect(401);
  }
  assert.equal(db.prepare('SELECT COUNT(*) AS count FROM shares WHERE user_id = ?').get(alice.user.id).count, 0);

  // It still works where it belongs.
  await request(app).get(`/dav/files/${alice.username}/notes.txt`).auth(alice.username, token).expect(200);
});

// --- AI: an address someone typed never reaches the server's network -------------------

function listen(handler) {
  const server = http.createServer(handler);
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(server)));
}

// `*.rebind.test` answers a public address to the check and loopback to the
// connection: what a DNS-rebinding name does.
function rebindingDns(t) {
  const { lookup } = dns;
  const promisesLookup = dns.promises.lookup;
  dns.promises.lookup = async (host, options) => (host.endsWith('.rebind.test')
    ? [{ address: '93.184.216.34', family: 4 }]
    : promisesLookup(host, options));
  dns.lookup = (host, options, callback) => {
    const done = typeof options === 'function' ? options : callback;
    const opts = typeof options === 'function' ? {} : options;
    if (!host.endsWith('.rebind.test')) return lookup(host, options, callback);
    return opts.all ? done(null, [{ address: '127.0.0.1', family: 4 }]) : done(null, '127.0.0.1', 4);
  };
  t.after(() => {
    dns.lookup = lookup;
    dns.promises.lookup = promisesLookup;
  });
}

test('a personal AI base URL that resolves into the server’s network is never connected to', async (t) => {
  rebindingDns(t);
  const hits = [];
  const internal = await listen((req, res) => {
    hits.push(req.url);
    res.writeHead(500);
    res.end('INTERNAL-ONLY');
  });
  t.after(() => internal.close());
  const { port } = internal.address();
  const user = await registerUser('ssrf');

  const tested = await user.agent.post('/api/settings/byok/ollama/test').send({ baseUrl: `http://ollama.rebind.test:${port}` }).expect(200);
  assert.equal(tested.body.ok, false);
  await user.agent.put('/api/settings/byok/openai-compatible').send({ baseUrl: `http://llm.rebind.test:${port}/v1`, apiKey: 'k' }).expect(200);
  const saved = await user.agent.post('/api/settings/byok/openai-compatible/test').send({}).expect(200);
  assert.equal(saved.body.ok, false);
  await user.agent.post('/api/neorouter/connect').send({ scope: 'user', address: `http://router.rebind.test:${port}` }).expect(400);

  assert.deepEqual(hits, []);
  assert.ok(!JSON.stringify([tested.body, saved.body]).includes('INTERNAL-ONLY'));
});

test('the AI fetch refuses private IP literals (redirect targets included) and keeps the admin’s addresses', async (t) => {
  const hits = [];
  const internal = await listen((req, res) => {
    hits.push(req.url);
    res.end('{"models":[]}');
  });
  t.after(() => internal.close());
  const local = `http://127.0.0.1:${internal.address().port}`;

  await assert.rejects(aiFetch(`${local}/v1/models`));
  await assert.rejects(fetchResponseText(`${local}/api/tags`));
  await assert.rejects(fetchResponseText(`http://[::ffff:127.0.0.1]:${internal.address().port}/api/tags`));
  assert.deepEqual(hits, []);

  // The address an admin configured may be on the LAN.
  const previous = process.env.NEOCLOUD_OLLAMA_URL;
  process.env.NEOCLOUD_OLLAMA_URL = local;
  t.after(() => {
    if (previous === undefined) delete process.env.NEOCLOUD_OLLAMA_URL;
    else process.env.NEOCLOUD_OLLAMA_URL = previous;
  });
  const { response } = await fetchResponseText(`${local}/api/tags`);
  assert.equal(response.status, 200);
  assert.equal((await aiFetch(`${local}/api/tags`)).status, 200);
  assert.equal(hits.length, 2);
});

// --- AI rules: name patterns can't stall the server ------------------------------------

test('a rule’s name pattern matches in linear time', async () => {
  const user = await registerUser('rules');
  const folder = await user.agent.post('/api/folders').send({ parentId: 'root', name: 'Target' }).expect(201);
  const create = (namePattern) => user.agent.post('/api/ai/rules')
    .send({ name: namePattern, match: { namePattern }, targetFolderId: folder.body.id })
    .expect(201);
  await create(`${'*a'.repeat(30)}*b`);
  const node = { name: `${'a'.repeat(250)}.txt` };
  const started = Date.now();
  assert.equal(matchingRule(user.user.id, node, {}), null);
  assert.ok(Date.now() - started < 1000, 'pattern matching backtracked');

  await create('Scan_??.PDF');
  assert.equal(matchingRule(user.user.id, { name: 'scan_07.pdf' }, {})?.name, 'Scan_??.PDF');
  assert.equal(matchingRule(user.user.id, { name: 'scan_7.pdf' }, {}), null);
  await create('*rechnung*');
  assert.equal(matchingRule(user.user.id, { name: '2026 Rechnung Telekom.pdf' }, {})?.name, '*rechnung*');
  assert.equal(matchingRule(user.user.id, { name: 'Vertrag.pdf' }, {}), null);
});

// --- Sharing with accounts ---------------------------------------------------------------

test('the owner’s export names a member only as the owner typed them', async () => {
  const owner = await registerUser('exp');
  const memberAccount = await registerUser('expmember');
  const folder = await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'Shared' }).expect(201);
  await owner.agent.post(`/api/nodes/${folder.body.id}/members`).send({ recipient: `${memberAccount.username}@example.com` }).expect(201);
  const res = await owner.agent.post('/api/account/export').send({ currentPassword: STRONG_PASSWORD }).expect(200);
  const [entry] = res.body.sharing.byMe;
  assert.equal(entry.recipient, `${memberAccount.username}@example.com`);
  assert.ok(!JSON.stringify(res.body.sharing).includes(`"${memberAccount.username}"`));
});

test('a disabled owner’s folders are no longer reachable by members', async () => {
  const owner = await registerUser('disowner');
  const memberAccount = await registerUser('dismember');
  const folder = await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'Shared' }).expect(201);
  await owner.agent.post(`/api/nodes/${folder.body.id}/members`).send({ recipient: memberAccount.username, permission: 'write' }).expect(201);
  await memberAccount.agent.get(`/api/folders/${folder.body.id}/children`).expect(200);

  await admin.agent.put(`/api/admin/users/${owner.user.id}/disabled`).send({ disabled: true }).expect(200);
  await memberAccount.agent.get(`/api/folders/${folder.body.id}/children`).expect(404);
  await upload(memberAccount.agent, 'x.txt', 'x', { parentId: folder.body.id }).expect(404);
  assert.deepEqual((await memberAccount.agent.get('/api/shared-with-me').expect(200)).body.shares, []);

  await admin.agent.put(`/api/admin/users/${owner.user.id}/disabled`).send({ disabled: false }).expect(200);
  await memberAccount.agent.get(`/api/folders/${folder.body.id}/children`).expect(200);
});
