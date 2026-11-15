'use strict';

// Send to device: files from one of the account's devices to another
// (docs/handoff.md). Every route knows who is asking as a device: a signed-in
// app by its token, a browser by its session.

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const delivery = require('../services/handoff/delivery');
const { sendFile, sendZip } = require('../services/handoff/download');
const { listDevices } = require('../services/handoff/devices');
const { requestKey } = require('../services/handoff/keys');
const { openEventStream } = require('../services/handoff/live');
const offers = require('../services/handoff/offers');
const { stageUpload } = require('../services/handoff/staging');
const { jsonHandler, rateLimiter, streaming } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuthOrDevice);

const createLimiter = rateLimiter(60);
const uploadLimiter = rateLimiter(600);
const actionLimiter = rateLimiter(300);

const me = (req) => [req.userId, requestKey(req)];

router.get('/devices', jsonHandler((req) => {
  const key = requestKey(req);
  return { devices: listDevices(req.userId).map((device) => ({ ...device, current: device.id === key })) };
}));

// Server-sent events for signed-in apps: ?receive=0 stays out of the list of devices that can be sent to.
router.get('/events', (req, res) => {
  openEventStream(req, res, { userId: req.userId, key: requestKey(req), receive: req.query.receive !== '0' });
});

router.get('/offers', jsonHandler((req) => {
  const [userId, key] = me(req);
  return { sent: offers.listSent(userId, key), inbox: offers.listInbox(userId, key) };
}));

router.post('/offers', createLimiter, jsonHandler((req) => {
  const [userId, key] = me(req);
  const { offer, ready } = offers.createOffer(userId, key, {
    targetId: req.body?.targetId,
    files: req.body?.files,
    nodeIds: req.body?.nodeIds,
    userAgent: req.get('user-agent'),
  });
  if (ready) delivery.markReadyWhenComplete(userId, offer.id);
  return offers.getOffer(userId, key, offer.id);
}, { status: 201 }));

router.get('/offers/:id', jsonHandler((req) => offers.getOffer(...me(req), req.params.id)));

// PUT /offers/:id/files/:fileId — the body is the file's bytes; the send becomes ready with the last one.
router.put('/offers/:id/files/:fileId', uploadLimiter, jsonHandler(async (req) => {
  const [userId, key] = me(req);
  const file = offers.uploadTarget(userId, key, req.params.id, req.params.fileId);
  await stageUpload(userId, file, req);
  delivery.markReadyWhenComplete(userId, file.offer_id);
  return offers.getOffer(userId, key, file.offer_id);
}));

router.post('/offers/:id/accept', actionLimiter, jsonHandler((req) => delivery.accept(...me(req), req.params.id)));
router.post('/offers/:id/decline', actionLimiter, jsonHandler((req) => delivery.decline(...me(req), req.params.id)));
router.post('/offers/:id/complete', actionLimiter, jsonHandler((req) => delivery.complete(...me(req), req.params.id)));
router.delete('/offers/:id', actionLimiter, jsonHandler((req) => delivery.cancel(...me(req), req.params.id)));

router.get('/offers/:id/files/:fileId', streaming((req, res) => sendFile(req, res, ...me(req), req.params.id, req.params.fileId)));
router.get('/offers/:id/zip', streaming((req, res) => sendZip(req, res, ...me(req), req.params.id)));

module.exports = router;
