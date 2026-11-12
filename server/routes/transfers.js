'use strict';

// Moving bytes: uploads (single request or resumable) and downloads.

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const transfers = require('../services/members/transfers');
const previews = require('../services/preview/previews');
const { ROOT_ALIAS } = require('../services/files/tree');
const { jsonHandler, streaming } = require('./_helpers/http');
const { isIndexEnabled } = require('../services/search_index/indexer');

const router = express.Router();
router.use(['/upload', '/uploads', '/files', '/download', '/capabilities'], requireAuthOrDevice);

// PUT /api/upload?parentId=&name=&conflict=fail|rename|replace — body is the file.
router.put('/upload', jsonHandler((req) => transfers.uploadStream(req.userId, {
  parentId: req.query.parentId || ROOT_ALIAS,
  name: req.query.name,
  conflict: req.query.conflict,
  baseEtag: req.get('if-match')?.replace(/"/g, '') || null,
  contentLength: req.get('content-length'),
  sha256: req.get('x-content-sha256'),
}, req), { status: 201 }));

router.post('/uploads', jsonHandler((req) => transfers.createUpload(req.userId, {
  parentId: req.body?.parentId || ROOT_ALIAS,
  name: req.body?.name,
  size: req.body?.size,
  conflict: req.body?.conflict,
  sha256: req.body?.sha256,
}), { status: 201 }));

// Before sending: is this content (SHA-256 and size) already in the folder
// or below it? Camera upload skips what the server has.
router.post('/uploads/check', jsonHandler((req) => transfers.contentExists(req.userId, {
  parentId: req.body?.parentId || ROOT_ALIAS,
  sha256: req.body?.sha256,
  size: req.body?.size,
})));

router.get('/uploads/:id', jsonHandler((req) => transfers.getUpload(req.userId, req.params.id)));

router.put('/uploads/:id/parts/:index', jsonHandler((req) => transfers.putPart(
  req.userId,
  req.params.id,
  req.params.index,
  req,
  { contentLength: req.get('content-length') },
)));

router.post('/uploads/:id/complete', jsonHandler((req) => transfers.completeUpload(req.userId, req.params.id, {
  baseEtag: req.body?.baseEtag || null,
}), { status: 201 }));

router.delete('/uploads/:id', jsonHandler((req) => transfers.cancelUpload(req.userId, req.params.id)));

// GET /api/files/:id/content?download=1&version=N — supports Range and If-None-Match.
router.get('/files/:id/content', streaming((req, res) => transfers.sendFile(req, res, req.userId, req.params.id, {
  download: req.query.download === '1' || req.query.download === 'true',
  version: req.query.version ?? null,
})));

// GET /api/files/:id/thumbnail?size=256|1024 — WebP thumbnail of an image.
router.get('/files/:id/thumbnail', streaming((req, res) => transfers.sendPreview(req, res, req.userId, req.params.id, previews.thumbnailVariant(req.query.size))));

// GET /api/files/:id/preview — a PDF rendition of an Office document.
router.get('/files/:id/preview', streaming((req, res) => transfers.sendPreview(req, res, req.userId, req.params.id, 'pdf')));

// What this server can render, so clients know which viewers to offer.
router.get('/capabilities', jsonHandler(async () => ({
  ...(await previews.getCapabilities()),
  fullTextSearch: isIndexEnabled(),
})));

// GET /api/download/zip?ids=a,b,c — folders and files as one archive.
router.get('/download/zip', streaming((req, res) => transfers.sendZip(res, req.userId, req.query.ids)));

module.exports = router;
