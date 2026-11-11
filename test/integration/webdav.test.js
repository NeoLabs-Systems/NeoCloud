'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { app, cleanup, registerUser, request, upload } = require('../helpers/app');
const db = require('../../server/db/database');

test.after(cleanup);

let alice;
let bob;
let token;
let base;

function dav(method, path, { auth = [alice.username, token] } = {}) {
  const req = request(app)[method.toLowerCase()](path);
  return auth ? req.auth(auth[0], auth[1]) : req;
}

// Raw bytes whatever the Content-Type, so unknown types read back too.
function binary(res, done) {
  const chunks = [];
  res.on('data', (chunk) => chunks.push(chunk));
  res.on('end', () => done(null, Buffer.concat(chunks)));
}

async function read(path, status = 200) {
  const res = await dav('GET', path).buffer(true).parse(binary).expect(status);
  return res.body.toString('utf8');
}

function propfind(path, { depth = '1', body = '' } = {}) {
  const req = dav('PROPFIND', path).set('Content-Type', 'application/xml');
  if (depth !== null) req.set('Depth', depth);
  return req.send(body);
}

function hrefs(xml) {
  return [...xml.matchAll(/<d:href>([^<]*)<\/d:href>/g)].map((match) => match[1]);
}

function responseFor(xml, href) {
  return xml.split('<d:response>').find((part) => part.includes(`<d:href>${href}</d:href>`)) || '';
}

async function createToken(user, platform = 'webdav') {
  const res = await user.agent.post('/api/account/devices').send({ name: `${platform} app`, platform }).expect(201);
  return res.body;
}

async function apiChildren(user, parentId = 'root') {
  return (await user.agent.get(`/api/folders/${parentId}/children`).expect(200)).body.items;
}

test.before(async () => {
  alice = await registerUser('alice');
  bob = await registerUser('bob');
  ({ token } = await createToken(alice));
  base = `/dav/files/${alice.username}`;
});

test('OPTIONS answers without credentials and advertises class 2', async () => {
  const res = await request(app).options(`${base}/`).expect(200);
  assert.match(res.headers.dav, /\b2\b/);
  assert.match(res.headers.allow, /PROPFIND/);
  assert.match(res.headers.allow, /LOCK/);
});

test('auth: Basic with a webdav app password only', async () => {
  const missing = await request(app).propfind(`${base}/`).expect(401);
  assert.match(missing.headers['www-authenticate'], /^Basic realm="NeoCloud"/);
  await dav('PROPFIND', `${base}/`, { auth: [alice.username, 'nc_wrong'] }).expect(401);
  await dav('PROPFIND', `${base}/`, { auth: [bob.username, token] }).expect(401);
  // Usernames compare case-insensitively.
  await dav('PROPFIND', `${base}/`, { auth: [alice.username.toUpperCase(), token] }).set('Depth', '0').expect(207);
  // A sync client's token is not an app password.
  const lite = await createToken(alice, 'lite');
  await dav('PROPFIND', `${base}/`, { auth: [alice.username, lite.token] }).expect(401);
  // Session cookies are not accepted.
  await alice.agent.propfind(`${base}/`).set('Depth', '0').expect(401);
  // A revoked app password stops working.
  const spare = await createToken(alice);
  await dav('PROPFIND', `${base}/`, { auth: [alice.username, spare.token] }).set('Depth', '0').expect(207);
  await alice.agent.delete(`/api/account/devices/${spare.device.id}`).expect(200);
  await dav('PROPFIND', `${base}/`, { auth: [alice.username, spare.token] }).set('Depth', '0').expect(401);
});

test('another account’s URL is not found, and a foreign Origin is refused', async () => {
  await propfind(`/dav/files/${bob.username}/`).expect(404);
  await propfind(`${base}/`).set('Origin', 'https://evil.example').expect(403);
});

