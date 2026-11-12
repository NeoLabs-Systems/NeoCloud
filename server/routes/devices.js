'use strict';

// Sign-in for native clients (device code, QR pairing, security key) and the
// token self-check. The sign-in POSTs are unauthenticated (the client has
// nothing yet) and rate-limited.

const express = require('express');
const db = require('../db/database');
const { requireDevice } = require('../middleware/auth');
const { isAdminUser } = require('../services/access/admin');
const { exchangeDeviceCode, startAuthorization } = require('../services/devices/authorizations');
const { redeemPairing } = require('../services/devices/pairing');
const securityKey = require('../services/devices/security_key_sign_in');
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

// Security key sign-in (docs/clients.md section 10). The relying party is
// this server's public origin, never the request's Origin header: the
// client builds clientDataJSON with that origin itself. Same budget as the
// browser's security key sign-in.
const securityKeyLimiter = rateLimiter(40);

router.post('/webauthn/options', securityKeyLimiter, jsonHandler((req) => securityKey.startSecurityKeySignIn(
  publicOrigin(req),
  req.body?.username,
)));

router.post('/webauthn/verify', securityKeyLimiter, jsonHandler((req) => securityKey.finishSecurityKeySignIn(
  publicOrigin(req),
  {
    challengeId: req.body?.challengeId,
    response: req.body?.response,
    name: req.body?.name,
    platform: req.body?.platform,
  },
)));

router.post('/webauthn/2fa', securityKeyLimiter, jsonHandler((req) => securityKey.finishTwoFactor({
  challengeId: req.body?.challengeId,
  code: req.body?.code,
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
