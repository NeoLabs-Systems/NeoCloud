'use strict';

// Every API route that takes an id, tried by another account with the ids of
// someone else's things: nodes, versions, trash, uploads, links, member
// grants, vaults, imports, devices, sessions, security keys, AI suggestions,
// rules and proposals. Each must answer like a missing id (4xx, never 2xx),
// and afterwards the owner's things are all still there, unchanged.
//
// The route list is read from the routers themselves (ROUTE_REGISTRY), and
// every route must be classified below: a new route fails this test until
// someone decides how another account's ids are tried on it.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const path = require('path');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { createDeviceToken } = require('../../server/services/devices/tokens');

test.after(cleanup);

const REGISTRY = [
  ['/api/auth', 'auth'], ['/api/account', 'account'], ['/api/settings', 'settings'], ['/api/admin', 'admin'],
  ['/api', 'files'], ['/api', 'transfers'], ['/api', 'members'], ['/api/sync', 'sync'], ['/api/devices', 'devices'],
  ['/api/storage', 'storage'], ['/api/shares', 'shares'], ['/api/public/shares', 'public_shares'],
  ['/api/vaults', 'vaults'], ['/api/imports', 'imports'], ['/api/ai', 'ai'], ['/api/neorouter', 'neorouter'],
  ['/api/photos', 'photos'],
];

function registeredRoutes() {
  const source = require('fs').readFileSync(path.join(__dirname, '../../server/http/routes.js'), 'utf8');
  const routes = [];
  for (const [basePath, module] of REGISTRY) {
    assert.ok(source.includes(`basePath: '${basePath}', modulePath: '../routes/${module}'`), `${module} is mounted at ${basePath}`);
    const router = require(`../../server/routes/${module}`);
    for (const layer of router.stack) {
      if (!layer.route) continue;
      for (const method of Object.keys(layer.route.methods)) {
        routes.push(`${method.toUpperCase()} ${basePath}${layer.route.path}`.replace(/\/$/, ''));
      }
    }
  }
  return routes;
}

function keyParams() {
  return { salt: crypto.randomBytes(16).toString('base64'), iterations: 600000, wrappedKey: crypto.randomBytes(60).toString('base64') };
}

let owner; // the victim
let other; // the account trying the victim's ids
let member; // a third account the victim shares a folder with
const v = {};

test.before(async () => {
  owner = await registerUser('owner');
  other = await registerUser('other');
  member = await registerUser('member');
  const folder = await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'Private' }).expect(201);
  v.folder = folder.body.id;
  const file = await upload(owner.agent, 'secret.txt', 'first', { parentId: v.folder }).expect(201);
  v.file = file.body.id;
  await upload(owner.agent, 'secret.txt', 'second', { parentId: v.folder, conflict: 'replace' }).expect(201);
  const trashed = await upload(owner.agent, 'old.txt', 'gone').expect(201);
  v.trashed = trashed.body.id;
  await owner.agent.delete(`/api/nodes/${v.trashed}`).expect(200);
  const link = await owner.agent.post('/api/shares').send({ nodeId: v.file }).expect(201);
  v.link = link.body.id;
  const grant = await owner.agent.post(`/api/nodes/${v.folder}/members`).send({ recipient: member.username }).expect(201);
  v.member = grant.body.id;
  const pending = await owner.agent.post('/api/uploads').send({ parentId: v.folder, name: 'big.bin', size: 10 }).expect(201);
  v.upload = pending.body.id;
  const vault = await owner.agent.post('/api/vaults').send({ parentId: 'root', name: 'Vault', ...keyParams() }).expect(201);
  v.vault = vault.body.id;
  v.device = createDeviceToken(owner.user.id, { name: 'Laptop', platform: 'linux' }).device.id;
  const pairing = await owner.agent.post('/api/account/devices/pairing').send({}).expect(200);
  v.pairing = pairing.body.code;
  v.session = (await owner.agent.get('/api/account/sessions').expect(200)).body.sessions[0].id;
  v.key = Number(db.prepare(`
    INSERT INTO user_webauthn_credentials (user_id, credential_id, public_key, counter, rp_id, transports_json, label)
    VALUES (?, ?, 'pk', 0, '127.0.0.1', '[]', 'Key') RETURNING id
  `).get(owner.user.id, crypto.randomBytes(16).toString('base64url')).id);
  const rule = await owner.agent.post('/api/ai/rules').send({ name: 'Bills', match: { extension: 'pdf' }, targetFolderId: v.folder }).expect(201);
  v.rule = rule.body.rule.id;
  v.proposal = db.prepare(`
    INSERT INTO ai_move_stats (user_id, doc_type, issuer, target_folder_id, accepts) VALUES (?, 'invoice', 'ACME', ?, 3) RETURNING id
  `).get(owner.user.id, v.folder).id;
  const node = db.prepare('SELECT * FROM nodes WHERE id = ?').get(v.file);
  v.suggestion = require('../../server/services/ai/organize/suggestions')
    .createSuggestion(owner.user.id, node, { kind: 'rename', from: node.name, to: 'renamed.txt', confidence: 0.9 }).id;
  v.import = crypto.randomUUID();
  db.prepare(`
    INSERT INTO import_jobs (id, user_id, source, url, username, source_path, target_folder_id, status)
    VALUES (?, ?, 'webdav', 'https://dav.example.com', 'me', '/', ?, 'paused')
  `).run(v.import, owner.user.id, v.folder);
  v.otherFile = (await upload(other.agent, 'mine.txt', 'mine').expect(201)).body.id;
});

