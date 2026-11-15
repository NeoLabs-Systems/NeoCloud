'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync } = require('child_process');
const { PDFDocument } = require('pdf-lib');

const home = fs.mkdtempSync(path.join(os.tmpdir(), 'neocloud-signing-pdf-'));
process.env.NEOCLOUD_HOME = home;
test.after(() => fs.rmSync(home, { recursive: true, force: true }));

const fx = require('../helpers/signing_pdf_fixtures');
const engine = require('../../server/services/signing/pdf');
const { boxMatrix, pageGeometry, userRectToArea, userToDisplayedTopLeft } = require('../../server/services/signing/pdf/geometry');

const { PdfError } = engine;

async function rejectsWith(promise, code) {
  await assert.rejects(promise, (err) => err instanceof PdfError && err.code === code);
}

test('probePdf reports page count and displayed sizes', async () => {
  const doc = await PDFDocument.create();
  doc.addPage([595.28, 841.89]);
  doc.addPage([841.89, 595.28]);
  const probe = await engine.probePdf(Buffer.from(await doc.save()));
  assert.equal(probe.pageCount, 2);
  assert.deepEqual(probe.pages, [
    { width: 595.28, height: 841.89, rotate: 0 },
    { width: 841.89, height: 595.28, rotate: 0 },
  ]);
  assert.equal(probe.hasAcroForm, false);
  assert.equal(probe.encrypted, false);
});

test('probePdf swaps width and height for /Rotate 90 and 270 and honours the CropBox', async () => {
  const rotated = await engine.probePdf(await fx.pageWithBoxes({ rotate: 90, media: [0, 0, 600, 800] }));
  assert.deepEqual(rotated.pages[0], { width: 800, height: 600, rotate: 90 });
  const flipped = await engine.probePdf(await fx.pageWithBoxes({ rotate: 270, media: [0, 0, 600, 800] }));
  assert.deepEqual(flipped.pages[0], { width: 800, height: 600, rotate: 270 });
  const upside = await engine.probePdf(await fx.pageWithBoxes({ rotate: 180 }));
  assert.deepEqual(upside.pages[0], { width: 600, height: 800, rotate: 180 });

  const cropped = await engine.probePdf(await fx.pageWithBoxes({ media: [50, 60, 650, 860], crop: [100, 100, 400, 300] }));
  assert.deepEqual(cropped.pages[0], { width: 300, height: 200, rotate: 0 });
  const croppedRotated = await engine.probePdf(await fx.pageWithBoxes({ rotate: 90, media: [0, 0, 600, 800], crop: [0, 0, 300, 200] }));
  assert.deepEqual(croppedRotated.pages[0], { width: 200, height: 300, rotate: 90 });
});

test('probePdf detects AcroForms', async () => {
  assert.equal((await engine.probePdf(await fx.acroFormPdf())).hasAcroForm, true);
});

test('probePdf rejects bad input with typed errors', async () => {
  await rejectsWith(engine.probePdf(Buffer.from('definitely not a pdf')), 'PDF_INVALID');
  await rejectsWith(engine.probePdf(Buffer.alloc(0)), 'PDF_INVALID');
  await rejectsWith(engine.probePdf(Buffer.from('%PDF-1.7\ngarbage garbage garbage')), 'PDF_INVALID');
  await rejectsWith(engine.probePdf(await fx.encryptedPdf()), 'PDF_ENCRYPTED');
  await rejectsWith(engine.probePdf(Buffer.alloc(50 * 1024 * 1024 + 1, 1)), 'PDF_TOO_LARGE');
  await rejectsWith(engine.probePdf(await fx.plainPdf({ pages: 301, label: false })), 'PDF_TOO_MANY_PAGES');
  assert.equal((await engine.probePdf(await fx.plainPdf({ pages: 300, label: false }))).pageCount, 300);
});

test('geometry: the content matrix is the inverse of the user-to-displayed mapping', () => {
  const boxes = [
    { x: 0, y: 0, w: 600, h: 800 },
    { x: 37, y: -20, w: 300, h: 200 },
  ];
  for (const box of boxes) {
    for (const rotate of [0, 90, 180, 270]) {
      const swapped = rotate === 90 || rotate === 270;
      const geo = { box, rotate, width: swapped ? box.h : box.w, height: swapped ? box.w : box.h };
      const [a, b, c, d, e, f] = boxMatrix(geo);
      for (const [X, Y] of [[0, 0], [geo.width, 0], [0, geo.height], [geo.width, geo.height], [12.5, 77.25]]) {
        const ux = a * X + c * Y + e;
        const uy = b * X + d * Y + f;
        const { dx, dy } = userToDisplayedTopLeft(ux, uy, geo);
        assert.ok(Math.abs(dx - X) < 1e-9, `rotate ${rotate} dx`);
        assert.ok(Math.abs(dy - (geo.height - Y)) < 1e-9, `rotate ${rotate} dy`);
      }
    }
  }
});

