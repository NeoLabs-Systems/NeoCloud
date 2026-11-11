'use strict';

// WebDAV methods that change the tree: PUT, MKCOL, DELETE, MOVE, COPY and
// PROPPATCH. Each one maps onto the file services the REST API uses, so
// quotas, versions, trash and the change journal behave the same.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { deleteBlobFiles } = require('../storage/blob_store');
const { assertUploadFits } = require('../storage/quota');
const { uploadStream } = require('../transfer/uploads');
const { announceChanges } = require('../files/changes');
const { copyNode, createFolder, setModifiedAt, touchFolder, updateNode } = require('../files/operations');
const { purgeSubtree, trashNode } = require('../files/trash');
const { listVersions, replaceContent } = require('../files/versions');
const { assertUnlocked, releaseLocks, submittedTokens } = require('./locks');
const { href, lockKey, parseDestination, resolvePath } = require('./paths');
const { parseProppatch, proppatchResponse, toDate } = require('./props');
const { DAV } = require('./xml');
const { hasBody, readXmlBody, sendXml } = require('./respond');

// Latest accepted file time: year 9999.
const MAX_TIME_MS = 253402300799000;

function notFound() {
  return httpError(404, 'File or folder not found', 'NOT_FOUND');
}

function parentMissing() {
  return httpError(409, 'The parent folder does not exist.', 'PARENT_MISSING');
}

function assertWritable(ctx, segments = ctx.segments, options = {}) {
  assertUnlocked(ctx.userId, lockKey(segments), submittedTokens(ctx.req.get('if')), options);
}

