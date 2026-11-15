'use strict';

// The account's own files as a connected app (NeoAgent) reaches them through
// /api/v1: by user-visible path ("/Contracts/Lease.md") or node id. Built on
// the account-scoped file services, so every change keeps the change
// journal, versions, quota and trash. Nodes inside a vault never show (a
// vault id answers like a missing one) and nothing is written into a vault.

const { Readable } = require('stream');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { validateName } = require('../../utils/file_names');
const tree = require('../files/tree');
const operations = require('../files/operations');
const trash = require('../files/trash');
const { search } = require('../files/search');
const { uploadStream } = require('../transfer/uploads');
const { readBlobBuffer } = require('../signing/blobs');

const MAX_TEXT_BYTES = 1024 * 1024;
const MAX_CONTENT_BYTES = 5 * 1024 * 1024;
const MAX_PATH_DEPTH = 64;
const TEXT_MIME = /^(text\/|application\/(json|xml|javascript|x-yaml|yaml|x-sh|sql|toml|x-ndjson)\b)|\+(json|xml)$/i;

function notFound() {
  return httpError(404, 'File or folder not found', 'NOT_FOUND');
}

function vaultRefused() {
  return httpError(403, 'Files in an encrypted vault can’t be reached from here.', 'VAULT_NOT_ALLOWED');
}

/** Path segments of a user-visible path; "/" is []. */
function pathParts(value) {
  if (value !== undefined && value !== null && typeof value !== 'string') throw httpError(400, 'path must be text.', 'INVALID_PATH');
  const parts = String(value ?? '/').split('/').filter((part) => part.length > 0);
  if (parts.length > MAX_PATH_DEPTH) throw httpError(400, 'path is too deep.', 'PATH_TOO_DEEP');
  for (const part of parts) {
    if (part === '.' || part === '..') throw httpError(400, 'path may not contain . or .. segments.', 'INVALID_PATH');
  }
  return parts;
}

/** Walks `parts` from the root. Returns { node, missing }: the deepest live node found and the parts below it. */
function walk(userId, parts) {
  let node = tree.getRoot(userId);
  for (let index = 0; index < parts.length; index += 1) {
    const child = tree.findLiveChildByName(node.id, parts[index]);
    if (!child) return { node, missing: parts.slice(index) };
    if (child.vault_id) throw vaultRefused();
    node = child;
  }
  return { node, missing: [] };
}

function nodeAtPath(userId, value, { type = null } = {}) {
  const parts = pathParts(value);
  let found;
  try {
    found = walk(userId, parts);
  } catch (err) {
    if (err.code === 'VAULT_NOT_ALLOWED') throw notFound();
    throw err;
  }
  if (found.missing.length > 0) throw notFound();
  if (type && found.node.type !== type) {
    throw type === 'folder' ? httpError(400, 'That is not a folder.', 'NOT_A_FOLDER') : httpError(400, 'That is not a file.', 'NOT_A_FILE');
  }
  return tree.getLiveNode(userId, found.node.id);
}

/** A live node of the account outside every vault, by id; anything else is 404. */
function visibleNode(userId, id, { type = null } = {}) {
  const node = tree.getLiveNode(userId, String(id || ''), { type });
  if (node.vault_id) throw notFound();
  return node;
}

function pathOf(userId, nodeId) {
  const names = tree.ancestry(userId, nodeId).filter((row) => row.parent_id !== null).map((row) => row.name).reverse();
  return `/${names.join('/')}`;
}

function itemOf(row, path) {
  return {
    id: row.id,
    name: row.parent_id === null ? '' : row.name,
    path,
    type: row.type,
    size: row.type === 'file' ? row.size : 0,
    mimeType: row.type === 'file' ? row.mime || 'application/octet-stream' : null,
    modifiedAt: row.modified_at ?? row.modifiedAt,
  };
}

function itemById(userId, id) {
  return itemOf(tree.nodeById(id), pathOf(userId, id));
}

const childPath = (folderPath, name) => (folderPath === '/' ? `/${name}` : `${folderPath}/${name}`);

