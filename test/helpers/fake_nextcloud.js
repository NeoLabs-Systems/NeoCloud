'use strict';

// A small stand-in for Nextcloud's WebDAV endpoint: PROPFIND (Depth 0/1,
// infinity refused like many Nextcloud setups) answering Nextcloud-shaped XML
// with d:/oc:/nc: namespaces and split propstats, and GET with content.
// Accounts, trees and per-file behaviour are set by the test.

const express = require('express');

const DAV_PREFIX = '/remote.php/dav/files/';

function httpDate(sqlTime) {
  return new Date(`${sqlTime.replace(' ', 'T')}Z`).toUTCString();
}

function escapeXml(text) {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function hrefFor(username, segments, folder) {
  const encoded = [username, ...segments].map(encodeURIComponent).join('/');
  return `${DAV_PREFIX}${encoded}${folder ? '/' : ''}`;
}

/**
 * `accounts`: { [username]: { password, files: { "a/b.txt": { content, mtime, favorite } },
 * folders: { "a": { mtime, favorite } } } }. Folders are implied by file paths too.
 * `behaviour[path]`: 'forbidden' (GET 403), 'flaky' (first GET 503 + Retry-After: 0),
 * or { hold: Promise } (GET sends half, waits for the promise, then the rest).
 */
function createFakeNextcloud(accounts) {
  const gets = new Map();
  const behaviour = {};
  const failedOnce = new Set();

  function entries(account) {
    const map = new Map([['', { type: 'folder', mtime: '2026-01-01 00:00:00', favorite: false }]]);
    for (const [path, folder] of Object.entries(account.folders || {})) {
      map.set(path, { type: 'folder', mtime: folder.mtime || '2026-01-01 00:00:00', favorite: Boolean(folder.favorite) });
    }
    for (const [path, file] of Object.entries(account.files)) {
      const parts = path.split('/');
      for (let depth = 1; depth < parts.length; depth += 1) {
        const folder = parts.slice(0, depth).join('/');
        if (!map.has(folder)) map.set(folder, { type: 'folder', mtime: '2026-01-01 00:00:00', favorite: false });
      }
      map.set(path, { type: 'file', content: Buffer.from(file.content), mtime: file.mtime, favorite: Boolean(file.favorite) });
    }
    return map;
  }

  function folderSize(map, path) {
    let size = 0;
    for (const [key, entry] of map) {
      if (entry.type === 'file' && (path === '' || key.startsWith(`${path}/`))) size += entry.content.length;
    }
    return size;
  }

  function responseXml(username, map, path) {
    const entry = map.get(path);
    const segments = path ? path.split('/') : [];
    const folder = entry.type === 'folder';
    const ok = folder
      ? `<d:resourcetype><d:collection/></d:resourcetype>
        <d:quota-used-bytes>${folderSize(map, path)}</d:quota-used-bytes>
        <oc:size>${folderSize(map, path)}</oc:size>`
      : `<d:resourcetype/>
        <d:getcontentlength>${entry.content.length}</d:getcontentlength>`;
    const missing = folder ? '<d:getcontentlength/>' : '<d:quota-used-bytes/><oc:size/>';
    return `<d:response>
    <d:href>${escapeXml(hrefFor(username, segments, folder))}</d:href>
    <d:propstat>
      <d:prop>
        ${ok}
        <d:getlastmodified>${httpDate(entry.mtime)}</d:getlastmodified>
        <oc:favorite>${entry.favorite ? 1 : 0}</oc:favorite>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
    <d:propstat>
      <d:prop>${missing}</d:prop>
      <d:status>HTTP/1.1 404 Not Found</d:status>
    </d:propstat>
  </d:response>`;
  }

  async function sendFile(req, res, key, entry) {
    gets.set(key, (gets.get(key) || 0) + 1);
    const mode = behaviour[key];
    if (mode === 'forbidden') return res.status(403).end();
    if (mode === 'flaky' && !failedOnce.has(key)) {
      failedOnce.add(key);
      return res.status(503).set('Retry-After', '0').end();
    }
    res.status(200).set('Content-Length', String(entry.content.length)).set('Content-Type', 'application/octet-stream');
    if (mode && mode.hold) {
      const half = Math.floor(entry.content.length / 2);
      res.write(entry.content.subarray(0, half));
      await mode.hold;
      if (!res.destroyed) res.end(entry.content.subarray(half));
      return undefined;
    }
    return res.end(entry.content);
  }

  const app = express();
  app.use(async (req, res) => {
    // An install that moved: everything below /old redirects to the real place.
    if (req.path.startsWith('/old/')) return res.redirect(301, req.originalUrl.slice('/old'.length));
    if (!req.path.startsWith(DAV_PREFIX)) return res.status(404).end();
    const [rawUser, ...rawSegments] = req.path.slice(DAV_PREFIX.length).split('/').filter(Boolean);
    const username = decodeURIComponent(rawUser || '');
    const account = accounts[username];
    const header = String(req.get('authorization') || '');
    const expected = `Basic ${Buffer.from(`${username}:${account?.password}`).toString('base64')}`;
    if (!account || header !== expected) {
      return res.status(401).set('WWW-Authenticate', 'Basic realm="Nextcloud"').end();
    }
    const map = entries(account);
    const key = rawSegments.map(decodeURIComponent).join('/');
    const entry = map.get(key);
    if (!entry) return res.status(404).end();

    if (req.method === 'GET') {
      if (entry.type === 'folder') return res.status(405).end();
      return sendFile(req, res, key, entry);
    }
    if (req.method !== 'PROPFIND') return res.status(405).end();
    const depth = String(req.get('depth') ?? 'infinity');
    if (depth === 'infinity') return res.status(403).end();
    const blocks = [responseXml(username, map, key)];
    if (depth === '1' && entry.type === 'folder') {
      for (const path of map.keys()) {
        const parent = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '';
        if (path !== '' && path !== key && parent === key) blocks.push(responseXml(username, map, path));
      }
    }
    return res.status(207).type('application/xml; charset=utf-8').send(`<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:s="http://sabredav.org/ns" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns">
  ${blocks.join('\n  ')}
</d:multistatus>`);
  });

  return new Promise((resolve) => {
    const server = app.listen(0, '127.0.0.1', () => {
      resolve({
        url: `http://127.0.0.1:${server.address().port}`,
        behaviour,
        gets,
        close: () => new Promise((done) => {
          server.closeAllConnections();
          server.close(done);
        }),
      });
    });
  });
}

module.exports = { createFakeNextcloud };
