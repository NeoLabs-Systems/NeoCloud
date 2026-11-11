'use strict';

// Running LibreOffice on a user's document. LibreOffice is large and the
// document is untrusted, so the conversion is fenced in as far as a plain
// process allows:
//
// - It gets a minimal environment: PATH, LANG, and HOME and TMPDIR pointing
//   into the private work folder. None of the server's own variables
//   (NEOCLOUD_MASTER_KEY, SESSION_SECRET, ...) reach it.
// - It starts from a fresh profile seeded with settings that stop a document
//   from reaching out: no link updates, no linked or remote graphics, no
//   external data or WEBSERVICE refresh, macros off (see PROFILE_SETTINGS).
// - It runs detached in its own process group. On timeout the whole group is
//   killed, and the render slot is only freed once the process is gone.
// - On Linux, when the host allows it, it runs without network access:
//   `bwrap --unshare-net`, else `unshare --net --map-root-user`
//   (NEOCLOUD_PREVIEW_SANDBOX=auto, the default; `off` skips this).
//
// It still runs as the server's user and can read what that user can read.

const fs = require('fs');
const path = require('path');
const { execFile, spawn } = require('child_process');
const { pathToFileURL } = require('url');
const { promisify } = require('util');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Previews');
const run = promisify(execFile);

// Wrappers that take the network away, tried in order; each is probed once
// by running `true` through it.
const SANDBOXES = [
  ['bwrap', '--dev-bind', '/', '/', '--unshare-net', '--die-with-parent', '--'],
  ['unshare', '--net', '--map-root-user', '--'],
];

// registrymodifications.xcu for the fresh profile. Keys are LibreOffice's
// documented configuration paths (officecfg/registry/schema/...):
//   Common/Security/Scripting
//     MacroSecurityLevel = 3        "very high": only macros from trusted locations, of which there are none
//     DisableMacrosExecution = true never run document macros
//     BlockUntrustedRefererLinks    do not load images or other resources a document links to
//     DisableActiveContent = true   macros, DDE, OLE and links off (LibreOffice 24.2+; ignored before)
//   Writer/Content/Update Link = 0  Writer link updates: never (0 never, 1 on request, 2 always)
//   Calc/Content/Update Link = 1    Calc link updates: never (0 always, 1 never, 2 on request);
//                                   this also stops external data, DDE and WEBSERVICE from refreshing on load
//   Jobs/.../UpdateCheck AutoCheckEnabled = false  no update check over the network
const PROFILE_SETTINGS = `<?xml version="1.0" encoding="UTF-8"?>
<oor:items xmlns:oor="http://openoffice.org/2001/registry" xmlns:xs="http://www.w3.org/2001/XMLSchema" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="DisableMacrosExecution" oor:op="fuse"><value>true</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="BlockUntrustedRefererLinks" oor:op="fuse"><value>true</value></prop></item>
<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="DisableActiveContent" oor:op="fuse"><value>true</value></prop></item>
<item oor:path="/org.openoffice.Office.Writer/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>0</value></prop></item>
<item oor:path="/org.openoffice.Office.Calc/Content/Update"><prop oor:name="Link" oor:op="fuse"><value>1</value></prop></item>
<item oor:path="/org.openoffice.Office.Jobs/Jobs/org.openoffice.Office.Jobs:Job['UpdateCheck']/Arguments"><prop oor:name="AutoCheckEnabled" oor:op="fuse"><value>false</value></prop></item>
</oor:items>
`;

let officeBinary;
let sandboxWrapper;

/** The LibreOffice executable, looked up once; null when not installed. */
async function findOfficeBinary() {
  if (officeBinary !== undefined) return officeBinary;
  officeBinary = null;
  const candidates = [
    process.env.NEOCLOUD_LIBREOFFICE_PATH,
    'soffice',
    'libreoffice',
    '/Applications/LibreOffice.app/Contents/MacOS/soffice',
    'C:\\Program Files\\LibreOffice\\program\\soffice.exe',
  ].filter(Boolean);
  for (const candidate of candidates) {
    try {
      await run(candidate, ['--version'], { timeout: 15000 });
      officeBinary = candidate;
      break;
    } catch {
      // Not this one.
    }
  }
  return officeBinary;
}

