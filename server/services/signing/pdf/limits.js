'use strict';

const MB = 1024 * 1024;

module.exports = {
  /** Largest PDF accepted for probing/import (the design caps uploads at 50 MB). */
  MAX_PDF_BYTES: 50 * MB,
  MAX_PAGES: 300,
  /** Largest an object or xref stream may inflate to while a PDF loads (decompression bomb guard). */
  MAX_DECODED_STREAM_BYTES: 64 * MB,
  /** Largest source image accepted by imageToPdf. */
  MAX_IMAGE_BYTES: 40 * MB,
  /** Decompression-bomb guard for sharp. */
  MAX_IMAGE_PIXELS: 120 * 1000 * 1000,
  /** Images drawn into a PDF are downscaled to fit within this edge length. */
  MAX_EMBED_EDGE: 2400,
  CONVERSION_TIMEOUT_MS: 90 * 1000,
};
