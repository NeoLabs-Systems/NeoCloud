'use strict';

// Document editing for the apps: whether the editor is on, and the session
// that opens a file in it (docs/office-editing.md). The editor itself talks
// to routes/wopi.js.

const express = require('express');
const rateLimit = require('express-rate-limit');
const { requireAuthOrDevice } = require('../middleware/auth');
const { createEditSession, editableExtensions } = require('../services/office/sessions');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(['/office', '/files/:id/edit-session'], requireAuthOrDevice);

const sessionLimiter = rateLimit({
  windowMs: 60 * 1000,
  max: 60,
  keyGenerator: (req) => `user:${req.userId}`,
  message: { error: 'Too many requests, try again shortly', code: 'RATE_LIMITED' },
  standardHeaders: true,
  legacyHeaders: false,
});

router.get('/office/config', jsonHandler(async () => {
  const extensions = await editableExtensions();
  return { enabled: extensions.length > 0, extensions };
}));

router.post('/files/:id/edit-session', sessionLimiter, jsonHandler((req) => createEditSession(
  req.userId,
  req.params.id,
  { baseUrl: `${req.protocol}://${req.get('host')}` },
)));

module.exports = router;
