'use strict';

// The `neocloud` command. Each command is one function; runCLI dispatches.

const fs = require('fs');
const path = require('path');
const { spawn, spawnSync } = require('child_process');
const dotenv = require('dotenv');
const {
  APP_DIR,
  DATA_DIR,
  ENV_FILE,
  LOG_DIR,
  RUNTIME_HOME,
  STORAGE_DIR,
  applySecureUmask,
  ensureRuntimeDirs,
  ensureSecureRuntimeEnv,
  readEnvFileRaw,
  upsertEnvValue,
} = require('../runtime/paths');
const { parseEnv } = require('../runtime/env');
const { DEFAULT_PORT } = require('./setup/contract');
const service = require('./setup/service_adapters');
const { fetchHealth, port, waitForHealth, waitForStop } = require('./cli/server_probe');
const { runDoctor } = require('./cli/doctor');
const { runRollback, runUpdate } = require('./cli/upgrade');
const ui = require('./cli/ui');
const { version } = require('../package.json');

const SECRET_KEYS = /SECRET|KEY|PASSWORD|TOKEN/;

function loadRuntimeEnv() {
  applySecureUmask();
  ensureRuntimeDirs();
  ensureSecureRuntimeEnv({ logger: null });
  dotenv.config({ path: ENV_FILE, override: true, quiet: true });
}

function flagValue(args, name) {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] : undefined;
}

async function cmdSetup(args) {
  loadRuntimeEnv();
  ui.title('NeoCloud setup');
  const portValue = flagValue(args, '--port');
  const publicUrl = flagValue(args, '--public-url');
  if (portValue) upsertEnvValue(ENV_FILE, 'PORT', String(Number(portValue)));
  if (publicUrl !== undefined) upsertEnvValue(ENV_FILE, 'PUBLIC_URL', publicUrl);
  if (!parseEnv(readEnvFileRaw()).has('PORT')) upsertEnvValue(ENV_FILE, 'PORT', String(DEFAULT_PORT));
  ui.ok(`Configuration: ${ENV_FILE}`);
  ui.ok(`Data:          ${DATA_DIR}`);
  ui.ok(`File storage:  ${STORAGE_DIR}`);
  ui.warn('Back up NEOCLOUD_MASTER_KEY (`neocloud keys export`). Without it, stored files cannot be decrypted.');
  ui.info(`Next: ${ui.accent('neocloud start')}`);
}

async function cmdStart(args) {
  loadRuntimeEnv();
  if (args.includes('--foreground')) {
    const child = spawn(process.execPath, [path.join(APP_DIR, 'server', 'index.js')], { stdio: 'inherit' });
    await new Promise((resolve) => child.on('exit', resolve));
    return;
  }
  ui.title('Starting NeoCloud');
  if (await fetchHealth()) {
    ui.ok(`Already running on http://localhost:${port()}`);
    return;
  }
  if (await startServer()) {
    ui.info('The first account you register becomes the admin.');
  } else {
    process.exitCode = 1;
  }
}

