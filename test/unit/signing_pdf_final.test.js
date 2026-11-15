'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('crypto');
const { PDFDocument, PDFName, PDFString } = require('pdf-lib');

const fx = require('../helpers/signing_pdf_fixtures');
const inspect = require('../helpers/signing_pdf_inspect');
const engine = require('../../server/services/signing/pdf');

const { PdfError } = engine;
const FINALIZED = '2026-11-14T10:06:00Z';

const base = (overrides = {}) => ({
  parties: [
    { id: 'p1', name: 'Max Mustermann', position: 1, color: '#d4a017', signerName: 'Max Mustermann', ip: '203.0.113.7', completedAt: '2026-11-14T09:05:00Z' },
  ],
  fields: [],
  values: [],
  events: [],
  document: { id: 'doc1', title: 'Test', finalizedAt: FINALIZED },
  locale: 'en',
  ...overrides,
});

const area = (page, x, y, w, h, extra) => ({ page, x, y, w, h, ...extra });
const field = (id, type, areas, extra = {}) => ({ id, partyId: 'p1', type, name: id, position: Number(id.replace(/\D/g, '')) || 0, areas, ...extra });

async function build(pdfBytes, overrides) {
  return engine.buildFinalPdf({ pdfBytes, ...base(overrides) });
}

test('buildFinalPdf returns the bytes and their SHA-256, and appends certificate pages', async () => {
  const original = await fx.plainPdf({ pages: 2 });
  const { bytes, sha256 } = await build(original);
  assert.equal(sha256, crypto.createHash('sha256').update(bytes).digest('hex'));
  const probe = await engine.probePdf(bytes);
  assert.ok(probe.pageCount >= 3, 'original pages plus at least one certificate page');
  assert.deepEqual(probe.pages[2], { width: 595.28, height: 841.89, rotate: 0 });
  const texts = await inspect.pageTexts(bytes);
  assert.match(texts[2], /Signing certificate/);
  assert.match(texts[2], new RegExp(crypto.createHash('sha256').update(original).digest('hex')));
});

test('buildFinalPdf is deterministic for the same inputs and finalizedAt', async () => {
  const original = await fx.plainPdf();
  const input = {
    fields: [field('f1', 'text', [area(0, 0.1, 0.1, 0.4, 0.04)]), field('f2', 'signature', [area(0, 0.1, 0.3, 0.3, 0.08)])],
    values: [{ fieldId: 'f1', value: 'Same' }, { fieldId: 'f2', bytes: await fx.signaturePng(), method: 'draw' }],
  };
  const a = await build(original, input);
  const b = await build(original, input);
  assert.equal(a.sha256, b.sha256);
  assert.ok(a.bytes.equals(b.bytes));
});

test('buildFinalPdf rejects an original that does not match its recorded hash', async () => {
  await assert.rejects(
    build(await fx.plainPdf(), { document: { id: 'd', title: 't', finalizedAt: FINALIZED, snapshotSha256: 'a'.repeat(64) } }),
    (err) => err instanceof PdfError && err.code === 'HASH_MISMATCH',
  );
  const original = await fx.plainPdf();
  const hash = crypto.createHash('sha256').update(original).digest('hex');
  await build(original, { document: { id: 'd', title: 't', finalizedAt: FINALIZED, snapshot_sha256: hash } });
});

