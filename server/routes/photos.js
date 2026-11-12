'use strict';

// The Photos timeline: the account's own photos and videos by the time
// they were taken. Thumbnails come from /api/files/:id/thumbnail.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const { listPhotos, photoSummary } = require('../services/photos/timeline');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

// GET /api/photos?cursor=&day=YYYY-MM-DD&limit=200
router.get('/', jsonHandler((req) => listPhotos(req.userId, {
  cursor: req.query.cursor || null,
  day: req.query.day || null,
  limit: req.query.limit,
})));

// GET /api/photos/summary — counts per day and month.
router.get('/summary', jsonHandler((req) => photoSummary(req.userId)));

module.exports = router;
