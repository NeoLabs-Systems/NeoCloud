'use strict';

// The remote server decides how big a tree an import scan sees. A server
// that never stops reporting folders must fail the job, not grow
// import_items forever.

process.env.NEOCLOUD_IMPORT_ALLOW_PRIVATE = 'true';

const test = require('node:test');
const assert = require('node:assert/strict');
const http = require('http');
const { cleanup, registerUser } = require('../helpers/app');
const db = require('../../server/db/database');
const runner = require('../../server/services/import/runner');

let owner;
let server;
let childName = 'a';
let propfinds = 0;
let wide = false;

// Every folder holds exactly one subfolder, forever; or, when `wide`, the
// root holds 30 folders of 9000 files each.
function endlessTree(req, res) {
  if (req.method !== 'PROPFIND') return res.writeHead(405).end();
  propfinds += 1;
  const self = new URL(req.url, 'http://x').pathname.replace(/\/?$/, '/');
  const folder = (href) => `<d:response><d:href>${href}</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`;
  const file = (href) => `<d:response><d:href>${href}</d:href></d:response>`;
  let children = '';
  if (req.headers.depth === '1' && !wide) children = folder(`${self}${encodeURIComponent(childName)}/`);
  else if (req.headers.depth === '1' && self === '/dav/') children = Array.from({ length: 30 }, (_, i) => folder(`${self}f${i}/`)).join('');
  else if (req.headers.depth === '1') children = Array.from({ length: 9000 }, (_, i) => file(`${self}${i}`)).join('');
  return res.writeHead(207, { 'Content-Type': 'application/xml' }).end(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">${folder(self)}${children}</d:multistatus>`);
}

test.before(async () => {
  owner = await registerUser('importer');
  server = http.createServer(endlessTree);
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
});

test.after(async () => {
  await runner.stopImports();
  server.closeAllConnections();
  server.close();
  cleanup();
});

async function runImport() {
  const job = (await owner.agent.post('/api/imports').send({
    source: 'webdav',
    url: `http://127.0.0.1:${server.address().port}/dav/`,
    username: 'u',
    password: 'p',
  }).expect(201)).body;
  const deadline = Date.now() + 20000;
  for (;;) {
    const res = await owner.agent.get(`/api/imports/${job.id}`).expect(200);
    if (['failed', 'done'].includes(res.body.status)) return res.body;
    if (Date.now() > deadline) throw new Error(`import stayed ${res.body.status}`);
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

function itemCount(jobId) {
  return db.prepare('SELECT COUNT(*) AS n FROM import_items WHERE job_id = ?').get(jobId).n;
}

test('an endlessly nested source fails the job at 64 levels', async () => {
  const job = await runImport();
  assert.equal(job.status, 'failed');
  assert.match(job.errors.at(-1).message, /deeper than 64 folders/);
  assert.ok(propfinds <= 70, `${propfinds} listings`);
  assert.ok(itemCount(job.id) <= 64);
});

test('paths longer than 4096 characters fail the job', async () => {
  childName = 'n'.repeat(1000);
  propfinds = 0;
  const job = await runImport();
  assert.equal(job.status, 'failed');
  assert.match(job.errors.at(-1).message, /longer than 4096 characters/);
  assert.ok(propfinds <= 8, `${propfinds} listings`);
});

test('a source with more than 250000 entries fails the job', async () => {
  wide = true;
  const job = await runImport();
  assert.equal(job.status, 'failed');
  assert.match(job.errors.at(-1).message, /more than 250000/);
  assert.ok(itemCount(job.id) <= 250000);
});
