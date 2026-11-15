'use strict';

// NeoAgent's companion OAuth and the agent API (/api/v1): bootstrap, sign-in
// and consent with PKCE, granted scopes, refresh rotation, revocation, scope
// checks, file operations, Markdown PDFs, signature requests and "sign out
// everywhere".

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { PDFDocument } = require('pdf-lib');
const { STRONG_PASSWORD, app, cleanup, registerUser, request, upload } = require('../helpers/app');
const { makePdf } = require('../helpers/signing');

test.after(cleanup);

const CALLBACK = 'https://agent.example.test/api/integrations/oauth/callback';
const ALL_SCOPES = 'files:read files:write signing:read signing:write';

async function bootstrap() {
  const res = await request(app).post('/api/oauth/companion/neoagent/bootstrap')
    .send({ redirectUri: CALLBACK, appName: 'NeoAgent' }).expect(200);
  return res.body;
}

function pkce() {
  const verifier = crypto.randomBytes(48).toString('base64url');
  return { verifier, challenge: crypto.createHash('sha256').update(verifier).digest('base64url') };
}

/** Runs sign-in and consent for `username`; returns the token response. */
async function connect(username, consent = {}, { scope = ALL_SCOPES } = {}) {
  const client = await bootstrap();
  const { verifier, challenge } = pkce();
  const params = {
    response_type: 'code', client_id: client.clientId, redirect_uri: CALLBACK,
    state: crypto.randomBytes(16).toString('hex'), scope, code_challenge: challenge, code_challenge_method: 'S256',
  };
  const browser = request.agent(app);
  const authorizePath = `/oauth/authorize?${new URLSearchParams(params)}`;
  const first = await browser.get(authorizePath).expect(302);
  assert.match(first.headers.location, /^\/oauth\/sign-in\?continue=/);
  const signIn = await browser.post('/oauth/sign-in').type('form')
    .send({ account: username, password: STRONG_PASSWORD, continue: authorizePath }).expect(302);
  assert.equal(signIn.headers.location, authorizePath);
  const page = await browser.get(authorizePath).set('Accept-Language', 'de-DE,de;q=0.9').expect(200);
  assert.match(page.text, /NeoAgent verbinden\?/);
  assert.match(page.headers['content-security-policy'], /form-action 'self' https:\/\/agent\.example\.test/);
  const consentToken = /name="consent" value="([^"]+)"/.exec(page.text)[1];
  const approval = await browser.post('/oauth/authorize').type('form')
    .send({ ...params, consent: consentToken, decision: 'approve', ...consent }).expect(302);
  const redirect = new URL(approval.headers.location);
  assert.equal(redirect.searchParams.get('state'), params.state);
  const exchange = await request(app).post('/oauth/token').type('form').send({
    grant_type: 'authorization_code', client_id: client.clientId, code: redirect.searchParams.get('code'),
    redirect_uri: CALLBACK, code_verifier: verifier,
  }).expect(200);
  return { ...exchange.body, clientId: client.clientId };
}

const api = (token) => ({
  get: (path) => request(app).get(`/api/v1${path}`).set('Authorization', `Bearer ${token}`),
  post: (path, body) => request(app).post(`/api/v1${path}`).set('Authorization', `Bearer ${token}`).send(body ?? {}),
  put: (path, body) => request(app).put(`/api/v1${path}`).set('Authorization', `Bearer ${token}`).send(body),
  patch: (path, body) => request(app).patch(`/api/v1${path}`).set('Authorization', `Bearer ${token}`).send(body),
  delete: (path) => request(app).delete(`/api/v1${path}`).set('Authorization', `Bearer ${token}`),
});

test('identification: health and version name the product', async () => {
  const version = await request(app).get('/api/version').expect(200);
  assert.equal(version.body.product, 'neocloud');
  const health = await request(app).get('/api/health').expect(200);
  assert.equal(health.body.status, 'ok');
});

test('bootstrap is idempotent per callback and refuses foreign callbacks', async () => {
  const first = await bootstrap();
  assert.equal(first.companion, 'neoagent');
  assert.deepEqual(first.scopes, ['files:read', 'files:write', 'signing:read', 'signing:write']);
  assert.match(first.revocationEndpoint, /\/oauth\/revoke$/);
  const again = await bootstrap();
  assert.equal(again.clientId, first.clientId);
  assert.equal(again.created, false);
  await request(app).post('/api/oauth/companion/neoagent/bootstrap')
    .send({ redirectUri: 'https://agent.example.test/callback' }).expect(400);
  await request(app).post('/api/oauth/companion/neoagent/bootstrap')
    .send({ redirectUri: 'http://agent.example.test/api/integrations/oauth/callback' }).expect(400);
  await request(app).post('/api/oauth/companion/neoagent/bootstrap')
    .send({ redirectUri: 'http://192.168.1.20:3333/api/integrations/oauth/callback' }).expect(200);
});

