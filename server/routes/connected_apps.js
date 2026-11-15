'use strict';

// The apps connected to the account (NeoAgent), as settings shows them, and
// disconnecting one. Mounted at /api/account beside routes/account.js.

const express = require('express');
const { requireSessionOrPhone } = require('../middleware/auth');
const { disconnectApp, listConnectedApps } = require('../services/oauth/connected_apps');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use('/connected-apps', requireSessionOrPhone);

router.get('/connected-apps', jsonHandler((req) => ({ apps: listConnectedApps(req.userId) })));

router.delete('/connected-apps/:id', jsonHandler((req) => ({
  ...disconnectApp(req.userId, String(req.params.id)),
  apps: listConnectedApps(req.userId),
})));

module.exports = router;
