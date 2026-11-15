'use strict';

// The agent API for connected apps (NeoAgent), mounted at /api/v1. Reached
// only with a connected app's access token; each route checks the scope the
// user granted on the consent page (403 INSUFFICIENT_SCOPE otherwise).

const express = require('express');
const { hasScope, requireOauthToken, requireScope } = require('../middleware/oauth_bearer');
const files = require('../services/agent/files');
const signing = require('../services/agent/signing');
const { renderMarkdownPdf } = require('../services/agent/markdown_pdf');
const { jsonHandler, publicOrigin, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireOauthToken);
router.use(rateLimiter(3000, { keyGenerator: (req) => `agent:${req.userId}` }));
const heavyLimiter = rateLimiter(200, { keyGenerator: (req) => `agent-heavy:${req.userId}` });

const read = requireScope('files:read');
const write = requireScope('files:write');
const signRead = requireScope('signing:read');
const signWrite = requireScope('signing:write');
const text = (value) => (typeof value === 'string' ? value : undefined);

router.get('/files', read, jsonHandler((req) => files.listFolder(req.userId, text(req.query.path) ?? '/')));
router.get('/files/search', read, jsonHandler((req) => files.searchFiles(req.userId, { q: text(req.query.q), limit: text(req.query.limit) })));
router.get('/files/:id', read, jsonHandler((req) => files.getItem(req.userId, req.params.id)));
router.get('/files/:id/content', read, jsonHandler((req) => files.readContent(req.userId, req.params.id)));

router.post('/files', write, jsonHandler((req) => files.createFile(req.userId, req.body), { status: 201 }));
router.put('/files/:id/content', write, jsonHandler((req) => files.updateContent(req.userId, req.params.id, req.body)));
router.patch('/files/:id', write, jsonHandler((req) => files.moveOrRename(req.userId, req.params.id, req.body)));
router.delete('/files/:id', write, jsonHandler((req) => files.trashItem(req.userId, req.params.id)));

router.post('/folders', write, jsonHandler((req, res) => {
  const { item, created } = files.createFolder(req.userId, req.body);
  res.status(created ? 201 : 200);
  return item;
}));

router.post('/documents/pdf', write, heavyLimiter, jsonHandler(async (req) => {
  const { bytes } = await renderMarkdownPdf({ title: text(req.body?.title), markdown: req.body?.markdown });
  return files.writeFile(req.userId, { path: req.body?.path, bytes, overwrite: req.body?.overwrite ?? false });
}, { status: 201 }));

router.post('/signing/requests', signWrite, heavyLimiter, jsonHandler((req) => signing.createRequest(req.userId, req.body, {
  req, origin: publicOrigin(req), canWriteFiles: hasScope(req, 'files:write'),
}), { status: 201 }));
router.get('/signing/requests', signRead, jsonHandler((req) => signing.listRequests(req.userId, {
  status: text(req.query.status), limit: text(req.query.limit),
})));
router.get('/signing/requests/:id', signRead, jsonHandler((req) => signing.getRequest(req.userId, req.params.id)));
router.post('/signing/requests/:id/remind', signWrite, jsonHandler((req) => signing.remindSigners(req.userId, req.params.id), { status: 202 }));
router.post('/signing/requests/:id/signers/:signerId/link', signWrite, jsonHandler((req) => signing.newSignerLink(
  req.userId, req.params.id, req.params.signerId, { req, origin: publicOrigin(req) },
)));
router.post('/signing/requests/:id/cancel', signWrite, jsonHandler((req) => signing.cancelRequest(req.userId, req.params.id, req)));

module.exports = router;