test('geometry: userRectToArea gives top-left fractions of the displayed page', async () => {
  const doc = await PDFDocument.create();
  const page = doc.addPage([600, 800]);
  page.setRotation({ type: 'degrees', angle: 90 });
  const geo = pageGeometry(page);
  // A rectangle at the user-space bottom-left corner is the TOP-left of a page rotated 90 degrees clockwise.
  const area = userRectToArea({ x: 0, y: 0, width: 60, height: 80 }, geo);
  assert.deepEqual(area, { x: 0, y: 0, w: 0.1, h: 0.1 });
});

test('importAcroFields maps widgets to field types with fractional coordinates', async () => {
  const fields = await engine.importAcroFields(await fx.acroFormPdf());
  const byName = Object.fromEntries(fields.map((field) => [field.name, field]));
  assert.deepEqual(Object.keys(byName).sort(), ['agree', 'country', 'full_name', 'notes', 'plan']); // "locked" is read-only

  assert.equal(byName.full_name.type, 'text');
  assert.equal(byName.full_name.required, true);
  assert.equal(byName.notes.type, 'text');
  assert.equal(byName.notes.prefs.multiline, true);
  assert.equal(byName.agree.type, 'checkbox');
  assert.equal(byName.agree.required, false);
  assert.equal(byName.country.type, 'select');
  assert.deepEqual(byName.country.options, ['Germany', 'Poland', 'Greece']);
  assert.equal(byName.plan.type, 'radio');
  assert.deepEqual(byName.plan.options, ['basic', 'pro']);
  assert.deepEqual(byName.plan.areas.map((area) => area.option), ['basic', 'pro']);

  // full_name widget: x 50, y 700 (bottom-left), 200 x 24 on a 600 x 800 page.
  const [area] = byName.full_name.areas;
  assert.equal(area.page, 0);
  // (pdf-lib pads widget rectangles by about half a point on each side.)
  assert.ok(Math.abs(area.x - 50 / 600) < 2e-3);
  assert.ok(Math.abs(area.y - (800 - 724) / 800) < 2e-3);
  assert.ok(Math.abs(area.w - 200 / 600) < 3e-3);
  assert.ok(Math.abs(area.h - 24 / 800) < 3e-3);

  const order = fields.map((field) => field.name);
  assert.ok(order.indexOf('full_name') < order.indexOf('notes'), 'reading order: top to bottom');
});

test('importAcroFields on a rotated page maps into the displayed page', async () => {
  const doc = await PDFDocument.create();
  const page = doc.addPage([600, 800]);
  const field = doc.getForm().createTextField('r');
  field.addToPage(page, { x: 0, y: 0, width: 60, height: 80 }); // user-space bottom-left corner
  page.setRotation({ type: 'degrees', angle: 90 });
  const [imported] = await engine.importAcroFields(Buffer.from(await doc.save()));
  const [area] = imported.areas;
  assert.equal(area.page, 0);
  assert.ok(area.x < 0.01 && area.y < 0.01, 'anchored at the displayed top-left corner');
  assert.ok(Math.abs(area.w - 80 / 800) < 3e-3 && Math.abs(area.h - 60 / 600) < 3e-3);
});

test('importAcroFields returns [] for a PDF without a form', async () => {
  assert.deepEqual(await engine.importAcroFields(await fx.plainPdf()), []);
});

test('imageToPdf wraps JPEG and PNG on an A4 page, fitted inside the margins', async () => {
  for (const [bytes, mime] of [[await fx.solidJpeg(400, 300), 'image/jpeg'], [await fx.solidPng(300, 400), 'image/png']]) {
    const pdf = await engine.imageToPdf(bytes, mime);
    const probe = await engine.probePdf(pdf);
    assert.equal(probe.pageCount, 1);
    assert.ok(Math.abs(probe.pages[0].width - 595.28) < 0.01 || Math.abs(probe.pages[0].width - 841.89) < 0.01);
  }
  const portrait = await engine.probePdf(await engine.imageToPdf(await fx.solidPng(300, 400), 'image/png'));
  assert.deepEqual([portrait.pages[0].width, portrait.pages[0].height], [595.28, 841.89]);
  const landscape = await engine.probePdf(await engine.imageToPdf(await fx.solidPng(400, 300), 'image/png'));
  assert.deepEqual([landscape.pages[0].width, landscape.pages[0].height], [841.89, 595.28]);
});