test('PUT creates, GET/HEAD read with ETag, Last-Modified and Range', async () => {
  const content = Buffer.from('Hello WebDAV world');
  const created = await dav('PUT', `${base}/hello.txt`).set('Content-Type', 'application/json').serialize((value) => value).send(content).expect(201);
  const etag = `"${crypto.createHash('sha256').update(content).digest('hex')}"`;
  assert.equal(created.headers.etag, etag);
  assert.ok(created.headers['oc-fileid']);

  const got = await dav('GET', `${base}/hello.txt`).buffer(true).parse(binary).expect(200);
  assert.deepEqual(got.body, content);
  assert.equal(got.headers.etag, etag);
  assert.ok(got.headers['last-modified']);
  assert.match(got.headers['content-security-policy'], /sandbox/);

  const head = await dav('HEAD', `${base}/hello.txt`).expect(200);
  assert.equal(head.headers['content-length'], String(content.length));

  const range = await dav('GET', `${base}/hello.txt`).set('Range', 'bytes=6-11').expect(206);
  assert.equal(range.text, 'WebDAV');
  assert.equal(range.headers['content-range'], `bytes 6-11/${content.length}`);
  await dav('GET', `${base}/hello.txt`).set('Range', 'bytes=500-').expect(416);
  await dav('GET', `${base}/hello.txt`).set('If-None-Match', etag).expect(304);

  // Names are matched case-insensitively.
  const upper = await dav('GET', `${base}/HELLO.TXT`).expect(200);
  assert.equal(upper.text, content.toString());
  await dav('GET', `${base}/missing.txt`).expect(404);
});

test('PUT overwrite keeps a version; X-OC-Mtime sets the file time', async () => {
  await dav('PUT', `${base}/notes.md`).send('first').expect(201);
  const replaced = await dav('PUT', `${base}/notes.md`).set('X-OC-Mtime', '1700000000').send('second').expect(204);
  assert.equal(replaced.headers['x-oc-mtime'], 'accepted');
  const file = (await apiChildren(alice)).find((item) => item.name === 'notes.md');
  const versions = await alice.agent.get(`/api/files/${file.id}/versions`).expect(200);
  assert.equal(versions.body.versions.length, 1);
  assert.equal(file.modifiedAt, '2023-11-14 22:13:20');

  const res = await propfind(`${base}/notes.md`, { depth: '0' }).expect(207);
  assert.match(res.text, /<d:getlastmodified>Tue, 14 Nov 2023 22:13:20 GMT<\/d:getlastmodified>/);

  // Preconditions.
  await dav('PUT', `${base}/notes.md`).set('If-None-Match', '*').send('x').expect(412);
  await dav('PUT', `${base}/notes.md`).set('If-Match', '"0000"').send('x').expect(412);
  await dav('PUT', `${base}/new.md`).set('If-Match', '*').send('x').expect(412);
  await dav('PUT', `${base}/no/such/folder.txt`).send('x').expect(409);
  await dav('PUT', `${base}/`).send('x').expect(405);
});

test('unicode names round-trip percent-encoded, NFD matches NFC', async () => {
  const name = 'Grüße 日本 #1%.txt';
  const encoded = encodeURIComponent(name);
  await dav('PUT', `${base}/${encoded}`).send('unicode').expect(201);
  const stored = (await apiChildren(alice)).find((item) => item.name === name.normalize('NFC'));
  assert.ok(stored);
  // macOS sends decomposed forms.
  const nfd = encodeURIComponent(name.normalize('NFD'));
  assert.equal(await read(`${base}/${nfd}`), 'unicode');
  const listing = await propfind(`${base}/`).expect(207);
  assert.ok(hrefs(listing.text).includes(`${base}/${encoded}`));
  await dav('GET', `${base}/%E0%A4%A`).expect(400);
});

