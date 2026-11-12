'use strict';

// Generated renditions of files, made on first request and cached encrypted:
//   thumb-256 / thumb-1024  WebP thumbnails of images (sharp, all in memory)
//   thumb-square            a small square crop for photo grids
//   pdf                     a PDF of an Office document (LibreOffice)
// A rendition belongs to a blob, not a node, so a new file version gets new
// previews and the old ones are deleted with the old blob.

const fs = require('fs');
const os = require('os');
const path = require('path');
const { Readable } = require('stream');
const sharp = require('sharp');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { readEnvBool } = require('../../../runtime/env');
const { createServiceLogger } = require('../../utils/logger');
const {
  openBlob,
  openTemporaryBlob,
  previewDirectory,
  removeQuietly,
  writeTemporaryBlob,
} = require('../storage/blob_store');
const { getLiveNode } = require('../files/tree');
const { convertToPdf, findOfficeBinary } = require('./office_convert');

const logger = createServiceLogger('Previews');

const THUMBNAIL_SIZES = new Set([256, 1024]);
// Grid tiles are about 100–140 points wide; 320 pixels is sharp on a phone.
const SQUARE_THUMBNAIL_PIXELS = 320;
const SQUARE_VARIANT = 'thumb-square';
const MAX_THUMBNAIL_SOURCE_BYTES = 200 * 1024 * 1024;
const MAX_OFFICE_SOURCE_BYTES = 100 * 1024 * 1024;
const OFFICE_TIMEOUT_MS = 120 * 1000;
const MAX_PARALLEL_RENDERS = 2;

const IMAGE_MIMES = new Set([
  'image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/avif',
  'image/tiff', 'image/heic', 'image/heif', 'image/bmp', 'image/svg+xml',
]);
const OFFICE_EXTENSIONS = new Set(['.doc', '.docx', '.odt', '.rtf', '.xls', '.xlsx', '.ods', '.ppt', '.pptx', '.odp']);

async function getCapabilities() {
  const office = readEnvBool('NEOCLOUD_OFFICE_PREVIEW', true) && Boolean(await findOfficeBinary());
  return { imageThumbnails: true, officePreview: office };
}

function isOfficeFile(name) {
  return OFFICE_EXTENSIONS.has(path.extname(String(name)).toLowerCase());
}

// At most a couple of renders at once, so a folder of photos opening in the
// grid can't starve uploads and downloads of CPU.
let active = 0;
const waiting = [];

async function withRenderSlot(task) {
  if (active >= MAX_PARALLEL_RENDERS) await new Promise((resolve) => waiting.push(resolve));
  active += 1;
  try {
    return await task();
  } finally {
    active -= 1;
    waiting.shift()?.();
  }
}

// Concurrent requests for the same rendition share one render.
const inFlight = new Map();

function previewPath(userId, blobId, variant) {
  return path.join(previewDirectory(userId, blobId), variant);
}

function previewBlobId(blobId, variant) {
  return `preview-${blobId}-${variant}`;
}

/** Encrypts `buffer` and records it as a rendition of the blob. */
async function storePreview(userId, blobId, variant, buffer, mime) {
  const blob = await writeTemporaryBlob(userId, Readable.from([buffer]), { blobId: previewBlobId(blobId, variant) });
  const target = previewPath(userId, blobId, variant);
  await fs.promises.mkdir(path.dirname(target), { recursive: true, mode: 0o700 });
  await fs.promises.rename(blob.tmpPath, target);
  // The blob may have been deleted (file purged or replaced) while this
  // rendered; its preview folder is then removed again instead of recorded.
  const stored = db.prepare(`
    INSERT INTO previews (blob_id, variant, user_id, size, mime)
    SELECT ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM blobs WHERE id = ?)
    ON CONFLICT(blob_id, variant) DO UPDATE SET size = excluded.size, mime = excluded.mime
  `).run(blobId, variant, userId, buffer.length, mime, blobId);
  if (stored.changes === 0) {
    await fs.promises.rm(previewDirectory(userId, blobId), { recursive: true, force: true });
    throw httpError(404, 'File not found', 'NOT_FOUND');
  }
}

function cachedPreview(blobId, variant) {
  return db.prepare('SELECT * FROM previews WHERE blob_id = ? AND variant = ?').get(blobId, variant) || null;
}

function canRenderThumbnail(node) {
  return IMAGE_MIMES.has(node.mime) && node.size <= MAX_THUMBNAIL_SOURCE_BYTES;
}

async function renderThumbnail(userId, node, variant) {
  if (!IMAGE_MIMES.has(node.mime)) throw httpError(415, 'No thumbnail for this file type.', 'PREVIEW_UNAVAILABLE');
  if (node.size > MAX_THUMBNAIL_SOURCE_BYTES) throw httpError(415, 'File is too large for a thumbnail.', 'PREVIEW_UNAVAILABLE');
  try {
    // Read first, then hand sharp a buffer (it would buffer a stream anyway):
    // piping would leave a failing blob read (missing or damaged file) as an
    // unhandled stream error, which ends the process.
    const source = await streamToBuffer(openBlob(userId, node.blob_id, node.size));
    const image = sharp(source, { failOn: 'none', limitInputPixels: 100_000_000 }).rotate();
    if (variant === SQUARE_VARIANT) {
      return await image
        .resize(SQUARE_THUMBNAIL_PIXELS, SQUARE_THUMBNAIL_PIXELS, { fit: 'cover', position: 'centre' })
        .webp({ quality: 70 })
        .toBuffer();
    }
    const size = Number(variant.slice('thumb-'.length));
    return await image
      .resize(size, size, { fit: 'inside', withoutEnlargement: true })
      .webp({ quality: 78 })
      .toBuffer();
  } catch (err) {
    logger.warn(`Thumbnail failed for a ${node.mime} file: ${err.message}`);
    throw httpError(415, 'This image could not be read.', 'PREVIEW_UNAVAILABLE');
  }
}