/** Starts the service and reports whether it answers /api/health in time. */
async function startServer(timeoutMs = 15000) {
  const result = service.startService();
  ui.info(`Service manager: ${result.manager}`);
  if (await waitForHealth(timeoutMs)) {
    ui.ok(`Running on ${process.env.PUBLIC_URL || `http://localhost:${port()}`}`);
    return true;
  }
  ui.error(`NeoCloud did not answer on port ${port()}. See ${ui.accent('neocloud logs')}.`);
  return false;
}

async function stopServer() {
  service.stopService();
  if (!(await waitForStop())) throw new Error('NeoCloud is still running 40 s after being asked to stop.');
}

async function cmdStop() {
  loadRuntimeEnv();
  ui.title('Stopping NeoCloud');
  ui.info('Letting running uploads and downloads finish (up to 30 s)...');
  await stopServer();
  ui.ok('Stopped');
}

async function cmdRestart() {
  await cmdStop();
  await cmdStart([]);
}

async function cmdStatus() {
  loadRuntimeEnv();
  ui.title('NeoCloud status');
  const healthy = await fetchHealth();
  ui.row('Server', healthy ? `running on port ${port()}` : 'not running');
  ui.row('Service', service.serviceManager() || 'not installed');
  ui.row('Version', version);
  ui.row('Config', ENV_FILE);
  ui.row('Data', DATA_DIR);
  ui.row('Storage', STORAGE_DIR);
  if (!healthy) process.exitCode = 1;
}

async function cmdLogs(args) {
  loadRuntimeEnv();
  const files = ['neocloud.log', 'neocloud.error.log'].map((name) => path.join(LOG_DIR, name)).filter((file) => fs.existsSync(file));
  if (files.length === 0) {
    ui.info(`No log files in ${LOG_DIR} yet.`);
    return;
  }
  const tailArgs = args.includes('-f') || args.includes('--follow') ? ['-n', '100', '-f', ...files] : ['-n', '100', ...files];
  if (process.platform === 'win32') {
    for (const file of files) process.stdout.write(fs.readFileSync(file, 'utf8').split('\n').slice(-100).join('\n'));
    return;
  }
  spawnSync('tail', tailArgs, { stdio: 'inherit' });
}

function maskValue(key, value) {
  if (!SECRET_KEYS.test(key) || !value) return value;
  return `${value.slice(0, 4)}••••${value.slice(-2)}`;
}

async function cmdEnv(args) {
  loadRuntimeEnv();
  const [action, key, ...rest] = args;
  const values = parseEnv(readEnvFileRaw());
  if (!action || action === 'list') {
    ui.title(`Settings in ${ENV_FILE}`);
    for (const [name, value] of values) ui.row(name, maskValue(name, value));
    return;
  }
  if (action === 'get' && key) {
    ui.plain(values.get(key) ?? '');
    return;
  }
  if (action === 'set' && key) {
    if (key === 'NEOCLOUD_MASTER_KEY') throw new Error('The master key can’t be changed: stored files are encrypted under it.');
    upsertEnvValue(ENV_FILE, key, rest.join(' '));
    ui.ok(`${key} saved. Restart with ${ui.accent('neocloud restart')} to apply.`);
    return;
  }
  throw new Error('Usage: neocloud env [list | get KEY | set KEY VALUE]');
}

async function cmdAdmin(args) {
  loadRuntimeEnv();
  const admin = require('../server/services/access/admin');
  const [action, username] = args;
  if (!action || action === 'list') {
    ui.title('Admins');
    const admins = admin.listAdmins();
    if (admins.length === 0) ui.info('No admin yet. The first account you register becomes one.');
    for (const user of admins) ui.ok(user.username);
    return;
  }
  if (!username) throw new Error(`Usage: neocloud admin ${action} <username>`);
  if (action === 'grant') {
    const result = admin.grantAdmin(username, { source: 'cli' });
    ui.ok(result.changed ? `${result.username} is now an admin.` : `${result.username} already was an admin.`);
    return;
  }
  if (action === 'revoke') {
    const result = admin.revokeAdmin(username, { source: 'cli' });
    ui.ok(result.changed ? `${result.username} is no longer an admin.` : `${result.username} was not an admin.`);
    return;
  }
  throw new Error('Usage: neocloud admin [list | grant <username> | revoke <username>]');
}

async function cmdBackup(args) {
  loadRuntimeEnv();
  const [action, rawId, snapshot] = args;
  const usage = 'Usage: neocloud backup [list | run <id> | verify <id> | snapshots <id> | restore <id> <snapshot>]';
  if (action === 'restore' && await fetchHealth()) throw new Error('Stop NeoCloud first (`neocloud stop`); a restore replaces the database.');
  const targets = require('../server/services/backup/targets');
  const runner = require('../server/services/backup/runner');
  const id = Number(rawId);
  if (!action || action === 'list') {
    ui.title('Backup targets');
    const list = targets.listTargets();
    if (list.length === 0) ui.info('None yet. Add one in the app under Admin › Backups.');
    for (const target of list) {
      const last = target.lastRun ? `${target.lastRun.status} · ${target.lastRun.startedAt}` : 'never run';
      const verified = target.lastVerification
        ? `verified ${target.lastVerification.ok ? 'ok' : 'FAILED'} · ${target.lastVerification.verifiedAt}`
        : 'never verified';
      ui.row(`#${target.id} ${target.type}`, `${target.name} · ${target.schedule} · ${last} · ${verified}`);
    }
    return;
  }
  if (!['run', 'verify', 'snapshots', 'restore'].includes(action)) throw new Error(usage);
  if (!Number.isInteger(id) || id <= 0) throw new Error(`Usage: neocloud backup ${action} <target-id>${action === 'restore' ? ' <snapshot>' : ''}`);
  if (action === 'run') {
    ui.title(`Backing up to target #${id}`);
    const run = await runner.runBackup(id);
    if (run.status === 'ok') ui.ok(`Snapshot ${run.snapshot}: ${run.blobsUploaded} new blob(s) uploaded.`);
    else throw new Error(run.error || 'Backup failed.');
    return;
  }
  if (action === 'verify') {
    ui.title(`Verifying the newest snapshot on target #${id}`);
    const result = await require('../server/services/backup/verify').verifyTarget(id);
    if (!result.ok) throw new Error(result.error || 'Verification failed.');
    ui.ok(`Snapshot ${result.snapshot}: database ${result.database}, all ${result.blobsChecked} blob(s) present.`);
    return;
  }
  if (action === 'snapshots') {
    ui.title(`Snapshots on target #${id}`);
    const { snapshots } = await runner.snapshotsOf(id);
    if (snapshots.length === 0) ui.info('No finished snapshots yet.');
    for (const name of snapshots) ui.info(name);
    return;
  }
  if (action === 'restore') {
    if (!snapshot) throw new Error('Usage: neocloud backup restore <target-id> <snapshot>  (see `neocloud backup snapshots <id>`)');
    ui.title(`Restoring ${snapshot} from target #${id}`);
    await require('../server/services/backup/restore').restoreSnapshot(id, snapshot, { log: ui.info });
    ui.ok(`Restored. Start NeoCloud again with ${ui.accent('neocloud start')}.`);
  }
}

async function cmdKeys(args) {
  loadRuntimeEnv();
  if (args[0] !== 'export') throw new Error('Usage: neocloud keys export');
  ui.title('Master key');
  ui.warn('Anyone with this key and a copy of your storage can read every file. Keep it offline.');
  ui.plain(`\n  NEOCLOUD_MASTER_KEY=${process.env.NEOCLOUD_MASTER_KEY}\n`);
}

async function cmdDoctor(args) {
  const problems = await runDoctor({ full: args.includes('--full'), loadRuntimeEnv });
  if (problems > 0) process.exitCode = 1;
}

async function cmdUpdate(args) {
  loadRuntimeEnv();
  ui.title('Updating NeoCloud');
  await runUpdate({ tag: flagValue(args, '--tag'), stopServer, startServer });
}

async function cmdRollback() {
  loadRuntimeEnv();
  await runRollback({ stopServer, startServer });
}

async function cmdUninstall() {
  loadRuntimeEnv();
  ui.title('Removing the NeoCloud service');
  service.uninstallService();
  ui.ok('Service removed. Your files and settings stay in place:');
  ui.info(RUNTIME_HOME);
}

function cmdVersion() {
  ui.plain(`neocloud ${version}`);
}

function cmdHelp() {
  ui.title(`NeoCloud ${version}`);
  const commands = [
    ['setup', 'Create configuration and secrets (--port, --public-url)'],
    ['start', 'Start in the background (--foreground to stay attached)'],
    ['stop', 'Stop the server'],
    ['restart', 'Stop and start again'],
    ['status', 'Show whether it runs and where data lives'],
    ['logs', 'Show recent logs (-f to follow)'],
    ['doctor', 'Check the installation, read-only (--full: integrity_check)'],
    ['env', 'list | get KEY | set KEY VALUE'],
    ['admin', 'list | grant <user> | revoke <user>'],
    ['backup', 'list | run <id> | verify <id> | snapshots <id> | restore <id> <snapshot>'],
    ['keys export', 'Print the master key for safekeeping'],
    ['update', 'Snapshot, update (--tag <version>), restart; rolls back on failure'],
    ['rollback', 'Undo the last update (code, and database if its schema changed)'],
    ['uninstall', 'Remove the background service (keeps data)'],
    ['version', 'Print the version'],
  ];
  for (const [name, description] of commands) ui.row(name, description);
  ui.plain();
}

const COMMANDS = {
  setup: cmdSetup,
  start: cmdStart,
  stop: cmdStop,
  restart: cmdRestart,
  status: cmdStatus,
  logs: cmdLogs,
  env: cmdEnv,
  admin: cmdAdmin,
  backup: cmdBackup,
  keys: cmdKeys,
  doctor: cmdDoctor,
  update: cmdUpdate,
  rollback: cmdRollback,
  uninstall: cmdUninstall,
  version: cmdVersion,
  '--version': cmdVersion,
  help: cmdHelp,
  '--help': cmdHelp,
};

async function runCLI(argv) {
  const [command = 'help', ...args] = argv;
  const handler = COMMANDS[command];
  if (!handler) throw new Error(`Unknown command "${command}". Run \`neocloud help\`.`);
  await handler(args);
}

module.exports = { runCLI };
