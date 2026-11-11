'use strict';

// Device-code sign-in for native clients, and the token self-check. The two
// POSTs are unauthenticated (the client has nothing yet) and rate-limited.

const express = require('express');
const db = require('../db/database');
const { requireDevice } = require('../middleware/auth');
const { isAdminUser } = require('../services/access/admin');
const { exchangeDeviceCode, startAuthorization } = require('../services/devices/authorizations');
const { redeemPairing } = require('../services/devices/pairing');
const { getDeviceToken } = require('../services/devices/tokens');
const { getUsage } = require('../services/storage/quota');
const { jsonHandler, publicOrigin, rateLimiter } = require('./_helpers/http');

const router = express.Router();

// One poll every 3 s for the 10 minutes a code lives is 200 requests.
const authorizeLimiter = rateLimiter(30);
const tokenLimiter = rateLimiter(600);

router.post('/authorize', authorizeLimiter, jsonHandler((req) => startAuthorization({
  name: req.body?.name,
  platform: req.body?.platform,
}, publicOrigin(req))));

router.post('/token', tokenLimiter, jsonHandler((req) => exchangeDeviceCode(
  typeof req.body?.deviceCode === 'string' ? req.body.deviceCode : '',
)));

// QR pairing (docs/clients.md section 5). Codes are 256-bit, so the limit is
// about keeping a misbehaving client cheap, not about guessing.
const pairLimiter = rateLimiter(30);

router.post('/pair', pairLimiter, jsonHandler((req) => redeemPairing({
  code: req.body?.code,
  name: req.body?.name,
  platform: req.body?.platform,
})));

router.get('/me', requireDevice, jsonHandler((req) => {
  const user = db.prepare('SELECT id, username FROM users WHERE id = ?').get(req.userId);
  const usage = getUsage(req.userId);
  const device = getDeviceToken(req.userId, req.deviceId);
  return {
    device: { id: device.id, name: device.name, platform: device.platform, createdAt: device.createdAt, lastSeenAt: device.lastSeenAt },
    user: { id: user.id, username: user.username, isAdmin: isAdminUser(req.userId) },
    quota: { used: usage.usedBytes, limit: usage.quotaBytes },
  };
}));

module.exports = router;
