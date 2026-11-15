'use strict';

// Simple Markdown to an A4 PDF, for the agent API: headings, paragraphs,
// **bold**, *italic*, bulleted and numbered lists and horizontal rules.
// Drawn with pdf-lib and the bundled Noto Sans fonts of the signing engine;
// characters those fonts lack print as "?". There is no italic font, so
// italic text is slanted.
//
// With `signers`, a signature block (signature, printed name and date per
// signer) is drawn at the end and the boxes come back as signing field
// areas: page counted from 0, fractions of the page, origin top-left.

const { PDFDocument, degrees, rgb } = require('pdf-lib');
const { embedFonts } = require('../signing/pdf/fonts');
const { sanitizeText } = require('../signing/pdf/text');
const { loadPdfDocument } = require('../signing/pdf/load');
const { httpError } = require('../../utils/http_error');

const A4 = [595.28, 841.89];
const MARGIN = 64;
const BODY_SIZE = 11;
const LINE = 1.45;
const HEADING_SIZES = [22, 18, 15, 13, 12, 11];
const INK = rgb(0.11, 0.13, 0.09);
const MUTED = rgb(0.45, 0.47, 0.42);
const RULE = rgb(0.75, 0.75, 0.72);
const ITALIC_SKEW = degrees(12);
const MAX_MARKDOWN = 200 * 1024;

const LABELS = {
  en: { signatures: 'Signatures', signature: 'Signature', name: 'Name', date: 'Date' },
  de: { signatures: 'Unterschriften', signature: 'Unterschrift', name: 'Name', date: 'Datum' },
};

