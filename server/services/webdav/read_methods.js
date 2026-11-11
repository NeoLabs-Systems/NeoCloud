'use strict';

// WebDAV methods that only read: OPTIONS, PROPFIND, GET and HEAD.

const { httpError } = require('../../utils/http_error');
const { listChildren, serializeNode } = require('../files/tree');
const { sendFile } = require('../transfer/downloads');
const { href, lockKey, resolvePath } = require('./paths');
const { multistatus, parsePropfind, propContext, propfindResponse } = require('./props');
const { readXmlBody, sendError, sendXml } = require('./respond');

const ALLOWED_METHODS = 'OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, COPY, MOVE, PROPFIND, PROPPATCH, LOCK, UNLOCK';
const LIST_PAGE_SIZE = 2000;

function sendOptions(res) {
  res.set({
    Allow: ALLOWED_METHODS,
    DAV: '1, 2',
    'MS-Author-Via': 'DAV',
    'Accept-Ranges': 'bytes',
    'Content-Length': '0',
  });
  res.status(200).end();
}

function requireNode(ctx) {
  const { node } = resolvePath(ctx.userId, ctx.segments);
  if (!node) throw httpError(404, 'File or folder not found', 'NOT_FOUND');
  return node;
}

// Every visible child of a folder, page by page.
function visibleChildren(userId, folderId) {
  const items = [];
  let cursor = null;
  do {
    const page = listChildren(userId, folderId, { limit: LIST_PAGE_SIZE, cursor });
    items.push(...page.items.filter((item) => !item.vaultId));
    cursor = page.nextCursor;
  } while (cursor);
  return items;
}

/** Depth 0 or 1. A missing Depth header is read as 1; infinity is refused. */
function propfindDepth(req) {
  const depth = String(req.get('depth') ?? '1').trim().toLowerCase();
  if (depth === 'infinity') return null;
  if (depth !== '0' && depth !== '1') throw httpError(400, 'Depth must be 0, 1 or infinity.', 'DEPTH_INVALID');
  return Number(depth);
}

async function propfind(ctx) {
  const { req, res } = ctx;
  const depth = propfindDepth(req);
  if (depth === null) {
    // RFC 4918 9.1: say why, so clients fall back to Depth 1.
    sendError(res, 403, 'Depth infinity is not supported.', 'propfind-finite-depth');
    return;
  }
  const request = parsePropfind(await readXmlBody(req));
  const row = requireNode(ctx);
  const node = serializeNode(row);
  const props = propContext(ctx.userId, ctx.username);
  const isFolder = node.type === 'folder';
  const responses = [propfindResponse({
    node,
    href: href(ctx.base, ctx.segments, isFolder),
    key: lockKey(ctx.segments),
  }, request, props)];
  if (depth === 1 && isFolder) {
    for (const item of visibleChildren(ctx.userId, node.id)) {
      const segments = [...ctx.segments, item.name];
      responses.push(propfindResponse({
        node: item,
        href: href(ctx.base, segments, item.type === 'folder'),
        key: lockKey(segments),
      }, request, props));
    }
  }
  sendXml(res, 207, multistatus(responses));
}

async function getOrHead(ctx) {
  const { req, res } = ctx;
  const node = requireNode(ctx);
  if (node.type === 'folder') {
    // Browsers that open the URL get a hint instead of an error.
    const text = 'This is a NeoCloud WebDAV folder. Connect a WebDAV client to browse it.\n';
    res.status(200).set({ 'Content-Type': 'text/plain; charset=utf-8', 'Content-Length': String(Buffer.byteLength(text)) });
    if (req.method === 'HEAD') res.end();
    else res.end(text);
    return;
  }
  res.set('OC-FileId', node.id);
  await sendFile(req, res, ctx.userId, node.id);
}

module.exports = { ALLOWED_METHODS, getOrHead, propfind, sendOptions };