test('geometry: images land where the fractions say for every rotation, CropBox and MediaBox offset', async () => {
  const png = await fx.solidPng(200, 100);
  const cases = [
    { rotate: 0, media: [0, 0, 600, 800] },
    { rotate: 90, media: [0, 0, 600, 800] },
    { rotate: 180, media: [0, 0, 600, 800] },
    { rotate: 270, media: [0, 0, 600, 800] },
    { rotate: 0, media: [50, 60, 650, 860], crop: [120, 130, 520, 700] },
    { rotate: 90, media: [50, 60, 650, 860], crop: [120, 130, 520, 700] },
    { rotate: 180, media: [-30, -40, 570, 760], crop: [10, 10, 400, 500] },
    { rotate: 270, media: [-30, -40, 570, 760], crop: [10, 10, 400, 500] },
  ];
  // Two areas per page so a mirrored or transposed mapping cannot pass by symmetry.
  const wanted = [area(0, 0.1, 0.2, 0.4, 0.1), area(0, 0.55, 0.7, 0.3, 0.15)];
  for (const c of cases) {
    const original = await fx.pageWithBoxes(c);
    const { bytes } = await build(original, {
      fields: [field('f1', 'signature', wanted)],
      values: [{ fieldId: 'f1', bytes: png }],
    });
    const { boxes, width, height } = await inspect.imageBoxes(bytes, 0);
    assert.equal(boxes.length, 2, JSON.stringify(c));
    boxes.sort((a, b) => a.y - b.y);
    const label = JSON.stringify(c);
    wanted.forEach((want, i) => {
      const box = boxes[i];
      const px = { x: want.x * width, y: want.y * height, w: want.w * width, h: want.h * height };
      // The image is contained in the area (4% padding), so check centre and containment.
      assert.ok(Math.abs(box.x + box.w / 2 - (px.x + px.w / 2)) < 0.6, `${label} centre x`);
      assert.ok(Math.abs(box.y + box.h / 2 - (px.y + px.h / 2)) < 0.6, `${label} centre y`);
      assert.ok(box.x >= px.x - 0.01 && box.y >= px.y - 0.01 && box.x + box.w <= px.x + px.w + 0.01 && box.y + box.h <= px.y + px.h + 0.01, `${label} inside`);
      assert.ok(Math.abs(box.w / box.h - 2) < 0.01, `${label} aspect ratio preserved`);
    });
  }
});

test('geometry: a source page that leaves a stray transform behind does not shift our marks', async () => {
  const { bytes } = await build(await fx.pageWithDirtyContent(), {
    fields: [field('f1', 'signature', [area(0, 0.25, 0.25, 0.5, 0.25)])],
    values: [{ fieldId: 'f1', bytes: await fx.solidPng(100, 50) }],
  });
  const { boxes, width, height } = await inspect.imageBoxes(bytes, 0);
  assert.equal(boxes.length, 1);
  assert.ok(Math.abs(boxes[0].x + boxes[0].w / 2 - 0.5 * width) < 0.6);
  assert.ok(Math.abs(boxes[0].y + boxes[0].h / 2 - 0.375 * height) < 0.6);
});

test('every field type renders without error', async () => {
  const png = await fx.signaturePng();
  const original = await fx.plainPdf();
  const fields = [
    field('f1', 'text', [area(0, 0.1, 0.05, 0.5, 0.03)]),
    field('f2', 'phone', [area(0, 0.1, 0.1, 0.5, 0.03)]),
    field('f3', 'number', [area(0, 0.1, 0.15, 0.3, 0.03)], { prefs: { format: 'usd' } }),
    field('f4', 'date', [area(0, 0.1, 0.2, 0.3, 0.03)], { prefs: { format: 'DD/MM/YYYY' } }),
    field('f5', 'cells', [area(0, 0.1, 0.25, 0.4, 0.04)], { prefs: { length: 6 } }),
    field('f6', 'checkbox', [area(0, 0.1, 0.3, 0.03, 0.02)]),
    field('f7', 'radio', [area(0, 0.1, 0.35, 0.03, 0.02), area(0, 0.2, 0.35, 0.03, 0.02)], { options: ['a', 'b'] }),
    field('f8', 'select', [area(0, 0.1, 0.4, 0.3, 0.03)], { options: [{ value: 'x', label: 'Ex' }] }),
    field('f9', 'multiple', [area(0, 0.1, 0.45, 0.5, 0.03)], { options: ['one', 'two', 'three'] }),
    field('f10', 'signature', [area(0, 0.1, 0.5, 0.3, 0.08)]),
    field('f11', 'initials', [area(0, 0.5, 0.5, 0.1, 0.05)]),
    field('f12', 'stamp', [area(0, 0.1, 0.6, 0.2, 0.1)]),
    field('f13', 'image', [area(0, 0.4, 0.6, 0.2, 0.1)]),
    field('f14', 'heading', [area(0, 0.1, 0.75, 0.6, 0.04)], { defaultValue: 'Section' }),
    field('f15', 'file', [area(0, 0.1, 0.8, 0.5, 0.03)]),
    field('f16', 'payment', [area(0, 0.1, 0.85, 0.2, 0.03)]), // not supported: ignored
    field('f17', 'text', [area(7, 0.1, 0.1, 0.2, 0.03)]), // page that does not exist: ignored
  ];
  const values = [
    { fieldId: 'f1', value: 'text' }, { fieldId: 'f2', value: '+49 170 1234567' }, { fieldId: 'f3', value: 1234.5 },
    { fieldId: 'f4', value: '2026-01-31' }, { fieldId: 'f5', value: 'ABC123' }, { fieldId: 'f6', value: true },
    { fieldId: 'f7', value: 'b' }, { fieldId: 'f8', value: 'x' }, { fieldId: 'f9', value: ['one', 'three'] },
    { fieldId: 'f10', bytes: png, method: 'draw' }, { fieldId: 'f11', bytes: png }, { fieldId: 'f12', bytes: await fx.solidPng(40, 40) },
    { fieldId: 'f13', bytes: await fx.solidJpeg(60, 30) },
    { fieldId: 'f15', bytes: Buffer.from('%PDF-attachment'), filename: 'id.pdf', mime: 'application/pdf' },
    { fieldId: 'f16', value: 'ignored' }, { fieldId: 'f17', value: 'ignored' },
  ];
  const { bytes } = await build(original, { fields, values });
  const texts = (await inspect.pageTexts(bytes))[0];
  for (const expected of ['text', '+49 170 1234567', '$1,234.5', '31/01/2026', 'Ex', 'one, three', 'Section', 'id.pdf']) {
    assert.ok(texts.includes(expected), `page text contains ${expected}: ${texts}`);
  }
  const { boxes } = await inspect.imageBoxes(bytes, 0);
  assert.ok(boxes.length >= 3, 'signature, initials and stamp images are drawn');

  const doc = await PDFDocument.load(bytes);
  assert.ok(doc.catalog.lookup(PDFName.of('Names')), 'the file field is attached to the PDF');
});