/** Inline runs of a line: [{ text, bold, italic }]. */
function inlineRuns(text) {
  const runs = [];
  const pattern = /(\*\*|__)(.+?)\1|(\*|_)(?!\s)(.+?)\3/g;
  let last = 0;
  for (const match of text.matchAll(pattern)) {
    if (match.index > last) runs.push({ text: text.slice(last, match.index), bold: false, italic: false });
    if (match[2] !== undefined) runs.push({ text: match[2], bold: true, italic: false });
    else runs.push({ text: match[4], bold: false, italic: true });
    last = match.index + match[0].length;
  }
  if (last < text.length) runs.push({ text: text.slice(last), bold: false, italic: false });
  return runs.map((run) => ({ ...run, text: run.text.replace(/`/g, '') }));
}

/** Blocks of the document: heading, paragraph, item (bullet or number), rule. */
function parseBlocks(markdown) {
  const blocks = [];
  let paragraph = [];
  const flush = () => {
    if (paragraph.length) blocks.push({ kind: 'paragraph', text: paragraph.join(' ') });
    paragraph = [];
  };
  for (const raw of String(markdown).replace(/\r\n?/g, '\n').split('\n')) {
    const line = raw.trim();
    let match;
    if (!line) {
      flush();
    } else if (/^([-*_])(\s*\1){2,}$/.test(line)) {
      flush();
      blocks.push({ kind: 'rule' });
    } else if ((match = /^(#{1,6})\s+(.*)$/.exec(line))) {
      flush();
      blocks.push({ kind: 'heading', level: match[1].length, text: match[2].replace(/\s+#+$/, '') });
    } else if ((match = /^[-*+]\s+(.*)$/.exec(line))) {
      flush();
      blocks.push({ kind: 'item', marker: '•', text: match[1], indent: raw.search(/\S/) >= 2 ? 1 : 0 });
    } else if ((match = /^(\d{1,4})[.)]\s+(.*)$/.exec(line))) {
      flush();
      blocks.push({ kind: 'item', marker: `${match[1]}.`, text: match[2], indent: raw.search(/\S/) >= 2 ? 1 : 0 });
    } else {
      paragraph.push(line);
    }
  }
  flush();
  return blocks;
}

class Writer {
  constructor(doc, fonts) {
    this.doc = doc;
    this.fonts = fonts;
    this.page = null;
    this.y = 0;
  }

  newPage() {
    this.page = this.doc.addPage(A4);
    this.y = A4[1] - MARGIN;
  }

  ensure(height) {
    if (!this.page || this.y - height < MARGIN) this.newPage();
  }

  font(run) {
    return run.bold ? this.fonts.bold : this.fonts.regular;
  }

  clean(text) {
    return sanitizeText(text, this.fonts.charSet).replace(/\n/g, ' ');
  }

  // Words with their style, so a line can mix bold, italic and plain text.
  words(runs, forceBold) {
    const words = [];
    for (const run of runs) {
      const style = { bold: forceBold || run.bold, italic: run.italic };
      const parts = this.clean(run.text).split(/(\s+)/);
      for (const part of parts) {
        if (part === '') continue;
        if (/^\s+$/.test(part)) words.push({ space: true });
        else words.push({ text: part, ...style });
      }
    }
    return words;
  }

  /** Wraps and draws styled text from `x` to the right margin. */
  paragraph(runs, { size = BODY_SIZE, bold = false, x = MARGIN, color = INK } = {}) {
    const maxWidth = A4[0] - MARGIN - x;
    const lineHeight = size * LINE;
    const spaceWidth = this.fonts.regular.widthOfTextAtSize(' ', size);
    let line = [];
    let width = 0;
    const draw = () => {
      this.ensure(lineHeight);
      let cursor = x;
      for (const word of line) {
        this.page.drawText(word.text, {
          x: cursor, y: this.y - size, size, font: this.font(word), color, ...(word.italic ? { xSkew: ITALIC_SKEW } : {}),
        });
        cursor += word.width + spaceWidth;
      }
      this.y -= lineHeight;
      line = [];
      width = 0;
    };
    for (const word of this.words(runs, bold)) {
      if (word.space) continue;
      const wordWidth = this.font(word).widthOfTextAtSize(word.text, size);
      const needed = line.length ? width + spaceWidth + wordWidth : wordWidth;
      if (line.length && needed > maxWidth) draw();
      line.push({ ...word, width: wordWidth });
      width = line.length === 1 ? wordWidth : width + spaceWidth + wordWidth;
    }
    if (line.length) draw();
  }

  rule() {
    this.ensure(18);
    this.y -= 8;
    this.page.drawLine({ start: { x: MARGIN, y: this.y }, end: { x: A4[0] - MARGIN, y: this.y }, thickness: 0.8, color: RULE });
    this.y -= 10;
  }

  block(block) {
    if (block.kind === 'rule') return this.rule();
    if (block.kind === 'heading') {
      const size = HEADING_SIZES[block.level - 1];
      this.ensure(size * LINE * 2);
      this.y -= size * 0.5;
      this.paragraph(inlineRuns(block.text), { size, bold: true });
      this.y -= size * 0.25;
      return undefined;
    }
    if (block.kind === 'item') {
      const x = MARGIN + 14 + block.indent * 16;
      this.ensure(BODY_SIZE * LINE);
      this.page.drawText(this.clean(block.marker), { x: x - 14, y: this.y - BODY_SIZE, size: BODY_SIZE, font: this.fonts.regular, color: INK });
      this.paragraph(inlineRuns(block.text), { x: x + (block.marker.length > 2 ? 6 : 0) });
      this.y -= 2;
      return undefined;
    }
    this.paragraph(inlineRuns(block.text));
    this.y -= BODY_SIZE * 0.6;
    return undefined;
  }

  // A box as a signing field area: displayed page, origin top-left, fractions.
  area(pageIndex, x, top, w, h) {
    return { page: pageIndex, x: x / A4[0], y: (A4[1] - top) / A4[1], w: w / A4[0], h: h / A4[1] };
  }

  caption(text, x, y) {
    this.page.drawText(this.clean(text), { x, y, size: 8.5, font: this.fonts.regular, color: MUTED });
  }

  /** Draws one block per signer; returns [{ signature, name, date }] areas in signer order. */
  signatureBlock(signers, labels) {
    const BLOCK = 118;
    this.ensure(30 + BLOCK);
    this.y -= 12;
    this.paragraph([{ text: labels.signatures, bold: true }], { size: 13 });
    this.y -= 6;
    return signers.map((signer) => {
      this.ensure(BLOCK);
      const pageIndex = this.doc.getPageCount() - 1;
      const top = this.y;
      // Signature: a 230 × 48 box above a line.
      const sigTop = top - 6;
      this.page.drawLine({ start: { x: MARGIN, y: sigTop - 52 }, end: { x: MARGIN + 240, y: sigTop - 52 }, thickness: 0.8, color: INK });
      this.caption(`${labels.signature} · ${signer.name}`, MARGIN, sigTop - 63);
      // Name and date side by side under it.
      const rowTop = sigTop - 74;
      const nameX = MARGIN;
      const dateX = MARGIN + 260;
      this.page.drawLine({ start: { x: nameX, y: rowTop - 22 }, end: { x: nameX + 230, y: rowTop - 22 }, thickness: 0.6, color: RULE });
      this.page.drawLine({ start: { x: dateX, y: rowTop - 22 }, end: { x: dateX + 140, y: rowTop - 22 }, thickness: 0.6, color: RULE });
      this.caption(labels.name, nameX, rowTop - 32);
      this.caption(labels.date, dateX, rowTop - 32);
      this.y = top - BLOCK;
      return {
        signature: this.area(pageIndex, MARGIN, sigTop, 240, 48),
        name: this.area(pageIndex, nameX, rowTop, 230, 20),
        date: this.area(pageIndex, dateX, rowTop, 140, 20),
      };
    });
  }
}

function labelsFor(locale) {
  return LABELS[locale] || LABELS.en;
}

/**
 * Renders `markdown` (with `title` as a heading above it). Returns
 * `{ bytes, signatureAreas }`; signatureAreas is null without `signers`.
 */
async function renderMarkdownPdf({ title, markdown, signers = null, locale = 'en' }) {
  if (typeof markdown !== 'string') throw httpError(400, 'markdown is required.', 'INVALID_REQUEST');
  if (markdown.length > MAX_MARKDOWN) throw httpError(413, 'The text is too long (200 KB at most).', 'TOO_LARGE');
  const doc = await PDFDocument.create();
  const fonts = await embedFonts(doc);
  if (title) doc.setTitle(sanitizeText(title, fonts.charSet));
  doc.setProducer('NeoCloud');
  doc.setCreator('NeoCloud');
  const writer = new Writer(doc, fonts);
  writer.newPage();
  if (title) writer.block({ kind: 'heading', level: 1, text: title });
  for (const block of parseBlocks(markdown)) writer.block(block);
  const signatureAreas = signers ? writer.signatureBlock(signers, labelsFor(locale)) : null;
  return { bytes: Buffer.from(await doc.save()), signatureAreas };
}

/** Appends a signature page to an existing PDF. Returns `{ bytes, signatureAreas }`. */
async function appendSignaturePage(pdfBytes, { signers, locale = 'en' }) {
  let doc;
  try {
    doc = await loadPdfDocument(pdfBytes);
  } catch (err) {
    if (err?.code === 'PDF_ENCRYPTED') throw httpError(400, 'This PDF is password-protected. Remove the protection first.', 'PDF_ENCRYPTED');
    if (err?.code === 'PDF_TOO_LARGE') throw httpError(413, 'This file is too large to sign (50 MB at most).', 'DOCUMENT_TOO_LARGE');
    if (err?.code === 'PDF_TOO_MANY_PAGES') throw httpError(400, 'This PDF has too many pages.', 'TOO_MANY_PAGES');
    throw httpError(400, 'This file could not be read as a PDF.', 'INVALID_PDF');
  }
  const fonts = await embedFonts(doc);
  const writer = new Writer(doc, fonts);
  writer.newPage();
  const signatureAreas = writer.signatureBlock(signers, labelsFor(locale));
  return { bytes: Buffer.from(await doc.save()), signatureAreas };
}

module.exports = { appendSignaturePage, renderMarkdownPdf };
