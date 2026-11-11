'use strict';

// WebDAV properties: which ones a PROPFIND asks for, their values for a
// node, and the multistatus that carries them back.

const db = require('../../db/database');
const { getUsage, remainingBytes } = require('../storage/quota');
const { freeDiskBytes, minFreeDiskBytes } = require('../../utils/disk');
const { httpError } = require('../../utils/http_error');
const { locksOn } = require('./locks');
const { DAV, NC, OC, child, document, element, escapeXml, isElement, parseXml } = require('./xml');

const STATUS_TEXT = {
  200: 'OK',
  403: 'Forbidden',
  404: 'Not Found',
  409: 'Conflict',
  424: 'Failed Dependency',
};

function toDate(sqliteTime) {
  return new Date(`${String(sqliteTime).replace(' ', 'T')}Z`);
}

/**
 * A folder's etag changes whenever something directly inside it does
 * (its modified time moves), so clients notice new and removed children.
 */
function etagFor(node) {
  if (node.type === 'file') return node.etag;
  return `${node.etag}-${toDate(node.modifiedAt).getTime() / 1000}`;
}

function folderSize(userId, folderId) {
  return db.prepare(`
    WITH RECURSIVE down(id) AS (
      SELECT id FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL SELECT n.id FROM nodes n JOIN down ON n.parent_id = down.id WHERE n.trashed_at IS NULL
    )
    SELECT COALESCE(SUM(nodes.size), 0) AS bytes FROM down JOIN nodes ON nodes.id = down.id
    WHERE nodes.type = 'file' AND nodes.vault_id IS NULL
  `).get(folderId, userId).bytes;
}

// What the account may still store: its quota, and never more than the
// disk can take before uploads are refused. null when neither is known.
function availableBytes(userId) {
  const free = freeDiskBytes();
  const disk = free === null ? Number.POSITIVE_INFINITY : Math.max(0, free - minFreeDiskBytes());
  const available = Math.min(remainingBytes(userId), disk);
  return Number.isFinite(available) ? available : null;
}

function supportedLock() {
  return ['exclusive', 'shared'].map((scope) => element(DAV, 'lockentry', [
    element(DAV, 'lockscope', element(DAV, scope)),
    element(DAV, 'locktype', element(DAV, 'write')),
  ].join(''))).join('');
}

function activeLock(lock) {
  return element(DAV, 'activelock', [
    element(DAV, 'locktype', element(DAV, 'write')),
    element(DAV, 'lockscope', element(DAV, lock.scope)),
    element(DAV, 'depth', lock.depth === 'infinity' ? 'infinity' : '0'),
    lock.owner ? element(DAV, 'owner', element(DAV, 'href', escapeXml(lock.owner))) : '',
    element(DAV, 'timeout', `Second-${lock.timeout}`),
    element(DAV, 'locktoken', element(DAV, 'href', escapeXml(lock.token))),
    element(DAV, 'lockroot', element(DAV, 'href', escapeXml(lock.href))),
  ].join(''));
}

function lockDiscovery(locks) {
  return locks.map(activeLock).join('');
}

const isFile = (item) => item.node.type === 'file';
const isFolder = (item) => item.node.type === 'folder';

// `${ns} ${name}` -> value as XML text, or null when the node has no such
// property (reported as 404 in the multistatus).
const PROPERTIES = {
  [`${DAV} displayname`]: (item, ctx) => escapeXml(item.node.parentId ? item.node.name : ctx.username),
  [`${DAV} getcontentlength`]: (item) => (isFile(item) ? String(item.node.size) : null),
  [`${DAV} getcontenttype`]: (item) => (isFile(item) ? escapeXml(item.node.mime || 'application/octet-stream') : null),
  [`${DAV} getetag`]: (item) => escapeXml(`"${etagFor(item.node)}"`),
  [`${DAV} getlastmodified`]: (item) => toDate(item.node.modifiedAt).toUTCString(),
  [`${DAV} creationdate`]: (item) => toDate(item.node.createdAt).toISOString().replace(/\.\d{3}Z$/, 'Z'),
  [`${DAV} resourcetype`]: (item) => (isFolder(item) ? element(DAV, 'collection') : ''),
  [`${DAV} supportedlock`]: () => supportedLock(),
  [`${DAV} lockdiscovery`]: (item, ctx) => lockDiscovery(locksOn(ctx.userId, item.key)),
  [`${DAV} quota-used-bytes`]: (item, ctx) => (isFolder(item) ? String(ctx.usage().usedBytes) : null),
  [`${DAV} quota-available-bytes`]: (item, ctx) => {
    if (!isFolder(item)) return null;
    const available = ctx.available();
    return available === null ? null : String(available);
  },
  [`${OC} fileid`]: (item) => escapeXml(item.node.id),
  [`${OC} favorite`]: (item) => (item.node.favorite ? '1' : '0'),
  [`${OC} size`]: (item, ctx) => String(isFile(item) ? item.node.size : folderSize(ctx.userId, item.node.id)),
  [`${NC} has-preview`]: () => 'false',
};

