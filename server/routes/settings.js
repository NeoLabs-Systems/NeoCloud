'use strict';

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const { readSettings, updateSettings } = require('../services/settings/store');
const { listModelsFor, listProvidersFor } = require('../services/ai/meta');
const byok = require('../services/ai/byok');
const { jsonHandler, rateLimiter } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// Saving and testing a key is a credential route: it may make the server
// call the address a person typed.
const byokLimiter = rateLimiter(30);

router.get('/', jsonHandler((req) => ({ settings: readSettings(req.userId) })));
router.put('/', jsonHandler((req) => ({ settings: updateSettings(req.userId, req.body) })));

router.get('/meta/models', jsonHandler((req) => listModelsFor(req.userId)));
router.get('/meta/ai-providers', jsonHandler((req) => listProvidersFor(req.userId)));

router.get('/byok', jsonHandler((req) => byok.listByok(req.userId)));
router.get('/byok/:providerId', jsonHandler((req) => byok.getByok(req.userId, req.params.providerId)));
router.put('/byok/:providerId', byokLimiter, jsonHandler((req) => byok.saveByok(req.userId, req.params.providerId, req.body)));
router.delete('/byok/:providerId', byokLimiter, jsonHandler((req) => byok.clearByok(req.userId, req.params.providerId)));
router.post('/byok/:providerId/test', byokLimiter, jsonHandler((req) => byok.testByok(req.userId, req.params.providerId, req.body)));

module.exports = router;
