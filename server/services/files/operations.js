'use strict';

// Creating, renaming, moving and copying nodes. Each operation runs in one
// transaction, appends to the change journal, and announces the folders it
// touched once committed.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { numberedName, validateName } = require('../../utils/file_names');
const { mimeForName } = require('../../utils/mime');
const { retainBlob } = require('../storage/blob_refs');
const { assertNodeRoom, nodeLimits } = require('../storage/quota');
const { announceChanges, recordChange } = require('./changes');
const {
  ancestry,
  findLiveChildByName,
  getLiveNode,
  liveSubtree,
  nodeById,
  serializeNode,
} = require('./tree');

const CONFLICT_MODES = new Set(['fail', 'rename', 'replace']);
const MAX_NUMBERED_NAMES = 10000;

function conflictMode(value, fallback = 'fail') {
  const mode = String(value || fallback);
  if (!CONFLICT_MODES.has(mode)) throw httpError(400, 'conflict must be fail, rename or replace.', 'CONFLICT_MODE_INVALID');
  return mode;
}

function nameExists(existing) {
  const error = httpError(409, `“${existing.name}” already exists here.`, 'NAME_EXISTS');
  error.existingId = existing.id;
  return error;
}

// `name`, or the first free "name (n)" in the folder.
function freeName(parentId, name, ignoreId = null) {
  const taken = (candidate) => {
    const existing = findLiveChildByName(parentId, candidate);
    return existing && existing.id !== ignoreId;
  };
  if (!taken(name)) return name;
  for (let number = 1; number <= MAX_NUMBERED_NAMES; number += 1) {
    const candidate = numberedName(name, number);
    if (!taken(candidate)) return candidate;
  }
  throw httpError(409, 'Too many items with that name.', 'NAME_EXISTS');
}

// Everything created inside a vault folder belongs to that vault. Every way a
// node comes into being (folders, uploads, copies, imports, WebDAV) passes
// here, so the account's node limit holds for all of them.
function insertNode(userId, { parentId, type, name, size = 0, blobId = null, etag }) {
  assertNodeRoom(userId, 1);
  const id = crypto.randomUUID();
  db.prepare(`
    INSERT INTO nodes (id, user_id, parent_id, type, name, size, mime, blob_id, etag, vault_id)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT vault_id FROM nodes WHERE id = ?))
  `).run(
    id,
    userId,
    parentId,
    type,
    name,
    size,
    type === 'file' ? mimeForName(name) : null,
    blobId,
    etag || crypto.randomUUID(),
    parentId,
  );
  recordChange(userId, id, 'create');
  return nodeById(id);
}

function vaultBoundary() {
  return httpError(409, 'Items can’t be moved or copied into or out of a vault.', 'VAULT_BOUNDARY');
}

// The vault a node sits in (null outside any), read from its parent: a vault's
// own folder sits outside it and may move like any folder.
function containingVault(node) {
  if (node.parent_id === null) return null;
  return db.prepare('SELECT vault_id FROM nodes WHERE id = ?').get(node.parent_id)?.vault_id ?? null;
}

function touchFolder(folderId) {
  db.prepare("UPDATE nodes SET modified_at = datetime('now') WHERE id = ?").run(folderId);
}

function createFolderInTransaction(userId, parentId, rawName, conflict) {
  const parent = getLiveNode(userId, parentId, { type: 'folder' });
  let name = validateName(rawName);
  const existing = findLiveChildByName(parent.id, name);
  if (existing) {
    if (conflict === 'replace' && existing.type === 'folder') return { node: existing, created: false };
    if (conflict !== 'rename') throw nameExists(existing);
    name = freeName(parent.id, name);
  }
  const node = insertNode(userId, { parentId: parent.id, type: 'folder', name });
  touchFolder(parent.id);
  return { node, created: true };
}

