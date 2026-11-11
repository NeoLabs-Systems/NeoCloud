'use strict';

// `neocloud update [--tag <version>]` and `neocloud rollback` for git installs.
//
// update: encrypted database snapshot first, then pull (or check out the
// tag), `npm ci`, restart and wait for /api/health. If the new version does
// not come up healthy it rolls back on its own.
// rollback: stops the server, resets the code to the commit before the last
// update, reinstalls its dependencies and, if the update changed the database
// schema, restores the snapshot taken before it. Writes made after the update
// are lost only in that last case.

const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const Database = require('better-sqlite3');
const { APP_DIR, DATABASE_FILE, LOCAL_BACKUP_DIR } = require('../../runtime/paths');
const { writeAtomicFile } = require('../setup/atomic_file');
const ui = require('./ui');

const STATE_FILE = path.join(LOCAL_BACKUP_DIR, 'last-update.json');
const HEALTH_TIMEOUT_MS = 60 * 1000;

function run(command, args, { capture = false, allowFail = false } = {}) {
  const result = spawnSync(command, args, {
    cwd: APP_DIR,
    stdio: capture ? ['ignore', 'pipe', 'inherit'] : 'inherit',
    encoding: 'utf8',
    shell: process.platform === 'win32',
  });
  if (result.status !== 0) {
    if (allowFail) return null;
    throw new Error(`${command} ${args.join(' ')} failed.`);
  }
  return capture ? result.stdout.trim() : '';
}

function git(args, options) {
  return run('git', ['-C', APP_DIR, ...args], options);
}

function installDependencies() {
  run('npm', ['ci', '--omit=dev', '--no-audit', '--no-fund']);
}

function assertGitInstall() {
  if (!fs.existsSync(path.join(APP_DIR, '.git'))) {
    throw new Error('This install is not a git checkout. With Docker, change the image tag and recreate the container; otherwise reinstall.');
  }
}

function schemaVersionOfDatabase() {
  const db = new Database(DATABASE_FILE, { readonly: true, fileMustExist: true });
  try {
    return db.pragma('user_version', { simple: true });
  } finally {
    db.close();
  }
}

function readUpdateState() {
  try {
    return JSON.parse(fs.readFileSync(STATE_FILE, 'utf8'));
  } catch {
    return null;
  }
}

function writeUpdateState(state) {
  writeAtomicFile(STATE_FILE, JSON.stringify(state, null, 2), { mode: 0o600 });
}

/**
 * Back to `state.previousCommit`. The snapshot code is loaded before the
 * reset: afterwards the files on disk are the old version's.
 */
async function rollbackTo(state, { stopServer, startServer }) {
  const { restoreLocalSnapshot } = require('../../server/services/backup/local_snapshots');
  ui.title(`Rolling back to ${state.previousCommit.slice(0, 10)}`);
  await stopServer();
  git(['reset', '--hard', state.previousCommit]);
  installDependencies();
  if (schemaVersionOfDatabase() !== state.schemaVersion) {
    ui.info('The update changed the database schema; restoring the snapshot taken before it.');
    const { previous } = await restoreLocalSnapshot(state.snapshot);
    if (previous) ui.info(`The database as the new version left it is at ${previous}.`);
  } else {
    ui.info('Database schema unchanged; keeping the current data.');
  }
  writeUpdateState({ ...state, rolledBackAt: new Date().toISOString() });
  if (await startServer(HEALTH_TIMEOUT_MS)) {
    ui.ok('Rolled back and running.');
  } else {
    throw new Error('Rolled back, but the server does not answer. See `neocloud logs`.');
  }
}

async function runUpdate({ tag, stopServer, startServer }) {
  assertGitInstall();
  if (git(['status', '--porcelain', '--untracked-files=no'], { capture: true })) {
    throw new Error(`${APP_DIR} has local changes; commit or discard them before updating.`);
  }
  const previousCommit = git(['rev-parse', 'HEAD'], { capture: true });

  ui.info('Taking a database snapshot...');
  const { createLocalSnapshot } = require('../../server/services/backup/local_snapshots');
  const snapshot = await createLocalSnapshot('pre-update');
  ui.ok(`Snapshot ${snapshot.name}`);

  if (tag) {
    git(['fetch', '--tags', 'origin']);
    git(['checkout', '--detach', tag]);
  } else {
    if (!git(['symbolic-ref', '-q', '--short', 'HEAD'], { capture: true, allowFail: true })) {
      throw new Error('This install is pinned to a version tag. Update with `neocloud update --tag <version>`.');
    }
    git(['pull', '--ff-only']);
  }
  const currentCommit = git(['rev-parse', 'HEAD'], { capture: true });
  if (currentCommit === previousCommit) {
    ui.ok('Already up to date.');
    return;
  }
  const state = { previousCommit, currentCommit, snapshot: snapshot.file, schemaVersion: snapshot.schemaVersion, updatedAt: new Date().toISOString() };
  writeUpdateState(state);

  try {
    installDependencies();
    await stopServer();
    if (!(await startServer(HEALTH_TIMEOUT_MS))) throw new Error('The new version did not answer /api/health.');
  } catch (err) {
    ui.error(`Update failed: ${err.message}`);
    await rollbackTo(state, { stopServer, startServer });
    throw new Error('The update was rolled back. See `neocloud logs` for why the new version failed.');
  }
  ui.ok(`Updated ${previousCommit.slice(0, 10)} → ${currentCommit.slice(0, 10)}. Undo with ${ui.accent('neocloud rollback')}.`);
}

async function runRollback({ stopServer, startServer }) {
  assertGitInstall();
  const state = readUpdateState();
  if (!state) throw new Error('No update to roll back: `neocloud update` records one before it pulls.');
  if (state.rolledBackAt) throw new Error(`The last update was already rolled back (${state.rolledBackAt}).`);
  await rollbackTo(state, { stopServer, startServer });
}

module.exports = { runRollback, runUpdate };