test('consent grants read only by default; refresh rotates; revoke ends access', async () => {
  const { username } = await registerUser('reader');
  const tokens = await connect(username);
  assert.equal(tokens.token_type, 'Bearer');
  assert.equal(tokens.expires_in, 3600);
  assert.equal(tokens.scope, 'files:read signing:read');
  const reader = api(tokens.access_token);
  await reader.get('/files').expect(200);
  const denied = await reader.post('/files', { path: '/x.txt', content: 'x' }).expect(403);
  assert.equal(denied.body.code, 'INSUFFICIENT_SCOPE');
  await reader.post('/signing/requests', {}).expect(403);

  const info = await request(app).get('/oauth/userinfo').set('Authorization', `Bearer ${tokens.access_token}`).expect(200);
  assert.equal(info.body.preferred_username, username);
  assert.equal(info.body.scope, 'files:read signing:read');

  const refreshed = await request(app).post('/oauth/token').type('form')
    .send({ grant_type: 'refresh_token', client_id: tokens.clientId, refresh_token: tokens.refresh_token }).expect(200);
  assert.notEqual(refreshed.body.refresh_token, tokens.refresh_token);
  assert.equal(refreshed.body.scope, 'files:read signing:read');
  await reader.get('/files').expect(401);
  await request(app).post('/oauth/token').type('form')
    .send({ grant_type: 'refresh_token', client_id: tokens.clientId, refresh_token: tokens.refresh_token }).expect(400);
  await api(refreshed.body.access_token).get('/files').expect(200);

  await request(app).post('/oauth/revoke').type('form')
    .send({ client_id: tokens.clientId, token: refreshed.body.refresh_token }).expect(200);
  await api(refreshed.body.access_token).get('/files').expect(401);
});

test('a bad PKCE verifier, a reused code and deny are refused', async () => {
  const { username } = await registerUser('pkce');
  const client = await bootstrap();
  const { challenge } = pkce();
  const params = {
    response_type: 'code', client_id: client.clientId, redirect_uri: CALLBACK,
    state: 'state-123456789', scope: ALL_SCOPES, code_challenge: challenge, code_challenge_method: 'S256',
  };
  const browser = request.agent(app);
  const path = `/oauth/authorize?${new URLSearchParams(params)}`;
  await browser.post('/oauth/sign-in').type('form').send({ account: username, password: 'wrong-password', continue: path }).expect(401);
  await browser.post('/oauth/sign-in').type('form').send({ account: username, password: STRONG_PASSWORD, continue: path }).expect(302);
  let page = await browser.get(path).expect(200);
  let consent = /name="consent" value="([^"]+)"/.exec(page.text)[1];
  const approval = await browser.post('/oauth/authorize').type('form').send({ ...params, consent, decision: 'approve' }).expect(302);
  const code = new URL(approval.headers.location).searchParams.get('code');
  await request(app).post('/oauth/token').type('form').send({
    grant_type: 'authorization_code', client_id: client.clientId, code, redirect_uri: CALLBACK, code_verifier: pkce().verifier,
  }).expect(400);
  await browser.post('/oauth/sign-in').type('form').send({ account: username, password: STRONG_PASSWORD, continue: path }).expect(302);
  page = await browser.get(path).expect(200);
  consent = /name="consent" value="([^"]+)"/.exec(page.text)[1];
  const denial = await browser.post('/oauth/authorize').type('form').send({ ...params, consent, decision: 'deny' }).expect(302);
  assert.equal(new URL(denial.headers.location).searchParams.get('error'), 'access_denied');
  // Without a challenge the request is not even shown.
  await browser.get(`/oauth/authorize?${new URLSearchParams({ ...params, code_challenge: '' })}`).expect(400);
});