test('data from the API layer may come as snake_case rows with JSON strings', async () => {
  const { bytes } = await build(await fx.plainPdf(), {
    fields: [
      {
        id: 'f1',
        party_id: 'p1',
        type: 'text',
        name: 'n',
        position: 1,
        prefs_json: JSON.stringify({ font_size: 16, color: '#cc0000' }),
        areas_json: JSON.stringify([{ page: 0, x: 0.1, y: 0.1, w: 0.5, h: 0.05 }]),
      },
    ],
    values: { f1: 'Row value' },
  });
  assert.match((await inspect.pageTexts(bytes))[0], /Row value/);
});

test('Unicode: umlauts, Polish, Cyrillic and Greek survive; unsupported glyphs become "?"', async () => {
  const lines = [
    'Müller Größe Ärger äöüß',
    'Zażółć gęślą jaźń Łódź',
    'Привет, мир! Съешь ещё',
    'Ελληνικά καλημέρα ΩΨ',
    'Mixed 日本語 and emoji \u{1F600}',
  ];
  const fields = lines.map((_, i) => field(`f${i + 1}`, 'text', [area(0, 0.05, 0.05 + i * 0.06, 0.9, 0.04)]));
  const values = lines.map((value, i) => ({ fieldId: `f${i + 1}`, value }));
  const { bytes } = await build(await fx.plainPdf(), { fields, values });
  const text = (await inspect.pageTexts(bytes))[0];
  for (const expected of ['Müller Größe Ärger äöüß', 'Zażółć gęślą jaźń Łódź', 'Привет, мир! Съешь ещё', 'Ελληνικά καλημέρα ΩΨ']) {
    assert.ok(text.includes(expected), `contains ${expected}`);
  }
  assert.ok(text.includes('Mixed ??? and emoji ?'), `unsupported glyphs are replaced: ${text}`);
});

test('long text shrinks, wraps and is cut with an ellipsis instead of overflowing', async () => {
  const long = 'word '.repeat(400).trim();
  const { bytes } = await build(await fx.plainPdf(), {
    fields: [
      field('f1', 'text', [area(0, 0.1, 0.1, 0.3, 0.02)]), // one narrow line
      field('f2', 'text', [area(0, 0.1, 0.2, 0.5, 0.15)], { prefs: { multiline: true } }),
      field('f3', 'text', [area(0, 0.1, 0.5, 0.1, 0.02)]), // a single huge word
    ],
    values: [{ fieldId: 'f1', value: long }, { fieldId: 'f2', value: long }, { fieldId: 'f3', value: 'x'.repeat(500) }],
  });
  const text = (await inspect.pageTexts(bytes))[0];
  assert.ok(text.includes('…'), 'truncation is marked with an ellipsis');
  assert.ok(text.length < 12000);
});

