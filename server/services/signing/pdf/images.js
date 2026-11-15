'use strict';

const sharp = require('sharp');
const { PdfError } = require('./errors');
const { MAX_EMBED_EDGE, MAX_IMAGE_BYTES, MAX_IMAGE_PIXELS } = require('./limits');
const { toBytes } = require('./normalize');

/**
 * Turns any image sharp can read into bytes pdf-lib can embed: `{ kind:
 * 'png' | 'jpg', bytes, width, height }`. Plain 8-bit PNG and baseline sRGB
 * JPEG with normal orientation and size pass through untouched (no
 * generation loss); everything else (WebP, HEIC when this sharp build can
 * decode it, EXIF-rotated, CMYK, 16-bit, oversized) is re-encoded.
 */
async function prepareImage(input) {
  const bytes = toBytes(input);
  if (!bytes || bytes.length === 0 || bytes.length > MAX_IMAGE_BYTES) {
    throw new PdfError('IMAGE_INVALID', 'The image is empty or too large.');
  }
  let meta;
  try {
    meta = await sharp(bytes, { limitInputPixels: MAX_IMAGE_PIXELS, failOn: 'none' }).metadata();
  } catch (err) {
    throw new PdfError('IMAGE_INVALID', 'The image could not be read.', { cause: err });
  }
  if (!meta.width || !meta.height) throw new PdfError('IMAGE_INVALID', 'The image could not be read.');

  const oriented = !meta.orientation || meta.orientation === 1;
  const small = Math.max(meta.width, meta.height) <= MAX_EMBED_EDGE;
  const plainPng = meta.format === 'png' && meta.depth === 'uchar';
  const plainJpeg = meta.format === 'jpeg' && (meta.space === 'srgb' || meta.space === 'b-w');
  if (oriented && small && (plainPng || plainJpeg)) {
    return { kind: plainPng ? 'png' : 'jpg', bytes, width: meta.width, height: meta.height };
  }

  try {
    const pipeline = sharp(bytes, { limitInputPixels: MAX_IMAGE_PIXELS, failOn: 'none' })
      .rotate()
      .resize({ width: MAX_EMBED_EDGE, height: MAX_EMBED_EDGE, fit: 'inside', withoutEnlargement: true });
    if (meta.hasAlpha) {
      const { data, info } = await pipeline.png().toBuffer({ resolveWithObject: true });
      return { kind: 'png', bytes: data, width: info.width, height: info.height };
    }
    const { data, info } = await pipeline
      .flatten({ background: '#ffffff' })
      .toColorspace('srgb')
      .jpeg({ quality: 90 })
      .toBuffer({ resolveWithObject: true });
    return { kind: 'jpg', bytes: data, width: info.width, height: info.height };
  } catch (err) {
    throw new PdfError('IMAGE_INVALID', 'The image could not be converted.', { cause: err });
  }
}

module.exports = { prepareImage };
