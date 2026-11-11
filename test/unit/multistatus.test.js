'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { parseXml } = require('../../server/services/webdav/xml');
const { isCollection, parseMultistatus, propText } = require('../../server/services/webdav/multistatus');

const ONE_MB = 1024 * 1024;

function elapsed(fn) {
  const started = process.hrtime.bigint();
  try {
    fn();
  } catch {
    // Only the time matters here; rejecting is fine.
  }
  return Number(process.hrtime.bigint() - started) / 1e6;
}

test('xml: attributes are read in one pass, namespaces still resolve', () => {
  const root = parseXml(`<d:propfind xmlns:d="DAV:" xmlns:x = 'urn:x' a="1 &amp; 2"><d:prop><x:thing/></d:prop></d:propfind>`);
  assert.equal(root.ns, 'DAV:');
  assert.equal(root.children[0].children[0].ns, 'urn:x');
  for (const bad of ['<a b>', '<a b=c/>', '<a b="c"d="e"/>', '<a ="c"/>', '<a b="c/>']) {
    assert.throws(() => parseXml(bad), (err) => err.code === 'XML_INVALID', bad);
  }
});

test('xml: a 1 MB start tag is refused in well under 100 ms', () => {
  // Used to backtrack quadratically in the attribute regex.
  for (const body of [`<a ${'a'.repeat(ONE_MB)}>`, `<a ${'a'.repeat(ONE_MB)}`, `<a b="${'x'.repeat(ONE_MB)}">`, `<a ${'b="c" '.repeat(ONE_MB / 6)}>`]) {
    assert.throws(() => parseXml(body), (err) => err.code === 'XML_INVALID');
    assert.ok(elapsed(() => parseXml(body)) < 100, 'took too long');
  }
});

test('multistatus: Nextcloud-shaped answers parse; failed propstats are ignored', () => {
  const responses = parseMultistatus(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response><d:href>/dav/a%20b/</d:href>
    <d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype><oc:size>12</oc:size></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
    <d:propstat><d:prop><d:getcontentlength>99</d:getcontentlength></d:prop><d:status>HTTP/1.1 404 Not Found</d:status></d:propstat>
  </d:response>
  <D:response xmlns:D="DAV:"><D:href>/dav/a&amp;b.txt</D:href>
    <D:propstat><D:prop><D:resourcetype/><D:getcontentlength>5</D:getcontentlength></D:prop></D:propstat>
  </D:response>
</d:multistatus>`);
  assert.equal(responses.length, 2);
  assert.equal(responses[0].href, '/dav/a%20b/');
  assert.equal(isCollection(responses[0].props), true);
  assert.equal(propText(responses[0].props, 'size'), '12');
  assert.equal(propText(responses[0].props, 'getcontentlength'), null);
  assert.equal(responses[1].href, '/dav/a&b.txt');
  assert.equal(isCollection(responses[1].props), false);
  assert.equal(propText(responses[1].props, 'getcontentlength'), '5');
  assert.throws(() => parseMultistatus('<html>multistatus</html>'), (err) => err.code === 'MULTISTATUS_INVALID');
});

test('multistatus: 1 MB adversarial bodies parse or fail in linear time', () => {
  const head = '<d:multistatus xmlns:d="DAV:">';
  const bodies = [
    // Unclosed openers made the old lazy regexes rescan to the end each time.
    head + '<d:response>'.repeat(ONE_MB / 12),
    head + '<d:response '.repeat(ONE_MB / 12),
    head + '<d:response><d:href>'.repeat(ONE_MB / 20),
    `${head}<d:response><d:href>${'<'.repeat(ONE_MB)}`,
    '<response '.repeat(ONE_MB / 10),
  ];
  for (const body of bodies) {
    assert.throws(() => parseMultistatus(body), (err) => err.code === 'MULTISTATUS_INVALID');
    assert.ok(elapsed(() => parseMultistatus(body)) < 1000, 'took too long');
  }
  const valid = `${head}${'<d:response><d:href>/x</d:href></d:response>'.repeat(ONE_MB / 45)}</d:multistatus>`;
  assert.ok(elapsed(() => parseMultistatus(valid)) < 1000, 'took too long');
  assert.equal(parseMultistatus(valid).length, Math.floor(ONE_MB / 45));
});
