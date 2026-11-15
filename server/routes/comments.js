'use strict';

// Comments on files and folders (session or device token): everyone who can
// reach the node reads and writes them; share links never do.

const express = require('express');
const rateLimit = require('express-rate-limit');
const { requireAuthOrDevice } = require('../middleware/auth');
const comments = require('../services/comments/comments');
const { intParam, jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use('/files/:id/comments', requireAuthOrDevice);

const writeLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 120,
  keyGenerator: (req) => `user:${req.userId}`,
  message: { error: 'Too many comments, try again later', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

router.get('/files/:id/comments', jsonHandler((req) => comments.listComments(req.userId, req.params.id, {
  cursor: typeof req.query.cursor === 'string' ? req.query.cursor : undefined,
  limit: typeof req.query.limit === 'string' ? req.query.limit : undefined,
})));

router.post('/files/:id/comments', writeLimiter, jsonHandler(
  (req) => comments.addComment(req.userId, req.params.id, req.body?.body),
  { status: 201 },
));

router.patch('/files/:id/comments/:commentId', writeLimiter, jsonHandler((req) => comments.editComment(
  req.userId,
  req.params.id,
  intParam(req.params.commentId, 'comment id'),
  req.body?.body,
)));

router.delete('/files/:id/comments/:commentId', writeLimiter, jsonHandler((req) => comments.deleteComment(
  req.userId,
  req.params.id,
  intParam(req.params.commentId, 'comment id'),
)));

module.exports = router;
