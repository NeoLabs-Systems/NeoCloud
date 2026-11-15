'use strict';

// Paths inside a mount: always relative to the mount's root, "/"-separated,
// no leading or trailing slash, "" for the root itself. Every path a client
// sends goes through normalizePath before anything touches a provider, so a
// ".." (or its encodings) can never lead above the root the user attached.

const { httpError } = require('../../utils/http_error');
const { validateName } = require('../../utils/file_names');

const MAX_PATH_LENGTH = 2048;
const MAX_DEPTH = 32;
const FORBIDDEN = /[\u0000-\u001f\u007f\\]/;

function invalid() {
  return httpError(400, 'That path is not valid.', 'INVALID_PATH');
}

/** "a//b/" → "a/b"; "" and "/" → "". Refuses ".", "..", control characters and backslashes. */
function normalizePath(raw) {
  if (raw === undefined || raw === null || raw === '') return '';
  if (typeof raw !== 'string' || raw.length > MAX_PATH_LENGTH || FORBIDDEN.test(raw)) throw invalid();
  const segments = raw.normalize('NFC').split('/').filter(Boolean);
  if (segments.length > MAX_DEPTH) throw invalid();
  for (const segment of segments) {
    if (segment === '.' || segment === '..' || Buffer.byteLength(segment, 'utf8') > 255) throw invalid();
  }
  return segments.join('/');
}

function joinPath(folder, name) {
  return folder ? `${folder}/${validateName(name)}` : validateName(name);
}

function parentOf(path) {
  const index = path.lastIndexOf('/');
  return index < 0 ? '' : path.slice(0, index);
}

function baseName(path) {
  return path.slice(path.lastIndexOf('/') + 1);
}

/** True when `path` is `folder` or lies below it. */
function isInside(folder, path) {
  return path === folder || path.startsWith(`${folder}/`);
}

/** A name a provider listed: one segment, never empty, ".", ".." or holding a slash. */
function isSafeListedName(name) {
  return typeof name === 'string' && name !== '' && name !== '.' && name !== '..' && !/[/\\\u0000]/.test(name);
}

module.exports = { baseName, isInside, isSafeListedName, joinPath, normalizePath, parentOf };
