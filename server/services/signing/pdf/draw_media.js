'use strict';

const crypto = require('crypto');
const { rgb } = require('pdf-lib');
const { drawTextBox } = require('./draw_text');
const { prepareImage } = require('./images');
const { toBytes } = require('./normalize');

/** Embeds an image once per distinct content; later uses share the XObject. */
async function embedImage(ctx, input) {
  const bytes = toBytes(input);
  if (!bytes) return null;
  const key = crypto.createHash('sha1').update(bytes).digest('hex');
  if (!ctx.imageCache.has(key)) {
    const pending = prepareImage(bytes).then((image) =>
      Promise.all([image, image.kind === 'png' ? ctx.doc.embedPng(image.bytes) : ctx.doc.embedJpg(image.bytes)]),
    );
    ctx.imageCache.set(key, pending.then(([image, embedded]) => ({ image, embedded })));
  }
  return ctx.imageCache.get(key);
}

/** Draws an image scaled to fit (never distorted) and centred in `rect`. */
async function drawImageContained(ctx, painter, rect, bytes) {
  const entry = await embedImage(ctx, bytes);
  if (!entry) return false;
  const pad = Math.min(rect.w, rect.h) * 0.04;
  const scale = Math.min((rect.w - 2 * pad) / entry.image.width, (rect.h - 2 * pad) / entry.image.height);
  const width = entry.image.width * scale;
  const height = entry.image.height * scale;
  painter.page.drawImage(entry.embedded, {
    x: rect.x + (rect.w - width) / 2,
    y: rect.y + (rect.h - height) / 2,
    width,
    height,
  });
  return true;
}

/** A small page icon followed by the file name, for `file` fields. */
function drawFileLabel(ctx, painter, rect, filename, prefs) {
  const icon = Math.min(rect.h * 0.7, 14);
  const iconX = rect.x + 2;
  const iconY = rect.y + (rect.h - icon) / 2;
  painter.page.drawRectangle({
    x: iconX,
    y: iconY,
    width: icon * 0.75,
    height: icon,
    borderColor: rgb(0.35, 0.35, 0.4),
    borderWidth: 0.7,
  });
  painter.page.drawLine({
    start: { x: iconX + icon * 0.15, y: iconY + icon * 0.55 },
    end: { x: iconX + icon * 0.6, y: iconY + icon * 0.55 },
    thickness: 0.6,
    color: rgb(0.35, 0.35, 0.4),
  });
  const textRect = { x: rect.x + icon, y: rect.y, w: Math.max(0, rect.w - icon), h: rect.h };
  drawTextBox(ctx, painter, textRect, filename, { ...prefs, valign: 'center' });
}

module.exports = { drawFileLabel, drawImageContained };
