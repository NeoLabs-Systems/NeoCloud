'use strict';

// Where remote items land in the account's tree: folders, files streamed
// into the blob store, dates and favorites, and the checks that decide
// whether a file is already there.

const db = require('../../db/database');
const { assertDiskSpace } = require('../../utils/disk');
const { formatBytes } = require('../../utils/bytes');
const { validateName } = require('../../utils/file_names');
const { httpError } = require('../../utils/http_error');
const { writeTemporaryBlob } = require('../storage/blob_store');
const { assertUploadFits, remainingBytes, uploadLimit } = require('../storage/quota');
const { announceChanges, recordChange } = require('../files/changes');
const { commitUploadedBlob } = require('../files/commit');
const { setFavorite } = require('../files/marks');
const { ensureFolderPath } = require('../files/operations');
const { findLiveChildByName, getLiveNode } = require('../files/tree');
const store = require('./store');

// Keeps the remote modification time. The journal entry lets sync clients
// that already saw the new node pick up its date.
function applyModifiedAt(userId, nodeId, mtime) {
  if (!mtime) return;
  db.transaction(() => {
    db.prepare('UPDATE nodes SET modified_at = ? WHERE id = ? AND user_id = ?').run(mtime, nodeId, userId);
    recordChange(userId, nodeId, 'update');
  })();
}

function targetFolder(job) {
  try {
    return getLiveNode(job.user_id, job.target_folder_id, { type: 'folder' });
  } catch {
    throw httpError(409, 'The folder to import into is gone (deleted or in the trash).', 'IMPORT_TARGET_MISSING');
  }
}

function cleanName(raw) {
  try {
    return validateName(raw);
  } catch {
    return null;
  }
}

/** The node at `path` below the job's folder, or null; creates nothing. */
function existingNode(job, path) {
  let node = { id: job.target_folder_id, type: 'folder' };
  for (const part of path.split('/')) {
    const name = cleanName(part);
    if (node.type !== 'folder' || !name) return null;
    node = findLiveChildByName(node.id, name);
    if (!node) return null;
  }
  return node;
}

// A file counts as already imported when name, size and date all match.
function isUnchanged(existing, name, item) {
  return Boolean(existing && existing.type === 'file' && existing.name === name
    && existing.size === item.size && item.mtime && existing.modified_at === item.mtime);
}

function folderFor(job, relativeFolder) {
  const folder = relativeFolder
    ? ensureFolderPath(job.user_id, { parentId: job.target_folder_id, path: relativeFolder })
    : targetFolder(job);
  const vaultId = folder.vaultId ?? folder.vault_id ?? null;
  if (vaultId) throw httpError(409, 'Files can’t be imported into a vault.', 'VAULT_BOUNDARY');
  return folder;
}

// Fails early when the files still to fetch cannot fit; per-file checks follow.
function assertRoomFor(job) {
  const pending = db.prepare(`
    SELECT path, size, mtime FROM import_items WHERE job_id = ? AND type = 'file' AND state = 'pending'
  `).all(job.id);
  let needed = 0;
  for (const item of pending) {
    const name = cleanName(item.path.split('/').pop());
    if (!isUnchanged(existingNode(job, item.path), name, item)) needed += item.size;
  }
  const remaining = remainingBytes(job.user_id);
  if (needed > remaining) {
    throw httpError(413, `Not enough storage: the import needs ${formatBytes(needed)}, ${formatBytes(remaining)} are left.`, 'QUOTA_EXCEEDED');
  }
  assertDiskSpace(needed);
}

function createFolders(job) {
  const folders = db.prepare(`
    SELECT path FROM import_items WHERE job_id = ? AND type = 'folder' AND state = 'pending' ORDER BY path
  `).all(job.id);
  for (const { path } of folders) {
    try {
      folderFor(job, path);
      store.setItemState(job.id, path, 'done');
    } catch (err) {
      store.failItem(job.id, path, err);
    }
  }
}

async function storeFile(job, client, item, signal) {
  const userId = job.user_id;
  const parts = item.path.split('/');
  const name = validateName(parts.pop());
  const parent = folderFor(job, parts.join('/'));
  const existing = findLiveChildByName(parent.id, name);
  if (existing && existing.type === 'folder') throw httpError(409, `A folder named “${existing.name}” is in the way.`, 'NAME_EXISTS');

  let nodeId = existing?.id;
  let state = 'skipped';
  if (!isUnchanged(existing, name, item)) {
    assertDiskSpace(item.size);
    assertUploadFits(userId, item.size);
    const download = await client.download(item.path, signal);
    const blob = await writeTemporaryBlob(userId, download.stream, { ...uploadLimit(userId), expectedSize: download.size });
    // A different spelling of the name ("Report.pdf" next to "report.pdf"
    // on a case-sensitive server) is kept beside it, never over it.
    const conflict = existing && existing.name === name ? 'replace' : 'rename';
    const node = await commitUploadedBlob(userId, { parentId: parent.id, name, conflict }, blob);
    applyModifiedAt(userId, node.id, item.mtime);
    if (blob.size !== item.size) {
      db.prepare('UPDATE import_items SET size = ? WHERE job_id = ? AND path = ?').run(blob.size, job.id, item.path);
    }
    nodeId = node.id;
    state = 'done';
  }
  if (item.favorite && job.include_favorites) setFavorite(userId, nodeId);
  return state;
}

// Folder dates and favorites last: adding files touches a folder's date.
function finishFolders(job) {
  const folders = db.prepare(`
    SELECT path, mtime, favorite FROM import_items WHERE job_id = ? AND type = 'folder' AND state = 'done'
    ORDER BY length(path) DESC
  `).all(job.id);
  const touched = [];
  for (const folder of folders) {
    const node = existingNode(job, folder.path);
    if (!node || node.type !== 'folder') continue;
    applyModifiedAt(job.user_id, node.id, folder.mtime);
    if (folder.favorite && job.include_favorites) setFavorite(job.user_id, node.id);
    touched.push(node.parent_id);
  }
  announceChanges(job.user_id, touched);
}

module.exports = { assertRoomFor, createFolders, finishFolders, storeFile, targetFolder };
