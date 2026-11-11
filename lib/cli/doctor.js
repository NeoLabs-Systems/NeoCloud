'use strict';

// `neocloud doctor [--full]`. Read-only: the database is opened read-only and
// never migrated, so it is safe next to a running server. --full runs
// integrity_check (reads every page) instead of quick_check.

const fs = require('fs');
const Database = require('better-sqlite3');
const {
  DATABASE_FILE,
  LOCAL_BACKUP_DIR,
  RUNTIME_HOME,
  STORAGE_DIR,
  isValidMasterKey,
} = require('../../runtime/paths');
const { latestSchemaVersion } = require('../schema_migrations');
const { formatBytes } = require('../../server/utils/bytes');
const { freeDiskBytes, minFreeDiskBytes } = require('../../server/utils/disk');
const { fetchHealth, port } = require('./server_probe');
const ui = require('./ui');

function createChecker() {
  let problems = 0;
  const check = (ok, good, bad) => {
    if (ok) {
      ui.ok(good);
    } else {
      ui.error(bad);
      problems += 1;
    }
  };
  return { check, problems: () => problems };
}

function checkDatabase(check, full) {
  if (!fs.existsSync(DATABASE_FILE)) {
    ui.info('No database yet; it is created on the first start.');
    return;
  }
  let db;
  try {
    db = new Database(DATABASE_FILE, { readonly: true, fileMustExist: true });
  } catch (err) {
    check(false, '', `Database could not be opened: ${err.message}`);
    return;
  }
  try {
    const current = db.pragma('user_version', { simple: true });
    const latest = latestSchemaVersion();
    if (current > latest) {
      check(false, '', `Database schema ${current} is newer than this NeoCloud (${latest}). Update again or run \`neocloud rollback\`.`);
    } else if (current < latest) {
      ui.warn(`Database schema ${current}; ${latest - current} migration(s) will run on the next start.`);
    } else {
      check(true, `Database schema ${current} (current)`, '');
    }

    const pragma = full ? 'integrity_check' : 'quick_check';
    const startedAt = Date.now();
    const results = db.pragma(pragma).map((row) => Object.values(row)[0]);
    const seconds = ((Date.now() - startedAt) / 1000).toFixed(1);
    const problems = results.filter((value) => value !== 'ok');
    check(problems.length === 0, `Database ${pragma} ok (${seconds} s)`, `Database ${pragma} failed (${seconds} s): ${problems.slice(0, 5).join('; ')}`);
    if (!full) ui.info(`${ui.accent('neocloud doctor --full')} runs the slower integrity_check.`);

    const walSize = fs.existsSync(`${DATABASE_FILE}-wal`) ? fs.statSync(`${DATABASE_FILE}-wal`).size : 0;
    ui.row('WAL', formatBytes(walSize));
  } finally {
    db.close();
  }
}

function newestLocalSnapshot() {
  try {
    return fs.readdirSync(LOCAL_BACKUP_DIR).filter((name) => name.endsWith('.db.enc')).sort().pop() || null;
  } catch {
    return null;
  }
}

async function runDoctor({ full = false, loadRuntimeEnv }) {
  ui.title('NeoCloud doctor');
  const { check, problems } = createChecker();
  const [major] = process.versions.node.split('.').map(Number);
  check(major >= 22, `Node.js ${process.version}`, `Node.js ${process.version} is too old; 22+ is required.`);
  try {
    loadRuntimeEnv();
    check(true, `Runtime directory ${RUNTIME_HOME}`, '');
  } catch (err) {
    check(false, '', err.message);
    return problems();
  }
  check(isValidMasterKey(process.env.NEOCLOUD_MASTER_KEY), 'Master key present', 'Master key missing or malformed');
  try {
    fs.accessSync(STORAGE_DIR, fs.constants.W_OK);
    check(true, `Storage writable: ${STORAGE_DIR}`, '');
  } catch {
    check(false, '', `Storage directory is not writable: ${STORAGE_DIR}`);
  }
  const free = freeDiskBytes();
  if (free !== null) {
    check(free >= minFreeDiskBytes(), `Free disk space: ${formatBytes(free)}`, `Only ${formatBytes(free)} free; uploads are refused below ${formatBytes(minFreeDiskBytes())} (NEOCLOUD_MIN_FREE_DISK).`);
  }
  checkDatabase(check, full);
  const snapshot = newestLocalSnapshot();
  ui.row('Local snapshot', snapshot || 'none yet');
  check(await fetchHealth(), `Server answers on port ${port()}`, `Server not reachable on port ${port()}`);
  return problems();
}

module.exports = { runDoctor };