test('PROPFIND depth 0 and 1, named props, propname; infinity is refused', async () => {
  const zero = await propfind(`${base}/`, { depth: '0' }).expect(207);
  assert.deepEqual(hrefs(zero.text), [`${base}/`]);
  assert.match(zero.text, /<d:resourcetype><d:collection\/><\/d:resourcetype>/);
  assert.match(zero.headers['content-type'], /application\/xml/);

  const one = await propfind(`${base}/`).expect(207);
  const list = hrefs(one.text);
  assert.ok(list.includes(`${base}/hello.txt`));
  const file = responseFor(one.text, `${base}/hello.txt`);
  assert.match(file, /<d:getcontentlength>18<\/d:getcontentlength>/);
  assert.match(file, /<d:getcontenttype>text\/plain<\/d:getcontenttype>/);
  assert.match(file, /<d:getetag>&quot;[0-9a-f]{64}&quot;<\/d:getetag>/);
  assert.match(file, /<d:resourcetype\/>/);

  const infinity = await propfind(`${base}/`, { depth: 'infinity' }).expect(403);
  assert.match(infinity.text, /propfind-finite-depth/);

  const named = await propfind(`${base}/`, {
    depth: '0',
    body: `<?xml version="1.0"?>
      <d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns" xmlns:z="urn:x-test">
        <d:prop><d:quota-used-bytes/><d:quota-available-bytes/><oc:fileid/><oc:favorite/><oc:size/>
        <nc:has-preview/><z:custom/><d:getcontentlength/></d:prop>
      </d:propfind>`,
  }).expect(207);
  assert.match(named.text, /<d:quota-used-bytes>\d+<\/d:quota-used-bytes>/);
  assert.match(named.text, /<d:quota-available-bytes>\d+<\/d:quota-available-bytes>/);
  assert.match(named.text, /<oc:fileid>[0-9a-f-]{36}<\/oc:fileid>/);
  assert.match(named.text, /<oc:favorite>0<\/oc:favorite>/);
  assert.match(named.text, /<oc:size>\d+<\/oc:size>/);
  assert.match(named.text, /<nc:has-preview>false<\/nc:has-preview>/);
  // Unknown props and a folder's content length come back as 404.
  assert.match(named.text, /<x:custom xmlns:x="urn:x-test"\/><d:getcontentlength\/><\/d:prop><d:status>HTTP\/1.1 404 Not Found/);

  const names = await propfind(`${base}/hello.txt`, {
    depth: '0',
    body: '<propfind xmlns="DAV:"><propname/></propfind>',
  }).expect(207);
  assert.match(names.text, /<d:getetag\/>/);

  await propfind(`${base}/`, { body: '<d:propfind xmlns:d="DAV:"><d:prop>' }).expect(400);
  await propfind(`${base}/`, {
    body: '<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "aaaa">]><d:propfind xmlns:d="DAV:"><d:allprop/></d:propfind>',
  }).expect(400);
  const huge = `<d:propfind xmlns:d="DAV:"><!--${'x'.repeat(1024 * 1024)}--><d:allprop/></d:propfind>`;
  await propfind(`${base}/`, { body: huge }).expect(413);
  await propfind(`${base}/missing/`).expect(404);
});

test('MKCOL creates folders; existing, missing parent and bodies are refused', async () => {
  await dav('MKCOL', `${base}/Docs`).expect(201);
  await dav('MKCOL', `${base}/Docs/Sub`).expect(201);
  await dav('MKCOL', `${base}/docs`).expect(405);
  await dav('MKCOL', `${base}/Nope/Sub`).expect(409);
  await dav('MKCOL', `${base}/WithBody`).set('Content-Type', 'application/xml').send('<x/>').expect(415);
  const listing = await propfind(`${base}/Docs/`).expect(207);
  assert.ok(hrefs(listing.text).includes(`${base}/Docs/Sub/`));
});

