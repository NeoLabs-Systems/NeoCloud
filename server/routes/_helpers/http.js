'use strict';

const rateLimit = require('express-rate-limit');
const { httpError } = require('../../utils/http_error');
const { sendJsonError } = require('../../http/errors');

// Wraps a handler body: its return value, or what its promise resolves to, is
// the JSON response, and a thrown error (usually an httpError) becomes the
// matching error response.
function jsonHandler(action, { status = 200 } = {}) {
  return async (req, res) => {
    try {
      res.status(status).json(await action(req, res));
    } catch (err) {
      sendJsonError(res, err);
    }
  };
}

// Streams a download; errors before the first byte become JSON, later ones
// can only cut the connection.
function streaming(action) {
  return async (req, res) => {
    try {
      await action(req, res);
    } catch (err) {
      if (!res.headersSent) return sendJsonError(res, err);
      return res.destroy(err);
    }
  };
}

// At most `max` requests per address in 15 minutes.
function rateLimiter(max) {
  return rateLimit({
    windowMs: 15 * 60 * 1000,
    max,
    message: { error: 'Too many requests, try again later', code: 'RATE_LIMITED' },
    standardHeaders: true,
    legacyHeaders: false,
  });
}

// `:id` route params that name a users row: a positive integer.
function intParam(value, label = 'id') {
  const id = Number(value);
  if (!Number.isInteger(id) || id <= 0) {
    throw httpError(400, `Invalid ${label}`, 'INVALID_ID');
  }
  return id;
}

// Where people reach this server: PUBLIC_URL when configured, else the
// origin the request came in on.
function publicOrigin(req) {
  const publicUrl = String(req.app?.locals?.httpRuntimeConfig?.publicUrl || '');
  if (publicUrl) return new URL(publicUrl).origin;
  return `${req.protocol}://${req.get('host')}`;
}

module.exports = { intParam, jsonHandler, publicOrigin, rateLimiter, streaming };