test('a text value shorter than the area is drawn at the requested size and alignment', async () => {
  const { bytes } = await build(await fx.plainPdf(), {
    fields: [field('f1', 'text', [area(0, 0.1, 0.1, 0.8, 0.05)], { prefs: { fontSize: 20, align: 'right' } })],
    values: [{ fieldId: 'f1', value: 'Right' }],
  });
  const { lib, doc } = await (async () => {
    const lib = await import('unpdf').then((m) => m.getResolvedPDFJS());
    return { lib, doc: await lib.getDocument({ data: new Uint8Array(bytes), verbosity: 0 }).promise };
  })();
  const page = await doc.getPage(1);
  const items = (await page.getTextContent()).items.filter((item) => item.str === 'Right');
  assert.equal(items.length, 1);
  assert.ok(Math.abs(items[0].height - 20) < 1.5 || items[0].transform[0] > 19, 'font size honoured');
  const rightEdge = items[0].transform[4] + items[0].width;
  assert.ok(Math.abs(rightEdge - 0.9 * 595.28) < 6, 'right aligned inside the area');
  void lib;
});

test('existing AcroForm fields are flattened and the form is gone', async () => {
  const original = await fx.acroFormPdf();
  const { bytes } = await build(original);
  const doc = await PDFDocument.load(bytes);
  assert.equal(doc.catalog.has(PDFName.of('AcroForm')), false);
  const annotations = doc.getPages()[0].node.get(PDFName.of('Annots'));
  assert.ok(!annotations || annotations.size() === 0, 'no widget annotations remain');
  assert.match((await inspect.pageTexts(bytes))[0], /Prefilled/);
});

test('active content from the source is stripped', async () => {
  const doc = await PDFDocument.create();
  doc.addPage();
  doc.catalog.set(PDFName.of('OpenAction'), doc.context.obj({ S: 'JavaScript', JS: PDFString.of('app.alert(1)') }));
  const { bytes } = await build(Buffer.from(await doc.save()));
  const out = await PDFDocument.load(bytes);
  assert.equal(out.catalog.has(PDFName.of('OpenAction')), false);
});

test('the certificate paginates 60 events and repeats the table header', async () => {
  const events = [];
  for (let i = 0; i < 60; i += 1) {
    events.push({ type: 'opened', at: new Date(Date.UTC(2026, 10, 14, 8, i)).toISOString(), partyId: 'p1', ip: `203.0.113.${i}`, hash: crypto.createHash('sha256').update(String(i)).digest('hex') });
  }
  const { bytes } = await build(await fx.plainPdf(), { events });
  const texts = await inspect.pageTexts(bytes);
  const certificatePages = texts.slice(1);
  assert.ok(certificatePages.length >= 2, `paginated: ${certificatePages.length}`);
  const headers = certificatePages.filter((text) => text.includes('Time (UTC)')).length;
  assert.ok(headers >= 2, 'header repeats on each page the table spans');
  assert.ok(certificatePages.join(' ').includes(events[59].hash), 'hash chain head is printed in full');
  assert.match(certificatePages.join(' '), /Certificate page 1 of \d/);
});

test('German locale uses German certificate strings and date formats', async () => {
  const { bytes } = await build(await fx.plainPdf(), {
    locale: 'de-DE',
    fields: [field('f1', 'date', [area(0, 0.1, 0.1, 0.3, 0.03)])],
    values: [{ fieldId: 'f1', value: '2026-03-05' }],
  });
  const texts = await inspect.pageTexts(bytes);
  assert.match(texts[0], /05\.03\.2026/);
  assert.match(texts[1], /Signaturprotokoll/);
  assert.match(texts[1], /Unterzeichnende/);
});

test('500 fields on 30 pages build within a few seconds', async () => {
  const original = await fx.plainPdf({ pages: 30 });
  const png = await fx.signaturePng();
  const fields = [];
  const values = [];
  for (let i = 0; i < 500; i += 1) {
    const page = i % 30;
    const row = Math.floor(i / 30);
    const type = i % 5 === 0 ? 'signature' : i % 5 === 1 ? 'checkbox' : 'text';
    fields.push(field(`f${i}`, type, [area(page, 0.05, 0.05 + row * 0.05, 0.4, 0.04)]));
    values.push(type === 'signature' ? { fieldId: `f${i}`, bytes: png } : { fieldId: `f${i}`, value: type === 'checkbox' ? true : `Value ${i}` });
  }
  const started = Date.now();
  const { bytes } = await build(original, { fields, values });
  const elapsed = Date.now() - started;
  console.log(`# 500 fields / 30 pages: ${elapsed} ms, ${(bytes.length / 1024).toFixed(0)} KB`);
  assert.ok(elapsed < 5000, `took ${elapsed} ms`);
});

