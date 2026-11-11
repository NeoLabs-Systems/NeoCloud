'use strict';

// Where an import reads from: validation of the connection fields and the
// WebDAV folder they lead to.

const { httpError } = require('../../utils/http_error');
const { createWebdavClient } = require('./webdav_client');

const SOURCES = new Set(['nextcloud', 'webdav']);
const MAX_FIELD_LENGTH = 1024;

function requiredText(value, label, code) {
  const text = typeof value === 'string' ? value : '';
  if (!text.trim() || text.length > MAX_FIELD_LENGTH) throw httpError(400, `${label} is required.`, code);
  return text;
}

/** "/Photos//2026/" → "/Photos/2026"; "" or undefined → "/". */
function normalizeSourcePath(raw) {
  if (raw !== undefined && raw !== null && typeof raw !== 'string') {
    throw httpError(400, 'sourcePath must be a folder path.', 'INVALID_PATH');
  }
  const parts = String(raw || '').split('/').filter(Boolean);
  if (parts.some((part) => part === '.' || part === '..') || String(raw || '').length > 4096) {
    throw httpError(400, 'sourcePath must be a folder path.', 'INVALID_PATH');
  }
  return `/${parts.join('/')}`;
}

function normalizeUrl(raw, source) {
  let url;
  try {
    url = new URL(requiredText(raw, 'The server address', 'INVALID_URL').trim());
  } catch (err) {
    if (err.code) throw err;
    throw httpError(400, 'Enter the server address, like https://cloud.example.com.', 'INVALID_URL');
  }
  if (!['http:', 'https:'].includes(url.protocol)) throw httpError(400, 'The server address must start with https:// or http://.', 'INVALID_URL');
  if (url.username || url.password) throw httpError(400, 'Put the user name and password in their own fields.', 'INVALID_URL');
  url.search = '';
  url.hash = '';
  if (source === 'nextcloud') {
    // People paste what their browser shows: …/index.php/apps/files or a DAV link.
    const cut = url.pathname.search(/\/(?:index|remote)\.php(?:\/|$)/);
    if (cut >= 0) url.pathname = url.pathname.slice(0, cut);
    return url.href.replace(/\/+$/, '');
  }
  return url.href.endsWith('/') ? url.href : `${url.href}/`;
}

/** Validates the connection part of a request body. */
function parseConnection(body = {}) {
  const source = String(body.source || 'nextcloud');
  if (!SOURCES.has(source)) throw httpError(400, 'source must be nextcloud or webdav.', 'INVALID_SOURCE');
  return {
    source,
    url: normalizeUrl(body.url, source),
    username: requiredText(body.username, 'The user name', 'CREDENTIALS_REQUIRED').trim(),
    password: requiredText(body.password, 'The app password', 'CREDENTIALS_REQUIRED'),
    sourcePath: normalizeSourcePath(body.sourcePath),
  };
}

/**
 * A client for a stored or parsed connection. For Nextcloud the account's
 * folder is /remote.php/dav/files/<user>/; the legacy /remote.php/webdav/
 * (always the signed-in account) covers logins that are not the user id.
 */
function clientFor({ source, url, username, sourcePath }, password, options = {}) {
  const roots = source === 'nextcloud'
    ? {
      davRoot: `${url}/remote.php/dav/files/${encodeURIComponent(username)}/`,
      fallbackRoot: `${url}/remote.php/webdav/`,
    }
    : { davRoot: url };
  return createWebdavClient({ ...roots, sourcePath, username, password, ...options });
}

module.exports = { clientFor, parseConnection };
