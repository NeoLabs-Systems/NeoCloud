'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('http');
const crypto = require('crypto');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { issueAccessToken } = require('../../server/services/office/tokens');

let editor;
let editorUrl;
let alice; // the admin
let bob;
let doc;
let session;

test.after(async () => {
  await new Promise((resolve) => editor.close(resolve));
  cleanup();
});

function discoveryXml(origin) {
  return `<wopi-discovery><net-zone name="external-http"><app name="writer">
    <action name="edit" ext="docx" urlsrc="${origin}/browser/x/cool.html?"/>
    <action name="view" ext="docx" urlsrc="${origin}/browser/x/cool.html?view=1&amp;"/>
  </app></net-zone></wopi-discovery>`;
}

test.before(async () => {
  editor = http.createServer((req, res) => {
    if (req.url === '/hosting/discovery') {
      res.setHeader('Content-Type', 'text/xml');
      res.end(discoveryXml(editorUrl));
    } else {
      res.statusCode = 404;
      res.end('nope');
    }
  });
  await new Promise((resolve) => editor.listen(0, '127.0.0.1', resolve));
  editorUrl = `http://127.0.0.1:${editor.address().port}`;
  alice = await registerUser('alice');
  bob = await registerUser('bob');
  doc = (await upload(alice.agent, 'report.docx', 'version one').expect(201)).body;
});

const wopi = {
  get: (id, token, path = '') => request(app).get(`/wopi/files/${id}${path}`).query({ access_token: token }),
  post: (id, token, path = '') => request(app).post(`/wopi/files/${id}${path}`).query({ access_token: token }),
};

function lock(id, token, value, extra = {}) {
  let req = wopi.post(id, token).set('X-WOPI-Override', 'LOCK').set('X-WOPI-Lock', value);
  for (const [key, header] of Object.entries(extra)) req = req.set(key, header);
  return req;
}

function put(id, token, content, lockValue, extra = {}) {
  let req = wopi.post(id, token, '/contents')
    .set('X-WOPI-Override', 'PUT')
    .set('Content-Type', 'application/octet-stream');
  if (lockValue !== undefined) req = req.set('X-WOPI-Lock', lockValue);
  for (const [key, header] of Object.entries(extra)) req = req.set(key, header);
  return req.send(Buffer.from(content));
}

test('the editor is off until an admin sets it up; others cannot', async () => {
  assert.deepEqual((await bob.agent.get('/api/office/config').expect(200)).body, { enabled: false, extensions: [] });
  await bob.agent.put('/api/admin/config/office').send({ url: editorUrl, enabled: true }).expect(403);
  await alice.agent.post(`/api/files/${doc.id}/edit-session`).expect(503);
});

test('admin: a wrong address is refused with a reason, a right one is verified', async () => {
  const bad = await alice.agent.put('/api/admin/config/office').send({ url: 'http://127.0.0.1:1', enabled: true }).expect(422);
  assert.equal(bad.body.code, 'EDITOR_UNREACHABLE');
  assert.match(bad.body.error, /could not connect/);
  await alice.agent.put('/api/admin/config/office').send({ url: 'ftp://x', enabled: true }).expect(400);
  await alice.agent.put('/api/admin/config/office').send({ url: `${editorUrl}/missing`, enabled: true }).expect(422);

  const ok = (await alice.agent.put('/api/admin/config/office').send({ url: editorUrl, enabled: true }).expect(200)).body;
  assert.equal(ok.enabled, true);
  assert.equal(ok.status.state, 'ok');
  assert.deepEqual(ok.status.extensions, ['docx']);
  assert.deepEqual((await bob.agent.get('/api/office/config').expect(200)).body, { enabled: true, extensions: ['docx'] });
});

test('the page may frame and post to the editor origin only', async () => {
  const res = await alice.agent.get('/api/health');
  const csp = res.headers['content-security-policy'];
  assert.match(csp, new RegExp(`frame-src 'self' blob: ${editorUrl}`));
  assert.match(csp, new RegExp(`form-action 'self' ${editorUrl}`));
});

test('an edit session carries the editor page, a token and its end', async () => {
  session = (await alice.agent.post(`/api/files/${doc.id}/edit-session`).expect(200)).body;
  assert.ok(session.editorUrl.startsWith(`${editorUrl}/browser/x/cool.html?WOPISrc=`));
  assert.ok(decodeURIComponent(session.editorUrl).endsWith(`/wopi/files/${doc.id}`));
  assert.ok(session.accessTokenTtl > Date.now() + 9 * 3600 * 1000);
  assert.equal(session.canWrite, true);
  await alice.agent.post(`/api/files/${doc.id}/edit-session`).expect(200);
  const txt = (await upload(alice.agent, 'notes.txt', 'x').expect(201)).body;
  await alice.agent.post(`/api/files/${txt.id}/edit-session`).expect(415);
  await bob.agent.post(`/api/files/${doc.id}/edit-session`).expect(404);
});

