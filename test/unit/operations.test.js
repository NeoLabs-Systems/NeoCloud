'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

// middleware.js opens the database: never the real ~/.neocloud.
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-unit-'));
process.env.NEOCLOUD_HOME = home;
test.after(() => fs.rmSync(home, { recursive: true, force: true }));

const Database = require('better-sqlite3');
const { latestSchemaVersion, runVersionedMigrations } = require('../../lib/schema_migrations');
const { parseTrustProxy, readHttpRuntimeConfig } = require('../../server/config/http');
const { isUploadRequest } = require('../../server/http/middleware');

test('migrations refuse a database from a newer NeoCloud', () => {
  const db = new Database(':memory:');
  try {
    runVersionedMigrations(db);
    assert.equal(db.pragma('user_version', { simple: true }), latestSchemaVersion());
    // Running again is a no-op.
    runVersionedMigrations(db);

    db.pragma(`user_version = ${latestSchemaVersion() + 1}`);
    assert.throws(() => runVersionedMigrations(db), (err) => err.code === 'SCHEMA_TOO_NEW' && /neocloud rollback/.test(err.message));
    assert.equal(db.pragma('user_version', { simple: true }), latestSchemaVersion() + 1);
  } finally {
    db.close();
  }
});

test('TRUST_PROXY takes a boolean, a hop count or a list of addresses', () => {
  assert.equal(parseTrustProxy('', false), false);
  // An https PUBLIC_URL alone trusts only proxies on this host or a private
  // network, never a bare hop count that any direct client could use.
  assert.deepEqual(parseTrustProxy('', true), ['loopback', 'linklocal', 'uniquelocal']);
  assert.equal(parseTrustProxy('true', false), 1);
  assert.equal(parseTrustProxy('off', true), false);
  assert.equal(parseTrustProxy('0', true), false);
  assert.equal(parseTrustProxy('2', false), 2);
  assert.deepEqual(parseTrustProxy(' 10.0.0.2, loopback ,', false), ['10.0.0.2', 'loopback']);
  assert.deepEqual(readHttpRuntimeConfig({ PUBLIC_URL: 'https://cloud.example.com' }).trustProxy, ['loopback', 'linklocal', 'uniquelocal']);
  assert.equal(readHttpRuntimeConfig({ PUBLIC_URL: 'https://cloud.example.com', TRUST_PROXY: '1' }).trustProxy, 1);
  assert.equal(readHttpRuntimeConfig({ PUBLIC_URL: 'http://192.168.1.2:3344' }).trustProxy, false);
});

test('the https default trusts proxies on this host or a private network, never a direct client', () => {
  const express = require('express');
  const app = express();
  app.set('trust proxy', readHttpRuntimeConfig({ PUBLIC_URL: 'https://cloud.example.com' }).trustProxy);
  const trusted = app.get('trust proxy fn');
  for (const peer of ['127.0.0.1', '::1', '172.17.0.1', '10.1.2.3', '192.168.1.5', '169.254.1.1']) {
    assert.equal(trusted(peer, 0), true, peer);
  }
  // A client reaching the port straight from the internet can't set its own
  // X-Forwarded-For.
  for (const peer of ['203.0.113.9', '2001:db8::1']) assert.equal(trusted(peer, 0), false, peer);
});

test('shutdown turns away new uploads only', () => {
  const upload = (method, url) => isUploadRequest({ method, originalUrl: url });
  assert.equal(upload('PUT', '/api/upload?parentId=root&name=a'), true);
  assert.equal(upload('POST', '/api/uploads'), true);
  assert.equal(upload('PUT', '/api/uploads/abc/parts/3'), true);
  assert.equal(upload('PUT', '/api/public/shares/tok/upload?name=x'), true);
  assert.equal(upload('POST', '/api/uploads/abc/complete'), false);
  assert.equal(upload('GET', '/api/files/abc/content'), false);
  assert.equal(upload('GET', '/api/upload'), false);
});
