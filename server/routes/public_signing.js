'use strict';

// Signing links as a signer without an account uses them. The token in the
// path is the only credential: no cookie is read or set, every response is
// uncacheable and unindexed, and a token that doesn't work is a plain 404.
// Limits apply per address, per token, and (tighter) to addresses that keep
// trying tokens that don't exist.

const crypto = require('crypto');
const express = require('express');
const publicSigning = require('../services/signing/public');
const { completeParty, declineParty } = require('../services/signing/complete');
const results = require('../services/signing/results');
const { sendCleanPdf, sendSnapshotPdf, sendValueImage } = require('../services/signing/serve');
const { sendFile } = require('../services/transfer/downloads');
const { sendJsonError } = require('../http/errors');
const { httpError } = require('../utils/http_error');
const { jsonHandler, rateLimiter, streaming } = require('./_helpers/http');

const router = express.Router();

router.use((req, res, next) => {
  res.set({
    'Cache-Control': 'no-store, max-age=0',
    'X-Robots-Tag': 'noindex, nofollow',
    'Referrer-Policy': 'no-referrer',
  });
  next();
});

const byToken = (req) => crypto.createHash('sha256').update(String(req.params.token)).digest('hex');
const readLimiter = rateLimiter(600);
const writeLimiter = rateLimiter(300);
const tokenLimiter = rateLimiter(500, { keyGenerator: byToken });
// Only answers that say "no such link" count here.
const guessLimiter = rateLimiter(60, { skipSuccessfulRequests: true, requestWasSuccessful: (req, res) => res.statusCode !== 404 });

const reading = [guessLimiter, readLimiter, tokenLimiter];
const writing = [guessLimiter, writeLimiter, tokenLimiter];

const context = (req) => publicSigning.openContext(req.params.token);

router.get('/:token', ...reading, jsonHandler((req) => publicSigning.describe(context(req), req)));

router.get('/:token/pdf', ...reading, streaming(async (req, res) => {
  const current = context(req);
  if (current.state !== 'ready' && current.state !== 'completed') throw publicSigning.refusal(current.state);
  await sendSnapshotPdf(req, res, current.document);
}));

router.post('/:token/consent', ...writing, jsonHandler((req) => publicSigning.recordConsent(context(req), req.body, req)));

router.put('/:token/fields/:fieldId', ...writing, jsonHandler((req) => {
  if (!req.body || !('value' in req.body)) throw httpError(400, 'The body needs a "value".', 'INVALID_REQUEST');
  return publicSigning.saveFieldValue(context(req), req.params.fieldId, req.body.value);
}));

// The body is the raw image or file; see services/signing/uploads.js.
router.post('/:token/fields/:fieldId/upload', ...writing, jsonHandler((req) => (
  publicSigning.uploadFieldFile(context(req), req.params.fieldId, req)
), { status: 201 }));

router.get('/:token/values/:fieldId/image', ...reading, streaming(async (req, res) => {
  const current = context(req);
  const { row, size } = publicSigning.imageValue(current, req.params.fieldId);
  await sendValueImage(res, current.document.owner_id, row, size);
}));

// A required field still empty answers 400 FIELDS_INCOMPLETE with `fieldIds`.
router.post('/:token/complete', ...writing, (req, res) => {
  try {
    res.json(completeParty(context(req), req));
  } catch (err) {
    if (!err.fieldIds) return sendJsonError(res, err);
    return res.status(err.status).json({ error: err.message, code: err.code, fieldIds: err.fieldIds });
  }
  return undefined;
});

router.post('/:token/decline', ...writing, jsonHandler((req) => declineParty(context(req), req.body?.reason, req)));

router.get('/:token/result', ...reading, jsonHandler((req) => {
  const current = context(req);
  return { state: current.state, finalAvailable: publicSigning.finalAvailable(current.document) };
}));

router.get('/:token/final', ...reading, streaming(async (req, res) => {
  if (!results.wantsCertificate(req.query)) {
    const { document, fileName } = await results.cleanForSigner(context(req), req);
    await sendCleanPdf(req, res, document, fileName);
    return;
  }
  const { ownerId, nodeId } = results.finalForSigner(context(req), req);
  await sendFile(req, res, ownerId, nodeId, { download: true });
}));

module.exports = router;
