'use strict';

// An in-memory WebDAV server for the external storage tests: PROPFIND
// (Depth 0/1), GET with Range, PUT, MKCOL, MOVE, DELETE, with Basic auth.
// `redirectTo` makes every request answer 302 there, to check that
// redirects are never followed. Every request is recorded in `requests`.

const http = require('http');

function createFakeWebdav({ username = 'dav', password = 'dav-pass', base = '/dav' } = {}) {
  const files = new Map(); // "a/b.txt" -> Buffer
  const folders = new Set(['']);
  const requests = [];
  let redirectTo = null;

  const keyOf = (pathname) => decodeURIComponent(pathname.slice(base.length)).split('/').filter(Boolean).join('/');
  const hrefOf = (key, folder) => `${base}/${key.split('/').filter(Boolean).map(encodeURIComponent).join('/')}${folder && key ? '/' : ''}`;
  const parentOf = (key) => key.split('/').slice(0, -1).join('/');

  function entry(key, folder) {
    const resource = folder ? '<d:resourcetype><d:collection/></d:resourcetype>' : '<d:resourcetype/>';
    const length = folder ? '' : `<d:getcontentlength>${files.get(key).length}</d:getcontentlength>`;
    return `<d:response><d:href>${hrefOf(key, folder)}</d:href><d:propstat><d:prop>${resource}${length}<d:getlastmodified>Fri, 02 Jan 2026 03:04:05 GMT</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`;
  }

  function handle(req, res, body) {
    requests.push({ method: req.method, url: req.url, authorization: req.headers.authorization || null, contentLength: req.headers['content-length'] || null });
    if (redirectTo) {
      res.writeHead(302, { location: redirectTo });
      return res.end();
    }
    const expected = `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`;
    if (req.headers.authorization !== expected) {
      res.writeHead(401, { 'www-authenticate': 'Basic realm="dav"' });
      return res.end();
    }
    const url = new URL(req.url, 'http://fake');
    if (!url.pathname.startsWith(base)) {
      res.writeHead(404);
      return res.end();
    }
    const key = keyOf(url.pathname);
    const isFolder = folders.has(key);
    const isFile = files.has(key);
    if (req.method === 'PROPFIND') {
      if (!isFolder && !isFile) {
        res.writeHead(404);
        return res.end();
      }
      let xml = entry(key, isFolder);
      if (isFolder && req.headers.depth !== '0') {
        for (const folder of folders) if (folder && parentOf(folder) === key) xml += entry(folder, true);
        for (const file of files.keys()) if (parentOf(file) === key) xml += entry(file, false);
      }
      res.writeHead(207, { 'content-type': 'application/xml' });
      return res.end(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">${xml}</d:multistatus>`);
    }
    if (req.method === 'GET') {
      if (!isFile) {
        res.writeHead(404);
        return res.end();
      }
      const content = files.get(key);
      const asked = /^bytes=(\d+)-(\d+)$/.exec(req.headers.range || '');
      if (asked) {
        const part = content.subarray(Number(asked[1]), Number(asked[2]) + 1);
        res.writeHead(206, { 'content-length': part.length, 'content-range': `bytes ${asked[1]}-${asked[2]}/${content.length}` });
        return res.end(part);
      }
      res.writeHead(200, { 'content-length': content.length });
      return res.end(content);
    }
    if (req.method === 'PUT') {
      if (!folders.has(parentOf(key))) {
        res.writeHead(409);
        return res.end();
      }
      files.set(key, body);
      res.writeHead(201);
      return res.end();
    }
    if (req.method === 'MKCOL') {
      if (isFolder || isFile) {
        res.writeHead(405);
        return res.end();
      }
      folders.add(key);
      res.writeHead(201);
      return res.end();
    }
    if (req.method === 'MOVE') {
      const target = keyOf(new URL(req.headers.destination).pathname);
      if (files.has(target) || folders.has(target)) {
        res.writeHead(412);
        return res.end();
      }
      if (isFile) {
        files.set(target, files.get(key));
        files.delete(key);
      } else if (isFolder) {
        for (const folder of [...folders]) if (folder === key || folder.startsWith(`${key}/`)) { folders.delete(folder); folders.add(target + folder.slice(key.length)); }
        for (const file of [...files.keys()]) if (file.startsWith(`${key}/`)) { files.set(target + file.slice(key.length), files.get(file)); files.delete(file); }
      } else {
        res.writeHead(404);
        return res.end();
      }
      res.writeHead(201);
      return res.end();
    }
    if (req.method === 'DELETE') {
      if (!isFile && !isFolder) {
        res.writeHead(404);
        return res.end();
      }
      files.delete(key);
      for (const folder of [...folders]) if (folder === key || folder.startsWith(`${key}/`)) folders.delete(folder);
      for (const file of [...files.keys()]) if (file.startsWith(`${key}/`)) files.delete(file);
      res.writeHead(204);
      return res.end();
    }
    res.writeHead(405);
    return res.end();
  }

  const server = http.createServer((req, res) => {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => handle(req, res, Buffer.concat(chunks)));
  });

  return {
    files,
    folders,
    requests,
    username,
    password,
    redirectTo(url) {
      redirectTo = url;
    },
    async start() {
      await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
      return `http://127.0.0.1:${server.address().port}${base}/`;
    },
    async stop() {
      server.closeAllConnections?.();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}

module.exports = { createFakeWebdav };