// How another account's ids go into each route. A string says why the route
// has no id of someone else to try.
const OWN = 'acts on the caller’s own account only';
const PUBLIC = 'public by design; the secret in the URL is the credential';
const ADMIN = 'admin only (tried below)';
const CLASSIFIED = {
  'GET /api/auth/status': OWN,
  'POST /api/auth/register': OWN,
  'POST /api/auth/login': OWN,
  'POST /api/auth/login/2fa': OWN,
  'POST /api/auth/webauthn/login/options': OWN,
  'POST /api/auth/webauthn/login/verify': OWN,
  'POST /api/auth/qr-login/challenge': OWN,
  'POST /api/auth/qr-login/challenge/:id/status': PUBLIC,
  'POST /api/auth/qr-login/challenge/:id/cancel': PUBLIC,
  'POST /api/auth/qr-login/challenge/:id/claim': PUBLIC,
  'POST /api/auth/logout': OWN,
  'GET /api/auth/me': OWN,
  'GET /api/account/devices/authorize/:userCode': 'device codes are approved by whoever is signed in',
  'POST /api/account/devices/authorize/:userCode': 'device codes are approved by whoever is signed in',
  'POST /api/account/qr-login/resolve': PUBLIC,
  'POST /api/account/qr-login/decline': PUBLIC,
  'POST /api/account/qr-login/approve': PUBLIC,
  'GET /api/account': OWN,
  'GET /api/account/usage': OWN,
  'PUT /api/account/display-name': OWN,
  'PUT /api/account/password': OWN,
  'POST /api/account/2fa/setup': OWN,
  'POST /api/account/2fa/enable': OWN,
  'POST /api/account/2fa/disable': OWN,
  'POST /api/account/2fa/recovery-codes': OWN,
  'GET /api/account/security-keys': OWN,
  'POST /api/account/security-keys/register/options': OWN,
  'POST /api/account/security-keys/register/verify': OWN,
  'PUT /api/account/security-keys/:id': () => ['PUT', `/api/account/security-keys/${v.key}`, { label: 'x' }],
  'DELETE /api/account/security-keys/:id': () => ['DELETE', `/api/account/security-keys/${v.key}`],
  'GET /api/account/sessions': OWN,
  'DELETE /api/account/sessions/:id': () => ['DELETE', `/api/account/sessions/${v.session}`],
  'GET /api/account/devices': OWN,
  'POST /api/account/devices': OWN,
  'POST /api/account/devices/pairing': OWN,
  'GET /api/account/devices/pairing/:code': () => ['GET', `/api/account/devices/pairing/${v.pairing}`],
  'DELETE /api/account/devices/:id': () => ['DELETE', `/api/account/devices/${v.device}`],
  'POST /api/account/export': OWN,
  'POST /api/account/delete': OWN,
  'GET /api/settings': OWN,
  'PUT /api/settings': OWN,
  'GET /api/settings/meta/models': OWN,
  'GET /api/settings/meta/ai-providers': OWN,
  'GET /api/settings/byok': OWN,
  'GET /api/settings/byok/:providerId': OWN,
  'PUT /api/settings/byok/:providerId': OWN,
  'DELETE /api/settings/byok/:providerId': OWN,
  'POST /api/settings/byok/:providerId/test': OWN,
  'GET /api/folders/:id/children': () => ['GET', `/api/folders/${v.folder}/children`],
  'POST /api/folders': () => ['POST', '/api/folders', { parentId: v.folder, name: 'x' }],
  'GET /api/nodes/:id': () => ['GET', `/api/nodes/${v.file}`],
  'PATCH /api/nodes/:id': () => ['PATCH', `/api/nodes/${v.file}`, { name: 'pwned.txt' }],
  'POST /api/nodes/:id/copy': () => ['POST', `/api/nodes/${v.file}/copy`, { parentId: 'root' }],
  'DELETE /api/nodes/:id': () => ['DELETE', `/api/nodes/${v.file}`],
  'GET /api/files/:id/versions': () => ['GET', `/api/files/${v.file}/versions`],
  'POST /api/files/:id/versions/:version/restore': () => ['POST', `/api/files/${v.file}/versions/1/restore`],
  'PUT /api/nodes/:id/favorite': () => ['PUT', `/api/nodes/${v.file}/favorite`],
  'DELETE /api/nodes/:id/favorite': () => ['DELETE', `/api/nodes/${v.file}/favorite`],
  'GET /api/favorites': OWN,
  'POST /api/nodes/:id/opened': () => ['POST', `/api/nodes/${v.file}/opened`],
  'GET /api/search': () => ['GET', `/api/search?q=secret&parentId=${v.folder}`],
  'GET /api/recent': OWN,
  'GET /api/trash': OWN,
  'POST /api/trash/:id/restore': () => ['POST', `/api/trash/${v.trashed}/restore`],
  'DELETE /api/trash/:id': () => ['DELETE', `/api/trash/${v.trashed}`],
  'DELETE /api/trash': OWN,
  'PUT /api/upload': () => ['PUT', `/api/upload?parentId=${v.folder}&name=x.txt`, Buffer.from('x')],
  'POST /api/uploads': () => ['POST', '/api/uploads', { parentId: v.folder, name: 'x.bin', size: 1 }],
  'POST /api/uploads/check': () => ['POST', '/api/uploads/check', { parentId: v.folder, sha256: '0'.repeat(64), size: 1 }],
  'POST /api/uploads/similar': OWN,
  'GET /api/uploads/:id': () => ['GET', `/api/uploads/${v.upload}`],
  'PUT /api/uploads/:id/parts/:index': () => ['PUT', `/api/uploads/${v.upload}/parts/0`, Buffer.from('x')],
  'POST /api/uploads/:id/complete': () => ['POST', `/api/uploads/${v.upload}/complete`],
  'DELETE /api/uploads/:id': () => ['DELETE', `/api/uploads/${v.upload}`],
  'GET /api/files/:id/content': () => ['GET', `/api/files/${v.file}/content?version=1`],
  'GET /api/files/:id/thumbnail': () => ['GET', `/api/files/${v.file}/thumbnail`],
  'GET /api/files/:id/preview': () => ['GET', `/api/files/${v.file}/preview`],
  'GET /api/capabilities': OWN,
  'GET /api/download/zip': () => ['GET', `/api/download/zip?ids=${v.folder}`],
  'POST /api/nodes/:id/members': () => ['POST', `/api/nodes/${v.folder}/members`, { recipient: other.username }],
  'GET /api/nodes/:id/members': () => ['GET', `/api/nodes/${v.folder}/members`],
  'PATCH /api/nodes/:id/members/:shareId': () => ['PATCH', `/api/nodes/${v.folder}/members/${v.member}`, { permission: 'write' }],
  'DELETE /api/nodes/:id/members/:shareId': () => ['DELETE', `/api/nodes/${v.folder}/members/${v.member}`],
  'GET /api/shared-with-me': OWN,
  'DELETE /api/shared-with-me/:shareId': () => ['DELETE', `/api/shared-with-me/${v.member}`],
  'GET /api/sync/tree': OWN,
  'GET /api/sync/changes': OWN,
  'POST /api/devices/authorize': OWN,
  'POST /api/devices/token': PUBLIC,
  'POST /api/devices/pair': PUBLIC,
  'POST /api/devices/webauthn/options': OWN,
  'POST /api/devices/webauthn/verify': OWN,
  'POST /api/devices/webauthn/2fa': PUBLIC,
  'GET /api/devices/me': OWN,
  'GET /api/storage/breakdown': OWN,
  'GET /api/storage/duplicates': OWN,
  // A cursor only says where to start; rows are always the caller's (photos.test.js).
  'GET /api/photos': OWN,
  'GET /api/photos/summary': OWN,
  'POST /api/shares': () => ['POST', '/api/shares', { nodeId: v.file }],
  'GET /api/shares': () => ['GET', `/api/shares?nodeId=${v.file}`],
  'PATCH /api/shares/:id': () => ['PATCH', `/api/shares/${v.link}`, { note: 'x' }],
  'DELETE /api/shares/:id': () => ['DELETE', `/api/shares/${v.link}`],
  'GET /api/public/shares/:token': PUBLIC,
  'POST /api/public/shares/:token/unlock': PUBLIC,
  'GET /api/public/shares/:token/files/:nodeId/content': PUBLIC,
  'GET /api/public/shares/:token/zip': PUBLIC,
  'PUT /api/public/shares/:token/upload': PUBLIC,
  'POST /api/vaults': () => ['POST', '/api/vaults', { parentId: v.folder, name: 'V', ...keyParams() }],
  'GET /api/vaults/:id': () => ['GET', `/api/vaults/${v.vault}`],
  'PUT /api/vaults/:id/key': () => ['PUT', `/api/vaults/${v.vault}/key`, keyParams()],
  'GET /api/imports': OWN,
  'POST /api/imports': () => ['POST', '/api/imports', { source: 'webdav', url: 'https://dav.example.com', username: 'x', password: 'y', targetFolderId: v.folder }],
  'POST /api/imports/test': OWN,
  'GET /api/imports/:id': () => ['GET', `/api/imports/${v.import}`],
  'POST /api/imports/:id/pause': () => ['POST', `/api/imports/${v.import}/pause`],
  'POST /api/imports/:id/resume': () => ['POST', `/api/imports/${v.import}/resume`, { password: 'y' }],
  'POST /api/imports/:id/cancel': () => ['POST', `/api/imports/${v.import}/cancel`],
  'DELETE /api/imports/:id': () => ['DELETE', `/api/imports/${v.import}`],
  'GET /api/ai/status': OWN,
  'PUT /api/ai/folders/:id/excluded': () => ['PUT', `/api/ai/folders/${v.folder}/excluded`, { excluded: true }],
  'GET /api/ai/suggestions': () => ['GET', `/api/ai/suggestions?nodeId=${v.file}`],
  'GET /api/ai/suggestions/counts': OWN,
  'POST /api/ai/suggestions/accept-high': OWN,
  'POST /api/ai/suggestions/bulk': () => ['POST', '/api/ai/suggestions/bulk', { ids: [v.suggestion], action: 'accept' }],
  'GET /api/ai/duplicates': OWN,
  'POST /api/ai/duplicates/dismiss': () => ['POST', '/api/ai/duplicates/dismiss', { nodeIds: [v.file] }],
  'POST /api/ai/suggestions/:id/accept': () => ['POST', `/api/ai/suggestions/${v.suggestion}/accept`],
  'POST /api/ai/suggestions/:id/reject': () => ['POST', `/api/ai/suggestions/${v.suggestion}/reject`],
  'POST /api/ai/suggestions/:id/undo': () => ['POST', `/api/ai/suggestions/${v.suggestion}/undo`],
  'GET /api/ai/jobs': OWN,
  'POST /api/ai/jobs': () => ['POST', '/api/ai/jobs', { nodeIds: [v.file] }],
  'GET /api/ai/rules': OWN,
  'POST /api/ai/rules': () => ['POST', '/api/ai/rules', { name: 'x', match: { extension: 'pdf' }, targetFolderId: v.folder }],
  'GET /api/ai/rules/proposals': OWN,
  'POST /api/ai/rules/proposals/:id': () => ['POST', `/api/ai/rules/proposals/${v.proposal}`],
  'DELETE /api/ai/rules/proposals/:id': () => ['DELETE', `/api/ai/rules/proposals/${v.proposal}`],
  'PATCH /api/ai/rules/:id': () => ['PATCH', `/api/ai/rules/${v.rule}`, { name: 'x' }],
  'DELETE /api/ai/rules/:id': () => ['DELETE', `/api/ai/rules/${v.rule}`],
  'POST /api/ai/files/:id/summary': () => ['POST', `/api/ai/files/${v.file}/summary`],
  'GET /api/neorouter': OWN,
  'POST /api/neorouter/connect': OWN,
  'GET /api/neorouter/connect/:id': 'flows live in memory and are checked against the account (sign_in.status)',
  'PUT /api/neorouter/:scope/roles': OWN,
  'DELETE /api/neorouter/:scope': OWN,
};

