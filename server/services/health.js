'use strict';

// GET /api/health: what load balancers, Docker and `neocloud start` poll.
// Cheap on purpose -- one trivial query and two access checks -- and
// unauthenticated, so it says nothing beyond ok / not ok per part.

const fs = require('fs');
const db = require('../db/database');
const { DATA_DIR, STORAGE_DIR } = require('../../runtime/paths');

function databaseState() {
  try {
    db.prepare('SELECT 1').get();
    return 'ok';
  } catch {
    return 'error';
  }
}

function storageState() {
  try {
    for (const directory of [DATA_DIR, STORAGE_DIR]) fs.accessSync(directory, fs.constants.W_OK);
    return 'ok';
  } catch {
    return 'error';
  }
}

function checkHealth() {
  const result = { db: databaseState(), storage: storageState() };
  const healthy = result.db === 'ok' && result.storage === 'ok';
  return { status: healthy ? 'ok' : 'degraded', ...result };
}

module.exports = { checkHealth };
