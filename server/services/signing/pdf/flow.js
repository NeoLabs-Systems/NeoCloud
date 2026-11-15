'use strict';

// A tiny top-to-bottom layout engine for the certificate pages: a cursor that
// moves down A4 pages, wraps text, breaks pages and repeats table headers.

const { rgb } = require('pdf-lib');
const { LINE_HEIGHT, sanitizeText, wrapText } = require('./text');

const PAGE = { width: 595.28, height: 841.89 };
const MARGIN = 48;
const BOTTOM = 56;
const CONTENT_WIDTH = PAGE.width - 2 * MARGIN;

const COLORS = {
  ink: rgb(0.1, 0.1, 0.13),
  muted: rgb(0.42, 0.42, 0.47),
  rule: rgb(0.8, 0.8, 0.83),
  band: rgb(0.955, 0.955, 0.965),
  accent: rgb(0.62, 0.45, 0.1),
};

class Flow {
  /** `footer(pageNumber, pageTotal)` returns the two footer strings { left, right }. */
  constructor(doc, fonts, footer) {
    this.doc = doc;
    this.fonts = fonts;
    this.footer = footer;
    this.pages = [];
    this.newPage();
  }

  newPage() {
    this.page = this.doc.addPage([PAGE.width, PAGE.height]);
    this.pages.push(this.page);
    this.y = PAGE.height - MARGIN;
  }

  /** Starts a new page unless `height` points still fit above the bottom margin. */
  ensure(height) {
    if (this.y - height < BOTTOM) {
      this.newPage();
      return true;
    }
    return false;
  }

  gap(points) {
    this.y -= points;
  }

  clean(text) {
    return sanitizeText(text, this.fonts.charSet);
  }

  fontOf(bold) {
    return bold ? this.fonts.bold : this.fonts.regular;
  }

  measurerOf(bold) {
    return bold ? this.fonts.measure.bold : this.fonts.measure.regular;
  }

  /** Wrapped text at the cursor; returns the number of lines drawn. */
  paragraph(raw, { size = 9, bold = false, color = COLORS.ink, x = MARGIN, width = CONTENT_WIDTH - (x - MARGIN) } = {}) {
    const font = this.fontOf(bold);
    const lines = wrapText(this.clean(raw), this.measurerOf(bold), size, width);
    const lineHeight = size * LINE_HEIGHT;
    for (const line of lines) {
      this.ensure(lineHeight);
      this.page.drawText(line, { x, y: this.y - size, size, font, color });
      this.y -= lineHeight;
    }
    return lines.length;
  }

  /** Section heading with a rule under it; keeps at least two lines with it. */
  heading(raw) {
    this.ensure(60);
    this.gap(10);
    this.paragraph(raw, { size: 12, bold: true });
    this.page.drawLine({
      start: { x: MARGIN, y: this.y - 1 },
      end: { x: MARGIN + CONTENT_WIDTH, y: this.y - 1 },
      thickness: 0.8,
      color: COLORS.accent,
    });
    this.gap(7);
  }

  /** "Label  value" on one row; the value wraps in its own column. */
  keyValue(label, value, { labelWidth = 130, size = 8.5 } = {}) {
    if (value === undefined || value === null || value === '') return;
    const font = this.fonts.regular;
    const lines = wrapText(this.clean(value), this.measurerOf(false), size, CONTENT_WIDTH - labelWidth);
    const lineHeight = size * LINE_HEIGHT;
    this.ensure(lineHeight);
    this.page.drawText(this.clean(label), { x: MARGIN, y: this.y - size, size, font, color: COLORS.muted });
    for (const line of lines) {
      this.ensure(lineHeight);
      this.page.drawText(line, { x: MARGIN + labelWidth, y: this.y - size, size, font, color: COLORS.ink });
      this.y -= lineHeight;
    }
  }

  /** A filled dot, used for the party colour. */
  dot(color, x, size = 3.2) {
    this.page.drawCircle({ x, y: this.y - 6.5, size, color });
  }

  /**
   * Table with a repeating header. `columns`: [{ title, width }]; `rows`:
   * arrays of strings. Cells wrap; a row never splits across pages.
   */
  table(columns, rows, { size = 8 } = {}) {
    const font = this.fonts.regular;
    const lineHeight = size * LINE_HEIGHT;
    const padX = 3;
    const padY = 2.5;
    const drawHeader = () => {
      this.ensure(lineHeight + 2 * padY);
      this.page.drawRectangle({ x: MARGIN, y: this.y - lineHeight - 2 * padY, width: CONTENT_WIDTH, height: lineHeight + 2 * padY, color: COLORS.band });
      let x = MARGIN;
      for (const column of columns) {
        this.page.drawText(this.clean(column.title), { x: x + padX, y: this.y - padY - size, size, font: this.fonts.bold, color: COLORS.ink });
        x += column.width;
      }
      this.y -= lineHeight + 2 * padY;
    };
    drawHeader();
    for (const row of rows) {
      const cells = row.map((cell, i) => wrapText(this.clean(cell), this.measurerOf(false), size, columns[i].width - 2 * padX));
      const height = Math.max(...cells.map((lines) => lines.length)) * lineHeight + 2 * padY;
      if (this.ensure(height)) drawHeader();
      let x = MARGIN;
      cells.forEach((lines, i) => {
        lines.forEach((line, n) => {
          this.page.drawText(line, { x: x + padX, y: this.y - padY - size - n * lineHeight, size, font, color: COLORS.ink });
        });
        x += columns[i].width;
      });
      this.page.drawLine({
        start: { x: MARGIN, y: this.y - height },
        end: { x: MARGIN + CONTENT_WIDTH, y: this.y - height },
        thickness: 0.4,
        color: COLORS.rule,
      });
      this.y -= height;
    }
  }

  /** Draws the footer of every page; call once, last. */
  finish() {
    const total = this.pages.length;
    this.pages.forEach((page, index) => {
      const { left, right } = this.footer(index + 1, total);
      const font = this.fonts.regular;
      const size = 7.5;
      page.drawLine({ start: { x: MARGIN, y: 38 }, end: { x: MARGIN + CONTENT_WIDTH, y: 38 }, thickness: 0.4, color: COLORS.rule });
      page.drawText(this.clean(left), { x: MARGIN, y: 26, size, font, color: COLORS.muted });
      const width = font.widthOfTextAtSize(this.clean(right), size);
      page.drawText(this.clean(right), { x: MARGIN + CONTENT_WIDTH - width, y: 26, size, font, color: COLORS.muted });
    });
  }
}

module.exports = { COLORS, CONTENT_WIDTH, MARGIN, Flow };
