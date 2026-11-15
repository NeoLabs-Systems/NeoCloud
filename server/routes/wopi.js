'use strict';

// The WOPI host endpoints the document editor (Collabora Online) calls
// (docs/office-editing.md). They are not reached with the session cookie:
// every call carries a signed, short-lived access token (query parameter
// access_token or a bearer header), and the account's right to the file is
// checked again on each one.

const express = require('express');
const rateLimit = require('express-rate-limit');
const { httpError } = require('../utils/http_error');
const { sendJsonError } = require('../http/errors');
const { verifyAccessToken } = require('../services/office/tokens');
const { openFile } = require('../services/office/file_access');
const files = require('../services/office/wopi_files');
const locks = require('../services/office/wopi_locks');
const { readOfficeSettings } = require('../services/office/settings');

const router = express.Router();

router.use((req, res, next) => {
  res.set('Cache-Control', 'no-store');
  next();
});

function tokenFrom(req) {
  const bearer = String(req.get('authorization') || '').match(/^Bearer\s+(\S+)$/i);
  return typeof req.query.access_token === 'string' ? req.query.access_token : bearer?.[1];
}

// The token must belong to this very file, and the editor must still be on;
// everything else is 401.
function authenticate(req, res, next) {
  const grant = verifyAccessToken(tokenFrom(req));
  if (!grant || grant.nodeId !== req.params.id || !readOfficeSettings().enabled) {
    return sendJsonError(res, httpError(401, 'The editing session is invalid or has ended.', 'INVALID_TOKEN'));
  }
  req.wopi = grant;
  return next();
}

// Per person and file, not per address: the editor server is one address.
const limiter = rateLimit({
  windowMs: 60 * 1000,
  max: 300,
  keyGenerator: (req) => `wopi:${req.wopi.userId}:${req.wopi.nodeId}`,
  message: { error: 'Too many requests', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

function answerError(res, err) {
  if (err.wopiLock !== undefined) res.set('X-WOPI-Lock', err.wopiLock);
  if (err.wopiBody) return res.status(err.status).json(err.wopiBody);
  return sendJsonError(res, err);
}

function handler(action) {
  return async (req, res) => {
    try {
      const file = openFile(req.wopi.userId, req.wopi.nodeId, { wantWrite: req.wopi.write });
      await action(req, res, file);
    } catch (err) {
      if (res.headersSent) return res.destroy(err);
      return answerError(res, err);
    }
    return undefined;
  };
}

router.use('/files/:id', authenticate, limiter);

router.get('/files/:id', handler((req, res, file) => {
  res.json(files.checkFileInfo(file, req.wopi.userId));
}));

router.get('/files/:id/contents', handler((req, res, file) => files.sendContents(req, res, file)));

router.post('/files/:id/contents', handler(async (req, res, file) => {
  const saved = await files.putContents(req, file, req.wopi.userId, {
    lock: req.get('x-wopi-lock'),
    timestamp: req.get('x-cool-wopi-timestamp') || req.get('x-lool-wopi-timestamp'),
  });
  res.set('X-WOPI-ItemVersion', String(saved.version));
  res.json({ LastModifiedTime: saved.modifiedAt });
}));

// Lock operations share one URL and tell themselves apart by X-WOPI-Override.
router.post('/files/:id', handler((req, res, file) => {
  const { node, canWrite } = file;
  const lock = req.get('x-wopi-lock');
  const operation = String(req.get('x-wopi-override') || '').toUpperCase();
  if (operation !== 'GET_LOCK' && !canWrite) throw httpError(403, 'You may only view this file.', 'READ_ONLY');
  res.set('X-WOPI-ItemVersion', String(node.version));
  switch (operation) {
    case 'LOCK':
      locks.lock(node.id, { lock, oldLock: req.get('x-wopi-oldlock') });
      break;
    case 'REFRESH_LOCK':
      locks.refreshLock(node.id, lock);
      break;
    case 'UNLOCK':
      locks.unlock(node.id, lock);
      break;
    case 'GET_LOCK':
      res.set('X-WOPI-Lock', locks.getLock(node.id));
      break;
    default:
      throw httpError(501, 'This WOPI operation is not supported.', 'NOT_IMPLEMENTED');
  }
  res.status(200).end();
}));

module.exports = router;