/**
 * A new folder. With conflict "replace" an existing folder of that name is
 * returned as is (mkdir -p semantics), "rename" picks a free name.
 */
function createFolder(userId, { parentId, name, conflict }) {
  const mode = conflictMode(conflict);
  const { node } = db.transaction(() => createFolderInTransaction(userId, parentId, name, mode))();
  announceChanges(userId, [node.parent_id]);
  return serializeNode(node);
}

/**
 * Makes sure every folder along `relativePath` ("Photos/2026/May") exists
 * below `parentId`, creating the missing ones. Returns the deepest folder.
 * Used for folder uploads, where the browser sends each file with its path.
 */
function ensureFolderPath(userId, { parentId, path }) {
  const parts = String(path || '').split('/').filter((part) => part.length > 0);
  if (parts.length === 0) throw httpError(400, 'path is empty.', 'PATH_EMPTY');
  if (parts.length > 64) throw httpError(400, 'path is too deep.', 'PATH_TOO_DEEP');
  const created = [];
  const node = db.transaction(() => {
    let current = getLiveNode(userId, parentId, { type: 'folder' });
    for (const part of parts) {
      const result = createFolderInTransaction(userId, current.id, part, 'replace');
      if (result.node.type !== 'folder') throw nameExists(result.node);
      if (result.created) created.push(current.id);
      current = result.node;
    }
    return current;
  })();
  announceChanges(userId, created);
  return serializeNode(node);
}

/** Rename and/or move. Moving a folder into itself or below itself is refused. */
function updateNode(userId, nodeId, { name, parentId, conflict }) {
  const mode = conflictMode(conflict);
  const result = db.transaction(() => {
    const node = getLiveNode(userId, nodeId);
    if (node.parent_id === null) throw httpError(400, 'The root folder can’t be renamed or moved.', 'ROOT_IMMUTABLE');

    let targetParentId = node.parent_id;
    if (parentId !== undefined && parentId !== null) {
      const target = getLiveNode(userId, parentId, { type: 'folder' });
      if (ancestry(userId, target.id).some((ancestor) => ancestor.id === node.id)) {
        throw httpError(400, 'A folder can’t be moved into itself.', 'MOVE_INTO_SELF');
      }
      if ((target.vault_id ?? null) !== containingVault(node)) throw vaultBoundary();
      targetParentId = target.id;
    }
    let targetName = name === undefined || name === null ? node.name : validateName(name);
    const moved = targetParentId !== node.parent_id;
    if (!moved && targetName === node.name) return { node, previousParentId: node.parent_id };

    const existing = findLiveChildByName(targetParentId, targetName);
    if (existing && existing.id !== node.id) {
      if (mode !== 'rename') throw nameExists(existing);
      targetName = freeName(targetParentId, targetName, node.id);
    }
    db.prepare(`
      UPDATE nodes SET parent_id = ?, name = ?, mime = CASE WHEN type = 'file' THEN ? ELSE NULL END
      WHERE id = ?
    `).run(targetParentId, targetName, mimeForName(targetName), node.id);
    recordChange(userId, node.id, moved ? 'move' : 'rename');
    touchFolder(targetParentId);
    if (moved) touchFolder(node.parent_id);
    return {
      node: nodeById(node.id),
      previousParentId: node.parent_id,
    };
  })();
  announceChanges(userId, [result.node.parent_id, result.previousParentId]);
  return serializeNode(result.node);
}

// Nodes in a folder's live subtree (itself included), counted no further
// than `cap` so an oversized tree costs no more than the cap to measure.
function subtreeSize(userId, folderId, cap) {
  return db.prepare(`
    WITH RECURSIVE down(id) AS (
      SELECT id FROM nodes WHERE id = ? AND user_id = ?
      UNION ALL
      SELECT n.id FROM nodes n JOIN down ON n.parent_id = down.id
      WHERE n.trashed_at IS NULL
      LIMIT ?
    )
    SELECT COUNT(*) AS count FROM down
  `).get(folderId, userId, cap).count;
}