/** The network-less wrapper argv for this host, or [] for none. */
async function findSandbox() {
  const mode = String(process.env.NEOCLOUD_PREVIEW_SANDBOX || 'auto').trim().toLowerCase();
  if (mode === 'off' || process.platform !== 'linux') return [];
  if (sandboxWrapper !== undefined) return sandboxWrapper;
  if (mode !== 'auto') logger.warn(`NEOCLOUD_PREVIEW_SANDBOX=${mode} is not auto or off; using auto.`);
  sandboxWrapper = [];
  for (const wrapper of SANDBOXES) {
    try {
      await run(wrapper[0], [...wrapper.slice(1), 'true'], { timeout: 5000, env: { PATH: process.env.PATH } });
      sandboxWrapper = wrapper;
      break;
    } catch {
      // Not installed, or the host does not allow new namespaces.
    }
  }
  if (sandboxWrapper.length === 0) logger.warn('Office previews run with network access: neither bwrap nor unshare can drop it on this host.');
  return sandboxWrapper;
}

/** Program, arguments and environment for converting `source` to PDF in `workDir`. */
function officeCommand({ binary, sandbox = [], workDir, source }) {
  const env = { PATH: process.env.PATH || '/usr/bin:/bin', LANG: process.env.LANG || 'C.UTF-8', HOME: workDir, TMPDIR: workDir };
  if (process.platform === 'win32') Object.assign(env, { SystemRoot: process.env.SystemRoot, TEMP: workDir, TMP: workDir });
  const args = [
    '--headless',
    '--norestore',
    `-env:UserInstallation=${pathToFileURL(path.join(workDir, 'profile')).href}`,
    '--convert-to', 'pdf',
    '--outdir', workDir,
    source,
  ];
  if (sandbox.length === 0) return { file: binary, args, env };
  return { file: sandbox[0], args: [...sandbox.slice(1), binary, ...args], env };
}

/** Writes the hardened settings into the (new) profile folder below `workDir`. */
async function seedProfile(workDir) {
  const userDir = path.join(workDir, 'profile', 'user');
  await fs.promises.mkdir(userDir, { recursive: true, mode: 0o700 });
  await fs.promises.writeFile(path.join(userDir, 'registrymodifications.xcu'), PROFILE_SETTINGS, { mode: 0o600 });
}

function killGroup(child) {
  try {
    if (process.platform === 'win32') child.kill('SIGKILL');
    else process.kill(-child.pid, 'SIGKILL');
  } catch {
    // Already gone.
  }
}

/**
 * Runs a program in its own process group. Resolves on exit code 0; on
 * timeout kills the whole group and rejects only once the program has exited.
 */
function runInProcessGroup(file, args, { env, cwd, timeoutMs }) {
  return new Promise((resolve, reject) => {
    const child = spawn(file, args, { env, cwd, detached: process.platform !== 'win32', stdio: 'ignore', windowsHide: true });
    let timedOut = false;
    let settled = false;
    const timer = setTimeout(() => {
      timedOut = true;
      killGroup(child);
    }, timeoutMs);
    const finish = (err) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      if (err) reject(err);
      else resolve();
    };
    child.once('error', finish);
    child.once('close', (code, signal) => {
      if (timedOut) finish(new Error(`Timed out after ${timeoutMs} ms.`));
      else if (code === 0) finish();
      else finish(new Error(`Exited with ${code ?? signal}.`));
    });
  });
}

/** Converts `source` (inside the private `workDir`) to workDir/<name>.pdf. */
async function convertToPdf({ binary, workDir, source, timeoutMs }) {
  await seedProfile(workDir);
  const { file, args, env } = officeCommand({ binary, sandbox: await findSandbox(), workDir, source });
  await runInProcessGroup(file, args, { env, cwd: workDir, timeoutMs });
}

module.exports = { convertToPdf, findOfficeBinary, officeCommand, runInProcessGroup, seedProfile };
