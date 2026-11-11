'use strict';

// A WebDAV share: another Nextcloud, a NAS, Hetzner Storage Box, ...
// Spoken directly over fetch; only PUT, GET, DELETE, MKCOL, PROPFIND and MOVE.
// Redirects are not followed. Listings are bounded: each PROPFIND answer is
// read with a byte cap and parsed in linear time, the walk stops at a fixed
// depth and number of entries, and a name that is empty, "." or ".." (or
// holds a slash) is refused, so a key can never point above the base folder.

const { Readable } = require('stream');
const { httpError } = require('../../../utils/http_error');
const { isCollection, readMultistatus } = require('../../webdav/multistatus');

// Blob folders hold one entry per blob, so a listing can be large.
const MAX_LISTING_BYTES = 64 * 1024 * 1024;
const MAX_WALK_DEPTH = 16;
const MAX_WALK_ENTRIES = 2000000;
const PROPFIND_BODY = '<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>';

function decodedSegments(pathname) {
  return pathname.split('/').filter(Boolean).map((segment) => {
    try {
      return decodeURIComponent(segment);
    } catch {
      throw new Error('WebDAV listing has a malformed name.');
    }
  });
}

function assertKeySegment(name) {
  if (!name || name === '.' || name === '..' || /[/\\]/.test(name)) throw new Error('WebDAV listing has an unsafe name.');
}

function createWebdavAdapter({ url, username, password }) {
  let root;
  try {
    root = new URL(String(url || '').replace(/\/*$/, '/'));
  } catch {
    throw httpError(400, 'Enter the WebDAV folder URL.', 'BACKUP_CONFIG_INVALID');
  }
  if (!['http:', 'https:'].includes(root.protocol)) throw httpError(400, 'WebDAV needs an http(s) URL.', 'BACKUP_CONFIG_INVALID');
  const auth = username ? `Basic ${Buffer.from(`${username}:${password || ''}`).toString('base64')}` : null;
  const knownFolders = new Set();

  const urlFor = (key) => new URL(key.split('/').map(encodeURIComponent).join('/'), root);

  async function request(method, target, { headers = {}, body, ok = [200, 201, 204, 207] } = {}) {
    const response = await fetch(target, {
      method,
      headers: { ...(auth ? { Authorization: auth } : {}), ...headers },
      body,
      duplex: body ? 'half' : undefined,
      redirect: 'manual',
    });
    if (!ok.includes(response.status)) {
      await response.body?.cancel();
      throw new Error(`WebDAV ${method} answered ${response.status}.`);
    }
    return response;
  }

  async function ensureFolder(key) {
    const parts = key.split('/').filter(Boolean);
    let current = '';
    for (const part of parts) {
      current = current ? `${current}/${part}` : part;
      if (knownFolders.has(current)) continue;
      // 405: already exists.
      await request('MKCOL', urlFor(`${current}/`), { ok: [201, 405] });
      knownFolders.add(current);
    }
  }

  async function walk(folderKey, out, depth = 0) {
    if (depth > MAX_WALK_DEPTH) throw new Error('WebDAV listing is nested too deeply.');
    const folderUrl = urlFor(folderKey ? `${folderKey}/` : '');
    const response = await fetch(folderUrl, {
      method: 'PROPFIND',
      headers: { ...(auth ? { Authorization: auth } : {}), Depth: '1', 'Content-Type': 'application/xml; charset=utf-8' },
      body: PROPFIND_BODY,
      redirect: 'manual',
    });
    if (response.status === 404) {
      await response.body?.cancel();
      return;
    }
    if (response.status !== 207) {
      await response.body?.cancel();
      throw new Error(`WebDAV PROPFIND answered ${response.status}.`);
    }
    const self = decodedSegments(folderUrl.pathname);
    for (const { href, props } of await readMultistatus(response, { limit: MAX_LISTING_BYTES })) {
      if (!href) continue;
      const segments = decodedSegments(new URL(href, root).pathname);
      // Only direct children of this folder; the folder itself comes back too.
      if (segments.length !== self.length + 1 || self.some((segment, index) => segment !== segments[index])) continue;
      const name = segments[self.length];
      assertKeySegment(name);
      out.entries += 1;
      if (out.entries > MAX_WALK_ENTRIES) throw new Error('WebDAV listing has too many entries.');
      const key = folderKey ? `${folderKey}/${name}` : name;
      if (isCollection(props)) await walk(key, out, depth + 1);
      else if (!name.startsWith('.tmp-')) out.keys.push(key);
    }
  }

  return {
    async open() {
      // 405: the folder already exists.
      await request('MKCOL', root, { ok: [201, 405] });
      const response = await request('PROPFIND', root, { headers: { Depth: '0' } });
      await response.body?.cancel();
    },
    async close() {},
    async list(prefix) {
      const out = { keys: [], entries: 0 };
      await walk(prefix.replace(/\/$/, ''), out);
      return out.keys;
    },
    async put(key, openSource) {
      const folder = key.split('/').slice(0, -1).join('/');
      await ensureFolder(folder);
      const name = key.split('/').pop();
      const temporary = folder ? `${folder}/.tmp-${name}` : `.tmp-${name}`;
      await request('PUT', urlFor(temporary), { body: Readable.toWeb(openSource()) });
      await request('MOVE', urlFor(temporary), { headers: { Destination: urlFor(key).href, Overwrite: 'T' } });
    },
    async get(key) {
      const response = await request('GET', urlFor(key), { ok: [200] });
      return Readable.fromWeb(response.body);
    },
    async delete(key) {
      await request('DELETE', urlFor(key), { ok: [200, 204, 404] });
    },
    async deletePrefix(prefix) {
      await request('DELETE', urlFor(`${prefix.replace(/\/$/, '')}/`), { ok: [200, 204, 404] });
    },
  };
}

module.exports = { createWebdavAdapter };
