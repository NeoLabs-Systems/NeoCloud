'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { parseByteSize } = require('../../server/utils/bytes');
const { numberedName, validateName, contentDisposition } = require('../../server/utils/file_names');
const { mimeForName, isActiveContent } = require('../../server/utils/mime');
const { parseRange } = require('../../server/utils/http_range');
const { redactSecrets } = require('../../server/utils/logger');

test('parseByteSize reads plain counts and binary units', () => {
  assert.equal(parseByteSize('1024'), 1024);
  assert.equal(parseByteSize('10GB'), 10 * 1024 ** 3);
  assert.equal(parseByteSize('512 mb'), 512 * 1024 ** 2);
  assert.equal(parseByteSize('1.5G'), 1.5 * 1024 ** 3);
  assert.equal(parseByteSize('2GiB'), 2 * 1024 ** 3);
  assert.equal(parseByteSize(0), 0);
  assert.equal(parseByteSize('lots'), null);
  assert.equal(parseByteSize(-5), null);
});

test('validateName accepts real names and rejects paths', () => {
  assert.equal(validateName('  Urlaub 2026.jpg '), 'Urlaub 2026.jpg');
  assert.equal(validateName('.bashrc'), '.bashrc');
  for (const bad of ['', '.', '..', 'a/b', 'a\\b', 'x\u0000y', 'x'.repeat(256)]) {
    assert.throws(() => validateName(bad), (err) => err.status === 400, bad);
  }
});

test('numberedName counts up without stacking suffixes', () => {
  assert.equal(numberedName('photo.jpg', 1), 'photo (1).jpg');
  assert.equal(numberedName('photo (1).jpg', 2), 'photo (2).jpg');
  assert.equal(numberedName('README', 3), 'README (3)');
  assert.equal(numberedName('.env', 1), '.env (1)');
});

test('contentDisposition keeps non-ASCII names', () => {
  assert.equal(contentDisposition('attachment', 'Bericht ü.pdf'), "attachment; filename=\"Bericht _.pdf\"; filename*=UTF-8''Bericht%20%C3%BC.pdf");
});

test('mime types and active content', () => {
  assert.equal(mimeForName('a.PDF'), 'application/pdf');
  assert.equal(mimeForName('noext'), 'application/octet-stream');
  assert.equal(isActiveContent(mimeForName('x.html')), true);
  assert.equal(isActiveContent(mimeForName('x.svg')), true);
  assert.equal(isActiveContent(mimeForName('x.png')), false);
});

test('parseRange handles the forms browsers send', () => {
  assert.equal(parseRange(undefined, 100), null);
  assert.deepEqual(parseRange('bytes=0-9', 100), { start: 0, end: 9 });
  assert.deepEqual(parseRange('bytes=90-', 100), { start: 90, end: 99 });
  assert.deepEqual(parseRange('bytes=-10', 100), { start: 90, end: 99 });
  assert.deepEqual(parseRange('bytes=50-500', 100), { start: 50, end: 99 });
  assert.deepEqual(parseRange('bytes=100-', 100), { unsatisfiable: true });
  assert.equal(parseRange('bytes=0-1,5-6', 100), null);
});

test('share tokens and device user codes are redacted from logged paths', () => {
  assert.equal(redactSecrets('GET /api/public/shares/abcDEF123/zip'), 'GET /api/public/shares/[redacted]/zip');
  assert.equal(redactSecrets('GET /s/abcDEF123'), 'GET /s/[redacted]');
  assert.equal(redactSecrets('POST /api/account/devices/authorize/ABCD-EFGH'), 'POST /api/account/devices/authorize/[redacted]');
});
