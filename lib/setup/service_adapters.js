'use strict';

// Runs the server as a background service: launchd on macOS, a systemd user
// unit on Linux, a logon task plus detached process on Windows, and a plain
// detached process anywhere else.

const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn, spawnSync } = require('child_process');
const {
  APP_DIR,
  ENV_FILE,
  LOG_DIR,
  PID_FILE,
  RUNTIME_HOME,
  ensureRuntimeDirs,
} = require('../../runtime/paths');
const { writeAtomicFile } = require('./atomic_file');
const { SERVICE_LABEL, SYSTEMD_UNIT, WINDOWS_TASK_NAME } = require('./contract');

const SERVER_ENTRY = path.join(APP_DIR, 'server', 'index.js');
const PLIST_SOURCE = path.join(APP_DIR, `${SERVICE_LABEL}.plist`);
const PLIST_DESTINATION = path.join(os.homedir(), 'Library', 'LaunchAgents', `${SERVICE_LABEL}.plist`);
const SYSTEMD_UNIT_FILE = path.join(os.homedir(), '.config', 'systemd', 'user', `${SYSTEMD_UNIT}.service`);

function run(command, args) {
  return spawnSync(command, args, { encoding: 'utf8' });
}

function commandExists(command) {
  const probe = process.platform === 'win32' ? run('where', [command]) : run('which', [command]);
  return probe.status === 0;
}

function platform() {
  if (process.platform === 'darwin') return 'macos';
  if (process.platform === 'win32') return 'windows';
  if (process.platform === 'linux') return 'linux';
  return 'other';
}

function escapeXml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

function renderLaunchAgent(template) {
  return String(template)
    .replace(/__NODE_BIN__/g, escapeXml(process.execPath))
    .replace(/__APP_DIR__/g, escapeXml(APP_DIR))
    .replace(/__HOME__/g, escapeXml(os.homedir()))
    .replace(/__RUNTIME_HOME__/g, escapeXml(RUNTIME_HOME))
    .replace(/__LOG_DIR__/g, escapeXml(LOG_DIR));
}

