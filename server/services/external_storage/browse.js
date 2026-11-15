'use strict';

// What a person can do inside a mount: list a folder, read a file (ranged),
// upload, make a folder, rename or move, delete. Every path is normalised
// first and a read-only mount refuses all changes. Files stay at the
// provider; nothing here writes to NeoCloud's own storage or file tree.

const { PassThrough, Transform } = require('stream');
const { recordAccessEvent } = require('../access/audit');
const { httpError } = require('../../utils/http_error');
const { mimeForName } = require('../../utils/mime');
const { numberedName } = require('../../utils/file_names');
const { openAdapter } = require('./connection');
const { getMountRow, readConfig } = require('./mounts');
const { baseName, isInside, joinPath, normalizePath, parentOf } = require('./paths');

const LIST_LIMIT = 5000;

function maxUploadBytes() {
  const configured = Number(process.env.NEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES);
  return Number.isFinite(configured) && configured > 0 ? configured : 20 * 1024 ** 3;
}

async function withMount(userId, mountId, { write = false } = {}, action) {
  const row = getMountRow(userId, mountId);
  if (write && row.read_only === 1) throw httpError(403, 'This connection is read-only.', 'EXTERNAL_READ_ONLY');
  const { adapter, release } = await openAdapter(userId, row.provider, readConfig(row));
  try {
    return await action(adapter, row);
  } finally {
    await release();
  }
}

function describe(path, entry) {
  return {
    name: entry.name,
    path: joinPath(path, entry.name),
    type: entry.type,
    size: entry.size,
    mtime: entry.mtime,
    ...(entry.type === 'file' ? { mime: mimeForName(entry.name) } : {}),
  };
}

function byFoldersThenName(a, b) {
  if (a.type !== b.type) return a.type === 'folder' ? -1 : 1;
  return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
}

async function listFolder(userId, mountId, rawPath) {
  const path = normalizePath(rawPath);
  return withMount(userId, mountId, {}, async (adapter, row) => {
    const { entries, truncated } = await adapter.list(path, { limit: LIST_LIMIT });
    return {
      mount: { id: row.id, name: row.name, readOnly: row.read_only === 1 },
      path,
      items: entries.map((entry) => describe(path, entry)).sort(byFoldersThenName),
      truncated,
    };
  });
}

/**
 * A file's bytes. `rangeFor(size)` picks the byte range to read ({ start, end },
 * { unsatisfiable: true } or null for all of it). Returns
 * { info, range, stream, release }: the caller sends the stream and then
 * calls release(), which closes the connection.
 */
async function openFile(userId, mountId, rawPath, { rangeFor = null } = {}) {
  const path = normalizePath(rawPath);
  const row = getMountRow(userId, mountId);
  const { adapter, release } = await openAdapter(userId, row.provider, readConfig(row));
  try {
    const info = await adapter.stat(path);
    if (!info) throw httpError(404, 'Not found at the provider.', 'EXTERNAL_NOT_FOUND');
    if (info.type !== 'file') throw httpError(400, 'That is a folder.', 'NOT_A_FILE');
    const range = rangeFor ? rangeFor(info.size) : null;
    const name = baseName(path);
    const meta = { ...info, name, mime: mimeForName(name) };
    if (range?.unsatisfiable) return { info: meta, range, stream: null, release };
    const stream = info.size === 0 ? PassThrough.from([]) : await adapter.read(path, range);
    return { info: meta, range, stream, release };
  } catch (err) {
    await release();
    throw err;
  }
}

// Passes the body on and fails it once it has more bytes than allowed or
// the sender went away before the end.
function limited(source, limit) {
  let seen = 0;
  const guard = new Transform({
    transform(chunk, encoding, callback) {
      seen += chunk.length;
      if (seen > limit) return callback(httpError(413, 'This file is larger than the upload limit.', 'FILE_TOO_LARGE'));
      return callback(null, chunk);
    },
  });
  source.on('error', (err) => guard.destroy(err));
  source.on('close', () => {
    if (!source.readableEnded) guard.destroy(new Error('The upload was interrupted.'));
  });
  return source.pipe(guard);
}

