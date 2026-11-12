'use strict';

// Auto-title's local half: which names are meaningless, how a name is built
// from a template and made safe, which date wins, and how the model's answer
// is checked. No model, no database.

const test = require('node:test');
const assert = require('node:assert/strict');
const {
  chooseDate,
  classifyName,
  dateFromName,
  renderName,
  sanitizeFileName,
} = require('../../server/services/ai/organize/naming');
const { readExif } = require('../../server/utils/exif');
const { normalizeExtraction } = require('../../server/services/ai/organize/fields');

test('the candidate heuristic flags meaningless names and leaves readable ones', () => {
  const table = [
    ['IMG_2041.jpg', { meaningless: true, camera: true }],
    ['PXL_20260312_142233512.jpg', { meaningless: true, camera: true }],
    ['DSC01234.JPG', { meaningless: true, camera: true }],
    ['20260312_142233.jpg', { meaningless: true, camera: true }],
    ['WhatsApp Image 2026-03-12 at 14.22.33.jpeg', { meaningless: true, camera: true }],
    ['Screenshot 2026-03-12 at 14.22.33.png', { meaningless: true, screenshot: true }],
    ['Bildschirmfoto 2026-03-12 um 14.22.33.png', { meaningless: true, screenshot: true }],
    ['Scan_20260312.pdf', { meaningless: true, scan: true }],
    ['scan0001.pdf', { meaningless: true, scan: true }],
    ['SCN_0042.pdf', { meaningless: true, scan: true }],
    ['doc00123.pdf', { meaningless: true, scan: true }],
    ['3f2a9c1e-1b2c-4d5e-8f90-123456789abc.pdf', { meaningless: true }],
    ['a8f3e9c2b1d4e5f6.pdf', { meaningless: true }],
    ['x7Kq9mZ2pL4vB8nR3t.pdf', { meaningless: true }],
    ['1710252153123.pdf', { meaningless: true }],
    ['2026-03-12 14.22.33.pdf', { meaningless: true }],
    ['download (3).pdf', { meaningless: true }],
    ['document.pdf', { meaningless: true }],
    ['document (2).pdf', { meaningless: true }],
    ['untitled.txt', { meaningless: true }],
    ['Untitled document.docx', { meaningless: true }],
    ['Unbenannt-1.docx', { meaningless: true }],
    ['invoice.pdf', { meaningless: true }],
    ['Rechnung Telekom März.pdf', { meaningless: false }],
    ['Holiday plan.docx', { meaningless: false }],
    ['project-notes-v2.md', { meaningless: false }],
    ['Bericht 2026.pdf', { meaningless: false }],
    ['2026-03-12 Telekom – Rechnung.pdf', { meaningless: false }],
    ['README', { meaningless: false }],
  ];
  for (const [name, expected] of table) {
    const result = classifyName(name);
    for (const [key, value] of Object.entries(expected)) {
      assert.equal(result[key], value, `${name}: ${key}`);
    }
  }
});

test('names come from the template, missing values drop out with their separators', () => {
  const fields = { date: '2026-03-12', issuer: 'Telekom', subject: 'Mobilfunk März', type: 'invoice', amount: 39.99, currency: 'eur' };
  assert.equal(renderName('{date} {issuer} – {subject}', fields, 'scan0001.pdf'), '2026-03-12 Telekom – Mobilfunk März.pdf');
  assert.equal(renderName('{date} {issuer} {type} {amount}{currency}', fields, 'x.pdf'), '2026-03-12 Telekom invoice 39.99EUR.pdf');
  assert.equal(renderName('{year}-{month} {subject} ({original})', fields, 'IMG_1.JPG'), '2026-03 Mobilfunk März (IMG_1).JPG');
  assert.equal(renderName('{date} {issuer} – {subject}', { subject: 'Mietvertrag' }, 'doc1.pdf'), 'Mietvertrag.pdf');
  assert.equal(renderName('{date} {issuer} – {subject}', { date: '2026-03-12', issuer: 'Telekom' }, 'doc1.pdf'), '2026-03-12 Telekom.pdf');
  assert.equal(renderName('{subject} [{amount}]', { subject: 'Quittung' }, 'a.pdf'), 'Quittung.pdf');
  // Nothing extracted: no name.
  assert.equal(renderName('{date} {issuer} – {subject}', {}, 'doc1.pdf'), null);
  // A template without known placeholders falls back to the default.
  assert.equal(renderName('nothing here', fields, 'a.pdf'), '2026-03-12 Telekom – Mobilfunk März.pdf');
  // The extension stays as it was, even without one.
  assert.equal(renderName('{subject}', { subject: 'Notes' }, 'README'), 'Notes');
});

