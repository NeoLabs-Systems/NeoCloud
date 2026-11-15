'use strict';

// Cloud storage a person connects with their own credentials (S3, WebDAV,
// SFTP) and browses beside their files. Pass-through: nothing is copied into
// NeoCloud unless the person copies it. A browser session or the phone app
// (requireSessionOrPhone): the connection settings hold other servers'
// passwords, so desktop tokens and WebDAV app passwords can't reach them.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const browse = require('../services/external_storage/browse');
const copy = require('../services/external_storage/copy');
const { sendFile } = require('../services/external_storage/downloads');
const manage = require('../services/external_storage/manage');
const mounts = require('../services/external_storage/mounts');
const { jsonHandler, rateLimiter, streaming } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// Each test or connect signs in to another server; keep this from becoming a probe.
const connectLimiter = rateLimiter(30);
const operationLimiter = rateLimiter(3000);

router.get('/', jsonHandler((req) => ({ items: mounts.listMounts(req.userId) })));
router.post('/test', connectLimiter, jsonHandler((req) => manage.test(req.userId, req.body)));
router.post('/', connectLimiter, jsonHandler((req) => manage.connect(req.userId, req.body), { status: 201 }));
router.patch('/:id', connectLimiter, jsonHandler((req) => manage.update(req.userId, req.params.id, req.body)));
router.delete('/:id', jsonHandler((req) => mounts.deleteMount(req.userId, req.params.id)));

router.get('/:id/list', operationLimiter, jsonHandler((req) => browse.listFolder(req.userId, req.params.id, req.query.path)));
router.get('/:id/file', operationLimiter, streaming((req, res) => sendFile(req, res, req.userId, req.params.id, req.query.path, {
  download: req.query.download === '1',
})));
router.put('/:id/file', operationLimiter, jsonHandler((req) => browse.uploadFile(req.userId, req.params.id, req.query.path, req, {
  size: req.get('content-length') === undefined ? null : Number(req.get('content-length')),
  conflict: req.query.conflict,
}), { status: 201 }));
router.post('/:id/folder', operationLimiter, jsonHandler((req) => browse.createFolder(req.userId, req.params.id, req.body?.path), { status: 201 }));
router.post('/:id/move', operationLimiter, jsonHandler((req) => browse.move(req.userId, req.params.id, req.body?.from, req.body?.to)));
router.delete('/:id/entry', operationLimiter, jsonHandler((req) => browse.remove(req.userId, req.params.id, req.query.path)));

router.post('/:id/copy-in', operationLimiter, jsonHandler((req) => copy.copyToMount(req.userId, req.params.id, {
  nodeId: req.body?.nodeId, folder: req.body?.folder, name: req.body?.name,
}), { status: 201 }));
router.post('/:id/copy-out', operationLimiter, jsonHandler((req) => copy.copyFromMount(req.userId, req.params.id, {
  path: req.body?.path, parentId: req.body?.parentId,
}), { status: 201 }));

module.exports = router;
