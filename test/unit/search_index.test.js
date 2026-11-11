'use strict';

// The pure parts of full-text search: query parsing and escaping, snippets,
// phrase checks, file kinds and charset sniffing.

const test = require('node:test');
const assert = require('node:assert/strict');
const {
  makeSnippet, matchExpression, matchesAll, needsVerification, parseQuery,
} = require('../../server/services/search_index/query_text');
const { extractText, indexableKind } = require('../../server/services/search_index/extractors');

test('queries become quoted prefix terms; quotes make exact phrases', () => {
  const terms = parseQuery('Crème "Quick  Brown" e-mail');
  assert.deepEqual(terms, [
    { tokens: ['creme'], prefix: true },
    { tokens: ['quick', 'brown'], prefix: false },
    { tokens: ['e', 'mail'], prefix: true },
  ]);
  assert.equal(matchExpression(terms), '"creme"* AND "quick" AND "brown" AND "e" AND "mail"*');
  assert.equal(needsVerification(terms), true);
  assert.equal(needsVerification(parseQuery('one two')), false);
});

test('FTS operators and punctuation never reach the query as syntax', () => {
  assert.equal(matchExpression(parseQuery('a OR b NOT c')), '"a"* AND "or"* AND "b"* AND "not"* AND "c"*');
  // A word that splits into several tokens ("col:z") is a phrase with a prefix end.
  assert.equal(matchExpression(parseQuery('NEAR(x y) col:z ^w *')), '"near" AND "x"* AND "y"* AND "col" AND "z"* AND "w"*');
  assert.equal(matchExpression(parseQuery('" * ( ) : ^ -')), null);
  assert.equal(matchExpression(parseQuery('"unclosed phrase')), '"unclosed" AND "phrase"');
  // At most 8 terms and 16 tokens.
  assert.equal(parseQuery('a b c d e f g h i j').length, 8);
  assert.equal(parseQuery('"a b c d e f g h i j k l m n o p q r s"')[0].tokens.length, 16);
});

test('snippets wrap matches in \\u0002/\\u0003 and cut around the first one', () => {
  const text = `${'x '.repeat(200)}Ein Bäcker backt Brötchen. ${'y '.repeat(200)}`;
  const snippet = makeSnippet(text, parseQuery('backer brot'));
  assert.match(snippet, /^… /);
  assert.match(snippet, / …$/);
  assert.match(snippet, /Ein \u0002Bäcker\u0003 backt \u0002Brötchen\u0003\./);
  // Marker characters in the text itself can't fake a highlight.
  assert.equal(makeSnippet('a \u0002fake\u0003 b', parseQuery('b')), 'a fake \u0002b\u0003');
  // No match (name-only results never get here, but stay safe): the start of the text.
  assert.equal(makeSnippet('short text', parseQuery('zzz')), 'short text');
});

test('phrases are checked against the text', () => {
  assert.equal(matchesAll('the quick brown fox', parseQuery('"quick brown"')), true);
  assert.equal(matchesAll('brown, quick fox', parseQuery('"quick brown"')), false);
  assert.equal(matchesAll('send an E-Mail now', parseQuery('e-ma')), true);
  assert.equal(matchesAll('send an e mailing', parseQuery('"e mail"')), false);
});

test('file kinds come from the extension, then the MIME type', () => {
  assert.equal(indexableKind('a.PDF', null), 'pdf');
  assert.equal(indexableKind('a.docx', null), 'office');
  assert.equal(indexableKind('a.odp', null), 'office');
  assert.equal(indexableKind('a.htm', null), 'html');
  assert.equal(indexableKind('a.go', null), 'text');
  assert.equal(indexableKind('README', 'text/plain'), 'text');
  assert.equal(indexableKind('a.doc', 'application/msword'), null);
  assert.equal(indexableKind('a.jpg', 'image/jpeg'), null);
});

test('plain text is decoded whatever its charset', async () => {
  const decode = (buffer, kind = 'text') => extractText(buffer, kind, 'x.txt', { maxChars: 1000 });
  assert.equal(await decode(Buffer.from('﻿bom utf-8 ä')), 'bom utf-8 ä');
  assert.equal(await decode(Buffer.from('plain utf-8 ß')), 'plain utf-8 ß');
  assert.equal(await decode(Buffer.from('café €', 'latin1')), 'café ¬');
  assert.equal(await decode(Buffer.from([0x63, 0x61, 0x66, 0xe9, 0x20, 0x80])), 'café €');
  assert.equal(await decode(Buffer.from('utf-16 without bom ü', 'utf16le')), 'utf-16 without bom ü');
  const be = Buffer.from('big endian', 'utf16le').swap16();
  assert.equal(await decode(Buffer.concat([Buffer.from([0xfe, 0xff]), be])), 'big endian');
  assert.equal(await decode(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x00, 0x00, 0x01, 0x02, 0xff, 0x00])), '');
  const koi8 = Buffer.from('<meta charset="koi8-r"><p>\xf0\xd2\xc9\xd7\xc5\xd4</p>', 'latin1');
  assert.equal((await decode(koi8, 'html')).trim(), 'Привет');
});
