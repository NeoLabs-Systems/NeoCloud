'use strict';

const express = require('express');
const { requireAuth } = require('../middleware/auth');
const { readSettings, updateSettings } = require('../services/settings/store');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuth);

router.get('/', jsonHandler((req) => ({ settings: readSettings(req.userId) })));
router.put('/', jsonHandler((req) => ({ settings: updateSettings(req.userId, req.body) })));

module.exports = router;