test('MOVE renames and moves; Overwrite, self and subfolder cases', async () => {
  await dav('PUT', `${base}/Docs/a.txt`).send('A').expect(201);
  await dav('MOVE', `${base}/Docs/a.txt`).set('Destination', `http://localhost${base}/Docs/Sub/b.txt`).expect(201);
  await dav('GET', `${base}/Docs/a.txt`).expect(404);
  assert.equal(await read(`${base}/Docs/Sub/b.txt`), 'A');

  // Onto itself: 403. A case-only rename is a rename.
  await dav('MOVE', `${base}/Docs/Sub/b.txt`).set('Destination', `${base}/Docs/Sub/b.txt`).expect(403);
  await dav('MOVE', `${base}/Docs/Sub/b.txt`).set('Destination', `${base}/Docs/Sub/B.txt`).expect(201);
  const sub = await propfind(`${base}/Docs/Sub/`).expect(207);
  assert.ok(hrefs(sub.text).includes(`${base}/Docs/Sub/B.txt`));

  // A folder into its own subfolder.
  await dav('MOVE', `${base}/Docs`).set('Destination', `${base}/Docs/Sub/Docs`).expect(409);
  // Missing destination parent, missing header, other account.
  await dav('MOVE', `${base}/Docs/Sub/B.txt`).set('Destination', `${base}/Nope/B.txt`).expect(409);
  await dav('MOVE', `${base}/Docs/Sub/B.txt`).expect(400);
  await dav('MOVE', `${base}/Docs/Sub/B.txt`).set('Destination', `/dav/files/${bob.username}/x.txt`).expect(403);

  // Overwrite F onto an existing file fails; T replaces it and keeps the
  // target's history (the KeePassXC / Obsidian "save to temp, move over" pattern).
  await dav('PUT', `${base}/Docs/target.kdbx`).send('v1').expect(201);
  await dav('PUT', `${base}/Docs/target.kdbx.tmp`).set('X-OC-Mtime', '1600000000').send('v2').expect(201);
  await dav('MOVE', `${base}/Docs/target.kdbx.tmp`).set('Destination', `${base}/Docs/target.kdbx`).set('Overwrite', 'F').expect(412);
  await dav('MOVE', `${base}/Docs/target.kdbx.tmp`).set('Destination', `${base}/Docs/target.kdbx`).expect(204);
  assert.equal(await read(`${base}/Docs/target.kdbx`), 'v2');
  await dav('GET', `${base}/Docs/target.kdbx.tmp`).expect(404);
  const docs = (await apiChildren(alice)).find((item) => item.name === 'Docs');
  const target = (await apiChildren(alice, docs.id)).find((item) => item.name === 'target.kdbx');
  assert.equal(target.modifiedAt, '2020-09-13 12:26:40');
  const versions = await alice.agent.get(`/api/files/${target.id}/versions`).expect(200);
  assert.equal(versions.body.versions.length, 1);

  // A file over a folder: the folder goes to the trash.
  await dav('MKCOL', `${base}/Docs/Folder`).expect(201);
  await dav('PUT', `${base}/Docs/plain.txt`).send('P').expect(201);
  await dav('MOVE', `${base}/Docs/plain.txt`).set('Destination', `${base}/Docs/Folder`).expect(204);
  assert.equal(await read(`${base}/Docs/Folder`), 'P');
  const trash = await alice.agent.get('/api/trash').expect(200);
  assert.ok(trash.body.items.some((item) => item.name === 'Folder'));

  // The alias spellings work as Destination too.
  await dav('MOVE', `${base}/Docs/Folder`).set('Destination', `/remote.php/webdav/Docs/plain.txt`).expect(201);
});

test('COPY files and folders, deep and shallow', async () => {
  await dav('COPY', `${base}/Docs`).set('Destination', `${base}/Docs Copy`).expect(201);
  assert.equal(await read(`${base}/Docs%20Copy/Sub/B.txt`), 'A');
  assert.equal(await read(`${base}/Docs/Sub/B.txt`), 'A');
  await dav('COPY', `${base}/Docs`).set('Destination', `${base}/Docs Copy`).set('Overwrite', 'F').expect(412);
  await dav('COPY', `${base}/Docs`).set('Destination', `${base}/Shallow`).set('Depth', '0').expect(201);
  const shallow = await propfind(`${base}/Shallow/`).expect(207);
  assert.equal(hrefs(shallow.text).length, 1);
  // COPY over an existing file adds a version to it.
  await dav('COPY', `${base}/hello.txt`).set('Destination', `${base}/notes.md`).expect(204);
  assert.equal(await read(`${base}/notes.md`), 'Hello WebDAV world');
  await dav('COPY', `${base}/hello.txt`).set('Destination', `${base}/hello.txt`).expect(403);
});

test('DELETE moves to the trash', async () => {
  await dav('PUT', `${base}/bye.txt`).send('bye').expect(201);
  await dav('DELETE', `${base}/bye.txt`).expect(204);
  await dav('GET', `${base}/bye.txt`).expect(404);
  await dav('DELETE', `${base}/bye.txt`).expect(404);
  await dav('DELETE', `${base}/`).expect(403);
  const trash = await alice.agent.get('/api/trash').expect(200);
  assert.ok(trash.body.items.some((item) => item.name === 'bye.txt'));
});