test('imageToPdf normalises WebP and EXIF orientation, and rejects non-images', async () => {
  const sharp = require('sharp');
  const webp = await sharp({ create: { width: 200, height: 100, channels: 3, background: '#336699' } }).webp().toBuffer();
  assert.equal((await engine.probePdf(await engine.imageToPdf(webp, 'image/webp'))).pageCount, 1);

  // 200x100 stored sideways with orientation 6 displays as 100x200: a portrait page.
  const rotated = await sharp({ create: { width: 200, height: 100, channels: 3, background: '#993333' } })
    .jpeg()
    .withMetadata({ orientation: 6 })
    .toBuffer();
  const probe = await engine.probePdf(await engine.imageToPdf(rotated, 'image/jpeg'));
  assert.deepEqual([probe.pages[0].width, probe.pages[0].height], [595.28, 841.89]);

  await rejectsWith(engine.imageToPdf(Buffer.from('nope'), 'image/png'), 'IMAGE_INVALID');
  await rejectsWith(engine.imageToPdf(await fx.solidPng(10, 10), 'application/pdf'), 'IMAGE_INVALID');
});

test('docToPdf validates the extension and reports a missing converter', async () => {
  await rejectsWith(engine.docToPdf(Buffer.from('x'), 'virus.exe'), 'UNSUPPORTED_DOCUMENT');
  await rejectsWith(engine.docToPdf(Buffer.alloc(0), 'empty.docx'), 'UNSUPPORTED_DOCUMENT');
});

test('canConvertDocuments is false without LibreOffice and docToPdf throws CONVERSION_UNAVAILABLE', (t) => {
  const fallbacks = ['/Applications/LibreOffice.app/Contents/MacOS/soffice', 'C:\\Program Files\\LibreOffice\\program\\soffice.exe'];
  if (fallbacks.some((candidate) => fs.existsSync(candidate))) t.skip('LibreOffice is installed at a fixed location');
  const script = `
    const e = require(${JSON.stringify(path.resolve('server/services/signing/pdf'))});
    e.docToPdf(Buffer.from('hello'), 'a.docx').then(
      () => console.log('converted'),
      (err) => console.log(e.canConvertDocuments(), err.code));
  `;
  const out = execFileSync(process.execPath, ['-e', script], {
    env: { PATH: '/nonexistent', NEOCLOUD_HOME: home },
    encoding: 'utf8',
  });
  assert.equal(out.trim(), 'false CONVERSION_UNAVAILABLE');
});

test('docToPdf converts through a LibreOffice stand-in and cleans up its work folder', async () => {
  const fixturePdf = path.join(home, 'result.pdf');
  fs.writeFileSync(fixturePdf, await fx.plainPdf({ pages: 2 }));
  const fake = path.join(home, 'soffice');
  fs.writeFileSync(
    fake,
    `#!/bin/sh
if [ "$1" = "--version" ]; then echo "LibreOffice 0.0 stand-in"; exit 0; fi
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--outdir" ]; then out="$2"; shift; fi
  shift
done
cp "${fixturePdf}" "$out/document.pdf"
`,
    { mode: 0o755 },
  );
  const script = `
    const e = require(${JSON.stringify(path.resolve('server/services/signing/pdf'))});
    (async () => {
      const ok = e.canConvertDocuments();
      const pdf = await e.docToPdf(Buffer.from('fake docx'), 'Contract.DOCX');
      const probe = await e.probePdf(pdf);
      console.log(ok, probe.pageCount);
    })().catch((err) => { console.log('failed', err.code, err.message); process.exit(1); });
  `;
  const tmpBefore = fs.readdirSync(os.tmpdir()).filter((name) => name.startsWith('neocloud-sign-'));
  const out = execFileSync(process.execPath, ['-e', script], {
    env: { PATH: process.env.PATH, NEOCLOUD_LIBREOFFICE_PATH: fake, NEOCLOUD_HOME: home },
    encoding: 'utf8',
  });
  assert.equal(out.trim(), 'true 2');
  const tmpAfter = fs.readdirSync(os.tmpdir()).filter((name) => name.startsWith('neocloud-sign-'));
  assert.deepEqual(tmpAfter.sort(), tmpBefore.sort());
});
