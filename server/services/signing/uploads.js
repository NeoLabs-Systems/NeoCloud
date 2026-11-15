'use strict';

// Uploads by signers: signature, initials, stamp and image fields take an
// image, file fields an attachment. The request body is the raw bytes (no
// multipart). Images are decoded and re-encoded as PNG with sharp, which
// drops metadata and anything that is not pixels; attachments are stored as
// they are, under a cleaned name.

const sharp = require('sharp');
const { httpError } = require('../../utils/http_error');
const { remainingBytes } = require('../storage/quota');
const { discard, storeBlob } = require('./blobs');
const { LIMITS } = require('./constants');
const { typeOf } = require('./field_types');
const { singleLine } = require('./plain_text');
const { saveBlobValue } = require('./values');

const IMAGE_FORMATS = new Set(['png', 'jpeg', 'webp', 'gif', 'heif', 'avif', 'tiff']);
const MAX_INPUT_PIXELS = 40 * 1000 * 1000;
const MIME_PATTERN = /^[a-z0-9][a-z0-9!#$&^_.+-]{0,60}\/[a-z0-9][a-z0-9!#$&^_.+-]{0,60}$/i;

/** The request body as a Buffer, refused with 413 as soon as it passes `limit` bytes. */
async function readBody(req, limit) {
  const declared = Number(req.get('content-length'));
  if (Number.isFinite(declared) && declared > limit) throw httpError(413, 'This upload is too large.', 'UPLOAD_TOO_LARGE');
  const chunks = [];
  let total = 0;
  for await (const chunk of req) {
    total += chunk.length;
    if (total > limit) throw httpError(413, 'This upload is too large.', 'UPLOAD_TOO_LARGE');
    chunks.push(chunk);
  }
  if (total === 0) throw httpError(400, 'The upload is empty.', 'INVALID_UPLOAD');
  return Buffer.concat(chunks);
}

/** A PNG of the picture, at most `maxPixels` ([width, height]) in size, plus its dimensions. */
async function toPng(input, [maxWidth, maxHeight]) {
  try {
    const source = sharp(input, { limitInputPixels: MAX_INPUT_PIXELS, failOn: 'error' });
    const { format } = await source.metadata();
    if (!IMAGE_FORMATS.has(format)) throw new Error('unsupported format');
    const { data, info } = await source
      .rotate()
      .resize({ width: maxWidth, height: maxHeight, fit: 'inside', withoutEnlargement: true })
      .png({ compressionLevel: 9 })
      .toBuffer({ resolveWithObject: true });
    return { data, width: info.width, height: info.height };
  } catch {
    throw httpError(400, 'This image could not be read. Use a PNG or JPEG picture.', 'INVALID_IMAGE');
  }
}

function cleanFileName(value) {
  const name = singleLine(String(value || '').split(/[\\/]/).pop()).slice(0, 120);
  return name || 'attachment';
}

function cleanMime(value) {
  const mime = String(value || '').split(';')[0].trim().toLowerCase();
  return MIME_PATTERN.test(mime) ? mime : 'application/octet-stream';
}

function signatureMeta(field, query) {
  const method = query.method === undefined ? 'upload' : String(query.method);
  const allowed = field.prefs.methods || ['draw', 'type', 'upload'];
  if (!['draw', 'type', 'upload'].includes(method) || !allowed.includes(method)) {
    throw httpError(400, 'This way of signing is not allowed for this field.', 'INVALID_UPLOAD');
  }
  const meta = { method };
  if (method === 'type') {
    const text = singleLine(query.text || '');
    if (!text || text.length > LIMITS.maxNameLength) throw httpError(400, 'Typed signatures need the typed text.', 'INVALID_UPLOAD');
    meta.text = text;
  }
  return meta;
}

// What to store for the upload: the bytes and the description clients get.
async function prepareUpload(field, req) {
  const type = typeOf(field.type);
  if (type.upload === 'image') {
    const png = await toPng(await readBody(req, LIMITS.maxImageBytes), type.maxPixels);
    const meta = { kind: 'image', mime: 'image/png', width: png.width, height: png.height };
    if (field.type === 'signature' || field.type === 'initials') Object.assign(meta, signatureMeta(field, req.query));
    return { bytes: png.data, meta };
  }
  const bytes = await readBody(req, LIMITS.maxFileBytes);
  return { bytes, meta: { kind: 'file', name: cleanFileName(req.query.name), mime: cleanMime(req.get('content-type')), size: bytes.length } };
}

/** Stores the request body as the field's value. Resolves with the value clients see. */
async function uploadFieldValue(document, party, field, req) {
  if (!typeOf(field.type).upload) throw httpError(400, 'This field takes a typed value, not an upload.', 'INVALID_UPLOAD');
  const { bytes, meta } = await prepareUpload(field, req);
  if (bytes.length > remainingBytes(document.owner_id)) throw httpError(413, 'The sender has no storage left for this.', 'QUOTA_EXCEEDED');
  const stored = await storeBlob(document.owner_id, bytes);
  try {
    return await saveBlobValue(document, party, field, { blob: stored.blob, meta: { ...meta, sha256: stored.blob.sha256 } });
  } catch (err) {
    await discard(stored.path);
    throw err;
  }
}

module.exports = { readBody, toPng, uploadFieldValue };
