'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { sanitiseFileName, uniqueFileNames } = require('../../server/services/handoff/names');
const { browserLabel } = require('../../server/services/handoff/device_label');

test('names lose paths, control and bidi characters, and trailing dots', () => {
  assert.equal(sanitiseFileName('../../etc/passwd'), '_.._etc_passwd');
  assert.equal(sanitiseFileName('a\\b/c.txt'), 'a_b_c.txt');
  assert.equal(sanitiseFileName('inv‮oice.exe'), 'invoice.exe');
  assert.equal(sanitiseFileName('bad\u0000name\n.txt'), 'badname.txt');
  assert.equal(sanitiseFileName('report. . '), 'report');
  assert.equal(sanitiseFileName('.hidden'), 'hidden');
  assert.equal(sanitiseFileName('   '), 'file');
  assert.equal(sanitiseFileName('..'), 'file');
  assert.equal(sanitiseFileName('nul.txt'), '_nul.txt');
});

test('long names are cut by bytes and keep their extension', () => {
  const name = sanitiseFileName(`${'ä'.repeat(300)}.pdf`);
  assert.ok(Buffer.byteLength(name) <= 200);
  assert.ok(name.endsWith('.pdf'));
});

test('names stay unique, ignoring case', () => {
  assert.deepEqual(uniqueFileNames(['a.txt', 'A.txt', 'a.txt', 'b']), ['a.txt', 'A (1).txt', 'a (2).txt', 'b']);
});

test('a browser is named from its user agent', () => {
  assert.equal(browserLabel('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Safari/605.1.15'), 'Safari on macOS');
  assert.equal(browserLabel(''), 'Browser on this device');
});
