'use strict';

// Imports from Nextcloud and other WebDAV servers. Browser session only: the
// request carries another server's password.

const express = require('express');
const { requireAuth } = require('../middleware/auth');
const imports = require('../services/import/jobs');
const { jsonHandler, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuth);

// Each test signs in to another server; keep this from becoming a password oracle.
const testLimiter = rateLimiter(20);

router.get('/', jsonHandler((req) => imports.listImports(req.userId)));
router.post('/', jsonHandler((req) => imports.createImport(req.userId, req.body), { status: 201 }));
router.post('/test', testLimiter, jsonHandler((req) => imports.testImport(req.userId, req.body)));
router.get('/:id', jsonHandler((req) => imports.getImport(req.userId, req.params.id)));
router.post('/:id/pause', jsonHandler((req) => imports.pauseImport(req.userId, req.params.id)));
router.post('/:id/resume', jsonHandler((req) => imports.resumeImport(req.userId, req.params.id, req.body)));
router.post('/:id/cancel', jsonHandler((req) => imports.cancelImport(req.userId, req.params.id)));
router.delete('/:id', jsonHandler((req) => imports.deleteImport(req.userId, req.params.id)));

module.exports = router;
