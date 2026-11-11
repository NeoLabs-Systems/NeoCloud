'use strict';

// Where an account's storage goes. Reached with a browser session or a
// device token.

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const { findDuplicates, storageBreakdown } = require('../services/storage/breakdown');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuthOrDevice);

router.get('/breakdown', jsonHandler((req) => storageBreakdown(req.userId)));
router.get('/duplicates', jsonHandler((req) => findDuplicates(req.userId)));

module.exports = router;
