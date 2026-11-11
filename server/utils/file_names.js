'use strict';

const { httpError } = require('./http_error');

const MAX_NAME_BYTES = 255;
// Control characters and path separators never belong in a name. Other
// characters Windows rejects (: * ? " < > |) stay allowed, like Nextcloud;
// the desktop client maps them on Windows.
const FORBIDDEN = /[\u0000-\u001f\u007f/\\]/;

function validateName(raw) {
  const name = String(raw ?? '').normalize('NFC').trim();
  if (!name) throw httpError(400, 'Name can’t be empty.', 'NAME_EMPTY');
  if (name === '.' || name === '..') throw httpError(400, 'That name is reserved.', 'NAME_RESERVED');
  if (FORBIDDEN.test(name)) throw httpError(400, 'Names can’t contain / or \\ or control characters.', 'NAME_INVALID');
  if (Buffer.byteLength(name, 'utf8') > MAX_NAME_BYTES) throw httpError(400, 'Name is too long.', 'NAME_TOO_LONG');
  return name;
}

// "report.final.pdf" -> ["report.final", ".pdf"]; dotfiles keep their name.
function splitExtension(name) {
  const index = name.lastIndexOf('.');
  if (index <= 0) return [name, ''];
  return [name.slice(0, index), name.slice(index)];
}

// "photo.jpg" -> "photo (1).jpg", "photo (1).jpg" -> "photo (2).jpg".
function numberedName(name, number) {
  const [stem, extension] = splitExtension(name);
  const base = stem.replace(/ \(\d+\)$/, '');
  return `${base} (${number})${extension}`;
}

// A filename for Content-Disposition: ASCII fallback plus the UTF-8 form.
function contentDisposition(type, name) {
  const fallback = name.replace(/[^\x20-\x7e]/g, '_').replace(/["\\]/g, '_');
  return `${type}; filename="${fallback}"; filename*=UTF-8''${encodeURIComponent(name)}`;
}

module.exports = { contentDisposition, numberedName, splitExtension, validateName };
