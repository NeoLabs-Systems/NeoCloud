'use strict';

// Full-text search: extraction of every supported format, the background
// indexer following renames, moves, trash and deletes, ranking and snippets,
// opt-outs, the admin switch, rebuilds, and what the index stores.

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const zlib = require('zlib');
const { cleanup, registerUser, upload } = require('../helpers/app');
const db = require('../../server/db/database');
const { runIndexerUntilIdle } = require('../../server/services/search_index/indexer');

test.after(cleanup);

// --- Fixtures built in the test --------------------------------------------------

const CRC_TABLE = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function crc32(buffer) {
  let crc = 0xffffffff;
  for (const byte of buffer) crc = CRC_TABLE[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

/** A zip archive (deflated entries) of { name: text }. */
function zip(files) {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const [name, text] of Object.entries(files)) {
    const data = Buffer.from(text, 'utf8');
    const packed = zlib.deflateRawSync(data);
    const fileName = Buffer.from(name, 'utf8');
    const crc = crc32(data);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(8, 8);
    local.writeUInt32LE(crc, 14);
    local.writeUInt32LE(packed.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(fileName.length, 26);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0);
    central.writeUInt16LE(20, 4);
    central.writeUInt16LE(20, 6);
    central.writeUInt16LE(8, 10);
    central.writeUInt32LE(crc, 16);
    central.writeUInt32LE(packed.length, 20);
    central.writeUInt32LE(data.length, 24);
    central.writeUInt16LE(fileName.length, 28);
    central.writeUInt32LE(offset, 42);
    locals.push(local, fileName, packed);
    centrals.push(central, fileName);
    offset += local.length + fileName.length + packed.length;
  }
  const directory = Buffer.concat(centrals);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(centrals.length / 2, 8);
  end.writeUInt16LE(centrals.length / 2, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, directory, end]);
}

/** A one-page PDF showing `text` in Helvetica. */
function pdf(text) {
  const stream = `BT /F1 18 Tf 72 720 Td (${text}) Tj ET`;
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>',
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
  ];
  let out = '%PDF-1.4\n';
  const offsets = [];
  objects.forEach((body, index) => {
    offsets.push(out.length);
    out += `${index + 1} 0 obj\n${body}\nendobj\n`;
  });
  const xref = out.length;
  out += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  out += offsets.map((value) => `${String(value).padStart(10, '0')} 00000 n \n`).join('');
  out += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(out, 'latin1');
}

const W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"';
const docx = (text) => zip({
  '[Content_Types].xml': '<?xml version="1.0"?><Types/>',
  'word/document.xml': `<?xml version="1.0"?><w:document ${W}><w:body><w:p><w:r><w:t>${text}</w:t></w:r></w:p>`
    + '<w:p><w:r><w:t xml:space="preserve">Second </w:t></w:r><w:r><w:t>par</w:t></w:r><w:r><w:t>agraph</w:t></w:r></w:p></w:body></w:document>',
});
const xlsx = (shared, inline) => zip({
  'xl/sharedStrings.xml': `<?xml version="1.0"?><sst><si><t>${shared}</t></si></sst>`,
  'xl/worksheets/sheet1.xml': '<?xml version="1.0"?><worksheet><sheetData><row r="1">'
    + '<c r="A1" t="s"><v>0</v></c><c r="B1"><v>4711</v></c>'
    + `<c r="C1" t="inlineStr"><is><t>${inline}</t></is></c></row></sheetData></worksheet>`,
});
const pptx = (text) => zip({
  'ppt/slides/slide1.xml': `<?xml version="1.0"?><p:sld><a:p><a:r><a:t>${text}</a:t></a:r></a:p></p:sld>`,
});
const openDocument = (text) => zip({
  mimetype: 'application/vnd.oasis.opendocument.text',
  'content.xml': `<?xml version="1.0"?><office:document-content><office:body><text:p>${text}</text:p></office:body></office:document-content>`,
});

// --- Helpers -------------------------------------------------------------------

let owner;
let other;

async function put(agent, name, content, options) {
  return (await upload(agent, name, content, options).expect(201)).body;
}

async function contentSearch(agent, q, extra = '') {
  return (await agent.get(`/api/search?content=1&q=${encodeURIComponent(q)}${extra}`).expect(200)).body.items;
}

function byName(items, name) {
  return items.find((item) => item.name === name);
}

async function setEnabled(enabled) {
  const res = await owner.agent.put('/api/admin/config').send({ fullTextSearch: enabled }).expect(200);
  assert.equal(res.body.fullTextSearch, enabled);
}

