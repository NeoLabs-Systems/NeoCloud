'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');

// The logger and its neighbours must never touch the real ~/.neocloud.
const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-unit-'));
process.env.NEOCLOUD_HOME = home;
test.after(() => fs.rmSync(home, { recursive: true, force: true }));

const { officeCommand, runInProcessGroup, seedProfile } = require('../../server/services/preview/office_convert');

test('office: the converter gets a minimal environment and a profile in the work folder', () => {
  process.env.NEOCLOUD_MASTER_KEY = 'a'.repeat(64);
  process.env.SESSION_SECRET = 'secret';
  const workDir = path.join(home, 'work');
  const source = path.join(workDir, 'document.docx');
  const { file, args, env } = officeCommand({ binary: '/usr/bin/soffice', workDir, source });
  assert.equal(file, '/usr/bin/soffice');
  assert.deepEqual(args.slice(-5), ['--convert-to', 'pdf', '--outdir', workDir, source]);
  assert.ok(args.includes('--headless') && args.includes('--norestore'));
  assert.ok(args.some((arg) => arg.startsWith('-env:UserInstallation=file://') && arg.endsWith('/work/profile')));
  assert.equal(env.HOME, workDir);
  assert.equal(env.TMPDIR, workDir);
  assert.ok(env.PATH && env.LANG);
  const allowed = new Set(['PATH', 'LANG', 'HOME', 'TMPDIR', 'SystemRoot', 'TEMP', 'TMP']);
  assert.deepEqual(Object.keys(env).filter((key) => !allowed.has(key)), []);
  assert.equal(JSON.stringify(env).includes('a'.repeat(64)), false);

  const wrapped = officeCommand({ binary: 'soffice', sandbox: ['bwrap', '--unshare-net', '--'], workDir, source });
  assert.equal(wrapped.file, 'bwrap');
  assert.deepEqual(wrapped.args.slice(0, 3), ['--unshare-net', '--', 'soffice']);
  assert.deepEqual(wrapped.args.slice(3), args);
});

test('office: the fresh profile turns off links, remote content and macros', async () => {
  const workDir = fs.mkdtempSync(path.join(home, 'office-'));
  await seedProfile(workDir);
  const file = path.join(workDir, 'profile', 'user', 'registrymodifications.xcu');
  const xcu = fs.readFileSync(file, 'utf8');
  if (process.platform !== 'win32') assert.equal(fs.statSync(file).mode & 0o777, 0o600);
  const setting = (item, name, value) => new RegExp(`oor:path="${item.replace(/[[\]]/g, '\\$&')}"><prop oor:name="${name}" oor:op="fuse"><value>${value}</value>`);
  assert.match(xcu, setting('/org.openoffice.Office.Common/Security/Scripting', 'MacroSecurityLevel', 3));
  assert.match(xcu, setting('/org.openoffice.Office.Common/Security/Scripting', 'DisableMacrosExecution', true));
  assert.match(xcu, setting('/org.openoffice.Office.Common/Security/Scripting', 'BlockUntrustedRefererLinks', true));
  assert.match(xcu, setting('/org.openoffice.Office.Writer/Content/Update', 'Link', 0));
  assert.match(xcu, setting('/org.openoffice.Office.Calc/Content/Update', 'Link', 1));
});

test('office: a timeout kills the whole process group before it reports back', { skip: process.platform === 'win32' }, async () => {
  const pidFile = path.join(home, 'grandchild.pid');
  const started = Date.now();
  await assert.rejects(
    runInProcessGroup('/bin/sh', ['-c', `sleep 30 & echo $! > "${pidFile}"; wait`], { env: { PATH: process.env.PATH }, cwd: home, timeoutMs: 300 }),
    /Timed out/,
  );
  assert.ok(Date.now() - started < 5000);
  const grandchild = Number(fs.readFileSync(pidFile, 'utf8'));
  // Orphans are reaped by init shortly after the kill.
  const deadline = Date.now() + 3000;
  let alive = true;
  while (alive && Date.now() < deadline) {
    try {
      process.kill(grandchild, 0);
      await new Promise((resolve) => setTimeout(resolve, 50));
    } catch {
      alive = false;
    }
  }
  assert.equal(alive, false, 'the background child survived the timeout');
  await runInProcessGroup('/bin/sh', ['-c', 'exit 0'], { env: { PATH: process.env.PATH }, cwd: home, timeoutMs: 5000 });
  await assert.rejects(runInProcessGroup('/bin/sh', ['-c', 'exit 3'], { env: { PATH: process.env.PATH }, cwd: home, timeoutMs: 5000 }), /Exited with 3/);
});