async function freeNameIn(adapter, folder, name) {
  for (let number = 0; number < 100; number += 1) {
    const candidate = number === 0 ? name : numberedName(name, number);
    if (!(await adapter.stat(joinPath(folder, candidate)))) return candidate;
  }
  throw httpError(409, 'An item with that name already exists.', 'NAME_EXISTS');
}

/**
 * Writes `stream` as `rawPath`. With overwrite false an existing file is
 * kept: the upload fails (conflict 'fail') or goes under "name (1)" ('rename').
 * Returns the item as stored.
 */
async function uploadFile(userId, mountId, rawPath, stream, { size = null, conflict = 'fail' } = {}) {
  const path = normalizePath(rawPath);
  if (!path) throw httpError(400, 'Give the file a name.', 'INVALID_PATH');
  if (size !== null && size > maxUploadBytes()) throw httpError(413, 'This file is larger than the upload limit.', 'FILE_TOO_LARGE');
  return withMount(userId, mountId, { write: true }, async (adapter) => {
    const folder = parentOf(path);
    let target = path;
    if (conflict !== 'replace' && await adapter.stat(path)) {
      if (conflict !== 'rename') throw httpError(409, 'An item with that name already exists.', 'NAME_EXISTS');
      target = joinPath(folder, await freeNameIn(adapter, folder, baseName(path)));
    }
    await adapter.write(target, limited(stream, maxUploadBytes()), { size });
    return describe(folder, { name: baseName(target), type: 'file', size: size ?? 0, mtime: new Date().toISOString() });
  });
}

function audit(action, userId, mountId, detail = {}) {
  recordAccessEvent(action, { actorUserId: userId, subjectUserId: userId, detail: { mountId, ...detail } });
}

async function createFolder(userId, mountId, rawPath) {
  const path = normalizePath(rawPath);
  if (!path) throw httpError(400, 'Give the folder a name.', 'INVALID_PATH');
  return withMount(userId, mountId, { write: true }, async (adapter) => {
    if (await adapter.stat(path)) throw httpError(409, 'An item with that name already exists.', 'NAME_EXISTS');
    await adapter.mkdir(path);
    return describe(parentOf(path), { name: baseName(path), type: 'folder', size: 0, mtime: null });
  });
}

async function move(userId, mountId, rawFrom, rawTo) {
  const from = normalizePath(rawFrom);
  const to = normalizePath(rawTo);
  if (!from || !to) throw httpError(400, 'The connection itself can’t be renamed or moved here.', 'INVALID_PATH');
  if (isInside(from, to)) throw httpError(400, 'An item can’t be moved into itself.', 'INVALID_PATH');
  return withMount(userId, mountId, { write: true }, async (adapter) => {
    const info = await adapter.stat(from);
    if (!info) throw httpError(404, 'Not found at the provider.', 'EXTERNAL_NOT_FOUND');
    if (await adapter.stat(to)) throw httpError(409, 'An item with that name already exists.', 'NAME_EXISTS');
    await adapter.move(from, to);
    audit('external_storage.move', userId, mountId);
    return describe(parentOf(to), { name: baseName(to), type: info.type, size: info.size, mtime: info.mtime });
  });
}

async function remove(userId, mountId, rawPath) {
  const path = normalizePath(rawPath);
  if (!path) throw httpError(400, 'The connection itself can’t be deleted here.', 'INVALID_PATH');
  return withMount(userId, mountId, { write: true }, async (adapter) => {
    await adapter.remove(path);
    audit('external_storage.delete', userId, mountId);
    return { success: true };
  });
}

module.exports = { createFolder, freeNameIn, listFolder, move, openFile, remove, uploadFile, withMount };
