'use strict';

// A read-only WebDAV client for imports: lists one folder at a time
// (PROPFIND Depth 1 -- Nextcloud often refuses Depth infinity) and streams
// files down. Understands Nextcloud's oc:/nc: properties, follows redirects
// itself (keeping credentials on the same origin only) and retries 401 once
// and 429/5xx/network failures with backoff. Listings are read with a byte
// cap and a time limit and parsed in linear time; `dispatcher` (see
// network_guard.js) decides where connections may go.

const { Readable } = require('stream');
const { fetch } = require('undici');
const { httpError } = require('../../utils/http_error');
const { isCollection, propText, readMultistatus } = require('../webdav/multistatus');

const PROPFIND_BODY = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns">
  <d:prop>
    <d:resourcetype/>
    <d:getcontentlength/>
    <d:getlastmodified/>
    <d:quota-used-bytes/>
    <oc:size/>
    <oc:favorite/>
  </d:prop>
</d:propfind>`;

const MAX_REDIRECTS = 5;
const REDIRECT_STATUSES = new Set([301, 302, 303, 307, 308]);
const RETRY_STATUSES = new Set([429, 500, 502, 503, 504]);
const MAX_RETRY_AFTER_MS = 60 * 1000;
// One folder listing: at most this much XML, read within this time.
const MAX_LISTING_BYTES = 16 * 1024 * 1024;
const LISTING_TIMEOUT_MS = 2 * 60 * 1000;

const DEFAULT_RETRIES = 4;
const DEFAULT_BASE_DELAY_MS = 500;

function remoteError(message, code, status = null) {
  const error = httpError(502, message, code);
  error.remoteStatus = status;
  return error;
}

function errorForStatus(status) {
  if (status === 401) return remoteError('The server refused the user name or app password.', 'REMOTE_AUTH_FAILED', status);
  if (status === 403) return remoteError('The server denied access.', 'REMOTE_FORBIDDEN', status);
  if (status === 404) return remoteError('Not found on the server.', 'REMOTE_NOT_FOUND', status);
  if (status === 429) return remoteError('The server is limiting requests; try again later.', 'REMOTE_RATE_LIMITED', status);
  return remoteError(`The server answered ${status}.`, 'REMOTE_ERROR', status);
}

function toSqlTime(httpDate) {
  const time = Date.parse(httpDate || '');
  if (!Number.isFinite(time)) return null;
  return new Date(time).toISOString().slice(0, 19).replace('T', ' ');
}

function toCount(value) {
  const number = Number(value);
  return Number.isSafeInteger(number) && number >= 0 ? number : null;
}

function safeDecode(segment) {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

// Names stay as the server spells them (requests must match byte for byte);
// comparisons go through sameSegments, which ignores NFC/NFD differences.
function decodedSegments(pathname) {
  return pathname.split('/').filter(Boolean).map(safeDecode);
}

function sameSegments(prefix, segments) {
  return segments.length >= prefix.length
    && prefix.every((segment, index) => segment.normalize('NFC') === segments[index].normalize('NFC'));
}

function encodePath(segments) {
  return segments.map(encodeURIComponent).join('/');
}

/** Multistatus responses as entries keyed by decoded path segments. */
function toEntries(responses, baseUrl) {
  return responses.filter((response) => response.href).map(({ href, props }) => {
    const folder = isCollection(props);
    const contentLength = toCount(propText(props, 'getcontentlength'));
    const folderSize = toCount(propText(props, 'size')) ?? toCount(propText(props, 'quota-used-bytes'));
    return {
      segments: decodedSegments(new URL(href, baseUrl).pathname),
      type: folder ? 'folder' : 'file',
      size: folder ? folderSize ?? 0 : contentLength ?? 0,
      mtime: toSqlTime(propText(props, 'getlastmodified')),
      favorite: propText(props, 'favorite') === '1',
    };
  });
}

function sleep(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(signal.reason);
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    function onAbort() {
      clearTimeout(timer);
      reject(signal.reason);
    }
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

function retryDelay(response, attempt, baseDelayMs) {
  const header = response?.headers.get('retry-after');
  if (header) {
    const seconds = Number(header);
    const ms = Number.isFinite(seconds) ? seconds * 1000 : Date.parse(header) - Date.now();
    if (Number.isFinite(ms)) return Math.min(Math.max(ms, 0), MAX_RETRY_AFTER_MS);
  }
  return baseDelayMs * 2 ** attempt + Math.floor(Math.random() * baseDelayMs);
}

/**
 * `davRoot` is the server's WebDAV folder of the account (ends in /);
 * `fallbackRoot`, when set, is tried once if davRoot answers 404 (Nextcloud
 * accounts whose user id differs from the login name). Paths given to
 * list() and download() are relative to `sourcePath` below the root.
 */
function createWebdavClient({
  davRoot,
  fallbackRoot = null,
  sourcePath = '/',
  username,
  password,
  retries = DEFAULT_RETRIES,
  baseDelayMs = DEFAULT_BASE_DELAY_MS,
  hostGuard = null,
  dispatcher = undefined,
}) {
  let root = new URL(davRoot);
  const sourceSegments = decodedSegments(sourcePath);
  const authorization = `Basic ${Buffer.from(`${username}:${password}`, 'utf8').toString('base64')}`;

  function urlFor(relativePath, { folder }) {
    const segments = [...sourceSegments, ...String(relativePath || '').split('/').filter(Boolean)];
    const suffix = segments.length > 0 && folder ? '/' : '';
    return new URL(encodePath(segments) + suffix, root);
  }

  // One request with redirects followed by hand, so the Authorization header
  // goes to the same origin (scheme, host and port) only and the final URL is
  // known. The guard runs on every hop; the dispatcher checks again the
  // address it actually connects to.
  async function send(method, startUrl, { headers = {}, body, signal }) {
    let url = new URL(startUrl);
    for (let hop = 0; hop <= MAX_REDIRECTS; hop += 1) {
      const sameOrigin = url.origin === root.origin;
      if (hostGuard) await hostGuard(url);
      const response = await fetch(url, {
        method,
        headers: { ...headers, ...(sameOrigin ? { Authorization: authorization } : {}) },
        body,
        redirect: 'manual',
        signal,
        dispatcher,
      });
      if (!REDIRECT_STATUSES.has(response.status)) return { response, url };
      await response.body?.cancel();
      const location = response.headers.get('location');
      if (!location) throw errorForStatus(response.status);
      url = new URL(location, url);
      if (!['http:', 'https:'].includes(url.protocol)) throw remoteError('The server redirected somewhere unsupported.', 'REMOTE_ERROR');
    }
    throw remoteError('The server redirected too many times.', 'REMOTE_TOO_MANY_REDIRECTS');
  }

  async function request(method, url, { expect, headers, body, signal } = {}) {
    let authRetried = false;
    for (let attempt = 0; ; attempt += 1) {
      let result;
      try {
        result = await send(method, url, { headers, body, signal });
      } catch (err) {
        // Our own errors, a blocked address and a cancelled job end here;
        // network failures retry.
        if (err.cause?.code === 'IMPORT_HOST_BLOCKED') throw err.cause;
        const ours = err.remoteStatus !== undefined || err.code === 'IMPORT_HOST_BLOCKED';
        if (signal?.aborted || ours) throw err;
        if (attempt >= retries) {
          throw remoteError(`Could not reach the server (${err.cause?.code || err.message}).`, 'REMOTE_UNREACHABLE');
        }
        await sleep(retryDelay(null, attempt, baseDelayMs), signal);
        continue;
      }
      const { response } = result;
      if (expect.includes(response.status)) return result;
      await response.body?.cancel();
      const retryAuth = response.status === 401 && !authRetried;
      if ((RETRY_STATUSES.has(response.status) && attempt < retries) || retryAuth) {
        if (response.status === 401) authRetried = true;
        await sleep(retryDelay(response, attempt, baseDelayMs), signal);
        continue;
      }
      throw errorForStatus(response.status);
    }
  }

  async function propfind(url, depth, signal) {
    const timeout = AbortSignal.timeout(LISTING_TIMEOUT_MS);
    try {
      const { response, url: finalUrl } = await request('PROPFIND', url, {
        expect: [207],
        headers: { Depth: String(depth), 'Content-Type': 'application/xml; charset=utf-8' },
        body: PROPFIND_BODY,
        signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
      });
      const responses = await readMultistatus(response, { limit: MAX_LISTING_BYTES });
      return { entries: toEntries(responses, finalUrl), finalUrl };
    } catch (err) {
      if (signal?.aborted) throw signal.reason;
      if (err.code === 'MULTISTATUS_INVALID') throw remoteError('This does not look like a WebDAV server.', 'REMOTE_NOT_WEBDAV');
      if (err.code === 'MULTISTATUS_TOO_LARGE') throw remoteError('The server sent a folder listing that is too large.', 'REMOTE_ERROR');
      if (timeout.aborted) throw remoteError('The server took too long to answer.', 'REMOTE_UNREACHABLE');
      throw err;
    }
  }

  return {
    /**
     * Checks the account root, moving to where redirects lead (http→https,
     * a sub-path) and to `fallbackRoot` if the root does not exist.
     */
    async connect(signal) {
      let finalUrl;
      try {
        ({ finalUrl } = await propfind(root, 0, signal));
      } catch (err) {
        if (err.code !== 'REMOTE_NOT_FOUND' || !fallbackRoot) throw err;
        root = new URL(fallbackRoot);
        ({ finalUrl } = await propfind(root, 0, signal));
      }
      root = new URL(finalUrl.href.endsWith('/') ? finalUrl.href : `${finalUrl.href}/`);
    },

    /** The folder itself and its direct children, paths relative to sourcePath. */
    async list(relativePath, signal) {
      const url = urlFor(relativePath, { folder: true });
      const { entries } = await propfind(url, 1, signal);
      const base = decodedSegments(url.pathname);
      let self = null;
      const children = [];
      for (const entry of entries) {
        if (!sameSegments(base, entry.segments)) continue;
        const rest = entry.segments.slice(base.length);
        if (rest.length === 0) self = entry;
        else if (rest.length === 1) children.push({ ...entry, name: rest[0] });
      }
      if (!self) self = { type: 'folder', size: children.reduce((sum, child) => sum + child.size, 0) };
      return { self, children };
    },

    /** The file's bytes as a Node stream, and its length when the server says. */
    async download(relativePath, signal) {
      const { response } = await request('GET', urlFor(relativePath, { folder: false }), { expect: [200], signal });
      const length = toCount(response.headers.get('content-length'));
      return { stream: Readable.fromWeb(response.body), size: length };
    },
  };
}

module.exports = { createWebdavClient };
