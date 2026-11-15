'use strict';

const { rgb } = require('pdf-lib');
const { parseColor } = require('./normalize');
const { BASELINE, LINE_HEIGHT, fitText, sanitizeText } = require('./text');

const INK = [0.07, 0.07, 0.1];
const GRID = [0.62, 0.62, 0.66];

function colorOf(input, fallback = INK) {
  return rgb(...(parseColor(input) || fallback));
}

/** The weight a field asks for: `{ font, measurer }`. */
function fontFor(ctx, prefs, boldByDefault = false) {
  const bold = prefs.fontType.includes('bold') || (boldByDefault && !prefs.fontType);
  return bold
    ? { font: ctx.fonts.bold, measurer: ctx.fonts.measure.bold }
    : { font: ctx.fonts.regular, measurer: ctx.fonts.measure.regular };
}

/**
 * Draws `text` inside `rect`: shrinks to fit, wraps when the field is
 * multi-line (or tall enough for two lines), truncates with "…" as a last
 * resort, and clips so nothing ever spills out of the area.
 */
function drawTextBox(ctx, painter, rect, rawText, prefs, { boldByDefault = false, defaultSize } = {}) {
  const { font, measurer } = fontFor(ctx, prefs, boldByDefault);
  const text = sanitizeText(rawText, ctx.fonts.charSet).replace(/^\n+|\n+$/g, '');
  if (!text.trim()) return;

  const pad = Math.min(3, rect.h * 0.12, rect.w * 0.05);
  const maxWidth = rect.w - 2 * pad;
  const maxHeight = rect.h - 2 * pad;
  const multiline = prefs.multiline || text.includes('\n');
  const autoSize = defaultSize || (multiline ? 10 : Math.min(12, maxHeight / LINE_HEIGHT));
  const baseSize = Math.max(prefs.fontSize || autoSize, 4);
  const minSize = prefs.fontSize ? Math.max(4, prefs.fontSize * 0.6) : Math.min(baseSize, 5);
  const allowWrap = multiline || maxHeight >= 2 * LINE_HEIGHT * baseSize;

  const { size, lines } = fitText({ text, measurer, maxWidth, maxHeight, baseSize, minSize, allowWrap });
  const lineHeight = size * LINE_HEIGHT;
  const blockHeight = lines.length * lineHeight;
  const valign = prefs.valign || (lines.length > 1 || multiline ? 'top' : 'center');
  let top = rect.y + rect.h - pad;
  if (valign === 'center' || valign === 'middle') top = rect.y + rect.h / 2 + blockHeight / 2;
  if (valign === 'bottom') top = rect.y + pad + blockHeight;

  const color = colorOf(prefs.color);
  if (prefs.background && parseColor(prefs.background)) {
    painter.page.drawRectangle({ x: rect.x, y: rect.y, width: rect.w, height: rect.h, color: colorOf(prefs.background) });
  }
  painter.withClip(rect, () => {
    lines.forEach((line, index) => {
      const width = measurer.width(line, size);
      let x = rect.x + pad;
      if (prefs.align === 'center') x = rect.x + (rect.w - width) / 2;
      if (prefs.align === 'right') x = rect.x + rect.w - pad - width;
      painter.page.drawText(line, { x, y: top - index * lineHeight - BASELINE * size, size, font, color });
    });
  });
}

/** One character per box; the box count is prefs.length or derived from the area's shape. */
function drawCells(ctx, painter, rect, rawText, prefs) {
  const text = Array.from(sanitizeText(rawText, ctx.fonts.charSet).replace(/\n/g, ' '));
  const count = prefs.length || Math.max(1, Math.round(rect.w / rect.h));
  const cellWidth = rect.w / count;
  const { font, measurer } = fontFor(ctx, prefs);
  const size = Math.max(4, Math.min(prefs.fontSize || 14, rect.h * 0.6, cellWidth * 0.75));
  const color = colorOf(prefs.color);
  const grid = rgb(...GRID);

  for (let i = 0; i < count; i += 1) {
    const x = rect.x + i * cellWidth;
    painter.page.drawRectangle({ x, y: rect.y, width: cellWidth, height: rect.h, borderColor: grid, borderWidth: 0.5 });
    const char = text[i];
    if (!char || char === ' ') continue;
    const width = measurer.width(char, size);
    painter.page.drawText(char, {
      x: x + (cellWidth - width) / 2,
      y: rect.y + (rect.h - size * 0.72) / 2,
      size,
      font,
      color,
    });
  }
}

module.exports = { colorOf, drawCells, drawTextBox };
