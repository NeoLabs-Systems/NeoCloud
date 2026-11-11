'use strict';

// Sharing with other accounts on this server (session or device token): the
// owner manages a node's members, a member lists and leaves what is shared
// with them. Access to the shared files themselves goes through the usual
// file and transfer routes.

const express = require('express');
const rateLimit = require('express-rate-limit');
const { requireAuthOrDevice } = require('../middleware/auth');
const grants = require('../services/members/grants');
const { intParam, jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(['/nodes/:id/members', '/shared-with-me'], requireAuthOrDevice);

// Adding members is the only place a typed name is matched against the
// accounts, so it is held to a small budget per account to keep anyone from
// probing which names exist.
const addLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 30,
  keyGenerator: (req) => `user:${req.userId}`,
  message: { error: 'Too many attempts, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

router.post('/nodes/:id/members', addLimiter, jsonHandler((req) => grants.addMember(req.userId, req.params.id, {
  recipient: req.body?.recipient,
  permission: req.body?.permission,
  expiresAt: req.body?.expiresAt,
}), { status: 201 }));

router.get('/nodes/:id/members', jsonHandler((req) => grants.listMembers(req.userId, req.params.id)));

router.patch('/nodes/:id/members/:shareId', jsonHandler((req) => grants.updateMember(
  req.userId,
  req.params.id,
  intParam(req.params.shareId, 'member id'),
  { permission: req.body?.permission, expiresAt: req.body?.expiresAt },
)));

router.delete('/nodes/:id/members/:shareId', jsonHandler((req) => grants.removeMember(
  req.userId,
  req.params.id,
  intParam(req.params.shareId, 'member id'),
)));

router.get('/shared-with-me', jsonHandler((req) => grants.listSharedWithMe(req.userId)));

router.delete('/shared-with-me/:shareId', jsonHandler((req) => grants.leaveShare(
  req.userId,
  intParam(req.params.shareId, 'share id'),
)));

module.exports = router;
