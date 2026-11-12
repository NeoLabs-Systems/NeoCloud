'use strict';

// "Connect NeoRouter": the server's connection (admins) and each person's own.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const neorouter = require('../services/ai/neorouter/service');
const { jsonHandler, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// Connecting makes the server call the address a person typed.
const writeLimiter = rateLimiter(30);

router.get('/', jsonHandler((req) => neorouter.describeConnections(req.userId)));
router.post('/connect', writeLimiter, jsonHandler((req) => neorouter.connect(req.userId, req.body?.scope, req.body?.address)));
router.get('/connect/:id', jsonHandler((req) => neorouter.connectStatus(req.userId, req.params.id)));
router.put('/:scope/roles', writeLimiter, jsonHandler((req) => neorouter.setRoles(req.userId, req.params.scope, req.body?.roles)));
router.delete('/:scope', writeLimiter, jsonHandler((req) => neorouter.disconnect(req.userId, req.params.scope)));

module.exports = router;
