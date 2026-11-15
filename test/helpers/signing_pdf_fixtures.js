'use strict';

// Generators for the PDF engine tests (pdf-lib builds every fixture).

const { PDFDocument, StandardFonts, degrees } = require('pdf-lib');
const sharp = require('sharp');

const A4 = [595.28, 841.89];

async function savePdf(doc) {
  return Buffer.from(await doc.save());
}

/** `pages` blank pages, each labelled "Page n" so renders are recognisable. */
async function plainPdf({ pages = 1, size = A4, label = true } = {}) {
  const doc = await PDFDocument.create();
  const font = await doc.embedFont(StandardFonts.Helvetica);
  for (let i = 0; i < pages; i += 1) {
    const page = doc.addPage(size);
    if (label) page.drawText(`Page ${i + 1}`, { x: 40, y: size[1] - 50, size: 14, font });
  }
  return savePdf(doc);
}

/** One page with /Rotate, optionally a CropBox and a MediaBox offset. */
async function pageWithBoxes({ rotate = 0, media = [0, 0, 600, 800], crop } = {}) {
  const doc = await PDFDocument.create();
  const font = await doc.embedFont(StandardFonts.Helvetica);
  const page = doc.addPage();
  page.setMediaBox(media[0], media[1], media[2] - media[0], media[3] - media[1]);
  if (crop) page.setCropBox(crop[0], crop[1], crop[2] - crop[0], crop[3] - crop[1]);
  page.setRotation(degrees(rotate));
  page.drawText('Top left of the unrotated page', { x: media[0] + 20, y: media[3] - 40, size: 12, font });
  return savePdf(doc);
}

/** Text, checkbox, radio and dropdown widgets on page 1. */
async function acroFormPdf() {
  const doc = await PDFDocument.create();
  const page = doc.addPage([600, 800]);
  const form = doc.getForm();

  const name = form.createTextField('full_name');
  name.addToPage(page, { x: 50, y: 700, width: 200, height: 24 });
  name.setText('Prefilled');
  name.enableRequired();

  const notes = form.createTextField('notes');
  notes.enableMultiline();
  notes.addToPage(page, { x: 50, y: 600, width: 300, height: 80 });

  const agree = form.createCheckBox('agree');
  agree.addToPage(page, { x: 50, y: 560, width: 16, height: 16 });

  const radio = form.createRadioGroup('plan');
  radio.addOptionToPage('basic', page, { x: 50, y: 520, width: 14, height: 14 });
  radio.addOptionToPage('pro', page, { x: 120, y: 520, width: 14, height: 14 });

  const country = form.createDropdown('country');
  country.addOptions(['Germany', 'Poland', 'Greece']);
  country.addToPage(page, { x: 50, y: 470, width: 160, height: 24 });

  const locked = form.createTextField('locked');
  locked.addToPage(page, { x: 50, y: 420, width: 100, height: 20 });
  locked.enableReadOnly();

  return savePdf(doc);
}

/** A PDF whose trailer carries /Encrypt, which is all pdf-lib looks at. */
async function encryptedPdf() {
  const doc = await PDFDocument.create();
  doc.addPage();
  const encrypt = doc.context.obj({ Filter: 'Standard', V: 1, R: 2, O: '<00>', U: '<00>', P: -4 });
  doc.context.trailerInfo.Encrypt = doc.context.register(encrypt);
  return savePdf(doc);
}

/** A page whose content stream leaves a stray transform behind (no q/Q). */
async function pageWithDirtyContent() {
  const doc = await PDFDocument.create();
  const page = doc.addPage([600, 800]);
  const stream = doc.context.flateStream('0.5 0 0 0.5 100 100 cm\n');
  page.node.addContentStream(doc.context.register(stream));
  return savePdf(doc);
}

async function solidPng(width, height, color = { r: 20, g: 60, b: 200 }) {
  return sharp({ create: { width, height, channels: 3, background: color } }).png().toBuffer();
}

async function solidJpeg(width, height, color = { r: 200, g: 40, b: 40 }) {
  return sharp({ create: { width, height, channels: 3, background: color } }).jpeg().toBuffer();
}

/** A transparent PNG with a diagonal "signature" stroke, as a client would send. */
async function signaturePng(width = 300, height = 100) {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}">
    <path d="M10 ${height * 0.8} C ${width * 0.3} ${height * 0.1}, ${width * 0.5} ${height * 0.95}, ${width - 10} ${height * 0.2}"
      fill="none" stroke="#0a2a8a" stroke-width="4"/></svg>`;
  return sharp(Buffer.from(svg)).png().toBuffer();
}

module.exports = {
  A4,
  acroFormPdf,
  encryptedPdf,
  pageWithBoxes,
  pageWithDirtyContent,
  plainPdf,
  signaturePng,
  solidJpeg,
  solidPng,
};
