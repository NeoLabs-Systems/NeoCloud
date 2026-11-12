'use strict';

// Imports from Nextcloud and other WebDAV servers. A browser session or the
// phone app (requireSessionOrPhone): the request carries another server's
// password, so desktop tokens and WebDAV app passwords can't start one.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const imports = require('../services/import/jobs');
const { jsonHandler, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// Each test signs in to another server; keep this from becoming a password oracle.
const testLimiter = rateLimiter(20);
// Starting or resuming a job signs in there too, just later.
const startLimiter = rateLimiter(20);

router.get('/', jsonHandler((req) => imports.listImports(req.userId)));
router.post('/', startLimiter, jsonHandler((req) => imports.createImport(req.userId, req.body), { status: 201 }));
router.post('/test', testLimiter, jsonHandler((req) => imports.testImport(req.userId, req.body)));
router.get('/:id', jsonHandler((req) => imports.getImport(req.userId, req.params.id)));
router.post('/:id/pause', jsonHandler((req) => imports.pauseImport(req.userId, req.params.id)));
router.post('/:id/resume', startLimiter, jsonHandler((req) => imports.resumeImport(req.userId, req.params.id, req.body)));
router.post('/:id/cancel', jsonHandler((req) => imports.cancelImport(req.userId, req.params.id)));
router.delete('/:id', jsonHandler((req) => imports.deleteImport(req.userId, req.params.id)));

module.exports = router;