const files = {};

test.before(async () => {
  owner = await registerUser('fts');
  other = await registerUser('ftsother');
  assert.equal(owner.user.isAdmin, true);
  await setEnabled(true);

  files.utf8 = await put(owner.agent, 'notes.txt', 'Grandma\'s crème brûlée needs zebrafish-free vanilla.');
  files.latin1 = await put(owner.agent, 'legacy.txt', Buffer.from('Herr Müller wohnt in der Hauptstraße.', 'latin1'));
  files.utf16 = await put(owner.agent, 'wide.txt', Buffer.concat([Buffer.from([0xff, 0xfe]), Buffer.from('Ünïcödé widechar marmalade', 'utf16le')]));
  files.html = await put(owner.agent, 'page.html', '<html><head><script>var hiddenscriptword = 1;</script></head><body><p>Visible &amp; gooseberry</p></body></html>');
  files.code = await put(owner.agent, 'main.dart', 'void main() { print("periwinkle"); }');
  files.docx = await put(owner.agent, 'report.docx', docx('Annual albatross summary'));
  files.xlsx = await put(owner.agent, 'budget.xlsx', xlsx('Quokka expenses', 'Wombat income'));
  files.pptx = await put(owner.agent, 'deck.pptx', pptx('Narwhal roadmap'));
  files.odt = await put(owner.agent, 'letter.odt', openDocument('Dear axolotl committee'));
  files.ods = await put(owner.agent, 'table.ods', openDocument('Capybara totals'));
  files.odp = await put(owner.agent, 'talk.odp', openDocument('Pangolin keynote'));
  files.pdf = await put(owner.agent, 'scan.pdf', pdf('Quarterly lighthouse inspection'));
  files.photo = await put(owner.agent, 'photo.jpg', crypto.randomBytes(500));
  files.broken = await put(owner.agent, 'broken.docx', Buffer.from('this is not a zip'));
  files.otherNote = await put(other.agent, 'private.txt', 'Other account mentions albatross too.');
  await runIndexerUntilIdle();
});

// --- Formats ----------------------------------------------------------------

test('every supported format is indexed and found by its text', async () => {
  const cases = [
    ['creme brulee', 'notes.txt'],
    ['muller hauptstrasse', null],
    ['müller', 'legacy.txt'],
    ['hauptstraße', 'legacy.txt'],
    ['marmalade unicode', 'wide.txt'],
    ['gooseberry', 'page.html'],
    ['periwinkle', 'main.dart'],
    ['albatross', 'report.docx'],
    ['paragraph', 'report.docx'],
    ['quokka', 'budget.xlsx'],
    ['wombat', 'budget.xlsx'],
    ['4711', 'budget.xlsx'],
    ['narwhal', 'deck.pptx'],
    ['axolotl', 'letter.odt'],
    ['capybara', 'table.ods'],
    ['pangolin', 'talk.odp'],
    ['lighthouse', 'scan.pdf'],
  ];
  for (const [query, expected] of cases) {
    const items = await contentSearch(owner.agent, query);
    if (expected === null) {
      // "strasse" is not "straße": no false match from loose folding.
      assert.equal(items.length, 0, query);
      continue;
    }
    assert.deepEqual(items.map((item) => item.name), [expected], query);
    assert.equal(items[0].match, 'content');
  }
  // Script contents are not text of the page.
  assert.deepEqual(await contentSearch(owner.agent, 'hiddenscriptword'), []);
});

test('the admin status counts indexed, failed and pending files', async () => {
  const status = (await owner.agent.get('/api/admin/search-index').expect(200)).body;
  assert.equal(status.enabled, true);
  assert.equal(status.pending, 0);
  assert.equal(status.failed, 1);
  // 12 readable files of the owner (not the photo, not the broken docx) and 1 of the other account.
  assert.equal(status.indexed, 13);
  assert.ok(status.bytes > 0);
  assert.equal(typeof status.available.pdftotext, 'boolean');
  assert.equal(status.available.office, true);
});

test('extracted text is capped at 1 MB per file', async () => {
  const big = await put(owner.agent, 'big.txt', `${'ä'.repeat(600 * 1024)} tailword`);
  await runIndexerUntilIdle();
  const doc = db.prepare('SELECT status, text_bytes FROM search_docs WHERE node_id = ?').get(big.id);
  assert.deepEqual(doc, { status: 'indexed', text_bytes: 1024 * 1024 });
  assert.deepEqual(await contentSearch(owner.agent, 'tailword'), []);
});