test('PROPPATCH sets the modified time, accepts other props, rejects bad dates', async () => {
  const body = (value) => `<?xml version="1.0"?>
    <d:propertyupdate xmlns:d="DAV:" xmlns:z="urn:schemas-microsoft-com:">
      <d:set><d:prop><d:getlastmodified>${value}</d:getlastmodified><z:Win32FileAttributes>00000020</z:Win32FileAttributes></d:prop></d:set>
      <d:remove><d:prop><z:Win32CreationTime/></d:prop></d:remove>
    </d:propertyupdate>`;
  const ok = await dav('PROPPATCH', `${base}/hello.txt`).set('Content-Type', 'application/xml')
    .send(body('Wed, 01 Jan 2020 10:00:00 GMT')).expect(207);
  assert.match(ok.text, /<d:status>HTTP\/1.1 200 OK<\/d:status>/);
  assert.match(ok.text, /<x:Win32FileAttributes xmlns:x="urn:schemas-microsoft-com:"\/>/);
  assert.doesNotMatch(ok.text, /404|409|424/);
  const after = await propfind(`${base}/hello.txt`, { depth: '0' }).expect(207);
  assert.match(after.text, /Wed, 01 Jan 2020 10:00:00 GMT/);

  // rclone's form: DAV:lastmodified in Unix seconds.
  await dav('PROPPATCH', `${base}/hello.txt`).send(
    '<d:propertyupdate xmlns:d="DAV:"><d:set><d:prop><d:lastmodified>1600000000</d:lastmodified></d:prop></d:set></d:propertyupdate>',
  ).expect(207);
  const rclone = await propfind(`${base}/hello.txt`, { depth: '0' }).expect(207);
  assert.match(rclone.text, /Sun, 13 Sep 2020 12:26:40 GMT/);

  const bad = await dav('PROPPATCH', `${base}/hello.txt`).send(body('not a date')).expect(207);
  assert.match(bad.text, /409 Conflict/);
  assert.match(bad.text, /424 Failed Dependency/);
  await dav('PROPPATCH', `${base}/missing.txt`).send(body('Wed, 01 Jan 2020 10:00:00 GMT')).expect(404);
});

test('LOCK / UNLOCK: tokens guard WebDAV writes, never the API', async () => {
  const lockBody = '<?xml version="1.0"?><d:lockinfo xmlns:d="DAV:"><d:lockscope><d:exclusive/></d:lockscope>'
    + '<d:locktype><d:write/></d:locktype><d:owner><d:href>Finder</d:href></d:owner></d:lockinfo>';
  const locked = await dav('LOCK', `${base}/hello.txt`).set('Timeout', 'Second-600').send(lockBody).expect(200);
  const lockToken = locked.headers['lock-token'];
  assert.match(lockToken, /^<opaquelocktoken:[0-9a-f-]{36}>$/);
  assert.match(locked.text, /<d:timeout>Second-600<\/d:timeout>/);
  assert.match(locked.text, /<d:owner><d:href>Finder<\/d:href><\/d:owner>/);

  await dav('PUT', `${base}/hello.txt`).send('blocked').expect(423);
  await dav('DELETE', `${base}/hello.txt`).expect(423);
  await dav('LOCK', `${base}/hello.txt`).send(lockBody).expect(423);
  await dav('PUT', `${base}/hello.txt`).set('If', `(${lockToken})`).send('with token').expect(204);

  const discovery = await propfind(`${base}/hello.txt`, { depth: '0' }).expect(207);
  assert.ok(discovery.text.includes(lockToken.slice(1, -1)));

  // The REST API ignores WebDAV locks.
  const file = (await apiChildren(alice)).find((item) => item.name === 'hello.txt');
  await upload(alice.agent, 'hello.txt', 'via api', { conflict: 'replace' }).expect(201);
  await alice.agent.patch(`/api/nodes/${file.id}`).send({ name: 'hello.txt' }).expect(200);

  // Refresh, then unlock.
  const refreshed = await dav('LOCK', `${base}/hello.txt`).set('If', `(${lockToken})`).set('Timeout', 'Second-60').expect(200);
  assert.match(refreshed.text, /Second-60/);
  await dav('UNLOCK', `${base}/hello.txt`).set('Lock-Token', '<opaquelocktoken:nope>').expect(409);
  await dav('UNLOCK', `${base}/hello.txt`).set('Lock-Token', lockToken).expect(204);
  await dav('PUT', `${base}/hello.txt`).send('free again').expect(204);

  // Locking an unmapped URL creates an empty file; a depth-infinity folder
  // lock covers what is inside.
  const fresh = await dav('LOCK', `${base}/Docs/new-doc.docx`).send(lockBody).expect(201);
  assert.equal(await read(`${base}/Docs/new-doc.docx`), '');
  await dav('UNLOCK', `${base}/Docs/new-doc.docx`).set('Lock-Token', fresh.headers['lock-token']).expect(204);
  const folderLock = await dav('LOCK', `${base}/Docs`).set('Depth', 'infinity').send(lockBody).expect(200);
  await dav('PUT', `${base}/Docs/inside.txt`).send('x').expect(423);
  await dav('MOVE', `${base}/hello.txt`).set('Destination', `${base}/Docs/hello.txt`).expect(423);
  await dav('UNLOCK', `${base}/Docs`).set('Lock-Token', folderLock.headers['lock-token']).expect(204);
  await dav('LOCK', `${base}/Nope/x.txt`).send(lockBody).expect(409);
});

