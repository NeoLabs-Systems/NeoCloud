'use strict';

// Shared request/response plumbing for the WebDAV methods: reading small
// XML bodies, sending XML, and turning NeoCloud errors into WebDAV statuses.

const { httpError, publicErrorCode } = require('../../utils/http_error');
const { sanitizeError } = require('../../utils/security');
const { createServiceLogger } = require('../../utils/logger');
const { SABRE, DAV, document, element, escapeXml } = require('./xml');

const logger = createServiceLogger('WebDAV');
const MAX_XML_BODY_BYTES = 1024 * 1024;

// NeoCloud's error codes where WebDAV expects a different status.
const STATUS_FOR_CODE = {
  QUOTA_EXCEEDED: 507,
  DISK_FULL: 507,
  NAME_EXISTS: 409,
  NOT_A_FOLDER: 409,
  NOT_A_FILE: 409,
  MOVE_INTO_SELF: 409,
  COPY_INTO_SELF: 409,
  ROOT_IMMUTABLE: 403,
  VAULT_BOUNDARY: 403,
  ETAG_MISMATCH: 412,
};

/** The request body as text, at most 1 MB; larger bodies are refused unread. */
async function readXmlBody(req) {
  const declared = Number(req.get('content-length'));
  if (declared > MAX_XML_BODY_BYTES) throw httpError(413, 'The XML body is too large.', 'BODY_TOO_LARGE');
  const chunks = [];
  let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > MAX_XML_BODY_BYTES) throw httpError(413, 'The XML body is too large.', 'BODY_TOO_LARGE');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

function hasBody(req) {
  return Number(req.get('content-length')) > 0 || req.get('transfer-encoding') !== undefined;
}

function sendXml(res, status, xml) {
  res.status(status).set('Content-Type', 'application/xml; charset=utf-8').send(xml);
}

/** A sabre-style error body, which Nextcloud clients know how to read. */
function sendError(res, status, message, condition = null) {
  if (res.headersSent) {
    res.destroy();
    return;
  }
  const body = (condition ? element(DAV, condition) : '') + element(SABRE, 'message', escapeXml(message));
  sendXml(res, status, document(DAV, 'error', body));
}

/** Sends `err` (usually an httpError) as the matching WebDAV status. */
function sendFailure(req, res, err) {
  const code = publicErrorCode(err);
  const status = STATUS_FOR_CODE[code] || Number(err?.status || err?.statusCode) || 500;
  if (status >= 500 && !code) {
    logger.error(`${req.method} failed:`, sanitizeError(err));
    return sendError(res, 500, 'Something went wrong.');
  }
  // A refused upload may still be sending its body; closing the connection
  // after the answer stops it without leaving the socket to drain.
  if (req.method === 'PUT' && status >= 400) res.set('Connection', 'close');
  return sendError(res, status, err.message || 'Request failed.', status === 423 ? 'lock-token-submitted' : null);
}

module.exports = { hasBody, readXmlBody, sendError, sendFailure, sendXml };
