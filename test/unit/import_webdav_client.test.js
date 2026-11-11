'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const dns = require('dns');
const fs = require('fs');
const http = require('http');
const os = require('os');
const path = require('path');

// network_guard.js reads the admin flag from the database: never the real ~/.neocloud.
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-unit-'));
process.env.NEOCLOUD_HOME = home;
delete process.env.NEOCLOUD_IMPORT_ALLOW_PRIVATE;
test.after(() => fs.rmSync(home, { recursive: true, force: true }));

const { createWebdavClient } = require('../../server/services/import/webdav_client');
const { importNetworkFor } = require('../../server/services/import/network_guard');

function listen(handler) {
  const server = http.createServer(handler);
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(server)));
}

function close(server) {
  server.closeAllConnections();
  return new Promise((resolve) => server.close(resolve));
}

function client(davRoot, options = {}) {
  return createWebdavClient({ davRoot, username: 'u', password: 'p', retries: 0, ...options });
}

test('import: a name that rebinds to a private address after the check is refused at connect time', async () => {
  const seen = [];
  const server = await listen((req, res) => {
    seen.push(req.url);
    res.end('SECRET');
  });
  const realLookup = dns.lookup;
  // The per-hop check saw a public address; the connection's lookup now says loopback.
  dns.lookup = (hostname, options, callback) => {
    if (hostname !== 'rebind.test') return realLookup(hostname, options, callback);
    const answer = [{ address: '127.0.0.1', family: 4 }];
    return options.all ? callback(null, answer) : callback(null, answer[0].address, answer[0].family);
  };
  try {
    const davRoot = `http://rebind.test:${server.address().port}/`;
    const guarded = importNetworkFor(null);
    assert.ok(guarded.hostGuard && guarded.dispatcher);
    await assert.rejects(
      client(davRoot, { hostGuard: async () => {}, dispatcher: guarded.dispatcher }).download('secret.txt'),
      (err) => err.code === 'IMPORT_HOST_BLOCKED',
    );
    assert.deepEqual(seen, []);

    // Control: where private addresses are allowed, the same lookup connects.
    process.env.NEOCLOUD_IMPORT_ALLOW_PRIVATE = 'true';
    const open = importNetworkFor(null);
    assert.equal(open.hostGuard, null);
    const { stream } = await client(davRoot, open).download('secret.txt');
    const chunks = [];
    for await (const chunk of stream) chunks.push(chunk);
    assert.equal(Buffer.concat(chunks).toString(), 'SECRET');
  } finally {
    dns.lookup = realLookup;
    delete process.env.NEOCLOUD_IMPORT_ALLOW_PRIVATE;
    await close(server);
  }
});

test('import: credentials follow a redirect only to the same origin', async () => {
  const other = await listen((req, res) => {
    other.authorization = req.headers.authorization || null;
    res.end('moved content');
  });
  const origin = await listen((req, res) => {
    res.writeHead(302, { Location: `http://127.0.0.1:${other.address().port}/file.txt` }).end();
  });
  try {
    // Same host, another port: a different origin.
    await client(`http://127.0.0.1:${origin.address().port}/`).download('file.txt');
    assert.equal(other.authorization, null);
  } finally {
    await close(origin);
    await close(other);
  }
});

test('import: listings are capped in size and parsed in linear time', async () => {
  let mode = 'huge';
  const server = await listen((req, res) => {
    res.writeHead(207, { 'Content-Type': 'application/xml' });
    if (mode === 'huge') {
      // Endless chunked body: the client must stop reading at its cap.
      const chunk = Buffer.alloc(1024 * 1024, 'x');
      const pump = () => {
        while (res.write(chunk)) { /* fill the socket */ }
      };
      res.on('drain', pump);
      res.on('error', () => {});
      pump();
      return;
    }
    res.end(`<d:multistatus xmlns:d="DAV:">${'<d:response>'.repeat(90000)}`);
  });
  try {
    const davRoot = `http://127.0.0.1:${server.address().port}/`;
    await assert.rejects(client(davRoot).connect(), (err) => err.code === 'REMOTE_ERROR' && /too large/.test(err.message));
    mode = 'adversarial';
    const started = Date.now();
    await assert.rejects(client(davRoot).connect(), (err) => err.code === 'REMOTE_NOT_WEBDAV');
    assert.ok(Date.now() - started < 1000, 'took too long');
  } finally {
    await close(server);
  }
});