test('CheckFileInfo describes the file and the person', async () => {
  const info = (await wopi.get(doc.id, session.accessToken).expect(200)).body;
  assert.equal(info.BaseFileName, 'report.docx');
  assert.equal(info.Size, 11);
  assert.equal(info.Version, '1');
  assert.equal(info.UserCanWrite, true);
  assert.equal(info.SupportsLocks, true);
  assert.equal(info.UserFriendlyName, alice.username);
  assert.equal(info.UserId, String(alice.user.id));
  assert.ok(!Number.isNaN(Date.parse(info.LastModifiedTime)));
});

test('GetFile returns the content and its version', async () => {
  const res = await wopi.get(doc.id, session.accessToken, '/contents').buffer().expect(200);
  assert.equal(res.headers['x-wopi-itemversion'], '1');
  assert.equal(res.text, 'version one');
});

test('locks follow the WOPI rules', async () => {
  const token = session.accessToken;
  const get = () => wopi.post(doc.id, token).set('X-WOPI-Override', 'GET_LOCK');
  assert.equal((await get().expect(200)).headers['x-wopi-lock'], '');
  await wopi.post(doc.id, token).set('X-WOPI-Override', 'UNLOCK').set('X-WOPI-Lock', 'a').expect(409)
    .expect('X-WOPI-Lock', '');
  await lock(doc.id, token, 'lock-a').expect(200);
  await lock(doc.id, token, 'lock-a').expect(200);
  await lock(doc.id, token, 'lock-b').expect(409).expect('X-WOPI-Lock', 'lock-a');
  assert.equal((await get().expect(200)).headers['x-wopi-lock'], 'lock-a');
  await wopi.post(doc.id, token).set('X-WOPI-Override', 'REFRESH_LOCK').set('X-WOPI-Lock', 'lock-b').expect(409)
    .expect('X-WOPI-Lock', 'lock-a');
  await wopi.post(doc.id, token).set('X-WOPI-Override', 'REFRESH_LOCK').set('X-WOPI-Lock', 'lock-a').expect(200);
  await lock(doc.id, token, 'lock-c', { 'X-WOPI-OldLock': 'wrong' }).expect(409).expect('X-WOPI-Lock', 'lock-a');
  await lock(doc.id, token, 'lock-c', { 'X-WOPI-OldLock': 'lock-a' }).expect(200);
  await wopi.post(doc.id, token).set('X-WOPI-Override', 'UNLOCK').set('X-WOPI-Lock', 'lock-a').expect(409)
    .expect('X-WOPI-Lock', 'lock-c');
  await wopi.post(doc.id, token).set('X-WOPI-Override', 'UNLOCK').set('X-WOPI-Lock', 'lock-c').expect(200);
  assert.equal((await get().expect(200)).headers['x-wopi-lock'], '');
});

test('PutFile needs the lock, then makes a new version authored by the editor', async () => {
  const token = session.accessToken;
  await put(doc.id, token, 'sneaky').expect(409).expect('X-WOPI-Lock', '');
  await lock(doc.id, token, 'edit-1').expect(200);
  await put(doc.id, token, 'wrong lock', 'other').expect(409).expect('X-WOPI-Lock', 'edit-1');
  await put(doc.id, token, 'stale', 'edit-1', { 'X-COOL-WOPI-Timestamp': '2001-01-01T00:00:00.000Z' })
    .expect(409);
  const saved = await put(doc.id, token, 'version two!', 'edit-1').expect(200);
  assert.equal(saved.headers['x-wopi-itemversion'], '2');
  assert.ok(saved.body.LastModifiedTime);

  const info = (await wopi.get(doc.id, token).expect(200)).body;
  assert.equal(info.Version, '2');
  assert.equal(info.Size, 12);
  assert.equal((await wopi.get(doc.id, token, '/contents').buffer().expect(200)).text, 'version two!');

  const versions = (await alice.agent.get(`/api/files/${doc.id}/versions`).expect(200)).body;
  assert.equal(versions.current.version, 2);
  assert.equal(versions.versions.length, 1);
  assert.equal(db.prepare('SELECT modified_by FROM nodes WHERE id = ?').get(doc.id).modified_by, alice.user.id);
  const journal = db.prepare("SELECT op FROM changes WHERE node_id = ? ORDER BY seq DESC LIMIT 1").get(doc.id);
  assert.equal(journal.op, 'update');

  // Saving the same content again changes nothing.
  await put(doc.id, token, 'version two!', 'edit-1').expect(200);
  assert.equal(db.prepare('SELECT version FROM nodes WHERE id = ?').get(doc.id).version, 2);
  await wopi.post(doc.id, token).set('X-WOPI-Override', 'UNLOCK').set('X-WOPI-Lock', 'edit-1').expect(200);
});

