'use strict';

const { isAdminUser } = require('../services/access/admin');
const { resolveDeviceToken } = require('../services/devices/tokens');
const { basicCredentials, resolveAppPassword } = require('../services/webdav/auth');

function unauthorized(res) {
  return res.status(401).json({ error: 'Unauthorized', code: 'UNAUTHORIZED' });
}

// Browser session only. Sets req.userId for the handlers below.
function requireAuth(req, res, next) {
  if (!req.session?.userId) return unauthorized(res);
  req.userId = req.session.userId;
  next();
}

function deviceFromHeader(req) {
  const header = String(req.get('authorization') || '');
  const match = header.match(/^Bearer\s+(\S+)$/i);
  return match ? resolveDeviceToken(match[1]) : null;
}

// A browser session or a device bearer token (desktop, mobile, lite clients).
function requireAuthOrDevice(req, res, next) {
  if (req.session?.userId) {
    req.userId = req.session.userId;
    return next();
  }
  const device = deviceFromHeader(req);
  if (!device) return unauthorized(res);
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
// session), so revoking it takes effect immediately.
function requireAdmin(req, res, next) {
  if (!req.session?.userId || !isAdminUser(req.session.userId)) {
    return res.status(403).json({ error: 'Admin access required', code: 'ADMIN_REQUIRED' });
  }
  req.userId = req.session.userId;
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

module.exports = { requireAdmin, requireAppPassword, requireAuth, requireAuthOrDevice, requireDevice, requireSessionTransport };