/**
 * LibreOffice converts from a file, so the document is decrypted into a
 * private (0700) directory for the conversion and removed right after.
 * Turn this off with NEOCLOUD_OFFICE_PREVIEW=false to keep every byte
 * encrypted on disk at the price of Office previews. How the converter is
 * confined is described in office_convert.js.
 */
async function renderOfficePdf(userId, node) {
  const binary = readEnvBool('NEOCLOUD_OFFICE_PREVIEW', true) ? await findOfficeBinary() : null;
  if (!binary) throw httpError(415, 'Office previews need LibreOffice on the server.', 'PREVIEW_UNAVAILABLE');
  if (node.size > MAX_OFFICE_SOURCE_BYTES) throw httpError(415, 'File is too large to preview.', 'PREVIEW_UNAVAILABLE');
  const workDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), 'neocloud-office-'));
  try {
    await fs.promises.chmod(workDir, 0o700);
    const source = path.join(workDir, `document${path.extname(node.name).toLowerCase()}`);
    await fs.promises.writeFile(source, await streamToBuffer(openBlob(userId, node.blob_id, node.size)), { mode: 0o600 });
    await convertToPdf({ binary, workDir, source, timeoutMs: OFFICE_TIMEOUT_MS });
    return await fs.promises.readFile(path.join(workDir, 'document.pdf'));
  } catch (err) {
    if (err.status) throw err;
    logger.warn(`Office conversion failed: ${err.message}`);
    throw httpError(415, 'This document could not be converted for preview.', 'PREVIEW_UNAVAILABLE');
  } finally {
    await fs.promises.rm(workDir, { recursive: true, force: true });
  }
}

async function streamToBuffer(stream) {
  const chunks = [];
  for await (const chunk of stream) chunks.push(chunk);
  return Buffer.concat(chunks);
}

// Renders and caches the rendition unless it is cached already.
async function ensurePreview(userId, node, variant) {
  if (cachedPreview(node.blob_id, variant)) return;
  const key = `${node.blob_id}:${variant}`;
  if (!inFlight.has(key)) {
    const job = withRenderSlot(async () => {
      let buffer;
      let mime;
      if (variant === 'pdf') {
        buffer = await renderOfficePdf(userId, node);
        mime = 'application/pdf';
      } else {
        buffer = await renderThumbnail(userId, node, variant);
        mime = 'image/webp';
      }
      await storePreview(userId, node.blob_id, variant, buffer, mime);
    }).finally(() => inFlight.delete(key));
    inFlight.set(key, job);
  }
  await inFlight.get(key);
}

/**
 * Makes the grid thumbnail of an image ahead of time (the Photos indexer),
 * so the first scroll through a timeline doesn't wait for renders. A file
 * with no thumbnail is skipped; a failed render is only logged.
 */
async function prepareGridThumbnail(userId, node) {
  if (!canRenderThumbnail(node)) return;
  try {
    await ensurePreview(userId, node, SQUARE_VARIANT);
  } catch (err) {
    if (err.code !== 'PREVIEW_UNAVAILABLE' && err.code !== 'NOT_FOUND') throw err;
  }
}

/**
 * The rendition `variant` of a file: { stream, size, mime }. Renders and
 * caches it on first use.
 */
async function getPreview(userId, fileId, variant) {
  const node = getLiveNode(userId, fileId, { type: 'file' });
  await ensurePreview(userId, node, variant);
  const row = cachedPreview(node.blob_id, variant);
  const file = previewPath(userId, node.blob_id, variant);
  if (!row || !fs.existsSync(file)) {
    db.prepare('DELETE FROM previews WHERE blob_id = ? AND variant = ?').run(node.blob_id, variant);
    await removeQuietly(file);
    throw httpError(503, 'Preview is not ready, try again.', 'PREVIEW_RETRY');
  }
  return {
    node,
    size: row.size,
    mime: row.mime,
    stream: openTemporaryBlob(userId, file, previewBlobId(node.blob_id, variant), row.size),
  };
}

function thumbnailVariant(size) {
  if (size === 'square') return SQUARE_VARIANT;
  const value = Number(size || 256);
  if (!THUMBNAIL_SIZES.has(value)) throw httpError(400, 'size must be 256, 1024 or square.', 'INVALID_SIZE');
  return `thumb-${value}`;
}

/** The PDF rendition of an Office file; for a PDF, use the file itself. */
function pdfVariantFor(node) {
  if (!isOfficeFile(node.name)) throw httpError(415, 'No document preview for this file type.', 'PREVIEW_UNAVAILABLE');
  return 'pdf';
}

module.exports = { getCapabilities, getPreview, isOfficeFile, pdfVariantFor, prepareGridThumbnail, thumbnailVariant };