// --- Ranking and snippets ------------------------------------------------------------

test('name matches come first, then content matches by BM25', async () => {
  const filler = 'lorem ipsum dolor sit amet consectetur '.repeat(5);
  await put(owner.agent, 'kiwi.txt', 'nothing about the fruit here');
  await put(owner.agent, 'mentions-once.txt', `${filler} kiwi ${filler}`);
  await put(owner.agent, 'mentions-often.txt', `${filler} kiwi kiwi kiwi kiwi ${filler}`);
  await put(owner.agent, 'kiwi-both.txt', `${filler} kiwi ${filler}`);
  await runIndexerUntilIdle();

  const items = await contentSearch(owner.agent, 'kiwi');
  assert.deepEqual(items.map((item) => [item.name, item.match]), [
    ['kiwi.txt', 'name'],
    ['kiwi-both.txt', 'both'],
    ['mentions-often.txt', 'content'],
    ['mentions-once.txt', 'content'],
  ]);
  assert.equal(items[0].snippet, undefined);
  assert.match(items[1].snippet, /\u0002kiwi\u0003/);
  assert.equal(items[2].path, '/');

  // Without content=1 the response is the plain name search.
  const plain = (await owner.agent.get('/api/search?q=kiwi').expect(200)).body.items;
  assert.deepEqual(plain.map((item) => item.name), ['kiwi.txt', 'kiwi-both.txt']);
  assert.equal(plain[0].match, undefined);
});

test('snippets mark every matched word, prefixes included, around the first match', async () => {
  const before = 'opening words that are far away from the match. '.repeat(10);
  await put(owner.agent, 'snippet.txt', `${before}The Sünflower seeds and sunflowers grow tall. ${'tail words '.repeat(60)}`);
  await runIndexerUntilIdle();
  const [item] = await contentSearch(owner.agent, 'sunflow seeds');
  assert.equal(item.name, 'snippet.txt');
  assert.match(item.snippet, /^… /);
  assert.match(item.snippet, / …$/);
  assert.match(item.snippet, /\u0002Sünflower\u0003 \u0002seeds\u0003 and \u0002sunflowers\u0003/);
  assert.ok(item.snippet.length < 300);
});

test('quoted words are a phrase', async () => {
  await put(owner.agent, 'phrase-yes.txt', 'the quick brown fox');
  await put(owner.agent, 'phrase-no.txt', 'brown is quick');
  await runIndexerUntilIdle();
  const phrase = await contentSearch(owner.agent, '"quick brown"');
  assert.deepEqual(phrase.map((item) => item.name), ['phrase-yes.txt']);
  assert.match(phrase[0].snippet, /\u0002quick brown\u0003/);
  const words = await contentSearch(owner.agent, 'quick brown');
  assert.deepEqual(words.map((item) => item.name).sort(), ['phrase-no.txt', 'phrase-yes.txt']);
});

test('every search filter still applies to content matches', async () => {
  const folder = (await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'Filtered' }).expect(201)).body;
  const inside = await put(owner.agent, 'inside.txt', 'tamarind inside', { parentId: folder.id });
  await put(owner.agent, 'outside.txt', 'tamarind outside');
  await put(owner.agent, 'tamarind.pdf', pdf('tamarind document'));
  await runIndexerUntilIdle();

  assert.deepEqual((await contentSearch(owner.agent, 'tamarind', `&parentId=${folder.id}`)).map((item) => item.name), ['inside.txt']);
  assert.deepEqual((await contentSearch(owner.agent, 'tamarind', '&type=pdf')).map((item) => item.name), ['tamarind.pdf']);
  assert.deepEqual((await contentSearch(owner.agent, 'tamarind', '&maxSize=12')).map((item) => item.name), []);
  db.prepare("UPDATE nodes SET modified_at = '2020-01-01 00:00:00' WHERE id = ?").run(inside.id);
  assert.deepEqual((await contentSearch(owner.agent, 'tamarind', '&before=2021-01-01')).map((item) => item.name), ['inside.txt']);
  assert.equal((await contentSearch(owner.agent, 'tamarind', '&limit=1')).length, 1);
});

// --- Following the tree ---------------------------------------------------------------

