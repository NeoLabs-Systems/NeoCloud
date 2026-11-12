'use strict';

// Serving file contents: one file with HTTP Range and ETag support, or
// folders and selections as a streamed ZIP. Both decrypt on the fly.

const { pipeline } = require('stream/promises');
const { ZipArchive } = require('archiver');
const { httpError } = require('../../utils/http_error');
const { contentDisposition } = require('../../utils/file_names');
const { parseRange } = require('../../utils/http_range');
const { isActiveContent } = require('../../utils/mime');
const { createServiceLogger } = require('../../utils/logger');
const { openBlob } = require('../storage/blob_store');
const { getLiveNode, liveSubtree } = require('../files/tree');
const { getVersion } = require('../files/versions');
const { getPreview, pdfVariantFor } = require('../preview/previews');

const logger = createServiceLogger('Downloads');
const MAX_ZIP_SELECTION = 1000;
const SANDBOX_CSP = "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'";

function etagMatches(ifNoneMatch, etag) {
  return String(ifNoneMatch || '')
    .split(',')
    .map((value) => value.trim().replace(/^W\//, ''))
    .some((value) => value === '*' || value === `"${etag}"`);
}

/**
 * Sends one file. `version` picks a kept version instead of the current
 * content. Active content (HTML, SVG, ...) is always an attachment, and every
 * response carries a sandbox CSP, so a stored file can never run as the app.
 */
async function sendFile(req, res, userId, fileId, { download = false, version = null } = {}) {
  let node;
  let source;
  if (version !== null && version !== undefined && version !== '') {
    const found = getVersion(userId, fileId, version);
    node = found.node;
    source = { blobId: found.version.blob_id, size: found.version.size, etag: found.version.etag, modifiedAt: found.version.created_at };
  } else {
    node = getLiveNode(userId, fileId, { type: 'file' });
    source = { blobId: node.blob_id, size: node.size, etag: node.etag, modifiedAt: node.modified_at };
  }

  const mime = node.mime || 'application/octet-stream';
  const disposition = download || isActiveContent(mime) ? 'attachment' : 'inline';
  res.set({
    'Content-Type': mime,
    'Content-Disposition': contentDisposition(disposition, node.name),
    'Accept-Ranges': 'bytes',
    ETag: `"${source.etag}"`,
    'Last-Modified': new Date(`${source.modifiedAt.replace(' ', 'T')}Z`).toUTCString(),
    'Cache-Control': 'private, no-cache',
    'X-Content-Type-Options': 'nosniff',
    'Content-Security-Policy': SANDBOX_CSP,
  });

  if (etagMatches(req.get('if-none-match'), source.etag)) {
    res.status(304).end();
    return;
  }

  const range = parseRange(req.get('range'), source.size);
  if (range?.unsatisfiable) {
    res.status(416).set('Content-Range', `bytes */${source.size}`).end();
    return;
  }
  const start = range ? range.start : 0;
  const end = range ? range.end : source.size - 1;
  if (range) {
    res.status(206).set('Content-Range', `bytes ${start}-${end}/${source.size}`);
  }
  res.set('Content-Length', String(Math.max(0, end - start + 1)));
  if (req.method === 'HEAD') {
    res.end();
    return;
  }
  await pipeline(openBlob(userId, source.blobId, source.size, { start, end }), res);
}

/**
 * Sends a generated rendition (thumbnail or Office PDF). Renditions never
 * change for a given content, so browsers may cache them.
 */
async function sendPreview(req, res, userId, fileId, variant) {
  const node = getLiveNode(userId, fileId, { type: 'file' });
  // Vault files are ciphertext: there is nothing to render.
  if (node.vault_id) throw httpError(404, 'No preview for vault files.', 'NOT_FOUND');
  const resolved = variant === 'pdf' ? pdfVariantFor(node) : variant;
  const etag = `"${node.etag}-${resolved}"`;
  if (etagMatches(req.get('if-none-match'), `${node.etag}-${resolved}`)) {
    res.status(304).end();
    return;
  }
  const preview = await getPreview(userId, fileId, resolved);
  res.set({
    'Content-Type': preview.mime,
    'Content-Length': String(preview.size),
    'Cache-Control': 'private, max-age=86400',
    ETag: etag,
    'X-Content-Type-Options': 'nosniff',
    'Content-Security-Policy': SANDBOX_CSP,
  });
  if (req.method === 'HEAD') {
    preview.stream.destroy();
    res.end();
    return;
  }
  await pipeline(preview.stream, res);
}

// Entries for the archive: [{ node, path }] with paths relative to the zip root.
function collectEntries(userId, ids) {
  const entries = [];
  const usedTopNames = new Set();
  for (const id of ids) {
    const node = getLiveNode(userId, id);
    if (node.parent_id === null) {
      for (const child of liveSubtree(userId, node.id).slice(1)) entries.push({ node: child, path: child.relative_path });
      continue;
    }
    let topName = node.name;
    for (let n = 1; usedTopNames.has(topName.toLowerCase()); n += 1) topName = `${node.name} (${n})`;
    usedTopNames.add(topName.toLowerCase());
    if (node.type === 'file') {
      entries.push({ node, path: topName });
      continue;
    }
    entries.push({ node, path: topName });
    for (const child of liveSubtree(userId, node.id).slice(1)) {
      entries.push({ node: child, path: `${topName}/${child.relative_path}` });
    }
  }
  return entries;
}

function zipName(userId, ids) {
  if (ids.length === 1) {
    const node = getLiveNode(userId, ids[0]);
    if (node.parent_id !== null) return `${node.name}.zip`;
  }
  return 'NeoCloud.zip';
}

/**
 * Streams the selected files and folders as one ZIP. Files are stored, not
 * recompressed: most large files (photos, video, archives) are compressed
 * already, and storing keeps the server's CPU free. `skipVaults` leaves vault
 * folders and their contents out (share links).
 */
async function sendZip(res, userId, rawIds, { skipVaults = false } = {}) {
  const ids = [...new Set(String(rawIds || '').split(',').map((id) => id.trim()).filter(Boolean))];
  if (ids.length === 0) throw httpError(400, 'Choose at least one file or folder.', 'NOTHING_SELECTED');
  if (ids.length > MAX_ZIP_SELECTION) throw httpError(400, 'Too many items selected.', 'TOO_MANY_ITEMS');
  const entries = collectEntries(userId, ids).filter(({ node }) => !(skipVaults && node.vault_id));

  res.set({
    'Content-Type': 'application/zip',
    'Content-Disposition': contentDisposition('attachment', zipName(userId, ids)),
    'X-Content-Type-Options': 'nosniff',
  });
  const archive = new ZipArchive({ store: true });
  archive.on('warning', (err) => logger.warn('ZIP warning:', err.message));
  for (const { node, path } of entries) {
    const date = new Date(`${node.modified_at.replace(' ', 'T')}Z`);
    if (node.type === 'folder') {
      archive.append(Buffer.alloc(0), { name: `${path}/`, date });
    } else {
      archive.append(openBlob(userId, node.blob_id, node.size), { name: path, date });
    }
  }
  const done = pipeline(archive, res);
  archive.finalize();
  await done;
}

module.exports = { MAX_ZIP_SELECTION, sendFile, sendPreview, sendZip };