function quoteSystemdValue(value) {
  return `"${String(value).replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
}

function renderSystemdUnit() {
  return [
    '[Unit]',
    'Description=NeoCloud — self-hosted encrypted file storage',
    'After=network.target',
    '',
    '[Service]',
    'Type=simple',
    `WorkingDirectory=${APP_DIR}`,
    `ExecStart=${quoteSystemdValue(process.execPath)} ${quoteSystemdValue(SERVER_ENTRY)}`,
    'Restart=always',
    'RestartSec=10',
    // The server finishes in-flight requests for up to 30 s on SIGTERM.
    'TimeoutStopSec=40',
    `EnvironmentFile=-${ENV_FILE}`,
    'Environment=NODE_ENV=production',
    `Environment=NEOCLOUD_HOME=${quoteSystemdValue(RUNTIME_HOME)}`,
    `StandardOutput=append:${path.join(LOG_DIR, 'neocloud.log')}`,
    `StandardError=append:${path.join(LOG_DIR, 'neocloud.error.log')}`,
    '',
    '[Install]',
    'WantedBy=default.target',
    '',
  ].join('\n');
}

function launchctlDomain() {
  return typeof process.getuid === 'function' ? `gui/${process.getuid()}` : null;
}

function readPid() {
  try {
    const pid = Number(fs.readFileSync(PID_FILE, 'utf8').trim());
    return Number.isInteger(pid) && pid > 0 ? pid : null;
  } catch {
    return null;
  }
}

function isRunning(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

/** The server's PID while that process is alive, else null. */
function runningServerPid() {
  const pid = readPid();
  return pid && isRunning(pid) ? pid : null;
}

function startDetached() {
  ensureRuntimeDirs();
  const out = fs.openSync(path.join(LOG_DIR, 'neocloud.log'), 'a');
  const err = fs.openSync(path.join(LOG_DIR, 'neocloud.error.log'), 'a');
  const child = spawn(process.execPath, [SERVER_ENTRY], {
    cwd: APP_DIR,
    detached: true,
    stdio: ['ignore', out, err],
    env: { ...process.env, NODE_ENV: process.env.NODE_ENV || 'production' },
    windowsHide: true,
  });
  child.unref();
  writeAtomicFile(PID_FILE, String(child.pid));
  return { manager: 'process', pid: child.pid };
}

function installMac() {
  writeAtomicFile(PLIST_DESTINATION, renderLaunchAgent(fs.readFileSync(PLIST_SOURCE, 'utf8')));
  const domain = launchctlDomain();
  run('launchctl', ['bootout', domain, PLIST_DESTINATION]);
  const boot = run('launchctl', ['bootstrap', domain, PLIST_DESTINATION]);
  if (boot.status !== 0) throw new Error(`launchctl bootstrap failed: ${boot.stderr.trim()}`);
  run('launchctl', ['kickstart', '-k', `${domain}/${SERVICE_LABEL}`]);
  return { manager: 'launchd' };
}

function installLinux() {
  writeAtomicFile(SYSTEMD_UNIT_FILE, renderSystemdUnit());
  for (const args of [['daemon-reload'], ['enable', SYSTEMD_UNIT], ['restart', SYSTEMD_UNIT]]) {
    const result = run('systemctl', ['--user', ...args]);
    if (result.status !== 0) throw new Error(`systemctl ${args.join(' ')} failed: ${result.stderr.trim()}`);
  }
  return { manager: 'systemd' };
}

function installWindows() {
  if (commandExists('schtasks')) {
    run('schtasks', ['/Create', '/TN', WINDOWS_TASK_NAME, '/TR', `"${process.execPath}" "${SERVER_ENTRY}"`, '/SC', 'ONLOGON', '/RL', 'LIMITED', '/F']);
  }
  return { ...startDetached(), manager: 'schtasks' };
}

/** Installs the service for this platform (if needed) and starts it. */
function startService() {
  ensureRuntimeDirs();
  const os_ = platform();
  if (os_ === 'macos' && commandExists('launchctl')) return installMac();
  if (os_ === 'linux' && commandExists('systemctl')) {
    try {
      return installLinux();
    } catch {
      // No user session bus (containers, plain SSH): fall through.
    }
  }
  if (os_ === 'windows') return installWindows();
  return startDetached();
}

function stopService() {
  const os_ = platform();
  if (os_ === 'macos' && fs.existsSync(PLIST_DESTINATION)) {
    run('launchctl', ['bootout', launchctlDomain(), PLIST_DESTINATION]);
  }
  if (os_ === 'linux' && fs.existsSync(SYSTEMD_UNIT_FILE)) {
    run('systemctl', ['--user', 'stop', SYSTEMD_UNIT]);
  }
  const pid = runningServerPid();
  // A server still finishing its requests removes the PID file itself on exit.
  if (pid) process.kill(pid, 'SIGTERM');
  else fs.rmSync(PID_FILE, { force: true });
}

/** Removes autostart entries; stored data stays. */
function uninstallService() {
  stopService();
  const os_ = platform();
  if (os_ === 'macos') fs.rmSync(PLIST_DESTINATION, { force: true });
  if (os_ === 'linux' && fs.existsSync(SYSTEMD_UNIT_FILE)) {
    run('systemctl', ['--user', 'disable', SYSTEMD_UNIT]);
    fs.rmSync(SYSTEMD_UNIT_FILE, { force: true });
    run('systemctl', ['--user', 'daemon-reload']);
  }
  if (os_ === 'windows' && commandExists('schtasks')) run('schtasks', ['/Delete', '/TN', WINDOWS_TASK_NAME, '/F']);
}

function serviceManager() {
  const os_ = platform();
  if (os_ === 'macos' && fs.existsSync(PLIST_DESTINATION)) return 'launchd';
  if (os_ === 'linux' && fs.existsSync(SYSTEMD_UNIT_FILE)) return 'systemd';
  return readPid() ? 'process' : null;
}

module.exports = {
  renderLaunchAgent,
  renderSystemdUnit,
  runningServerPid,
  serviceManager,
  startService,
  stopService,
  uninstallService,
};