function stripEtag(value) {
  return String(value).trim().replace(/^W\//, '').replace(/^"(.*)"$/, '$1');
}

/** A Unix time in seconds (X-OC-Mtime, rclone) or an HTTP/ISO date. */
function parseFileTime(value) {
  const text = String(value ?? '').trim();
  const ms = /^\d+(\.\d+)?$/.test(text) ? Number(text) * 1000 : Date.parse(text);
  if (!Number.isFinite(ms) || ms < 0 || ms > MAX_TIME_MS) return null;
  return new Date(Math.floor(ms / 1000) * 1000);
}

async function put(ctx) {
  const { req, res, userId, segments } = ctx;
  if (segments.length === 0) throw httpError(405, 'The root folder is not a file.', 'IS_A_FOLDER');
  const { node, parent } = resolvePath(userId, segments);
  if (!parent) throw parentMissing();
  if (node?.type === 'folder') throw httpError(405, 'A folder can’t be overwritten with a file.', 'IS_A_FOLDER');
  assertWritable(ctx);

  const ifMatch = req.get('if-match');
  const ifNoneMatch = String(req.get('if-none-match') || '').trim();
  if ((ifMatch && !node) || (ifNoneMatch === '*' && node)) {
    throw httpError(412, 'The precondition failed.', 'PRECONDITION_FAILED');
  }
  const mtimeHeader = req.get('x-oc-mtime');
  const mtime = mtimeHeader === undefined ? null : parseFileTime(mtimeHeader);
  // Finder streams chunked and announces the size in this header instead.
  const expected = Number(req.get('x-expected-entity-length'));
  if (Number.isSafeInteger(expected) && expected > 0) assertUploadFits(userId, expected);

  let file = await uploadStream(userId, {
    parentId: parent.id,
    name: node ? node.name : segments[segments.length - 1],
    conflict: 'replace',
    baseEtag: ifMatch && ifMatch.trim() !== '*' ? stripEtag(ifMatch) : null,
    contentLength: req.get('content-length'),
  }, req);
  if (mtime) {
    file = setModifiedAt(userId, file.id, mtime);
    res.set('X-OC-MTime', 'accepted');
  }
  res.set({ ETag: `"${file.etag}"`, 'OC-ETag': `"${file.etag}"`, 'OC-FileId': file.id });
  res.status(node ? 204 : 201).end();
}

async function mkcol(ctx) {
  const { req, res, userId, segments } = ctx;
  if (hasBody(req)) throw httpError(415, 'MKCOL with a body is not supported.', 'UNSUPPORTED_BODY');
  const { node, parent } = resolvePath(userId, segments);
  if (node) throw httpError(405, 'Something with this name already exists.', 'RESOURCE_EXISTS');
  if (!parent) throw parentMissing();
  assertWritable(ctx);
  createFolder(userId, { parentId: parent.id, name: segments[segments.length - 1], conflict: 'fail' });
  res.status(201).end();
}

function remove(ctx) {
  const { res, userId, segments } = ctx;
  if (segments.length === 0) throw httpError(403, 'The root folder can’t be deleted.', 'ROOT_IMMUTABLE');
  const { node } = resolvePath(userId, segments);
  if (!node) throw notFound();
  assertWritable(ctx, segments, { deep: true });
  trashNode(userId, node.id);
  releaseLocks(userId, lockKey(segments));
  res.status(204).end();
}

// Inside a transaction: `target` (an existing file) takes the content of
// `source` as a new version, so overwriting keeps the target's history.
function replaceFileContent(userId, source, target, isMove) {
  const orphaned = replaceContent(userId, target, { blobId: source.blob_id, size: source.size, etag: source.etag });
  touchFolder(target.parent_id);
  if (!isMove) return orphaned;
  setModifiedAt(userId, target.id, toDate(source.modified_at));
  // The moved file lives on as the target's content. Its own old versions
  // stay recoverable in the trash; without any it simply goes away.
  if (listVersions(userId, source.id).versions.length > 0) {
    trashNode(userId, source.id);
    return orphaned;
  }
  return [...orphaned, ...purgeSubtree(userId, source.id)];
}

function copyFolderShallow(userId, parentId, name) {
  return createFolder(userId, { parentId, name, conflict: 'fail' });
}

async function moveOrCopy(ctx) {
  const { req, res, userId, segments } = ctx;
  const isMove = req.method === 'MOVE';
  if (segments.length === 0) throw httpError(403, 'The root folder can’t be moved or copied.', 'ROOT_IMMUTABLE');
  const source = resolvePath(userId, segments).node;
  if (!source) throw notFound();

  const destination = parseDestination(req.get('destination'));
  if (destination.username !== null && destination.username.toLowerCase() !== ctx.username.toLowerCase()) {
    throw httpError(403, 'The destination belongs to another account.', 'DESTINATION_INVALID');
  }
  const targetSegments = destination.segments;
  if (targetSegments.length === 0) throw httpError(403, 'The root folder can’t be replaced.', 'ROOT_IMMUTABLE');
  const target = resolvePath(userId, targetSegments);
  if (!target.parent) throw parentMissing();
  const targetName = targetSegments[targetSegments.length - 1];
  const sameNode = target.node?.id === source.id;
  // Same node, same spelling: nothing to do (RFC 4918 9.8.5, 9.9.4). A
  // MOVE that changes only the case is a rename.
  if (sameNode && (!isMove || target.node.name === targetName)) {
    throw httpError(403, 'Source and destination are the same.', 'SAME_RESOURCE');
  }
  const existing = sameNode ? null : target.node;
  if (existing && String(req.get('overwrite') || 'T').trim().toUpperCase() === 'F') {
    throw httpError(412, 'The destination exists and Overwrite is F.', 'DESTINATION_EXISTS');
  }
  if (isMove) assertWritable(ctx, segments, { deep: true });
  assertWritable(ctx, targetSegments, { deep: true });

  const shallow = !isMove && source.type === 'folder' && String(req.get('depth') || '').trim() === '0';
  const orphaned = db.transaction(() => {
    if (existing?.type === 'file' && source.type === 'file') {
      return replaceFileContent(userId, source, existing, isMove);
    }
    if (existing) trashNode(userId, existing.id);
    if (isMove) updateNode(userId, source.id, { name: targetName, parentId: target.parent.id, conflict: 'fail' });
    else if (shallow) copyFolderShallow(userId, target.parent.id, targetName);
    else copyNode(userId, source.id, { parentId: target.parent.id, name: targetName, conflict: 'fail' });
    return [];
  })();
  await deleteBlobFiles(userId, orphaned);
  announceChanges(userId, [source.parent_id, target.parent.id]);
  if (isMove) releaseLocks(userId, lockKey(segments));
  if (existing) releaseLocks(userId, lockKey(targetSegments));
  res.status(existing ? 204 : 201).end();
}

// Properties a client may set. File times are kept; everything else
// (Windows' Win32* attributes, editor metadata) is accepted and dropped.
const TIME_PROPERTIES = new Set(['getlastmodified', 'lastmodified']);

async function proppatch(ctx) {
  const { req, res, userId, segments } = ctx;
  const updates = parseProppatch(await readXmlBody(req));
  const { node } = resolvePath(userId, segments);
  if (!node) throw notFound();
  assertWritable(ctx);

  let mtime = null;
  const invalid = new Set();
  for (const update of updates) {
    if (update.ns !== DAV || !TIME_PROPERTIES.has(update.name) || update.action !== 'set') continue;
    const time = parseFileTime(update.value);
    if (time) mtime = time;
    else invalid.add(update);
  }
  // PROPPATCH is all or nothing: one bad value fails the others with 424.
  const results = updates.map((update) => {
    let status = 200;
    if (invalid.size > 0) status = invalid.has(update) ? 409 : 424;
    return { ns: update.ns, name: update.name, status };
  });
  if (mtime && invalid.size === 0) setModifiedAt(userId, node.id, mtime);
  sendXml(res, 207, proppatchResponse(href(ctx.base, segments, node.type === 'folder'), results));
}

module.exports = { moveOrCopy, mkcol, proppatch, put, remove };
