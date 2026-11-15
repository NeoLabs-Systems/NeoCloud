'use strict';

// Connecting, editing and testing a cloud storage. A connection is saved
// only after it was opened and its root listed, so a typo in a key or a
// wrong address shows up at once and never as a broken folder later.

const { httpError } = require('../../utils/http_error');
const { testConnection } = require('./connection');
const { draftMount, getMountRow, insertMount, saveMount } = require('./mounts');

function testFailed(result) {
  return httpError(422, result.error, 'EXTERNAL_TEST_FAILED');
}

// Remembers the server's identity (SFTP host key) the first time it is seen.
function withHostKey(draft, result) {
  if (result.hostKey && !draft.config.hostKey) draft.config.hostKey = result.hostKey;
  return draft;
}

/** Tests the settings in `body` (secrets left blank come from connection `body.id`). */
async function test(userId, body = {}) {
  const previous = body.id ? getMountRow(userId, body.id) : null;
  const draft = draftMount({ name: 'test', ...body }, previous);
  const result = await testConnection(userId, draft.provider, draft.config);
  return result.ok ? { ok: true, items: result.items } : { ok: false, error: result.error, code: result.code };
}

async function connect(userId, body = {}) {
  const draft = draftMount(body);
  const result = await testConnection(userId, draft.provider, draft.config);
  if (!result.ok) throw testFailed(result);
  return insertMount(userId, withHostKey(draft, result));
}

async function update(userId, mountId, body = {}) {
  const row = getMountRow(userId, mountId);
  const draft = draftMount(body, row);
  if (body.config) {
    const result = await testConnection(userId, draft.provider, draft.config);
    if (!result.ok) throw testFailed(result);
    withHostKey(draft, result);
  }
  return saveMount(userId, row, draft);
}

module.exports = { connect, test, update };
