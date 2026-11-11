'use strict';

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const { listChanges } = require('../services/files/changes');
const { treeSnapshot } = require('../services/sync/sync');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuthOrDevice);

router.get('/tree', jsonHandler((req) => treeSnapshot(req.userId)));
router.get('/changes', jsonHandler((req) => listChanges(req.userId, req.query.cursor, { limit: req.query.limit })));

module.exports = router;