// allprop: the DAV: properties of RFC 4918 (quota and the oc:/nc: ones only
// when asked for by name, as Nextcloud clients do).
const ALLPROP = [
  'displayname', 'getcontentlength', 'getcontenttype', 'getetag', 'getlastmodified',
  'creationdate', 'resourcetype', 'supportedlock', 'lockdiscovery',
].map((name) => ({ ns: DAV, name }));

const ALL_NAMES = Object.keys(PROPERTIES).map((key) => {
  const split = key.lastIndexOf(' ');
  return { ns: key.slice(0, split), name: key.slice(split + 1) };
});

/** What a PROPFIND body asks for: { mode: allprop|propname|prop, props }. */
function parsePropfind(body) {
  if (!body.trim()) return { mode: 'allprop', props: ALLPROP };
  const root = parseXml(body);
  if (!isElement(root, DAV, 'propfind')) throw httpError(400, 'Expected a DAV:propfind body.', 'XML_INVALID');
  if (child(root, DAV, 'propname')) return { mode: 'propname', props: ALL_NAMES };
  const prop = child(root, DAV, 'prop');
  if (prop) return { mode: 'prop', props: prop.children.map(({ ns, name }) => ({ ns, name })) };
  const include = child(root, DAV, 'include');
  const extra = include ? include.children.map(({ ns, name }) => ({ ns, name })) : [];
  return { mode: 'allprop', props: [...ALLPROP, ...extra] };
}

function statusLine(status) {
  return `HTTP/1.1 ${status} ${STATUS_TEXT[status]}`;
}

function propstat(status, content) {
  return element(DAV, 'propstat', element(DAV, 'prop', content) + element(DAV, 'status', statusLine(status)));
}

function response(itemHref, propstats) {
  return element(DAV, 'response', element(DAV, 'href', escapeXml(itemHref)) + propstats.join(''));
}

/** One <response> for an item ({ node, href, key }). */
function propfindResponse(item, request, ctx) {
  const found = [];
  const missing = [];
  for (const { ns, name } of request.props) {
    const read = PROPERTIES[`${ns} ${name}`];
    const value = read ? read(item, ctx) : null;
    if (value === null) {
      // allprop lists only what the node has; a missing named prop is a 404.
      if (request.mode !== 'allprop') missing.push(element(ns, name));
    } else {
      found.push(element(ns, name, request.mode === 'propname' ? null : value));
    }
  }
  const propstats = [];
  if (found.length > 0 || missing.length === 0) propstats.push(propstat(200, found.join('')));
  if (missing.length > 0) propstats.push(propstat(404, missing.join('')));
  return response(item.href, propstats);
}

/** The context property readers share; usage is read once, when needed. */
function propContext(userId, username) {
  let usage = null;
  let available;
  return {
    userId,
    username,
    usage() {
      usage = usage || getUsage(userId);
      return usage;
    },
    available() {
      if (available === undefined) available = availableBytes(userId);
      return available;
    },
  };
}

function multistatus(responses) {
  return document(DAV, 'multistatus', responses.join(''));
}

/**
 * A PROPPATCH body as a list of { ns, name, action: set|remove, value }
 * in document order.
 */
function parseProppatch(body) {
  const root = parseXml(body);
  if (!isElement(root, DAV, 'propertyupdate')) throw httpError(400, 'Expected a DAV:propertyupdate body.', 'XML_INVALID');
  const updates = [];
  for (const instruction of root.children) {
    if (instruction.ns !== DAV || !['set', 'remove'].includes(instruction.name)) continue;
    for (const prop of instruction.children.filter((item) => isElement(item, DAV, 'prop'))) {
      for (const { ns, name, text } of prop.children) {
        updates.push({ ns, name, action: instruction.name, value: text.trim() });
      }
    }
  }
  return updates;
}

/** The multistatus answer to a PROPPATCH: [{ ns, name, status }]. */
function proppatchResponse(itemHref, results) {
  const byStatus = new Map();
  for (const { ns, name, status } of results) {
    if (!byStatus.has(status)) byStatus.set(status, []);
    byStatus.get(status).push(element(ns, name));
  }
  const propstats = [...byStatus].map(([status, props]) => propstat(status, props.join('')));
  return multistatus([response(itemHref, propstats)]);
}

module.exports = {
  activeLock,
  multistatus,
  parsePropfind,
  parseProppatch,
  propContext,
  propfindResponse,
  proppatchResponse,
  toDate,
};