test('quota exceeded is 507 Insufficient Storage', async () => {
  const quotaUser = await registerUser('quota');
  const { token: quotaToken } = await createToken(quotaUser);
  db.prepare('UPDATE users SET quota_bytes = ? WHERE id = ?').run(10, quotaUser.user.id);
  const res = await request(app).put(`/dav/files/${quotaUser.username}/big.bin`)
    .auth(quotaUser.username, quotaToken).send(Buffer.alloc(100)).expect(507);
  assert.match(res.text, /Not enough storage/);
  // Finder streams chunked and announces the size in X-Expected-Entity-Length.
  await request(app).put(`/dav/files/${quotaUser.username}/big2.bin`)
    .auth(quotaUser.username, quotaToken).set('X-Expected-Entity-Length', '100').send(Buffer.alloc(1)).expect(507);
});

test('vault folders are hidden', async () => {
  await alice.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Tresor',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201);
  const listing = await propfind(`${base}/`).expect(207);
  assert.ok(!listing.text.includes('Tresor'));
  await propfind(`${base}/Tresor/`).expect(404);
  await dav('PUT', `${base}/Tresor/leak.txt`).send('x').expect(409);
});

test('Nextcloud aliases and discovery', async () => {
  const legacy = await propfind('/remote.php/webdav/', { depth: '1' }).expect(207);
  assert.ok(hrefs(legacy.text).includes('/remote.php/webdav/hello.txt'));
  const nc = await propfind(`/remote.php/dav/files/${alice.username}/`, { depth: '0' }).expect(207);
  assert.deepEqual(hrefs(nc.text), [`/remote.php/dav/files/${alice.username}/`]);
  const wellKnown = await request(app).propfind('/.well-known/webdav').expect(301);
  assert.equal(wellKnown.headers.location, '/remote.php/webdav/');
  // Unknown methods.
  const post = await dav('POST', `${base}/hello.txt`).expect(405);
  assert.match(post.headers.allow, /PROPFIND/);
});

test('failed sign-ins are rate limited per username', async () => {
  const target = await registerUser('limited');
  const { token: good } = await createToken(target);
  for (let attempt = 0; attempt < 30; attempt += 1) {
    await dav('PROPFIND', `/dav/files/${target.username}/`, { auth: [target.username, 'nc_bad'] }).expect(401);
  }
  const limited = await dav('PROPFIND', `/dav/files/${target.username}/`, { auth: [target.username, 'nc_bad'] }).expect(429);
  assert.match(limited.text, /Too many/);
  // Even the right password waits now; other accounts are not affected.
  await dav('PROPFIND', `/dav/files/${target.username}/`, { auth: [target.username, good] }).expect(429);
  await propfind(`${base}/`, { depth: '0' }).expect(207);
});
