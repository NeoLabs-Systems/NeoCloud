'use strict';

const { PDFDocument } = require('pdf-lib');
const { PdfError } = require('./errors');
const { prepareImage } = require('./images');

const A4 = { short: 595.28, long: 841.89 };
const MARGIN = 36;

/**
 * One image on one A4 page (landscape for wide images), scaled to fit inside
 * the margins and centred. `mime` is only a guard: the real format is sniffed.
 */
async function imageToPdf(bytes, mime) {
  if (mime && !/^image\//i.test(String(mime))) throw new PdfError('IMAGE_INVALID', 'Not an image.');
  const image = await prepareImage(bytes);

  const doc = await PDFDocument.create();
  doc.setProducer('NeoCloud');
  doc.setCreator('NeoCloud');
  const embedded = image.kind === 'png' ? await doc.embedPng(image.bytes) : await doc.embedJpg(image.bytes);

  const landscape = image.width > image.height;
  const pageW = landscape ? A4.long : A4.short;
  const pageH = landscape ? A4.short : A4.long;
  const scale = Math.min((pageW - 2 * MARGIN) / image.width, (pageH - 2 * MARGIN) / image.height);
  const w = image.width * scale;
  const h = image.height * scale;
  const page = doc.addPage([pageW, pageH]);
  page.drawImage(embedded, { x: (pageW - w) / 2, y: (pageH - h) / 2, width: w, height: h });
  return Buffer.from(await doc.save());
}

module.exports = { imageToPdf };