test('rename, move, update, trash, restore and delete keep the index in step', async () => {
  const file = await put(owner.agent, 'journey.txt', 'persimmon journey');
  const folder = (await owner.agent.post('/api/folders').send({ parentId: 'root', name: 'Journey' }).expect(201)).body;
  await runIndexerUntilIdle();
  const found = async () => (await contentSearch(owner.agent, 'persimmon')).map((item) => item.name);
  assert.deepEqual(await found(), ['journey.txt']);

  // Move: still found, now under the folder.
  await owner.agent.patch(`/api/nodes/${file.id}`).send({ parentId: folder.id }).expect(200);
  await runIndexerUntilIdle();
  assert.equal((await contentSearch(owner.agent, 'persimmon', `&parentId=${folder.id}`)).length, 1);

  // Renamed to a type the index doesn't read: dropped. Back: indexed again.
  await owner.agent.patch(`/api/nodes/${file.id}`).send({ name: 'journey.bin' }).expect(200);
  await runIndexerUntilIdle();
  assert.deepEqual(await found(), []);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_docs WHERE node_id = ?').get(file.id).n, 0);
  await owner.agent.patch(`/api/nodes/${file.id}`).send({ name: 'journey.md' }).expect(200);
  await runIndexerUntilIdle();
  assert.deepEqual(await found(), ['journey.md']);

  // New content replaces the old words.
  await upload(owner.agent, 'journey.md', 'loquat journey', { parentId: folder.id, conflict: 'replace' }).expect(201);
  await runIndexerUntilIdle();
  assert.deepEqual(await found(), []);
  assert.deepEqual((await contentSearch(owner.agent, 'loquat')).map((item) => item.name), ['journey.md']);

  // Trashing the folder drops everything below it; restoring brings it back.
  await owner.agent.delete(`/api/nodes/${folder.id}`).expect(200);
  await runIndexerUntilIdle();
  assert.deepEqual(await contentSearch(owner.agent, 'loquat'), []);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_docs WHERE node_id = ?').get(file.id).n, 0);
  await owner.agent.post(`/api/trash/${folder.id}/restore`).expect(200);
  await runIndexerUntilIdle();
  assert.deepEqual((await contentSearch(owner.agent, 'loquat')).map((item) => item.name), ['journey.md']);

  // Deleted for good: the row and its FTS entry are gone.
  const docId = db.prepare('SELECT id FROM search_docs WHERE node_id = ?').get(file.id).id;
  await owner.agent.delete(`/api/nodes/${folder.id}`).expect(200);
  await owner.agent.delete(`/api/trash/${folder.id}`).expect(200);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_docs WHERE id = ?').get(docId).n, 0);
  assert.equal(db.prepare("SELECT COUNT(*) AS n FROM search_fts WHERE search_fts MATCH 'loquat'").get().n, 0);
});

test('vault files are never indexed', async () => {
  const vault = (await owner.agent.post('/api/vaults').send({
    parentId: 'root',
    name: 'Vault',
    salt: crypto.randomBytes(16).toString('base64'),
    iterations: 600000,
    wrappedKey: crypto.randomBytes(60).toString('base64'),
  }).expect(201)).body;
  const secret = await put(owner.agent, 'secret.txt', 'rambutan inside the vault', { parentId: vault.folder.id });
  await runIndexerUntilIdle();
  assert.deepEqual(await contentSearch(owner.agent, 'rambutan'), []);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_docs WHERE node_id = ?').get(secret.id).n, 0);
});

// --- Accounts, switches and rebuilds ---------------------------------------------------------

test('another account never sees content matches from this one', async () => {
  const items = await contentSearch(other.agent, 'albatross');
  assert.deepEqual(items.map((item) => item.name), ['private.txt']);
  assert.deepEqual(await contentSearch(other.agent, 'lighthouse'), []);
});

test('an account that opts out is removed from the index and searched by name only', async () => {
  await other.agent.put('/api/settings').send({ fullTextSearch: false }).expect(200);
  await runIndexerUntilIdle();
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_docs WHERE user_id = ?').get(other.user.id).n, 0);
  const items = await contentSearch(other.agent, 'albatross');
  assert.deepEqual(items, []);
  const byFileName = await contentSearch(other.agent, 'private');
  assert.deepEqual(byFileName.map((item) => [item.name, item.match]), [['private.txt', 'name']]);
  // New files of an opted-out account are not read either.
  await put(other.agent, 'later.txt', 'albatross again');
  await runIndexerUntilIdle();
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_docs WHERE user_id = ?').get(other.user.id).n, 0);

  await other.agent.put('/api/settings').send({ fullTextSearch: true }).expect(200);
  await runIndexerUntilIdle();
  assert.deepEqual((await contentSearch(other.agent, 'albatross')).map((item) => item.name).sort(), ['later.txt', 'private.txt']);
  assert.equal((await other.agent.get('/api/settings').expect(200)).body.settings.fullTextSearch, true);
});

