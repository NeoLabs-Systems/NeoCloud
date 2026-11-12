'use strict';

const { isAdminUser } = require('../services/access/admin');
const { isAccountActive } = require('../services/account/status');
const { resolveDeviceToken } = require('../services/devices/tokens');
const { basicCredentials, resolveAppPassword } = require('../services/webdav/auth');

function unauthorized(res) {
  return res.status(401).json({ error: 'Unauthorized', code: 'UNAUTHORIZED' });
}

// The session's account while it exists and isn't disabled. Read on every
// request, so disabling an account locks out a session nobody revoked.
function sessionUserId(req) {
  const userId = req.session?.userId;
  return userId && isAccountActive(userId) ? userId : null;
}

function deviceFromHeader(req) {
  const header = String(req.get('authorization') || '');
  const match = header.match(/^Bearer\s+(\S+)$/i);
  return match ? resolveDeviceToken(match[1]) : null;
}

// A browser session or a device bearer token (desktop, mobile, lite clients).
function requireAuthOrDevice(req, res, next) {
  const userId = sessionUserId(req);
  if (userId) {
    req.userId = userId;
    return next();
  }
  const device = deviceFromHeader(req);
  if (!device) return unauthorized(res);
  req.userId = device.userId;
  req.deviceId = device.deviceId;
  next();
}

// The phone app is a full client like the browser: its token (android/ios
// only) reaches account, settings, imports and admin, and approves other
// sign-ins. Desktop tokens and WebDAV app passwords never do.
const PHONE_PLATFORMS = new Set(['android', 'ios']);

function phoneRequired(res) {
  return res.status(403).json({ error: 'Use a browser or the NeoCloud phone app for this.', code: 'PHONE_REQUIRED' });
}

function requireSessionOrPhone(req, res, next) {
  const userId = sessionUserId(req);
  if (userId) {
    req.userId = userId;
    return next();
  }
  const device = deviceFromHeader(req);
  if (!device) return unauthorized(res);
  if (!PHONE_PLATFORMS.has(device.platform)) return phoneRequired(res);
  req.userId = device.userId;
  req.deviceId = device.deviceId;
  next();
}

// A device bearer token only.
function requireDevice(req, res, next) {
  const device = deviceFromHeader(req);
  if (!device) return unauthorized(res);
  req.userId = device.userId;
  req.deviceId = device.deviceId;
  next();
}

// Admin is read from the account row on every request (never cached in the
// session or the token), so revoking it takes effect immediately. A browser
// session or the phone app's token; a disabled account's token resolves to
// nothing (resolveDeviceToken).
function requireAdmin(req, res, next) {
  let userId = sessionUserId(req);
  let deviceId = null;
  if (!userId) {
    const device = deviceFromHeader(req);
    if (device && PHONE_PLATFORMS.has(device.platform)) {
      userId = device.userId;
      deviceId = device.deviceId;
    }
  }
  if (!userId || !isAdminUser(userId)) {
    return res.status(403).json({ error: 'Admin access required', code: 'ADMIN_REQUIRED' });
  }
  req.userId = userId;
  if (deviceId) req.deviceId = deviceId;
  next();
}

// With secure cookies on, a request over plain http never gets its session
// cookie back. Refuse up front instead of "signing in" an account that is
// logged out again on the next request.
function requireSessionTransport(req, res, next) {
  if (req.session?.cookie?.secure !== true || req.secure) return next();
  const publicUrl = String(req.app?.locals?.httpRuntimeConfig?.publicUrl || '');
  const httpsUrl = publicUrl.startsWith('https://') ? new URL(publicUrl).origin : `https://${req.get('host')}`;
  return res.status(400).json({
    error: `Sign-in needs a secure connection. Open ${httpsUrl} instead.`,
    code: 'INSECURE_TRANSPORT',
    httpsUrl,
  });
}

// WebDAV: HTTP Basic with the username and a `webdav` app password. Session
// cookies are never looked at.
function requireAppPassword(req, res, next) {
  const credentials = basicCredentials(req);
  const account = credentials ? resolveAppPassword(credentials) : null;
  if (!account) {
    res.set('WWW-Authenticate', 'Basic realm="NeoCloud", charset="UTF-8"');
    return res.status(401).end();
  }
  req.userId = account.userId;
  req.deviceId = account.deviceId;
  req.username = account.username;
  return next();
}

module.exports = { requireAdmin, requireAppPassword, requireAuthOrDevice, requireDevice, requireSessionOrPhone, requireSessionTransport };
