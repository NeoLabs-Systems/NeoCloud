'use strict';

// Share links as a visitor without an account uses them. A password-protected
// link is unlocked with a short-lived signed cookie scoped to the link's path.
// Every endpoint is rate-limited per address.

const express = require('express');
const shares = require('../services/shares/public');
const { continuationProof, isContinuation } = require('../services/shares/proofs');
const { sendFile, sendZip } = require('../services/transfer/downloads');
const { sendJsonError } = require('../http/errors');
const { parseRange } = require('../utils/http_range');
const { jsonHandler, publicOrigin, rateLimiter, streaming } = require('./_helpers/http');

const UNLOCK_COOKIE = 'nc_share_unlock';
const CONTINUE_COOKIE = 'nc_share_continue';

const router = express.Router();
const browseLimiter = rateLimiter(600);
const unlockLimiter = rateLimiter(20);
const uploadLimiter = rateLimiter(100);

function cookiePath(token) {
  return `/api/public/shares/${encodeURIComponent(token)}`;
}

function readCookie(req, name) {
  for (const part of String(req.get('cookie') || '').split(';')) {
    const index = part.indexOf('=');
    if (index > 0 && part.slice(0, index).trim() === name) return part.slice(index + 1).trim();
  }
  return '';
}

function openFromRequest(req, options) {
  return shares.openShare(req.params.token, readCookie(req, UNLOCK_COOKIE), options);
}

function linkCookie(req, path, maxAge) {
  return {
    path,
    maxAge,
    httpOnly: true,
    sameSite: 'strict',
    secure: req.app?.locals?.httpRuntimeConfig?.secureCookies === true,
  };
}

// A Range request that starts past byte 0 (after resolving suffix ranges) and
// carries the cookie a counted download of this file set: it resumes that
// download. Anything that includes byte 0 is a new download.
function resumesDownload(req, context, node) {
  const range = parseRange(req.get('range'), node.size);
  if (!range || range.unsatisfiable || range.start === 0) return false;
  return isContinuation(context.share, node.id, readCookie(req, CONTINUE_COOKIE));
}

function isDownloadFlag(value) {
  return value === '1' || value === 'true';
}

router.get('/:token', browseLimiter, jsonHandler((req) => shares.describeShare(
  openFromRequest(req),
  typeof req.query.folderId === 'string' ? req.query.folderId : '',
  new URL(publicOrigin(req)).host,
)));

// A throttled link answers 429 SHARE_LOCKED with Retry-After and `retryAfter`
// (seconds) in the body.
router.post('/:token/unlock', unlockLimiter, async (req, res) => {
  try {
    const { proof, maxAgeMs } = await shares.unlockShare(req.params.token, req.body?.password);
    res.cookie(UNLOCK_COOKIE, proof, linkCookie(req, cookiePath(req.params.token), maxAgeMs));
    res.json({ success: true });
  } catch (err) {
    if (err.retryAfter) {
      res.set('Retry-After', String(err.retryAfter));
      res.status(err.status).json({ error: err.message, code: err.code, retryAfter: err.retryAfter });
      return;
    }
    sendJsonError(res, err);
  }
});

router.get('/:token/files/:nodeId/content', browseLimiter, streaming(async (req, res) => {
  const download = isDownloadFlag(req.query.download);
  // Only downloads count, not inline views. Resuming a counted download
  // doesn't count again and may finish even once the limit is reached.
  const context = openFromRequest(req, { allowExhausted: true });
  shares.assertCanView(context);
  const node = shares.nodeInShare(context, req.params.nodeId, { type: 'file' });
  if (!(download && resumesDownload(req, context, node))) {
    shares.assertDownloadsLeft(context.share);
    if (download && req.method !== 'HEAD') {
      shares.consumeDownload(context.share);
      const { proof, maxAgeMs } = continuationProof(context.share, node.id);
      const path = `${cookiePath(req.params.token)}/files/${encodeURIComponent(node.id)}`;
      res.cookie(CONTINUE_COOKIE, proof, linkCookie(req, path, maxAgeMs));
    }
  }
  await sendFile(req, res, context.share.user_id, node.id, { download });
}));

router.get('/:token/zip', browseLimiter, streaming(async (req, res) => {
  const context = openFromRequest(req);
  shares.assertCanView(context);
  const node = shares.nodeInShare(context, typeof req.query.folderId === 'string' ? req.query.folderId : '');
  // A HEAD (link previews, download managers probing) is not a download.
  if (req.method !== 'HEAD') shares.consumeDownload(context.share);
  await sendZip(res, context.share.user_id, node.id, { skipVaults: true });
}));

router.put('/:token/upload', uploadLimiter, jsonHandler((req) => shares.uploadIntoShare(openFromRequest(req), {
  folderId: typeof req.query.folderId === 'string' ? req.query.folderId : '',
  name: req.query.name,
  contentLength: req.get('content-length'),
  sha256: req.get('x-content-sha256'),
}, req), { status: 201 }));

module.exports = router;
