'use strict';

// The activity feed: what happened to the files an account can see (session
// or phone app).

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const { listActivity } = require('../services/activity/feed');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(requireSessionOrPhone);

function queryText(value) {
  return typeof value === 'string' ? value : undefined;
}

router.get('/', jsonHandler((req) => listActivity(req.userId, {
  scope: queryText(req.query.scope),
  nodeId: queryText(req.query.nodeId),
  cursor: queryText(req.query.cursor),
  limit: queryText(req.query.limit),
})));

module.exports = router;
