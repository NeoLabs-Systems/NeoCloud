'use strict';

const express = require('express');
const rateLimit = require('express-rate-limit');
const db = require('../db/database');
const { requireSessionOrPhone } = require('../middleware/auth');
const { changePassword } = require('../services/account/credentials');
const twoFactor = require('../services/account/two_factor');
const webauthn = require('../services/account/webauthn');
const { listSessions, ownSessionId, regenerateCurrentSession, revokeSession } = require('../services/account/sessions');
const { createDeviceToken, listDeviceTokens, revokeDeviceToken } = require('../services/devices/tokens');
const { decideAuthorization, describeAuthorization } = require('../services/devices/authorizations');
const { getPairingStatus, startPairing } = require('../services/devices/pairing');
const qrLogin = require('../services/account/qr_login');
const { getUsage } = require('../services/storage/quota');
const { jsonHandler, intParam, publicOrigin, rateLimiter } = require('./_helpers/http');
const { toUserPayload } = require('./auth');
const { sendJsonError } = require('../http/errors');
const { httpError } = require('../utils/http_error');
const { exportAccount } = require('../services/account/export');
const { assertNotLastAdmin, eraseAccount } = require('../services/account/erasure');
const { SESSION_COOKIE_NAME } = require('../http/middleware');

const router = express.Router();

const accountLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 30,
  message: { error: 'Too many account security attempts, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

// The phone app is a full client holding a device token, not a session:
// every route here takes a browser session or the phone app's token
// (requireSessionOrPhone; desktop tokens and WebDAV app passwords get 403).
// Handlers that touch the session check req.deviceId, set for a phone.

// Approving a device-code sign-in (see /api/devices). Its own limiter: the
// user codes are short, so guessing them must stay slow.
const deviceCodeLimiter = rateLimiter(30);

router.get('/devices/authorize/:userCode', deviceCodeLimiter, requireSessionOrPhone, jsonHandler((req) => describeAuthorization(req.params.userCode)));

router.post('/devices/authorize/:userCode', deviceCodeLimiter, requireSessionOrPhone, jsonHandler((req) => decideAuthorization(
  req.userId,
  req.params.userCode,
  req.body?.approve,
)));

// QR sign-in (services/account/qr_login.js): the phone scanned the code a
// signed-out browser shows. resolve marks it scanned and returns the preview.
function qrLoginTarget(req) {
  const challengeId = String(req.body?.challengeId || '').trim();
  const secret = String(req.body?.secret || '').trim();
  if (!challengeId || !secret) throw httpError(400, 'Challenge id and QR secret are required', 'MISSING_FIELDS');
  return { challengeId, secret };
}

router.post('/qr-login/resolve', accountLimiter, requireSessionOrPhone, jsonHandler((req) => qrLogin.markChallengeScanned({
  ...qrLoginTarget(req),
  scannerMetadata: req.body?.scannerMetadata,
})));

router.post('/qr-login/decline', accountLimiter, requireSessionOrPhone, jsonHandler((req) => {
  qrLogin.declineChallenge(qrLoginTarget(req));
  return { success: true };
}));

router.post('/qr-login/approve', accountLimiter, requireSessionOrPhone, jsonHandler((req) => qrLogin.approveChallenge({
  ...qrLoginTarget(req),
  userId: req.userId,
  sessionId: req.deviceId ? null : req.sessionID,
  deviceId: req.deviceId || null,
  approvalMetadata: req.body?.approvalMetadata,
})));

router.use(requireSessionOrPhone);

// The password, and a code when 2FA is on, for twoFactor.confirmIdentity.
function identity(req) {
  return { currentPassword: req.body?.currentPassword, code: req.body?.code };
}

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

// Signs out every other session and every device. The caller stays: a
// browser session gets a new id, a phone keeps its token.
router.put('/password', accountLimiter, jsonHandler(async (req) => {
  await changePassword(req.userId, {
    currentPassword: req.body?.currentPassword,
    newPassword: req.body?.newPassword,
  }, { keepSessionId: ownSessionId(req), keepDeviceId: req.deviceId || null });
  if (!req.deviceId) await regenerateCurrentSession(req);
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
// challenge waits in the session between options and verify, and it is bound
// to the web origin: the phone app lists, renames and removes keys but adds
// them only in a browser (BROWSER_REQUIRED). A key signs in on its own and
// survives a password change, so starting one asks for the password again,
// and a code when 2FA is on.
router.get('/security-keys', jsonHandler((req) => ({ securityKeys: webauthn.listCredentials(req.userId) })));

function requireBrowser(req, res, next) {
  if (!req.deviceId) return next();
  return res.status(403).json({ error: 'Add security keys and passkeys in a browser.', code: 'BROWSER_REQUIRED' });
}

router.post('/security-keys/register/options', accountLimiter, requireBrowser, jsonHandler(async (req) => {
  await twoFactor.confirmIdentity(req.userId, identity(req));
  const { options, challenge } = await webauthn.beginRegistration({ req, userId: req.userId });
  req.session.pendingWebAuthnRegistration = challenge;
  return { options };
}));

router.post('/security-keys/register/verify', accountLimiter, requireBrowser, jsonHandler((req) => {
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
// `current` marks the phone asking; it signs out with Disconnect, not here.
router.get('/devices', jsonHandler((req) => ({
  devices: listDeviceTokens(req.userId).map((device) => ({ ...device, current: device.id === req.deviceId })),
})));

router.post('/devices', accountLimiter, jsonHandler((req) => createDeviceToken(req.userId, {
  name: req.body?.name,
  platform: req.body?.platform,
}), { status: 201 }));

// QR pairing: the web page shows a code for a phone to redeem at
// POST /api/devices/pair, and polls its status (or waits for devices:paired).
const pairingLimiter = rateLimiter(30);

router.post('/devices/pairing', pairingLimiter, jsonHandler((req) => startPairing(req.userId, {
  name: req.body?.name,
  platform: req.body?.platform,
}, publicOrigin(req))));

router.get('/devices/pairing/:code', jsonHandler((req) => getPairingStatus(req.userId, req.params.code)));

router.delete('/devices/:id', accountLimiter, jsonHandler((req) => {
  const id = intParam(req.params.id, 'device id');
  if (id === req.deviceId) throw httpError(400, 'Use Disconnect to sign this phone out', 'CURRENT_DEVICE');
  return revokeDeviceToken(req.userId, id);
}));

// Your data (NeoRouter's account settings): export the account's data or
// delete the account. Both ask for the password again, and a code when 2FA
// is on; a few tries a minute.
const accountDataLimiter = rateLimit({
  windowMs: 60 * 1000,
  max: 5,
  message: { error: 'Too many attempts, try again in a minute', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

router.post('/export', accountDataLimiter, jsonHandler(async (req, res) => {
  await twoFactor.confirmIdentity(req.userId, identity(req));
  const data = exportAccount(req.userId);
  const date = data.exportedAt.slice(0, 10);
  res.set('Cache-Control', 'no-store');
  res.attachment(`neocloud-account-${data.profile.username}-${date}.json`);
  return data;
}));

// Deletes the account with everything in it; `confirmUsername` must repeat
// the username. Ends this session too (a phone's token goes with the account).
router.post('/delete', accountDataLimiter, async (req, res) => {
  try {
    const user = currentUser(req);
    await twoFactor.confirmIdentity(req.userId, identity(req));
    const typed = String(req.body?.confirmUsername ?? '').trim().toLowerCase();
    if (typed !== user.username.toLowerCase()) {
      throw httpError(400, 'Type your username to confirm.', 'CONFIRMATION_MISMATCH');
    }
    assertNotLastAdmin(user);
    await eraseAccount(user.id, { actorUserId: user.id });
    if (req.deviceId) return res.json({ success: true });
    return req.session.destroy(() => {
      res.clearCookie(SESSION_COOKIE_NAME, {
        path: '/',
        httpOnly: true,
        sameSite: 'lax',
        secure: req.app?.locals?.httpRuntimeConfig?.secureCookies === true,
      });
      res.json({ success: true });
    });
  } catch (err) {
    sendJsonError(res, err);
  }
});

module.exports = router;
