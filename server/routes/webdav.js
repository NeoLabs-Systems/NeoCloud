'use strict';

// WebDAV at /dav/files/<username>/, with the Nextcloud-style aliases
// /remote.php/dav/files/<username>/ and /remote.php/webdav/. Mounted before
// the API middleware: WebDAV clients send no Origin, use methods CORS would
// answer itself (OPTIONS), and stream bodies the JSON parser must not touch.

const express = require('express');
const rateLimit = require('express-rate-limit');
const { isAllowedOrigin } = require('../config/origins');
const { requireAppPassword } = require('../middleware/auth');
const { basicCredentials } = require('../services/webdav/auth');
const { parseDavPath } = require('../services/webdav/paths');
const { ALLOWED_METHODS, getOrHead, propfind, sendOptions } = require('../services/webdav/read_methods');
const { moveOrCopy, mkcol, proppatch, put, remove } = require('../services/webdav/write_methods');
const { lock, unlock } = require('../services/webdav/lock_methods');
const { sendError, sendFailure } = require('../services/webdav/respond');

const router = express.Router();

const HANDLERS = {
  PROPFIND: propfind,
  PROPPATCH: proppatch,
  GET: getOrHead,
  HEAD: getOrHead,
  PUT: put,
  MKCOL: mkcol,
  DELETE: remove,
  MOVE: moveOrCopy,
  COPY: moveOrCopy,
  LOCK: lock,
  UNLOCK: unlock,
};

function requestPath(req) {
  return req.originalUrl.split('?')[0];
}

// Wrong app passwords: at most 60 per address and 30 per username in
// 15 minutes. Requests without credentials (the first leg of the Basic
// challenge) and successful ones don't count.
function failedAuthLimiter(max, keyGenerator) {
  return rateLimit({
    windowMs: 15 * 60 * 1000,
    max,
    ...(keyGenerator ? { keyGenerator } : {}),
    skip: (req) => !basicCredentials(req),
    skipSuccessfulRequests: true,
    requestWasSuccessful: (req, res) => res.statusCode !== 401,
    standardHeaders: true,
    legacyHeaders: false,
    handler: (req, res) => sendError(res, 429, 'Too many failed sign-ins. Try again later.'),
  });
}

const ipLimiter = failedAuthLimiter(60);
const usernameLimiter = failedAuthLimiter(30, (req) => `user:${basicCredentials(req).username.toLowerCase()}`);

// Discovery: /.well-known/webdav points at the account's files.
router.all(/^\/\.well-known\/webdav\/?$/i, (req, res) => res.redirect(301, '/remote.php/webdav/'));

router.use((req, res, next) => {
  let target;
  try {
    target = parseDavPath(requestPath(req));
  } catch (err) {
    return sendFailure(req, res, err);
  }
  if (!target) return next('router');
  req.dav = target;
  res.set({
    'X-Content-Type-Options': 'nosniff',
    'Content-Security-Policy': "sandbox; default-src 'none'",
    'Referrer-Policy': 'no-referrer',
    'Cache-Control': 'no-store',
  });
  // WebDAV clients send no Origin; a browser page on another site does.
  if (!isAllowedOrigin(req.get('origin'))) return sendError(res, 403, 'Origin not allowed.');
  // Windows asks OPTIONS before it sends credentials.
  if (req.method === 'OPTIONS') return sendOptions(res);
  return next();
}, ipLimiter, usernameLimiter, requireAppPassword, async (req, res) => {
  const handler = HANDLERS[req.method];
  if (!handler) {
    res.set('Allow', ALLOWED_METHODS);
    return sendError(res, 405, 'Method not allowed.');
  }
  const { base, username, segments } = req.dav;
  if (username !== null && username.toLowerCase() !== req.username.toLowerCase()) {
    return sendError(res, 404, 'File or folder not found');
  }
  try {
    return await handler({ req, res, userId: req.userId, username: req.username, base, segments });
  } catch (err) {
    return sendFailure(req, res, err);
  }
});

module.exports = router;
