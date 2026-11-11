'use strict';

// LOCK and UNLOCK. Locking a path that doesn't exist yet creates an empty
// file there (RFC 4918 9.10.4), which is how Windows and Office start a save.

const { Readable } = require('stream');
const { httpError } = require('../../utils/http_error');
const { uploadStream } = require('../transfer/uploads');
const { createLock, parseTimeout, refreshLock, removeLock, submittedTokens } = require('./locks');
const { href, lockKey, resolvePath } = require('./paths');
const { activeLock } = require('./props');
const { readXmlBody, sendError, sendXml } = require('./respond');
const { DAV, child, document, element, isElement, parseXml } = require('./xml');

function lockBody(lock) {
  return document(DAV, 'prop', element(DAV, 'lockdiscovery', activeLock(lock)));
}

function textOf(node) {
  if (!node) return '';
  return (node.text + node.children.map(textOf).join('')).trim();
}

function parseLockInfo(body) {
  const root = parseXml(body);
  if (!isElement(root, DAV, 'lockinfo')) throw httpError(400, 'Expected a DAV:lockinfo body.', 'XML_INVALID');
  const scope = child(root, DAV, 'lockscope');
  const shared = Boolean(child(scope, DAV, 'shared'));
  if (!shared && !child(scope, DAV, 'exclusive')) throw httpError(400, 'lockscope must be exclusive or shared.', 'XML_INVALID');
  return { scope: shared ? 'shared' : 'exclusive', owner: textOf(child(root, DAV, 'owner')).slice(0, 512) };
}

async function lock(ctx) {
  const { req, res, userId, segments } = ctx;
  const body = await readXmlBody(req);
  const timeout = parseTimeout(req.get('timeout'));
  const key = lockKey(segments);

  if (!body.trim()) {
    const refreshed = refreshLock(userId, key, submittedTokens(req.get('if')), timeout);
    if (!refreshed) throw httpError(412, 'No matching lock to refresh.', 'LOCK_NOT_FOUND');
    sendXml(res, 200, lockBody(refreshed));
    return;
  }

  const info = parseLockInfo(body);
  const { node, parent } = resolvePath(userId, segments);
  if (!node && !parent) throw httpError(409, 'The parent folder does not exist.', 'PARENT_MISSING');
  const depth = String(req.get('depth') || 'infinity').trim().toLowerCase() === '0' ? '0' : 'infinity';
  const created = createLock(userId, {
    key,
    href: href(ctx.base, segments, node?.type === 'folder'),
    depth: node?.type === 'folder' ? depth : '0',
    scope: info.scope,
    owner: info.owner,
    timeout,
  });
  if (!node) {
    try {
      await uploadStream(userId, { parentId: parent.id, name: segments[segments.length - 1], conflict: 'fail' }, Readable.from([]));
    } catch (err) {
      removeLock(userId, key, created.token);
      throw err;
    }
  }
  res.set('Lock-Token', `<${created.token}>`);
  sendXml(res, node ? 200 : 201, lockBody(created));
}

function unlock(ctx) {
  const { req, res, userId, segments } = ctx;
  const token = String(req.get('lock-token') || '').trim().replace(/^<(.*)>$/, '$1');
  if (!removeLock(userId, lockKey(segments), token)) {
    sendError(res, 409, 'No such lock on this resource.', 'lock-token-matches-request-uri');
    return;
  }
  res.status(204).end();
}

module.exports = { lock, unlock };
