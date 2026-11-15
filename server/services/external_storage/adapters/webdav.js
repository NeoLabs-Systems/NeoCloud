'use strict';

// A WebDAV share (another Nextcloud, a NAS, Hetzner Storage Box, ...) spoken
// directly over fetch: PROPFIND, GET (with Range), PUT, MKCOL, MOVE, DELETE.
// Redirects are never followed -- the password would travel with them -- the
// owner is told where the server wants to go instead. Connections go through
// the guarded dispatcher (see network.js); listings are read with a byte cap
// and a time limit.

const { Readable } = require('stream');
const { fetch } = require('undici');
const { httpError } = require('../../../utils/http_error');
const { isCollection, propText, readMultistatus } = require('../../webdav/multistatus');
const { assertHttpUrl } = require('../config_checks');
const { dispatcherFor } = require('../network');
const { isSafeListedName } = require('../paths');
const { sliceStream } = require('./slice');

const PROPFIND_BODY = '<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>';
const MAX_LISTING_BYTES = 16 * 1024 * 1024;
const LISTING_TIMEOUT_MS = 60 * 1000;

function remoteError(status, message, code) {
  return httpError(status, message, code);
}

function errorForStatus(status) {
  if (status === 401) return remoteError(502, 'The server refused the user name or password.', 'EXTERNAL_AUTH_FAILED');
  if (status === 403) return remoteError(502, 'The server denied access.', 'EXTERNAL_FORBIDDEN');
  if (status === 404) return remoteError(404, 'Not found at the provider.', 'EXTERNAL_NOT_FOUND');
  if (status === 409) return remoteError(409, 'The folder does not exist at the provider.', 'EXTERNAL_CONFLICT');
  if (status === 412) return remoteError(409, 'An item with that name already exists.', 'NAME_EXISTS');
  if (status === 405) return remoteError(409, 'An item with that name already exists.', 'NAME_EXISTS');
  if (status === 423) return remoteError(409, 'The item is locked at the provider.', 'EXTERNAL_LOCKED');
  if (status === 507) return remoteError(507, 'The provider is out of space.', 'EXTERNAL_FULL');
  return remoteError(502, `The server answered ${status}.`, 'EXTERNAL_ERROR');
}

function segments(pathname) {
  return pathname.split('/').filter(Boolean).map((part) => {
    try {
      return decodeURIComponent(part);
    } catch {
      return part;
    }
  });
}

function toIso(httpDate) {
  const time = Date.parse(httpDate || '');
  return Number.isFinite(time) ? new Date(time).toISOString() : null;
}

function count(value) {
  const number = Number(value);
  return Number.isSafeInteger(number) && number >= 0 ? number : 0;
}