test('a 50 MB source PDF builds with bounded memory', async () => {
  const doc = await PDFDocument.create();
  for (let i = 0; i < 5; i += 1) doc.addPage();
  // ~48 MB of incompressible data in 12 streams, referenced from the catalog so it is kept.
  const refs = [];
  for (let i = 0; i < 12; i += 1) refs.push(doc.context.register(doc.context.stream(crypto.randomBytes(4 * 1024 * 1024))));
  doc.catalog.set(PDFName.of('NeoBallast'), doc.context.obj(refs));
  const original = Buffer.from(await doc.save({ useObjectStreams: false }));
  assert.ok(original.length > 47 * 1024 * 1024 && original.length <= 50 * 1024 * 1024, `fixture is ${original.length} bytes`);

  global.gc?.();
  const rssBefore = process.memoryUsage().rss;
  const started = Date.now();
  const probe = await engine.probePdf(original);
  const probed = Date.now() - started;
  const { bytes } = await build(original, {
    fields: [field('f1', 'text', [area(0, 0.1, 0.1, 0.3, 0.03)])],
    values: [{ fieldId: 'f1', value: 'big' }],
  });
  const elapsed = Date.now() - started;
  const rssGrowthMb = Math.round((process.memoryUsage().rss - rssBefore) / 1024 / 1024);
  console.log(`# 50 MB PDF: probe ${probed} ms, probe+build ${elapsed} ms, rss +${rssGrowthMb} MB, output ${(bytes.length / 1024 / 1024).toFixed(1)} MB`);
  assert.equal(probe.pageCount, 5);
  assert.ok(elapsed < 30000, `took ${elapsed} ms`);
  assert.ok(rssGrowthMb < 1500, `rss grew by ${rssGrowthMb} MB`);
});

test('date and number formats', () => {
  const { formatDate, formatNumber } = require('../../server/services/signing/pdf/format');
  assert.equal(formatDate('2026-03-05', 'D. MMMM YYYY', 'de'), '5. März 2026');
  assert.equal(formatDate('2026-03-05T23:30:00Z', 'MM/DD/YY', 'en'), '03/05/26');
  assert.equal(formatDate('2026-03-05', undefined, 'de'), '05.03.2026');
  assert.equal(formatDate('not a date', 'YYYY', 'en'), 'not a date');
  assert.equal(formatNumber('1234567.891', 'comma'), '1,234,567.891');
  assert.equal(formatNumber(1234567.5, 'eur'), '1.234.567,5 €');
  assert.equal(formatNumber('-9876', 'usd'), '-$9,876');
  assert.equal(formatNumber('12', 'none'), '12');
});

test('certificate: false draws the values but appends no audit pages, deterministically', async () => {
  const original = await fx.plainPdf({ pages: 2 });
  const input = {
    fields: [field('f1', 'text', [area(0, 0.1, 0.1, 0.4, 0.04)])],
    values: [{ fieldId: 'f1', value: 'Visible value' }],
  };
  const full = await build(original, input);
  const clean = await build(original, { ...input, certificate: false });
  const again = await build(original, { ...input, certificate: false });
  assert.equal((await engine.probePdf(clean.bytes)).pageCount, 2);
  assert.ok((await engine.probePdf(full.bytes)).pageCount >= 3);
  assert.notEqual(clean.sha256, full.sha256);
  assert.equal(clean.sha256, again.sha256);
  const texts = await inspect.pageTexts(clean.bytes);
  assert.match(texts[0], /Visible value/);
  assert.ok(texts.every((text) => !/Signing certificate/.test(text)));
});

test('certificate: false keeps the digital seal, over its own content', async () => {
  const original = await fx.plainPdf();
  const { p12Buffer, passphrase } = engine.generateSelfSignedP12({ commonName: 'Test seal' });
  const signer = { p12Buffer, passphrase, reason: 'Signed with NeoCloud', name: 'NeoCloud' };
  const full = await build(original, { signer });
  const clean = await build(original, { signer, certificate: false });
  assert.notEqual(full.sha256, clean.sha256);
  assert.deepEqual(await engine.verifySignedPdf(clean.bytes), { signed: true, intact: true });
  assert.deepEqual(await engine.verifySignedPdf(full.bytes), { signed: true, intact: true });
});
