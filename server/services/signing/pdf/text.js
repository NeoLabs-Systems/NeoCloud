'use strict';

// Text preparation and layout. Pure apart from the font's width metrics.

const REPLACEMENT = '?';
const ELLIPSIS = '…';
const LINE_HEIGHT = 1.25;
// Distance from the top of a line box to its baseline, in em (Noto Sans).
const BASELINE = 1.013;
const SIZE_STEP = 0.5;
// Hard cap on drawn text; API validation is far below this.
const MAX_TEXT_CHARS = 5000;

/**
 * Normalises (NFC, so umlauts are single glyphs), unifies line breaks and
 * replaces every code point the font cannot draw by "?". Never throws.
 */
function sanitizeText(input, charSet) {
  const text = String(input ?? '')
    .slice(0, MAX_TEXT_CHARS)
    .normalize('NFC')
    .replace(/\r\n?/g, '\n')
    .replace(/\t/g, ' ');
  let out = '';
  for (const char of text) {
    const cp = char.codePointAt(0);
    if (char === '\n') out += char;
    else if (cp < 0x20 || (cp >= 0x7f && cp < 0xa0) || cp === 0x200b || cp === 0xfeff) continue;
    else out += charSet.has(cp) ? char : REPLACEMENT;
  }
  return out;
}

/**
 * Width measurement with a per-size cache; pdf-lib's own measuring runs the
 * font's layout engine on every call, which dominates long texts. Line widths
 * are the sum of their words plus spaces (kerning across a space is ignored,
 * which is below a tenth of a point); drawing clips to the area regardless.
 */
class Measurer {
  constructor(font) {
    this.font = font;
    this.cache = new Map();
  }

  width(text, size) {
    const key = `${size}\u0000${text}`;
    let value = this.cache.get(key);
    if (value === undefined) {
      value = this.font.widthOfTextAtSize(text, size);
      this.cache.set(key, value);
    }
    return value;
  }
}

/** Breaks one over-long word into pieces that each fit `maxWidth`. */
function breakWord(word, measurer, size, maxWidth) {
  const pieces = [];
  let current = '';
  let currentWidth = 0;
  for (const char of word) {
    const charWidth = measurer.width(char, size);
    if (current && currentWidth + charWidth > maxWidth) {
      pieces.push(current);
      current = '';
      currentWidth = 0;
    }
    current += char;
    currentWidth += charWidth;
  }
  if (current) pieces.push(current);
  return pieces;
}

/** Greedy word wrap that honours explicit "\n" and splits words wider than the box. */
function wrapText(text, measurer, size, maxWidth) {
  const space = measurer.width(' ', size);
  const lines = [];
  for (const paragraph of text.split('\n')) {
    let line = '';
    let lineWidth = 0;
    for (const word of paragraph.split(' ').filter((w) => w !== '')) {
      const wordWidth = measurer.width(word, size);
      if (line && lineWidth + space + wordWidth <= maxWidth) {
        line += ` ${word}`;
        lineWidth += space + wordWidth;
        continue;
      }
      if (line) lines.push(line);
      if (wordWidth <= maxWidth) {
        line = word;
        lineWidth = wordWidth;
      } else {
        const pieces = breakWord(word, measurer, size, maxWidth);
        line = pieces.pop();
        lineWidth = measurer.width(line, size);
        lines.push(...pieces);
      }
    }
    lines.push(line);
  }
  return lines;
}

/** Cuts `line` so that line + "…" fits `maxWidth`. */
function ellipsize(line, measurer, size, maxWidth) {
  const chars = Array.from(line);
  let low = 0;
  let high = chars.length;
  while (low < high) {
    const mid = Math.ceil((low + high) / 2);
    if (measurer.width(chars.slice(0, mid).join('') + ELLIPSIS, size) <= maxWidth) low = mid;
    else high = mid - 1;
  }
  return chars.slice(0, low).join('') + ELLIPSIS;
}

/**
 * Finds the largest size (from `baseSize` down to `minSize`) at which `text`
 * fits the box, wrapping only when `allowWrap`. At the minimum size text that
 * still does not fit is cut with an ellipsis. Returns `{ size, lines }`.
 */
function fitText({ text, measurer, maxWidth, maxHeight, baseSize, minSize, allowWrap }) {
  const lineHeightOf = (size) => size * LINE_HEIGHT;
  const layout = (size) => (allowWrap ? wrapText(text, measurer, size, maxWidth) : [text]);
  const fits = (size, lines) =>
    lines.length * lineHeightOf(size) <= maxHeight + 0.01 &&
    lines.every((line) => measurer.width(line, size) <= maxWidth + 0.01);

  for (let size = baseSize; size >= minSize - 1e-6; size -= SIZE_STEP) {
    const lines = layout(size);
    if (fits(size, lines)) return { size, lines };
  }

  const size = minSize;
  const maxLines = Math.max(1, Math.floor(maxHeight / lineHeightOf(size)));
  let lines = allowWrap ? wrapText(text, measurer, size, maxWidth) : [text.replace(/\n/g, ' ')];
  const truncated = lines.length > maxLines;
  lines = lines.slice(0, maxLines);
  const last = lines.length - 1;
  if (truncated || measurer.width(lines[last], size) > maxWidth) {
    lines[last] = ellipsize(lines[last], measurer, size, maxWidth);
  }
  return { size, lines };
}

module.exports = { BASELINE, LINE_HEIGHT, Measurer, fitText, sanitizeText, wrapText };