test('with the admin switch off the indexer pauses and search ignores content', async () => {
  await setEnabled(false);
  await put(owner.agent, 'paused.txt', 'guava while paused');
  assert.equal(await runIndexerUntilIdle(), 0);
  assert.deepEqual(await contentSearch(owner.agent, 'albatross'), []);
  const status = (await owner.agent.get('/api/admin/search-index').expect(200)).body;
  assert.equal(status.enabled, false);
  assert.equal(status.pending, 1);

  await setEnabled(true);
  assert.equal(await runIndexerUntilIdle(), 1);
  assert.deepEqual((await contentSearch(owner.agent, 'guava')).map((item) => item.name), ['paused.txt']);
  await owner.agent.put('/api/admin/config').send({ fullTextSearch: 'yes' }).expect(400);
});

test('a rebuild empties the index and reads every file again', async () => {
  const before = (await owner.agent.get('/api/admin/search-index').expect(200)).body;
  const res = await owner.agent.post('/api/admin/search-index/rebuild').expect(202);
  assert.equal(res.body.indexed, 0);
  assert.equal(res.body.failed, 0);
  assert.equal(res.body.pending, before.indexed + before.failed);
  assert.equal(db.prepare('SELECT COUNT(*) AS n FROM search_fts_docsize').get().n, 0);
  await runIndexerUntilIdle();
  const after = (await owner.agent.get('/api/admin/search-index').expect(200)).body;
  assert.equal(after.indexed, before.indexed);
  assert.equal(after.failed, before.failed);
  assert.deepEqual((await contentSearch(owner.agent, 'lighthouse')).map((item) => item.name), ['scan.pdf']);
});

// --- Query safety and storage ---------------------------------------------------------------

test('FTS syntax in queries is taken literally', async () => {
  const queries = ['"', '""', 'OR', 'albatross OR lighthouse', 'NEAR(albatross lighthouse)', '*', 'body:albatross',
    '^albatross', 'albatross AND', '(albatross', "'; DROP TABLE search_docs; --", 'alba*', '-albatross', '\u0000'];
  for (const query of queries) {
    const res = await owner.agent.get(`/api/search?content=1&q=${encodeURIComponent(query)}`);
    assert.equal(res.status, 200, query);
  }
  // Operators are words like any other, so they must be in the text too.
  assert.deepEqual(await contentSearch(owner.agent, 'albatross OR lighthouse'), []);
  assert.deepEqual((await contentSearch(owner.agent, 'body:albatross')).map((item) => item.name), []);
  assert.deepEqual((await contentSearch(owner.agent, 'alba*')).map((item) => item.name), ['report.docx']);
  assert.ok(db.prepare("SELECT name FROM sqlite_master WHERE name = 'search_docs'").get());
});

test('the index keeps no plaintext copy of the text', () => {
  const tables = db.prepare("SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'search_fts%'").all()
    .map((row) => row.name);
  assert.deepEqual(tables.sort(), ['search_fts', 'search_fts_config', 'search_fts_data', 'search_fts_docsize', 'search_fts_idx']);
  const sentences = ['Quarterly lighthouse inspection', 'Annual albatross summary', 'crème brûlée', 'creme brulee'];
  const shadowBytes = Buffer.concat(tables.filter((name) => name !== 'search_fts').flatMap((name) => db.prepare(`SELECT * FROM ${name}`).all()
    .flatMap((row) => Object.values(row).filter((value) => value !== null).map((value) => (Buffer.isBuffer(value) ? value : Buffer.from(String(value)))))));
  for (const sentence of sentences) {
    assert.equal(shadowBytes.includes(Buffer.from(sentence)), false, sentence);
  }
  // Reading the FTS table itself gives nothing back either.
  const row = db.prepare('SELECT body FROM search_fts LIMIT 1').get();
  assert.equal(row.body, null);
  // The snippet text is encrypted.
  const texts = Buffer.concat(db.prepare('SELECT text_enc FROM search_docs WHERE text_enc IS NOT NULL').all().map((doc) => doc.text_enc));
  for (const sentence of sentences) assert.equal(texts.includes(Buffer.from(sentence)), false, sentence);
});