test('read & write: create, read, update, move, trash; vaults stay hidden; other accounts 404', async () => {
  const owner = await registerUser('writer');
  const tokens = await connect(owner.username, { files: 'write' });
  assert.equal(tokens.scope, 'files:read files:write signing:read');
  const agent = api(tokens.access_token);

  const created = await agent.post('/files', { path: '/Contracts/Lease.md', content: '# Lease\n' }).expect(201);
  assert.equal(created.body.path, '/Contracts/Lease.md');
  assert.equal(created.body.type, 'file');
  const clash = await agent.post('/files', { path: '/Contracts/Lease.md', content: 'again' }).expect(409);
  assert.equal(clash.body.code, 'ALREADY_EXISTS');

  const listing = await agent.get('/files?path=/Contracts').expect(200);
  assert.deepEqual(listing.body.items.map((item) => item.name), ['Lease.md']);
  const content = await agent.get(`/files/${created.body.id}/content`).expect(200);
  assert.equal(content.body.encoding, 'utf8');
  assert.equal(content.body.content, '# Lease\n');

  const updated = await agent.put(`/files/${created.body.id}/content`, { content: '# Lease v2\n', baseRevision: content.body.revision }).expect(200);
  assert.notEqual(updated.body.revision, content.body.revision);
  const stale = await agent.put(`/files/${created.body.id}/content`, { content: 'x', baseRevision: content.body.revision }).expect(409);
  assert.equal(stale.body.code, 'CONFLICT');
  const versions = await owner.agent.get(`/api/files/${created.body.id}/versions`).expect(200);
  assert.ok(JSON.stringify(versions.body).length > 2);

  await agent.post('/folders', { path: '/Archive/2026' }).expect(201);
  await agent.post('/folders', { path: '/Archive/2026' }).expect(200);
  const moved = await agent.patch(`/files/${created.body.id}`, { name: 'Lease-2026.md', parentPath: '/Archive/2026' }).expect(200);
  assert.equal(moved.body.path, '/Archive/2026/Lease-2026.md');
  const binary = await agent.post('/files', { path: '/bin.dat', contentBase64: Buffer.from([0, 1, 2, 255]).toString('base64') }).expect(201);
  const binaryContent = await agent.get(`/files/${binary.body.id}/content`).expect(200);
  assert.equal(binaryContent.body.encoding, 'base64');
  const found = await agent.get('/files/search?q=lease').expect(200);
  assert.ok(found.body.items.some((item) => item.path === '/Archive/2026/Lease-2026.md'));

  const trashed = await agent.delete(`/files/${created.body.id}`).expect(200);
  assert.deepEqual(trashed.body, { trashed: true });
  await agent.get(`/files/${created.body.id}`).expect(404);
  const trash = await owner.agent.get('/api/trash').expect(200);
  assert.ok(JSON.stringify(trash.body).includes(created.body.id));

  const salt = crypto.randomBytes(16).toString('base64');
  const vault = await owner.agent.post('/api/vaults').send({
    parentId: 'root', name: 'Secret', salt, iterations: 600000, wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201);
  const root = await agent.get('/files').expect(200);
  assert.ok(!root.body.items.some((item) => item.name === 'Secret'));
  await agent.get(`/files/${vault.body.folder.id}`).expect(404);
  const refused = await agent.post('/files', { path: '/Secret/note.txt', content: 'x' }).expect(403);
  assert.equal(refused.body.code, 'VAULT_NOT_ALLOWED');

  const other = await registerUser('stranger');
  const foreign = (await upload(other.agent, 'theirs.txt', 'theirs').expect(201)).body.id;
  await agent.get(`/files/${foreign}`).expect(404);
  await agent.get(`/files/${foreign}/content`).expect(404);
  await agent.delete(`/files/${foreign}`).expect(404);
  // A browser session or device token is not an agent token.
  await owner.agent.get('/api/v1/files').expect(401);
});

test('Markdown to PDF', async () => {
  const { username } = await registerUser('pdf');
  const agent = api((await connect(username, { files: 'write' })).access_token);
  const res = await agent.post('/documents/pdf', {
    path: '/Docs/Offer.pdf', title: 'Offer', markdown: '# Offer\n\nHello **bold** and *italic* Grüße.\n\n- one\n- two\n\n1. first\n\n---\n\nEnd.',
  }).expect(201);
  assert.equal(res.body.mimeType, 'application/pdf');
  const content = await agent.get(`/files/${res.body.id}/content`).expect(200);
  assert.equal(content.body.encoding, 'base64');
  const doc = await PDFDocument.load(Buffer.from(content.body.content, 'base64'));
  assert.ok(doc.getPageCount() >= 1);
});

test('signature requests: from Markdown and from a PDF, links, list, cancel; scope checks', async () => {
  const owner = await registerUser('signer');
  const full = await connect(owner.username, { files: 'write', signing: 'on' });
  assert.equal(full.scope, ALL_SCOPES);
  const agent = api(full.access_token);

  const created = await agent.post('/signing/requests', {
    document: { path: '/Contracts/NDA.pdf', title: 'NDA', markdown: '# NDA\n\nWe agree.' },
    signers: [{ name: 'Ada Lovelace', email: 'ada@example.com' }, { name: 'Alan Turing' }],
    signingOrder: 'sequential',
  }).expect(201);
  assert.equal(created.body.title, 'NDA');
  assert.equal(created.body.status, 'pending');
  assert.equal(created.body.signers.length, 2);
  for (const signer of created.body.signers) assert.match(signer.url, /\/app\/sign\/[A-Za-z0-9_-]{43}$/);
  const token = created.body.signers[0].url.split('/').pop();
  const signerView = await request(app).get(`/api/public/sign/${token}`).expect(200);
  assert.ok(JSON.stringify(signerView.body).includes('signature'));

  const pdf = await upload(owner.agent, 'Lease.pdf', await makePdf(2)).expect(201);
  const fromPdf = await agent.post('/signing/requests', { fileId: pdf.body.id, signers: [{ name: 'Grace Hopper' }] }).expect(201);
  assert.equal(fromPdf.body.signers.length, 1);
  const pending = await agent.get('/signing/requests?status=pending').expect(200);
  assert.equal(pending.body.items.length, 2);
  await agent.get('/signing/requests?status=bogus').expect(400);

  const link = await agent.post(`/signing/requests/${fromPdf.body.id}/signers/${fromPdf.body.signers[0].id}/link`).expect(200);
  assert.notEqual(link.body.url, fromPdf.body.signers[0].url);
  await request(app).get(`/api/public/sign/${fromPdf.body.signers[0].url.split('/').pop()}`).expect(404);

  const reminded = await agent.post(`/signing/requests/${fromPdf.body.id}/remind`).expect(409);
  assert.ok(reminded.body.code);
  const canceled = await agent.post(`/signing/requests/${fromPdf.body.id}/cancel`).expect(200);
  assert.equal(canceled.body.status, 'cancelled');
  const one = await agent.get(`/signing/requests/${created.body.id}`).expect(200);
  assert.equal(one.body.signers[0].name, 'Ada Lovelace');

  // Signatures on, files read only: a document needs files:write.
  const signOnly = api((await connect(owner.username, { signing: 'on' })).access_token);
  const noFiles = await signOnly.post('/signing/requests', {
    document: { path: '/x.pdf', markdown: 'x' }, signers: [{ name: 'X' }],
  }).expect(403);
  assert.equal(noFiles.body.code, 'INSUFFICIENT_SCOPE');
  const other = await registerUser('peer');
  const peer = api((await connect(other.username, { signing: 'on' })).access_token);
  await peer.get(`/signing/requests/${created.body.id}`).expect(404);
});

test('connected apps are listed and revoked by disconnecting and by sign out everywhere', async () => {
  const owner = await registerUser('apps');
  const first = await connect(owner.username);
  const listed = await owner.agent.get('/api/account/connected-apps').expect(200);
  assert.equal(listed.body.apps.length, 1);
  assert.equal(listed.body.apps[0].name, 'NeoAgent');
  const stranger = await registerUser('nosy');
  await stranger.agent.delete(`/api/account/connected-apps/${listed.body.apps[0].id}`).expect(404);
  await api(first.access_token).get('/files').expect(200);
  await owner.agent.delete(`/api/account/connected-apps/${listed.body.apps[0].id}`).expect(200);
  await api(first.access_token).get('/files').expect(401);

  const second = await connect(owner.username);
  const db = require('../../server/db/database');
  const { revokeUserSessions } = require('../../server/services/admin/users');
  const userId = db.prepare('SELECT id FROM users WHERE username = ?').get(owner.username).id;
  revokeUserSessions(userId, { actorUserId: userId, sessionId: null, deviceId: null });
  await api(second.access_token).get('/files').expect(401);
  await request(app).post('/oauth/token').type('form')
    .send({ grant_type: 'refresh_token', client_id: second.clientId, refresh_token: second.refresh_token }).expect(400);
});
