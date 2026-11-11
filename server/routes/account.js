'use strict';

const express = require('express');
const rateLimit = require('express-rate-limit');
const db = require('../db/database');
const { requireAuth } = require('../middleware/auth');
const { changePassword } = require('../services/account/credentials');
const twoFactor = require('../services/account/two_factor');
const webauthn = require('../services/account/webauthn');
const { listSessions, regenerateCurrentSession, revokeSession } = require('../services/account/sessions');
const { createDeviceToken, listDeviceTokens, revokeDeviceToken } = require('../services/devices/tokens');
const { decideAuthorization, describeAuthorization } = require('../services/devices/authorizations');
const { getPairingStatus, startPairing } = require('../services/devices/pairing');
const { getUsage } = require('../services/storage/quota');
const { jsonHandler, intParam, publicOrigin, rateLimiter } = require('./_helpers/http');
const { toUserPayload } = require('./auth');

const router = express.Router();
router.use(requireAuth);

const accountLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 30,
  message: { error: 'Too many account security attempts, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

function currentUser(req) {
  return db.prepare('SELECT * FROM users WHERE id = ?').get(req.userId);
}

router.get('/', jsonHandler((req) => ({
  user: toUserPayload(currentUser(req)),
  twoFactor: twoFactor.getTwoFactorStatus(req.userId),
  securityKeys: webauthn.listCredentials(req.userId),
})));

router.get('/usage', jsonHandler((req) => getUsage(req.userId)));

router.put('/display-name', accountLimiter, jsonHandler((req) => {
  const name = String(req.body?.displayName ?? '').trim().slice(0, 80);
  db.prepare('UPDATE users SET display_name = ? WHERE id = ?').run(name || null, req.userId);
  return { user: toUserPayload(currentUser(req)) };
}));

// Signs out every other session and every device; this one gets a new id.
router.put('/password', accountLimiter, jsonHandler(async (req) => {
  await changePassword(req.userId, {
    currentPassword: req.body?.currentPassword,
    newPassword: req.body?.newPassword,
  }, { keepSessionId: req.sessionID });
  await regenerateCurrentSession(req);
  return { success: true };
}));

// With 2FA already on, replacing the factor needs a current code (TOTP or
// recovery code): `code` at setup, `currentCode` at enable.
router.post('/2fa/setup', accountLimiter, jsonHandler(async (req) => ({
  ...(await twoFactor.beginSetup(req.userId, req.body?.currentPassword, req.body?.code)),
  status: twoFactor.getTwoFactorStatus(req.userId),
})));

router.post('/2fa/enable', accountLimiter, jsonHandler((req) => twoFactor.enable(
  req.userId,
  req.body?.code,
  req.body?.currentCode,
)));

router.post('/2fa/disable', accountLimiter, jsonHandler((req) => twoFactor.disable(req.userId, {
  currentPassword: req.body?.currentPassword,
  code: req.body?.code,
})));

router.post('/2fa/recovery-codes', accountLimiter, jsonHandler((req) => twoFactor.regenerateRecoveryCodes(req.userId, {
  currentPassword: req.body?.currentPassword,
  code: req.body?.code,
})));

// Security keys and passkeys. Registration is a two-step ceremony whose
// challenge waits in the session between options and verify.
router.get('/security-keys', jsonHandler((req) => ({ securityKeys: webauthn.listCredentials(req.userId) })));

router.post('/security-keys/register/options', accountLimiter, jsonHandler(async (req) => {
  const { options, challenge } = await webauthn.beginRegistration({ req, userId: req.userId });
  req.session.pendingWebAuthnRegistration = challenge;
  return { options };
}));

router.post('/security-keys/register/verify', accountLimiter, jsonHandler((req) => {
  const pending = req.session.pendingWebAuthnRegistration;
  delete req.session.pendingWebAuthnRegistration;
  return webauthn.completeRegistration({
    req,
    userId: req.userId,
    response: req.body?.response,
    label: req.body?.label,
    pending,
  });
}));

router.put('/security-keys/:id', accountLimiter, jsonHandler((req) => webauthn.renameCredential(
  req.userId,
  intParam(req.params.id, 'security key id'),
  req.body?.label,
)));

router.delete('/security-keys/:id', accountLimiter, jsonHandler((req) => webauthn.deleteCredential(
  req.userId,
  intParam(req.params.id, 'security key id'),
)));

router.get('/sessions', jsonHandler((req) => ({ sessions: listSessions(req, req.userId) })));

router.delete('/sessions/:id', accountLimiter, jsonHandler((req) => ({
  ...revokeSession(req, req.userId, intParam(req.params.id, 'session id')),
  sessions: listSessions(req, req.userId),
})));

// Connected clients (desktop, mobile, lite). The token is returned once.
router.get('/devices', jsonHandler((req) => ({ devices: listDeviceTokens(req.userId) })));

router.post('/devices', accountLimiter, jsonHandler((req) => createDeviceToken(req.userId, {
  name: req.body?.name,
  platform: req.body?.platform,
}), { status: 201 }));

// Approving a device-code sign-in (see /api/devices). Its own limiter: the
// user codes are short, so guessing them must stay slow.
const deviceCodeLimiter = rateLimiter(30);

router.get('/devices/authorize/:userCode', deviceCodeLimiter, jsonHandler((req) => describeAuthorization(req.params.userCode)));

router.post('/devices/authorize/:userCode', deviceCodeLimiter, jsonHandler((req) => decideAuthorization(
  req.userId,
  req.params.userCode,
  req.body?.approve,
)));

// QR pairing: the web page shows a code for a phone to redeem at
// POST /api/devices/pair, and polls its status (or waits for devices:paired).
const pairingLimiter = rateLimiter(30);

router.post('/devices/pairing', pairingLimiter, jsonHandler((req) => startPairing(req.userId, {
  name: req.body?.name,
  platform: req.body?.platform,
}, publicOrigin(req))));

router.get('/devices/pairing/:code', jsonHandler((req) => getPairingStatus(req.userId, req.params.code)));

router.delete('/devices/:id', accountLimiter, jsonHandler((req) => revokeDeviceToken(req.userId, intParam(req.params.id, 'device id'))));

module.exports = router;