function create(config, { userId }) {
  const root = new URL(config.url.endsWith('/') ? config.url : `${config.url}/`);
  const authorization = `Basic ${Buffer.from(`${config.username}:${config.password || ''}`, 'utf8').toString('base64')}`;
  const dispatcher = dispatcherFor(userId);

  function urlFor(path, { folder = false } = {}) {
    const encoded = path.split('/').filter(Boolean).map(encodeURIComponent).join('/');
    const url = new URL(encoded + (folder && encoded ? '/' : ''), root);
    if (url.origin !== root.origin || !url.pathname.startsWith(root.pathname)) throw httpError(400, 'That path is not valid.', 'INVALID_PATH');
    return url;
  }

  async function request(method, url, { expect, headers = {}, body, signal } = {}) {
    let response;
    try {
      response = await fetch(url, {
        method, headers: { Authorization: authorization, ...headers }, body, duplex: body ? 'half' : undefined, redirect: 'manual', signal, dispatcher,
      });
    } catch (err) {
      const blocked = err.cause?.code === 'EXTERNAL_HOST_BLOCKED' ? err.cause : null;
      throw blocked || remoteError(502, 'Could not reach the server.', 'EXTERNAL_UNREACHABLE');
    }
    if (expect.includes(response.status)) return response;
    await response.body?.cancel();
    if (response.status >= 300 && response.status < 400) {
      const target = response.headers.get('location');
      const hint = target ? ` It points to ${target}.` : '';
      throw remoteError(502, `The server redirected the request. Enter the address it redirects to.${hint}`, 'EXTERNAL_REDIRECT');
    }
    throw errorForStatus(response.status);
  }

  async function propfind(path, depth) {
    const timeout = AbortSignal.timeout(LISTING_TIMEOUT_MS);
    const url = urlFor(path, { folder: true });
    const response = await request('PROPFIND', url, {
      expect: [207], headers: { Depth: String(depth), 'Content-Type': 'application/xml; charset=utf-8' }, body: PROPFIND_BODY, signal: timeout,
    });
    let responses;
    try {
      responses = await readMultistatus(response, { limit: MAX_LISTING_BYTES });
    } catch (err) {
      if (err.code === 'MULTISTATUS_TOO_LARGE') throw remoteError(502, 'The folder listing is too large.', 'EXTERNAL_TOO_LARGE');
      throw remoteError(502, 'This does not look like a WebDAV server.', 'EXTERNAL_NOT_WEBDAV');
    }
    return { url, responses };
  }

  return {
    async open() {
      await propfind('', 0);
    },
    async close() {
      await dispatcher.close().catch(() => {});
    },
    async list(path, { limit }) {
      const { url, responses } = await propfind(path, 1);
      const base = segments(url.pathname);
      const entries = [];
      for (const { href, props } of responses) {
        if (!href) continue;
        const parts = segments(new URL(href, url).pathname);
        if (parts.length !== base.length + 1 || base.some((part, index) => part.normalize('NFC') !== parts[index].normalize('NFC'))) continue;
        const name = parts[base.length];
        if (!isSafeListedName(name)) continue;
        const folder = isCollection(props);
        entries.push({ name, type: folder ? 'folder' : 'file', size: folder ? 0 : count(propText(props, 'getcontentlength')), mtime: toIso(propText(props, 'getlastmodified')) });
      }
      return { entries: entries.slice(0, limit), truncated: entries.length > limit };
    },
    async stat(path) {
      try {
        const { responses } = await propfind(path, 0);
        const own = responses[0];
        if (!own) return null;
        const folder = isCollection(own.props);
        return { type: folder ? 'folder' : 'file', size: folder ? 0 : count(propText(own.props, 'getcontentlength')), mtime: toIso(propText(own.props, 'getlastmodified')) };
      } catch (err) {
        if (err.code === 'EXTERNAL_NOT_FOUND') return null;
        throw err;
      }
    },
    async read(path, range) {
      const response = await request('GET', urlFor(path), {
        expect: range ? [200, 206] : [200], headers: range ? { Range: `bytes=${range.start}-${range.end}` } : {},
      });
      const stream = Readable.fromWeb(response.body);
      return range && response.status === 200 ? stream.pipe(sliceStream(range)) : stream;
    },
    async write(path, stream, { size }) {
      await request('PUT', urlFor(path), {
        expect: [200, 201, 204],
        headers: size === null || size === undefined ? {} : { 'Content-Length': String(size) },
        body: Readable.toWeb(stream),
      });
    },
    async mkdir(path) {
      await request('MKCOL', urlFor(path, { folder: true }), { expect: [201] });
    },
    async move(from, to) {
      await request('MOVE', urlFor(from), { expect: [201, 204], headers: { Destination: urlFor(to).href, Overwrite: 'F' } });
    },
    async remove(path) {
      await request('DELETE', urlFor(path), { expect: [200, 204] });
    },
  };
}

module.exports = {
  type: 'webdav',
  fields: [
    { name: 'url', required: true, max: 1024 },
    { name: 'username', required: true, max: 256 },
    { name: 'password', required: true, secret: true, max: 1024 },
  ],
  validate(config) {
    assertHttpUrl(config.url, 'The address');
  },
  hostOf: (config) => new URL(config.url).hostname,
  create,
};