test('names are sanitized for every OS and capped', () => {
  assert.equal(renderName('{subject}', { subject: 'a/b\\c: d*e?f"g<h>i|j' }, 'x.txt'), 'a-b-c- defghij.txt');
  assert.equal(renderName('{issuer} {subject}', { issuer: '..hidden', subject: 'trail...' }, 'x.txt'), 'hidden trail.txt');
  assert.equal(renderName('{subject}', { subject: 'CON' }, 'x.txt'), 'CON_.txt');
  assert.equal(renderName('{subject}', { subject: 'line\nbreak\ttab' }, 'x.txt'), 'line break tab.txt');
  const long = renderName('{subject}', { subject: 'Wort '.repeat(60) }, 'x.pdf');
  assert.ok(long.length <= 120);
  assert.ok(long.endsWith('.pdf'));
  assert.ok(!/\s\.pdf$/.test(long));
  assert.equal(sanitizeFileName('   ', '.txt'), null);
  const emoji = sanitizeFileName('🙂'.repeat(200), '.txt');
  assert.ok(Buffer.byteLength(emoji) <= 255 && emoji.endsWith('.txt'));
});

test('dates: content, then EXIF, then the file name, then the modification time, each plausible', () => {
  const base = { modifiedAt: '2026-04-01 10:00:00', uploadedAt: '2026-04-02 10:00:00' };
  assert.deepEqual(chooseDate({ ...base, contentDate: '2026-03-12', exifDate: '2025-05-05', name: 'IMG_20240101.jpg' }),
    { date: '2026-03-12', dateSource: 'content' });
  assert.deepEqual(chooseDate({ ...base, contentDate: '12.03.2026' }), { date: '2026-03-12', dateSource: 'content' });
  // A content date after the upload, or before 1900, or not a date at all is not believed.
  assert.deepEqual(chooseDate({ ...base, contentDate: '2099-01-01', exifDate: '2025-05-05' }), { date: '2025-05-05', dateSource: 'exif' });
  assert.deepEqual(chooseDate({ ...base, contentDate: '1850-01-01', name: 'scan_2024-01-31.pdf' }), { date: '2024-01-31', dateSource: 'filename' });
  assert.deepEqual(chooseDate({ ...base, contentDate: '2026-02-30', name: 'PXL_20240229_101010.jpg' }), { date: '2024-02-29', dateSource: 'filename' });
  assert.deepEqual(chooseDate({ ...base, contentDate: 'soon', name: 'download.pdf' }), { date: '2026-04-01', dateSource: 'modified' });
  assert.deepEqual(chooseDate({ contentDate: null, name: 'x', modifiedAt: '', uploadedAt: '' }), { date: null, dateSource: null });
  assert.equal(dateFromName('IMG_20260312_101010.jpg'), '2026-03-12');
  assert.equal(dateFromName('Scan 2026_03_12.pdf'), '2026-03-12');
  assert.equal(dateFromName('IMG_2041.jpg'), null);
  assert.equal(dateFromName('20261340.pdf'), null);
});