test('tokens that are forged, expired, for another file or another account are refused', async () => {
  const other = (await upload(alice.agent, 'other.docx', 'o').expect(201)).body;
  const [payload, signature] = session.accessToken.split('.');
  const forged = `${Buffer.from(JSON.stringify({ u: bob.user.id, n: doc.id, w: 1, e: Date.now() + 1e6 })).toString('base64url')}.${signature}`;
  const expired = issueAccessToken({ userId: alice.user.id, nodeId: doc.id, write: true }, Date.now() - 11 * 3600 * 1000).token;
  for (const token of [forged, expired, `${payload}.AAAA`, 'garbage', '']) {
    await wopi.get(doc.id, token).expect(401);
    await put(doc.id, token, 'x', 'l').expect(401);
  }
  await request(app).get(`/wopi/files/${doc.id}`).expect(401);
  await wopi.get(other.id, session.accessToken).expect(401);

  // Someone else's file, with a validly signed token: reads as missing.
  const foreign = issueAccessToken({ userId: bob.user.id, nodeId: doc.id, write: true }).token;
  await wopi.get(doc.id, foreign).expect(404);
  await put(doc.id, foreign, 'hijack', 'l').expect(404);
  assert.equal((await wopi.get(doc.id, session.accessToken, '/contents').buffer().expect(200)).text, 'version two!');
});

test('a read-only token cannot save or lock', async () => {
  const readOnly = issueAccessToken({ userId: alice.user.id, nodeId: doc.id, write: false }).token;
  assert.equal((await wopi.get(doc.id, readOnly).expect(200)).body.UserCanWrite, false);
  await put(doc.id, readOnly, 'nope', 'l').expect(403);
  await lock(doc.id, readOnly, 'l').expect(403);
  await wopi.get(doc.id, readOnly, '/contents').expect(200);
});

test('shares: write edits, read views, revoking ends the session; vault files are out', async () => {
  const folder = (await alice.agent.post('/api/folders').send({ parentId: 'root', name: 'Team' }).expect(201)).body;
  const shared = (await upload(alice.agent, 'plan.docx', 'plan', { parentId: folder.id }).expect(201)).body;
  const grant = (await alice.agent.post(`/api/nodes/${folder.id}/members`)
    .send({ recipient: bob.username, permission: 'write' }).expect(201)).body;

  const writer = (await bob.agent.post(`/api/files/${shared.id}/edit-session`).expect(200)).body;
  assert.equal(writer.canWrite, true);
  await lock(shared.id, writer.accessToken, 'b1').expect(200);
  await put(shared.id, writer.accessToken, 'plan by bob', 'b1').expect(200);
  assert.equal(db.prepare('SELECT modified_by FROM nodes WHERE id = ?').get(shared.id).modified_by, bob.user.id);
  assert.equal(db.prepare('SELECT user_id FROM nodes WHERE id = ?').get(shared.id).user_id, alice.user.id);

  await alice.agent.patch(`/api/nodes/${folder.id}/members/${grant.id}`).send({ permission: 'read' }).expect(200);
  const viewer = (await bob.agent.post(`/api/files/${shared.id}/edit-session`).expect(200)).body;
  assert.equal(viewer.canWrite, false);
  // The old write token no longer writes.
  assert.equal((await wopi.get(shared.id, writer.accessToken).expect(200)).body.UserCanWrite, false);
  await put(shared.id, writer.accessToken, 'late', 'b1').expect(403);

  db.prepare('DELETE FROM user_shares WHERE node_id = ?').run(folder.id);
  await wopi.get(shared.id, writer.accessToken).expect(404);
  await wopi.get(shared.id, writer.accessToken, '/contents').expect(404);
  await put(shared.id, writer.accessToken, 'after revoke', 'b1').expect(404);
  await bob.agent.post(`/api/files/${shared.id}/edit-session`).expect(404);

  const vault = (await alice.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Vault',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  const sealed = (await upload(alice.agent, 'secret.docx', 'NCV1 ciphertext', { parentId: vault.folderId }).expect(201)).body;
  const token = issueAccessToken({ userId: alice.user.id, nodeId: sealed.id, write: true }).token;
  await wopi.get(sealed.id, token).expect(404);
  await alice.agent.post(`/api/files/${sealed.id}/edit-session`).expect(404);
});

test('trashing the file ends its sessions', async () => {
  const gone = (await upload(alice.agent, 'gone.docx', 'g').expect(201)).body;
  const { accessToken } = (await alice.agent.post(`/api/files/${gone.id}/edit-session`).expect(200)).body;
  await wopi.get(gone.id, accessToken).expect(200);
  await alice.agent.delete(`/api/nodes/${gone.id}`).expect(200);
  await wopi.get(gone.id, accessToken).expect(404);
});

test('switching the editor off ends new and running sessions', async () => {
  await wopi.get(doc.id, session.accessToken).expect(200);
  await alice.agent.put('/api/admin/config/office').send({ enabled: false }).expect(200);
  assert.deepEqual((await bob.agent.get('/api/office/config').expect(200)).body, { enabled: false, extensions: [] });
  await alice.agent.post(`/api/files/${doc.id}/edit-session`).expect(503);
  await wopi.get(doc.id, session.accessToken).expect(401);
  const status = (await alice.agent.get('/api/admin/config/office').expect(200)).body.status;
  assert.equal(status.state, 'off');
  const csp = (await alice.agent.get('/api/health')).headers['content-security-policy'];
  assert.ok(!csp.includes(editorUrl));
});