// Requests that a route answers with the caller's own (empty) data rather
// than an error: they must simply show nothing of the owner's.
const EMPTY_ANSWER = {
  'GET /api/shares': (body) => assert.deepEqual(body.shares, []),
  'GET /api/ai/suggestions': (body) => assert.deepEqual(body.items, []),
  // Bulk answers per id: another account's id is a failure, nothing done.
  'POST /api/ai/suggestions/bulk': (body) => {
    assert.equal(body.done, 0);
    assert.equal(body.failed.length, 1);
  },
  // Dismissing names the caller's own view only; the owner's files stay.
  'POST /api/ai/duplicates/dismiss': () => {},
  'GET /api/search': (body) => assert.ok(!JSON.stringify(body).includes('secret.txt')),
};

function send(agent, [method, url, body]) {
  const req = agent[method.toLowerCase()](url);
  if (Buffer.isBuffer(body)) return req.set('Content-Type', 'application/octet-stream').send(body);
  return body === undefined ? req : req.send(body);
}

test('every registered route is classified', () => {
  const routes = registeredRoutes().filter((route) => !route.startsWith('GET /api/admin')
    && !/^(PUT|POST|DELETE) \/api\/admin/.test(route));
  const unclassified = routes.filter((route) => !(route in CLASSIFIED));
  assert.deepEqual(unclassified, [], 'classify new routes in route_idor.test.js');
  const stale = Object.keys(CLASSIFIED).filter((route) => !routes.includes(route));
  assert.deepEqual(stale, [], 'routes that no longer exist');
});