// File-service errors under the codes of the agent API.
async function mapped(action) {
  try {
    return await action();
  } catch (err) {
    if (err?.code === 'NAME_EXISTS') throw httpError(409, err.message, 'ALREADY_EXISTS');
    if (err?.code === 'ETAG_MISMATCH') throw httpError(409, 'The file changed since that revision.', 'CONFLICT');
    throw err;
  }
}

function listFolder(userId, path) {
  const folder = nodeAtPath(userId, path, { type: 'folder' });
  const folderPath = pathOf(userId, folder.id);
  const rows = db.prepare(`
    SELECT * FROM nodes WHERE parent_id = ? AND user_id = ? AND trashed_at IS NULL AND vault_id IS NULL
    ORDER BY type = 'folder' DESC, lower(name)
  `).all(folder.id, userId);
  return { path: folderPath, items: rows.map((row) => itemOf(row, childPath(folderPath, row.name))) };
}

function searchFiles(userId, { q, limit }) {
  const text = typeof q === 'string' ? q : '';
  if (!text.trim()) return { items: [] };
  const { items } = search(userId, { q: text, limit: typeof limit === 'string' ? limit : undefined });
  return {
    items: items.filter((item) => !item.vaultId).map((item) => ({
      id: item.id,
      name: item.name,
      path: childPath(item.path, item.name),
      type: item.type,
      size: item.type === 'file' ? item.size : 0,
      mimeType: item.type === 'file' ? item.mime || 'application/octet-stream' : null,
      modifiedAt: item.modifiedAt,
    })),
  };
}

function getItem(userId, id) {
  const node = visibleNode(userId, id);
  return { ...itemOf(node, pathOf(userId, node.id)), revision: node.etag };
}

function isUtf8Text(buffer) {
  if (buffer.includes(0)) return false;
  try {
    new TextDecoder('utf-8', { fatal: true }).decode(buffer);
    return true;
  } catch {
    return false;
  }
}

async function readContent(userId, id) {
  const node = visibleNode(userId, id, { type: 'file' });
  if (node.size > MAX_CONTENT_BYTES) throw httpError(413, 'This file is too large to read here (5 MB at most).', 'FILE_TOO_LARGE');
  const bytes = await readBlobBuffer(userId, node.blob_id, node.size);
  const mimeType = node.mime || 'application/octet-stream';
  const textual = TEXT_MIME.test(mimeType) || mimeType === 'application/octet-stream';
  const asText = textual && bytes.length <= MAX_TEXT_BYTES && isUtf8Text(bytes);
  return {
    id: node.id,
    name: node.name,
    mimeType,
    encoding: asText ? 'utf8' : 'base64',
    content: asText ? bytes.toString('utf8') : bytes.toString('base64'),
    revision: node.etag,
  };
}

/** The bytes of a write request: `content` (text) or `contentBase64`, exactly one. */
function contentBytes({ content, contentBase64 }) {
  const hasText = content !== undefined && content !== null;
  const hasBase64 = contentBase64 !== undefined && contentBase64 !== null;
  if (hasText === hasBase64) throw httpError(400, 'Send either content or contentBase64.', 'INVALID_REQUEST');
  if (hasText && typeof content !== 'string') throw httpError(400, 'content must be text.', 'INVALID_REQUEST');
  if (hasBase64 && (typeof contentBase64 !== 'string' || !/^[A-Za-z0-9+/_-]*={0,2}$/.test(contentBase64.replace(/\s/g, '')))) {
    throw httpError(400, 'contentBase64 is not valid base64.', 'INVALID_REQUEST');
  }
  const bytes = hasText ? Buffer.from(content, 'utf8') : Buffer.from(contentBase64, 'base64');
  if (bytes.length > MAX_CONTENT_BYTES) throw httpError(413, 'The content is too large (5 MB at most).', 'TOO_LARGE');
  return bytes;
}

/** The folder for `parts` (made where missing); refuses vaults. */
function ensureFolder(userId, parts) {
  const { node, missing } = walk(userId, parts);
  if (node.vault_id) throw vaultRefused();
  if (node.type !== 'folder') throw httpError(409, `“${node.name}” is a file, not a folder.`, 'NOT_A_FOLDER');
  if (missing.length === 0) return { folder: node, created: false };
  for (const name of missing) validateName(name);
  const deepest = operations.ensureFolderPath(userId, { parentId: node.id, path: missing.join('/') });
  return { folder: tree.nodeById(deepest.id), created: true };
}

