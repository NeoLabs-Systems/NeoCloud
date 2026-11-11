'use strict';

// WebDAV URLs and the file tree. A URL names a path below the account root;
// each segment is percent-encoded UTF-8 and is matched against node names
// case-insensitively (NFC), like everywhere else in NeoCloud. Vault folders
// hold ciphertext names and contents, so WebDAV never shows them.

const { httpError } = require('../../utils/http_error');
const { findLiveChildByName, getRoot, nodeById } = require('../files/tree');

// The Nextcloud-style aliases let apps that only speak "Nextcloud" connect.
const MOUNTS = [
  /^(\/dav\/files\/([^/]+))(\/.*)?$/,
  /^(\/remote\.php\/dav\/files\/([^/]+))(\/.*)?$/,
  /^(\/remote\.php\/webdav)(\/.*)?$/,
];

function decodeSegment(raw) {
  let segment;
  try {
    segment = decodeURIComponent(raw).normalize('NFC');
  } catch {
    throw httpError(400, 'The path is not valid percent-encoded UTF-8.', 'PATH_INVALID');
  }
  if (segment === '.' || segment === '..' || segment.includes('/') || segment.includes('\0')) {
    throw httpError(400, 'The path contains an invalid segment.', 'PATH_INVALID');
  }
  return segment;
}

function splitPath(rawPath = '') {
  return rawPath.split('/').filter((part) => part.length > 0).map(decodeSegment);
}

/**
 * { base, username, segments } for a URL path inside a WebDAV mount, else
 * null. `base` is the mount as the client spelled it (hrefs echo it back);
 * `username` is null for /remote.php/webdav, which serves the signed-in user.
 */
function parseDavPath(rawPath) {
  for (const pattern of MOUNTS) {
    const match = pattern.exec(rawPath);
    if (!match) continue;
    const withUser = match.length === 4;
    return {
      base: match[1],
      username: withUser ? decodeSegment(match[2]) : null,
      segments: splitPath(withUser ? match[3] : match[2]),
    };
  }
  return null;
}

/** True when a raw request path is inside a WebDAV mount. */
function isDavPath(rawPath) {
  return MOUNTS.some((pattern) => pattern.test(rawPath));
}

/** Parses a Destination header (absolute URL or absolute path). */
function parseDestination(header) {
  let pathname;
  try {
    pathname = new URL(String(header || ''), 'http://destination.invalid').pathname;
  } catch {
    pathname = null;
  }
  const target = pathname ? parseDavPath(pathname) : null;
  if (!header || !target) throw httpError(400, 'Destination is missing or outside WebDAV.', 'DESTINATION_INVALID');
  return target;
}

function encodePath(segments) {
  return segments.map((segment) => encodeURIComponent(segment)).join('/');
}

function href(base, segments, isFolder) {
  const path = encodePath(segments);
  if (!path) return `${base}/`;
  return `${base}/${path}${isFolder ? '/' : ''}`;
}

/** The key locks are stored under: case-insensitive, like node names. */
function lockKey(segments) {
  return segments.map((segment) => segment.toLowerCase()).join('/');
}

function visibleChild(parentId, name) {
  const node = findLiveChildByName(parentId, name);
  return node && !node.vault_id ? node : null;
}

/**
 * Walks `segments` from the account root. Returns { node, parent }: `node`
 * is the row at the path (with its favorite flag) or null, `parent` the
 * folder that holds or would hold it, or null when that folder is missing.
 */
function resolvePath(userId, segments) {
  let current = getRoot(userId);
  if (segments.length === 0) return { node: nodeById(current.id), parent: null };
  for (let index = 0; index < segments.length - 1; index += 1) {
    const next = visibleChild(current.id, segments[index]);
    if (!next || next.type !== 'folder') return { node: null, parent: null };
    current = next;
  }
  const node = visibleChild(current.id, segments[segments.length - 1]);
  return { node: node ? nodeById(node.id) : null, parent: current };
}

module.exports = {
  href,
  isDavPath,
  lockKey,
  parseDavPath,
  parseDestination,
  resolvePath,
};