test('another account’s ids behave as missing on every route that takes one', async () => {
  for (const [route, entry] of Object.entries(CLASSIFIED)) {
    if (typeof entry !== 'function') continue;
    const res = await send(other.agent, entry());
    if (EMPTY_ANSWER[route] && res.status === 200) {
      EMPTY_ANSWER[route](res.body);
      continue;
    }
    assert.ok(res.status >= 400 && res.status < 500, `${route} answered ${res.status}`);
    assert.ok(![401, 429].includes(res.status), `${route} answered ${res.status}: the test itself is off`);
  }
});

test('the owner’s things are all still there and unchanged', async () => {
  const node = db.prepare('SELECT name, parent_id, trashed_at, version FROM nodes WHERE id = ?').get(v.file);
  assert.equal(node.name, 'secret.txt');
  assert.equal(node.parent_id, v.folder);
  assert.equal(node.trashed_at, null);
  assert.equal(node.version, 2);
  assert.equal(db.prepare('SELECT COUNT(*) AS count FROM favorites WHERE node_id = ?').get(v.file).count, 0);
  assert.ok(db.prepare('SELECT 1 FROM nodes WHERE id = ? AND trashed_at IS NOT NULL').get(v.trashed));
  assert.equal(db.prepare('SELECT revoked_at, note FROM shares WHERE id = ?').get(v.link).revoked_at, null);
  assert.equal(db.prepare('SELECT permission FROM user_shares WHERE id = ?').get(v.member).permission, 'read');
  assert.ok(db.prepare('SELECT 1 FROM upload_sessions WHERE id = ?').get(v.upload));
  assert.equal(db.prepare('SELECT revoked_at FROM device_tokens WHERE id = ?').get(v.device).revoked_at, null);
  assert.equal(db.prepare('SELECT revoked_at FROM user_sessions WHERE id = ?').get(v.session).revoked_at, null);
  assert.equal(db.prepare('SELECT label FROM user_webauthn_credentials WHERE id = ?').get(v.key).label, 'Key');
  assert.equal(db.prepare('SELECT name FROM ai_rules WHERE id = ?').get(v.rule).name, 'Bills');
  assert.equal(db.prepare('SELECT dismissed FROM ai_move_stats WHERE id = ?').get(v.proposal).dismissed, 0);
  assert.equal(db.prepare('SELECT status FROM ai_suggestions WHERE id = ?').get(v.suggestion).status, 'pending');
  assert.equal(db.prepare('SELECT status FROM import_jobs WHERE id = ?').get(v.import).status, 'paused');
  assert.equal(db.prepare('SELECT ai_excluded FROM nodes WHERE id = ?').get(v.folder).ai_excluded, 0);
  const owned = db.prepare('SELECT COUNT(*) AS count FROM nodes WHERE user_id = ?').get(other.user.id).count;
  const { body } = await other.agent.get('/api/sync/tree').expect(200);
  assert.ok(!JSON.stringify(body).includes('secret.txt'));
  assert.ok(owned >= 2);
  // The member still has the grant nobody else could take away.
  await member.agent.get(`/api/folders/${v.folder}/children`).expect(200);
});