function splitFilePath(value) {
  const parts = pathParts(value);
  if (parts.length === 0) throw httpError(400, 'path must name a file.', 'INVALID_PATH');
  return { parents: parts.slice(0, -1), name: validateName(parts[parts.length - 1]) };
}

const streamOf = (bytes) => Readable.from(bytes.length > 0 ? [bytes] : []);

/** Writes `bytes` as a new file at `path` (or a new version when `overwrite`). */
function writeFile(userId, { path, bytes, overwrite = false }) {
  if (overwrite !== undefined && typeof overwrite !== 'boolean') throw httpError(400, 'overwrite must be true or false.', 'INVALID_REQUEST');
  const { parents, name } = splitFilePath(path);
  return mapped(async () => {
    const existing = walk(userId, parents);
    if (existing.missing.length === 0) {
      const clash = tree.findLiveChildByName(existing.node.id, name);
      if (clash && (!overwrite || clash.type !== 'file')) throw httpError(409, `“${clash.name}” already exists.`, 'ALREADY_EXISTS');
    }
    const { folder } = ensureFolder(userId, parents);
    const node = await uploadStream(userId, {
      parentId: folder.id, name, conflict: overwrite ? 'replace' : 'fail', contentLength: bytes.length,
    }, streamOf(bytes));
    return itemById(userId, node.id);
  });
}

function createFile(userId, body) {
  return writeFile(userId, { path: body?.path, bytes: contentBytes(body || {}), overwrite: body?.overwrite ?? false });
}

function updateContent(userId, id, body) {
  const node = visibleNode(userId, id, { type: 'file' });
  const bytes = contentBytes(body || {});
  const baseRevision = body?.baseRevision;
  if (baseRevision !== undefined && baseRevision !== null && typeof baseRevision !== 'string') {
    throw httpError(400, 'baseRevision must be text.', 'INVALID_REQUEST');
  }
  if (baseRevision && baseRevision !== node.etag) return Promise.reject(httpError(409, 'The file changed since that revision.', 'CONFLICT'));
  return mapped(async () => {
    const saved = await uploadStream(userId, {
      parentId: node.parent_id, name: node.name, conflict: 'replace', baseEtag: baseRevision || null, contentLength: bytes.length,
    }, streamOf(bytes));
    return { ...itemById(userId, saved.id), revision: tree.nodeById(saved.id).etag };
  });
}

/** Makes a folder (and missing parents). `created` false when it was there already. */
function createFolder(userId, body) {
  const parts = pathParts(body?.path);
  if (parts.length === 0) throw httpError(400, 'path must name a folder.', 'INVALID_PATH');
  const { folder, created } = ensureFolder(userId, parts);
  return { item: itemById(userId, folder.id), created };
}

function moveOrRename(userId, id, body) {
  const node = visibleNode(userId, id);
  const { name, parentPath } = body || {};
  if (name !== undefined && typeof name !== 'string') throw httpError(400, 'name must be text.', 'INVALID_REQUEST');
  if (name === undefined && parentPath === undefined) throw httpError(400, 'Send name or parentPath.', 'INVALID_REQUEST');
  const parentId = parentPath === undefined ? undefined : nodeAtPath(userId, parentPath, { type: 'folder' }).id;
  return mapped(() => {
    const moved = operations.updateNode(userId, node.id, { name, parentId, conflict: 'fail' });
    return itemById(userId, moved.id);
  });
}

function trashItem(userId, id) {
  const node = visibleNode(userId, id);
  trash.trashNode(userId, node.id);
  return { trashed: true };
}

/** The bytes of a file the account can see, for signing. */
async function fileBytes(userId, id) {
  const node = visibleNode(userId, id, { type: 'file' });
  return { node, bytes: await readBlobBuffer(userId, node.blob_id, node.size) };
}

module.exports = {
  createFile,
  createFolder,
  fileBytes,
  getItem,
  listFolder,
  moveOrRename,
  readContent,
  searchFiles,
  trashItem,
  updateContent,
  writeFile,
};