/**
 * Copies a file or a whole folder. File copies share the encrypted blob, so
 * a copy costs no storage until one side changes, but each node is a row:
 * more than NEOCLOUD_MAX_COPY_NODES is refused (413 COPY_TOO_LARGE) before
 * anything is written. Defaults to "name (1)" next to the original.
 */
function copyNode(userId, nodeId, { parentId, name, conflict }) {
  const mode = conflictMode(conflict, 'rename');
  const top = db.transaction(() => {
    const source = getLiveNode(userId, nodeId);
    if (source.parent_id === null) throw httpError(400, 'The root folder can’t be copied.', 'ROOT_IMMUTABLE');
    const target = getLiveNode(userId, parentId ?? source.parent_id, { type: 'folder' });
    if (source.type === 'folder' && ancestry(userId, target.id).some((ancestor) => ancestor.id === source.id)) {
      throw httpError(400, 'A folder can’t be copied into itself.', 'COPY_INTO_SELF');
    }
    // A copy takes the vault of its new parent, so ciphertext may only be
    // copied within its own vault (a vault folder itself not at all).
    if ((source.vault_id ?? null) !== (target.vault_id ?? null)) throw vaultBoundary();
    let targetName = name === undefined || name === null ? source.name : validateName(name);
    const existing = findLiveChildByName(target.id, targetName);
    if (existing) {
      if (mode !== 'rename') throw nameExists(existing);
      targetName = freeName(target.id, targetName);
    }

    if (source.type === 'folder') {
      const { maxCopyNodes } = nodeLimits();
      const count = subtreeSize(userId, source.id, maxCopyNodes + 1);
      if (count > maxCopyNodes) {
        throw httpError(413, `A copy can hold at most ${maxCopyNodes} files and folders.`, 'COPY_TOO_LARGE');
      }
      assertNodeRoom(userId, count);
    }

    const newIds = new Map();
    let first = null;
    // A vault folder inside the copied folder stays behind with everything
    // in it: its copy would take the new parent's (non-)vault and put the
    // vault's ciphertext outside any vault.
    const nodes = source.type === 'folder'
      ? liveSubtree(userId, source.id).filter((node) => (node.vault_id ?? null) === (source.vault_id ?? null))
      : [source];
    for (const node of nodes) {
      const isTop = node.id === source.id;
      const copy = insertNode(userId, {
        parentId: isTop ? target.id : newIds.get(node.parent_id),
        type: node.type,
        name: isTop ? targetName : node.name,
        size: node.size,
        blobId: node.blob_id,
        etag: node.type === 'file' ? node.etag : null,
      });
      if (node.blob_id) retainBlob(node.blob_id);
      newIds.set(node.id, copy.id);
      if (isTop) first = copy;
    }
    touchFolder(target.id);
    return first;
  })();
  announceChanges(userId, [top.parent_id]);
  return serializeNode(top);
}

/**
 * Sets a node's modified time to `date` (a Date), for clients that carry
 * their own file times (WebDAV's X-OC-Mtime and PROPPATCH).
 */
function setModifiedAt(userId, nodeId, date) {
  const node = db.transaction(() => {
    const target = getLiveNode(userId, nodeId);
    const stamp = date.toISOString().slice(0, 19).replace('T', ' ');
    db.prepare('UPDATE nodes SET modified_at = ? WHERE id = ?').run(stamp, target.id);
    recordChange(userId, target.id, 'update');
    return nodeById(target.id);
  })();
  announceChanges(userId, [node.parent_id]);
  return serializeNode(node);
}

module.exports = {
  conflictMode,
  copyNode,
  createFolder,
  createFolderInTransaction,
  ensureFolderPath,
  freeName,
  insertNode,
  nameExists,
  setModifiedAt,
  touchFolder,
  updateNode,
  vaultBoundary,
};
