'use strict';

// Small JSON documents the server keeps between restarts, one file each in
// STATE_DIR: blob scrub progress, backup verification results. Never file
// contents, names or secrets. Written atomically; a missing or unreadable
// file reads as the fallback.

const fs = require('fs');
const path = require('path');
const { STATE_DIR } = require('../../runtime/paths');
const { writeAtomicFile } = require('../../lib/setup/atomic_file');

const NAME_PATTERN = /^[a-z][a-z0-9-]*$/;

function statePath(name) {
  if (!NAME_PATTERN.test(name)) throw new Error(`Invalid state name: ${name}`);
  return path.join(STATE_DIR, `${name}.json`);
}

function readState(name, fallback) {
  try {
    return JSON.parse(fs.readFileSync(statePath(name), 'utf8'));
  } catch {
    return fallback;
  }
}

function writeState(name, value) {
  writeAtomicFile(statePath(name), JSON.stringify(value), { mode: 0o600 });
}

module.exports = { readState, writeState };
