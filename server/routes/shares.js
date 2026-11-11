'use strict';

// Share links as their owner manages them (session or device token). What a
// visitor can do with a link is in public_shares.js.

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const links = require('../services/shares/links');
const { intParam, jsonHandler, publicOrigin } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuthOrDevice);

// The editable fields of a link, as the body names them.
function shareFields(body) {
  return {
    expiresAt: body?.expiresAt,
    password: body?.password,
    mode: body?.mode,
    allowUpload: body?.allowUpload,
    maxDownloads: body?.maxDownloads,
    maxUploadBytes: body?.maxUploadBytes,
    note: body?.note,
  };
}

router.post('/', jsonHandler((req) => links.createShare(req.userId, {
  nodeId: req.body?.nodeId,
  ...shareFields(req.body),
}, publicOrigin(req)), { status: 201 }));

router.get('/', jsonHandler((req) => links.listShares(req.userId, publicOrigin(req), {
  nodeId: typeof req.query.nodeId === 'string' ? req.query.nodeId : undefined,
})));

router.patch('/:id', jsonHandler((req) => links.updateShare(
  req.userId,
  intParam(req.params.id, 'share id'),
  shareFields(req.body),
  publicOrigin(req),
)));

router.delete('/:id', jsonHandler((req) => links.revokeShare(req.userId, intParam(req.params.id, 'share id'))));

module.exports = router;