test('the EXIF reader finds the camera and the capture date', () => {
  // A little-endian TIFF block: IFD0 with Make and an EXIF IFD with DateTimeOriginal.
  const make = Buffer.from('Google\0');
  const date = Buffer.from('2026:03:12 14:22:33\0');
  const ifd0 = 8;
  const ifd0Size = 2 + 2 * 12 + 4;
  const exifIfd = ifd0 + ifd0Size;
  const exifSize = 2 + 12 + 4;
  const makeAt = exifIfd + exifSize;
  const dateAt = makeAt + make.length;
  const block = Buffer.alloc(dateAt + date.length);
  block.write('II', 0, 'latin1');
  block.writeUInt16LE(42, 2);
  block.writeUInt32LE(ifd0, 4);
  block.writeUInt16LE(2, ifd0);
  block.writeUInt16LE(0x010f, ifd0 + 2);
  block.writeUInt16LE(2, ifd0 + 4);
  block.writeUInt32LE(make.length, ifd0 + 6);
  block.writeUInt32LE(makeAt, ifd0 + 10);
  block.writeUInt16LE(0x8769, ifd0 + 14);
  block.writeUInt16LE(4, ifd0 + 16);
  block.writeUInt32LE(1, ifd0 + 18);
  block.writeUInt32LE(exifIfd, ifd0 + 22);
  block.writeUInt16LE(1, exifIfd);
  block.writeUInt16LE(0x9003, exifIfd + 2);
  block.writeUInt16LE(2, exifIfd + 4);
  block.writeUInt32LE(date.length, exifIfd + 6);
  block.writeUInt32LE(dateAt, exifIfd + 10);
  make.copy(block, makeAt);
  date.copy(block, dateAt);
  assert.deepEqual(readExif(Buffer.concat([Buffer.from('Exif\0\0', 'latin1'), block])), { make: 'Google', model: null, takenAt: '2026-03-12' });
  assert.deepEqual(readExif(Buffer.from('garbage')), { make: null, model: null, takenAt: null });
  assert.deepEqual(readExif(undefined), { make: null, model: null, takenAt: null });
});

test('the model’s answer is checked against the text and cleaned', () => {
  const node = { name: 'scan0001.pdf', modified_at: '2026-04-01 10:00:00', created_at: '2026-04-02 10:00:00' };
  const text = 'Telekom Deutschland GmbH, Rechnung vom 12.03.2026, Betrag 39,99 EUR';
  const confidence = { date: 0.9, issuer: 0.9, subject: 0.9, type: 0.9, amount: 0.9, tags: 0.9 };
  const fields = normalizeExtraction({
    date: '2026-03-12', issuer: 'Telekom', subject: '  Mobilfunk\nMärz ', type: 'Invoice', amount: '39,99', currency: 'eur',
    tags: ['Telefon', 'telefon', '<b>x</b>'], language: 'DE', confidence, overall: 2,
  }, { text, node, exif: null });
  assert.equal(fields.issuer, 'Telekom');
  assert.equal(fields.subject, 'Mobilfunk März');
  assert.equal(fields.type, 'invoice');
  assert.equal(fields.amount, 39.99);
  assert.equal(fields.currency, 'EUR');
  assert.deepEqual(fields.tags, ['telefon', 'bxb']);
  assert.equal(fields.language, 'de');
  assert.equal(fields.overall, 1);
  assert.equal(fields.confidence.issuer, 0.9);
  assert.equal(fields.dateSource, 'content');
  // An issuer or amount the text doesn't contain is believed less; an unknown type is "other".
  const doubtful = normalizeExtraction({ issuer: 'Vodafone', amount: 77, type: 'poem', confidence }, { text, node, exif: { takenAt: '2025-05-05' } });
  assert.ok(doubtful.confidence.issuer < 0.9);
  assert.ok(doubtful.confidence.amount < 0.9);
  assert.equal(doubtful.type, 'other');
  assert.equal(doubtful.dateSource, 'exif');
  assert.equal(doubtful.confidence.date, 0.9);
});
