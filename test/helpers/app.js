'use strict';

// Each test file runs in its own process (node --test), so pointing the
// runtime at a fresh temp directory before the first require gives every
// file its own database and storage.

const crypto = require('crypto');
const fs = require('fs');
const os = require('os');
const path = require('path');

const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-test-'));
process.env.NEOCLOUD_HOME = home;
process.env.NEOCLOUD_MASTER_KEY = crypto.randomBytes(32).toString('hex');
process.env.SESSION_SECRET = crypto.randomBytes(32).toString('hex');
process.env.NODE_ENV = 'test';

const request = require('supertest');
const { createApp } = require('../../server/app');

const { app } = createApp({ publicUrl: null, secureCookies: false, trustProxy: false });

const STRONG_PASSWORD = 'Plum-Orbit-73!quartz';
let userCounter = 0;

/** Registers a fresh account and returns a cookie-carrying agent for it. */
async function registerUser(prefix = 'user') {
  userCounter += 1;
  const username = `${prefix}${userCounter}`;
  const agent = request.agent(app);
  const res = await agent.post('/api/auth/register').send({
    username,
    email: `${username}@example.com`,
    password: STRONG_PASSWORD,
  });
  if (res.status !== 200) throw new Error(`register failed: ${res.status} ${JSON.stringify(res.body)}`);
  return { agent, user: res.body.user, username };
}

function upload(agent, name, content, { parentId = 'root', conflict } = {}) {
  const query = new URLSearchParams({ parentId, name, ...(conflict ? { conflict } : {}) });
  return agent
    .put(`/api/upload?${query}`)
    .set('Content-Type', 'application/octet-stream')
    .send(Buffer.isBuffer(content) ? content : Buffer.from(content));
}

function cleanup() {
  fs.rmSync(home, { recursive: true, force: true });
}

module.exports = { STRONG_PASSWORD, app, cleanup, home, registerUser, request, upload };
