'use strict';

const { sanitizeError } = require('../utils/security');
const { logRequestSummary } = require('../utils/logger');
const { httpError, publicErrorCode } = require('../utils/http_error');
const { redactResponseBody } = require('../utils/public_error');
const { isAdminUser } = require('../services/access/admin');

// Every API response passes here, so no route can show a non-admin the
// technical cause of a failure. The server log keeps it for admins.
function redactErrorsForNonAdmins(req, res, next) {
  if (!req.path.startsWith('/api/')) return next();
  const sendJson = res.json.bind(res);
  res.json = (body) => {
    // req.userId: set by the auth guard, for a session or a token alike.
    const userId = req.userId || req.session?.userId;
    if (userId && isAdminUser(userId)) return sendJson(body);
    if (res.statusCode >= 500 && typeof body?.error === 'string' && !body.code) {
      console.error('[API failure hidden from non-admin]', req.method, req.path, res.statusCode, body?.error);
    }
    return sendJson(redactResponseBody(body, res.statusCode));
  };
  next();
}

function statusOf(err, fallback = 500) {
  return Number(err?.status || err?.statusCode || fallback) || fallback;
}

function sendJsonError(res, err, fallbackStatus = 500) {
  const status = statusOf(err, fallbackStatus);
  const code = publicErrorCode(err);
  // A 5xx with an application code (507 DISK_FULL) was written for people.
  if (status >= 500 && !code) {
    console.error('[API error]', sanitizeError(err), err?.stack);
    return res.status(status).json({ error: sanitizeError(err) });
  }
  // A refused upload may still be sending its body; closing the connection
  // after the answer stops it without leaving the socket to drain.
  if (status === 413 || status === 507) res.set('Connection', 'close');
  return res.status(status).json({ error: err?.message || 'Request failed.', ...(code ? { code } : {}) });
}

// The JSON and form parsers' errors carry a `type`, not a code; their
// messages are parser internals ("Unexpected token } in JSON at …").
const BODY_PARSER_ERRORS = {
  'entity.parse.failed': [400, 'The request body is not valid JSON.', 'INVALID_JSON'],
  'entity.too.large': [413, 'The request body is too large.', 'BODY_TOO_LARGE'],
};

function fromBodyParser(err) {
  const known = BODY_PARSER_ERRORS[err?.type];
  if (known) return httpError(...known);
  if (typeof err?.type === 'string' && statusOf(err) < 500) {
    return httpError(statusOf(err), 'The request body could not be read.', 'INVALID_BODY');
  }
  return err;
}

function registerErrorHandler(app) {
  app.use('/api', (req, res) => res.status(404).json({ error: 'Not found', code: 'NOT_FOUND' }));
  // eslint-disable-next-line no-unused-vars
  app.use((rawError, req, res, _next) => {
    const err = fromBodyParser(rawError);
    const status = statusOf(err);
    const message = sanitizeError(err);
    logRequestSummary(status >= 500 ? 'error' : 'warn', req, `failed with ${status}`, {
      error: { message, code: publicErrorCode(err), stack: err?.stack },
    });
    if (res.headersSent) {
      res.destroy();
      return;
    }
    if (req.path.startsWith('/api/')) {
      const code = publicErrorCode(err);
      return res.status(status).json({ error: message, ...(code ? { code } : {}) });
    }
    return res.status(status).send('Something went wrong.');
  });
}

module.exports = { redactErrorsForNonAdmins, registerErrorHandler, sendJsonError };
