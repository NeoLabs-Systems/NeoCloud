'use strict';

// Sending a mount's file to the browser: Range supported, always a sandboxed
// response. Only raster images, audio and video may show inline; everything
// else (HTML, SVG, PDF, ...) is an attachment, so a file at a provider can
// never run as part of the app.

const { pipeline } = require('stream/promises');
const { contentDisposition } = require('../../utils/file_names');
const { parseRange } = require('../../utils/http_range');
const { openFile } = require('./browse');

const SANDBOX_CSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'";
const INLINE_TYPES = /^(?:image\/(?:png|jpeg|gif|webp|avif|bmp)|audio\/[\w.+-]+|video\/[\w.+-]+)$/;

async function sendFile(req, res, userId, mountId, path, { download = false } = {}) {
  const { info, range, stream, release } = await openFile(userId, mountId, path, {
    rangeFor: (size) => parseRange(req.get('range'), size),
  });
  try {
    if (range?.unsatisfiable) {
      res.status(416).set('Content-Range', `bytes */${info.size}`).end();
      return;
    }
    const start = range ? range.start : 0;
    const end = range ? range.end : info.size - 1;
    res.set({
      'Content-Type': info.mime,
      'Content-Disposition': contentDisposition(!download && INLINE_TYPES.test(info.mime) ? 'inline' : 'attachment', info.name),
      'Content-Length': String(Math.max(0, end - start + 1)),
      'Accept-Ranges': 'bytes',
      'Cache-Control': 'private, no-store',
      'X-Content-Type-Options': 'nosniff',
      'Content-Security-Policy': SANDBOX_CSP,
    });
    if (range) res.status(206).set('Content-Range', `bytes ${start}-${end}/${info.size}`);
    if (req.method === 'HEAD') {
      stream.destroy();
      res.end();
      return;
    }
    await pipeline(stream, res);
  } finally {
    await release();
  }
}

module.exports = { sendFile };
