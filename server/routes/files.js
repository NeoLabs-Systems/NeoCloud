'use strict';

// The file tree: browsing, folders, rename/move/copy, trash and versions.
// Reached with a browser session or a device token.

const express = require('express');
const rateLimit = require('express-rate-limit');
const { requireAuthOrDevice } = require('../middleware/auth');
const tree = require('../services/files/tree');
const marks = require('../services/files/marks');
const { recentFiles, search } = require('../services/files/search');
const operations = require('../services/files/operations');
const trash = require('../services/files/trash');
const versions = require('../services/files/versions');
const { sendJsonError } = require('../http/errors');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(['/folders', '/nodes', '/files', '/search', '/recent', '/favorites', '/trash'], requireAuthOrDevice);

// Tree changes per account (not per address: one account behind many
// addresses is still one account). Copies are the expensive kind and get a
// tighter budget of their own.
function accountLimiter({ windowMs, max }) {
  return rateLimit({
    windowMs,
    max,
    keyGenerator: (req) => `user:${req.userId}`,
    message: { error: 'Too many changes, try again later', code: 'RATE_LIMITED' },
    standardHeaders: true,
    legacyHeaders: false,
  });
}

const mutationLimiter = accountLimiter({ windowMs: 60 * 1000, max: 600 });
const copyLimiter = accountLimiter({ windowMs: 15 * 60 * 1000, max: 100 });

function queryText(value) {
  return typeof value === 'string' ? value : undefined;
}

router.get('/folders/:id/children', jsonHandler((req) => tree.listChildren(req.userId, req.params.id, {
  sort: req.query.sort,
  order: req.query.order,
  limit: req.query.limit,
  offset: req.query.offset,
  cursor: queryText(req.query.cursor),
})));

// { parentId, name } creates one folder; { parentId, path: "a/b/c" } creates
// every missing folder along the path and returns the deepest.
router.post('/folders', mutationLimiter, jsonHandler((req) => (req.body?.path
  ? operations.ensureFolderPath(req.userId, { parentId: req.body.parentId ?? tree.ROOT_ALIAS, path: req.body.path })
  : operations.createFolder(req.userId, {
    parentId: req.body?.parentId ?? tree.ROOT_ALIAS,
    name: req.body?.name,
    conflict: req.body?.conflict,
  })), { status: 201 }));

router.get('/nodes/:id', jsonHandler((req) => {
  const node = tree.getLiveNode(req.userId, req.params.id);
  return { node: tree.serializeNode(node), path: tree.breadcrumbs(req.userId, node.id) };
}));

router.patch('/nodes/:id', mutationLimiter, jsonHandler((req) => operations.updateNode(req.userId, req.params.id, {
  name: req.body?.name,
  parentId: req.body?.parentId,
  conflict: req.body?.conflict,
})));

router.post('/nodes/:id/copy', mutationLimiter, copyLimiter, jsonHandler((req) => operations.copyNode(req.userId, req.params.id, {
  parentId: req.body?.parentId,
  name: req.body?.name,
  conflict: req.body?.conflict,
}), { status: 201 }));

router.delete('/nodes/:id', jsonHandler((req) => trash.trashNode(req.userId, req.params.id)));

router.get('/files/:id/versions', jsonHandler((req) => versions.listVersions(req.userId, req.params.id)));
router.post('/files/:id/versions/:version/restore', jsonHandler((req) => versions.restoreVersion(
  req.userId,
  req.params.id,
  req.params.version,
)));

router.put('/nodes/:id/favorite', jsonHandler((req) => marks.setFavorite(req.userId, req.params.id)));
router.delete('/nodes/:id/favorite', jsonHandler((req) => marks.clearFavorite(req.userId, req.params.id)));
router.get('/favorites', jsonHandler((req) => marks.listFavorites(req.userId)));

// Quick Access: the client says a file was opened. 204, no body.
router.post('/nodes/:id/opened', (req, res) => {
  try {
    marks.recordOpen(req.userId, req.params.id);
    res.status(204).end();
  } catch (err) {
    sendJsonError(res, err);
  }
});

router.get('/search', jsonHandler((req) => search(req.userId, {
  q: queryText(req.query.q),
  type: queryText(req.query.type),
  after: queryText(req.query.after),
  before: queryText(req.query.before),
  minSize: queryText(req.query.minSize),
  maxSize: queryText(req.query.maxSize),
  parentId: queryText(req.query.parentId),
  limit: queryText(req.query.limit),
  content: queryText(req.query.content),
})));
router.get('/recent', jsonHandler((req) => recentFiles(req.userId, {
  by: queryText(req.query.by),
  limit: queryText(req.query.limit),
})));

router.get('/trash', jsonHandler((req) => trash.listTrash(req.userId)));
router.post('/trash/:id/restore', jsonHandler((req) => trash.restoreNode(req.userId, req.params.id)));
router.delete('/trash/:id', jsonHandler((req) => trash.purgeNode(req.userId, req.params.id)));
router.delete('/trash', jsonHandler((req) => trash.emptyTrash(req.userId)));

module.exports = router;