test('moves and copies into another account’s folder are refused', async () => {
  await other.agent.patch(`/api/nodes/${v.otherFile}`).send({ parentId: v.folder }).expect(404);
  await other.agent.post(`/api/nodes/${v.otherFile}/copy`).send({ parentId: v.folder }).expect(404);
});

test('admin routes: refused for a non-admin, for an admin’s desktop token and for a non-admin’s phone', async () => {
  const adminRoutes = registeredRoutes().filter((route) => route.includes(' /api/admin'));
  assert.ok(adminRoutes.length > 20);
  // The first account on the install is the admin.
  const adminDesktop = createDeviceToken(owner.user.id, { name: 'Desktop', platform: 'macos' }).token;
  const otherPhone = createDeviceToken(other.user.id, { name: 'Phone', platform: 'android' }).token;
  for (const route of adminRoutes) {
    const [method, rawPath] = route.split(' ');
    const url = rawPath.replace(/:id\b/g, String(owner.user.id));
    const tries = [
      other.agent[method.toLowerCase()](url),
      request(app)[method.toLowerCase()](url).set('Authorization', `Bearer ${adminDesktop}`),
      request(app)[method.toLowerCase()](url).set('Authorization', `Bearer ${otherPhone}`),
    ];
    for (const attempt of tries) {
      const res = await attempt.send({});
      assert.equal(res.status, 403, `${route} answered ${res.status}`);
      assert.equal(res.body.code, 'ADMIN_REQUIRED');
    }
  }
});
