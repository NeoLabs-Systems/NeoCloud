'use strict';

// Copying between two accounts' trees (a member copying out of a shared
// folder into their own files, or into a shared folder from their own).
// Blobs are encrypted under each account's own key, so nothing can be
// shared: every file is decrypted and written again under the destination
// account, and counts against that account's quota and node limit. Vault
// nodes are never copied. The copy is checked up front (node count, bytes,
// disk) but written file by file, so a failure midway leaves what was
// copied so far.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { assertDiskSpace } = require('../../utils/disk');
const { validateName } = require('../../utils/file_names');
const { openBlob, writeTemporaryBlob } = require('../storage/blob_store');
const { assertNodeRoom, nodeLimits, remainingBytes, uploadLimit } = require('../storage/quota');
const { announceChanges } = require('./changes');
const { commitUploadedBlob } = require('./commit');
const { conflictMode, createFolderInTransaction } = require('./operations');
const { getLiveNode, liveSubtree, nodeById, serializeNode } = require('./tree');

function quotaExceeded() {
  return httpError(413, 'Not enough storage left for this copy.', 'QUOTA_EXCEEDED');
}

// The source nodes, parents before children, vault nodes left out.
function sourceNodes(fromUserId, source) {
  if (source.type !== 'folder') return [source];
  return liveSubtree(fromUserId, source.id).filter((node) => !node.vault_id);
}

function assertCopyFits(toUserId, nodes) {
  const { maxCopyNodes } = nodeLimits();
  if (nodes.length > maxCopyNodes) {
    throw httpError(413, `A copy can hold at most ${maxCopyNodes} files and folders.`, 'COPY_TOO_LARGE');
  }
  assertNodeRoom(toUserId, nodes.length);
  const bytes = nodes.reduce((sum, node) => sum + (node.type === 'file' ? node.size : 0), 0);
  if (bytes > remainingBytes(toUserId)) throw quotaExceeded();
  assertDiskSpace(bytes);
}

function createFolder(toUserId, parentId, name, mode) {
  const { node } = db.transaction(() => createFolderInTransaction(toUserId, parentId, name, mode))();
  announceChanges(toUserId, [node.parent_id]);
  return node;
}

async function copyFile(fromUserId, node, toUserId, { parentId, name, conflict }) {
  const blob = await writeTemporaryBlob(toUserId, openBlob(fromUserId, node.blob_id, node.size), {
    ...uploadLimit(toUserId),
    limitError: quotaExceeded,
    expectedSize: node.size,
  });
  return commitUploadedBlob(toUserId, { parentId, name, conflict }, blob);
}

/**
 * Copies `sourceId` of `fromUserId` into folder `targetId` of `toUserId`.
 * Like copyNode, the copy defaults to "name (1)" when the name is taken.
 * The caller has already checked that the acting account may read the
 * source and write the target.
 */
async function copyAcrossAccounts({ fromUserId, sourceId, toUserId, targetId, name, conflict }) {
  const mode = conflictMode(conflict, 'rename');
  const source = getLiveNode(fromUserId, sourceId);
  if (source.parent_id === null) throw httpError(400, 'The root folder can’t be copied.', 'ROOT_IMMUTABLE');
  if (source.vault_id) throw httpError(409, 'Items can’t be moved or copied into or out of a vault.', 'VAULT_BOUNDARY');
  const target = getLiveNode(toUserId, targetId, { type: 'folder' });
  if (target.vault_id) throw httpError(409, 'Items can’t be moved or copied into or out of a vault.', 'VAULT_BOUNDARY');
  const topName = name === undefined || name === null ? source.name : validateName(name);
  const nodes = sourceNodes(fromUserId, source);
  assertCopyFits(toUserId, nodes);

  if (source.type === 'file') {
    return copyFile(fromUserId, source, toUserId, { parentId: target.id, name: topName, conflict: mode });
  }
  const top = createFolder(toUserId, target.id, topName, mode);
  const newIds = new Map([[source.id, top.id]]);
  for (const node of nodes.slice(1)) {
    const parentId = newIds.get(node.parent_id);
    if (node.type === 'folder') {
      newIds.set(node.id, createFolder(toUserId, parentId, node.name, 'rename').id);
    } else {
      await copyFile(fromUserId, node, toUserId, { parentId, name: node.name, conflict: 'rename' });
    }
  }
  return serializeNode(nodeById(top.id));
}

module.exports = { copyAcrossAccounts };
