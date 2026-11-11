'use strict';

// What a visitor with a share link may do: see the shared file or browse the
// shared folder, download (single files with Range, folders as ZIP) and,
// when the link's mode allows it, upload into the folder against the owner's
// quota. An upload-only link shows nothing of the folder's contents. Every
// node id a visitor sends is checked to lie inside the shared subtree, live
// and outside any vault; anything else is a plain 404.

const bcrypt = require('bcrypt');
const db = require('../../db/database');
const { validateName } = require('../../utils/file_names');
const { httpError } = require('../../utils/http_error');
const { ancestry, getLiveNode, listChildren } = require('../files/tree');
const { uploadStream } = require('../transfer/uploads');
const { isValidUnlockProof, unlockProof } = require('./proofs');

const MAX_LISTING = 2000;
// Wrong passwords per link: from the 5th on, each next try waits twice as
// long (2 s, 4 s, ...); the 10th locks the link for the rest of the window.
const FAILURE_WINDOW_MS = 30 * 60 * 1000;
const FAILURES_BEFORE_DELAY = 5;
const FAILURES_BEFORE_LOCK = 10;
const BASE_DELAY_MS = 2000;

function notFound() {
  return httpError(404, 'This link does not exist.', 'NOT_FOUND');
}

function publicNode(row) {
  return {
    id: row.id,
    type: row.type,
    name: row.name,
    size: row.size,
    mime: row.mime,
    modifiedAt: row.modified_at,
  };
}

// The same fields from a node already serialized by tree.serializeNode.
function publicItem(item) {
  return publicNode({ ...item, modified_at: item.modifiedAt });
}

function exhausted() {
  return httpError(410, 'This link has reached its download limit.', 'SHARE_EXHAUSTED');
}

// 410 SHARE_EXHAUSTED once the link's download limit is used up.
function assertDownloadsLeft(share) {
  if (share.max_downloads !== null && share.download_count >= share.max_downloads) throw exhausted();
}

// The link row with its state checked: unknown 404, revoked, expired or used
// up 410 (`allowExhausted` defers the last check to the caller, for a
// download being resumed). Carries the owner's username for the page.
function findShare(token, { allowExhausted = false } = {}) {
  const share = db.prepare(`
    SELECT s.*, u.username AS owner_username,
           (s.expires_at IS NOT NULL AND datetime(s.expires_at) <= datetime('now')) AS is_expired
    FROM shares s JOIN users u ON u.id = s.user_id WHERE s.token = ?
  `).get(String(token || ''));
  if (!share) throw notFound();
  if (share.revoked_at) throw httpError(410, 'This link has been turned off.', 'SHARE_REVOKED');
  if (share.is_expired === 1) throw httpError(410, 'This link has expired.', 'SHARE_EXPIRED');
  if (!allowExhausted) assertDownloadsLeft(share);
  return share;
}

// The shared node, still live and outside any vault.
function shareRoot(share) {
  let root;
  try {
    root = getLiveNode(share.user_id, share.node_id);
  } catch {
    throw notFound();
  }
  if (root.vault_id) throw notFound();
  return root;
}

/**
 * Everything a public request needs: { share, root }. `proof` is the unlock
 * cookie; a password-protected link without a valid one answers 401.
 */
function openShare(token, proof, options) {
  const share = findShare(token, options);
  const root = shareRoot(share);
  if (share.password_hash && !isValidUnlockProof(share, proof)) {
    throw httpError(401, 'This link is protected by a password.', 'SHARE_PASSWORD_REQUIRED');
  }
  return { share, root };
}

function shareLocked(retryAfterMs) {
  const error = httpError(429, 'Too many wrong passwords. Try again later.', 'SHARE_LOCKED');
  error.retryAfter = Math.max(1, Math.ceil(retryAfterMs / 1000));
  return error;
}

// Refuses a password try while the link is throttled: 429 SHARE_LOCKED with
// `retryAfter` seconds.
function assertUnlockAllowed(share, now) {
  const failures = db.prepare(`
    SELECT failed_at_ms FROM share_unlock_failures WHERE share_id = ? AND failed_at_ms > ? ORDER BY failed_at_ms
  `).all(share.id, now - FAILURE_WINDOW_MS).map((row) => row.failed_at_ms);
  if (failures.length >= FAILURES_BEFORE_LOCK) {
    throw shareLocked(failures[failures.length - FAILURES_BEFORE_LOCK] + FAILURE_WINDOW_MS - now);
  }
  if (failures.length >= FAILURES_BEFORE_DELAY) {
    const delay = BASE_DELAY_MS * 2 ** (failures.length - FAILURES_BEFORE_DELAY);
    const next = failures[failures.length - 1] + delay;
    if (now < next) throw shareLocked(next - now);
  }
}

/**
 * Checks the password; returns the proof to keep in a cookie. Each try is
 * recorded as a failure before bcrypt runs, so parallel guesses count too;
 * the right password clears the link's failures.
 */
async function unlockShare(token, password) {
  const share = findShare(token);
  shareRoot(share);
  if (share.password_hash) {
    const now = Date.now();
    assertUnlockAllowed(share, now);
    db.prepare('DELETE FROM share_unlock_failures WHERE share_id = ? AND failed_at_ms <= ?')
      .run(share.id, now - FAILURE_WINDOW_MS);
    db.prepare('INSERT INTO share_unlock_failures (share_id, failed_at_ms) VALUES (?, ?)').run(share.id, now);
    if (!(await bcrypt.compare(String(password || ''), share.password_hash))) {
      throw httpError(401, 'Wrong password.', 'SHARE_PASSWORD_INVALID');
    }
    db.prepare('DELETE FROM share_unlock_failures WHERE share_id = ?').run(share.id);
  }
  return unlockProof(share);
}

/**
 * A node inside the shared subtree (the root itself when `nodeId` is empty).
 * Another account's node, one outside the subtree, a trashed one or a vault
 * node all look missing.
 */
function nodeInShare({ share, root }, nodeId, { type = null } = {}) {
  let node = root;
  if (nodeId && nodeId !== root.id) {
    try {
      node = getLiveNode(share.user_id, String(nodeId));
    } catch {
      throw notFound();
    }
    if (!ancestry(share.user_id, node.id).some((ancestor) => ancestor.id === root.id)) throw notFound();
  }
  if (node.vault_id) throw notFound();
  if (type && node.type !== type) {
    throw type === 'folder'
      ? httpError(400, 'That is not a folder.', 'NOT_A_FOLDER')
      : httpError(400, 'That is not a file.', 'NOT_A_FILE');
  }
  return node;
}

// Folder names from the shared root down to `node`.
function pathInShare(context, node) {
  const chain = ancestry(context.share.user_id, node.id).reverse();
  const start = chain.findIndex((ancestor) => ancestor.id === context.root.id);
  return chain.slice(start).map((ancestor) => ({ id: ancestor.id, name: ancestor.name }));
}

function uploadOnly() {
  return httpError(403, 'This link only accepts uploads.', 'SHARE_UPLOAD_ONLY');
}

/** Downloads and listings: refused on an upload-only link. */
function assertCanView({ share }) {
  if (share.mode === 'upload_only') throw uploadOnly();
}

/**
 * The link's metadata, plus the listing of `folderId` for a shared folder.
 * `host` is where the link points (the server's public host name). An
 * upload-only link lists nothing.
 */
function describeShare(context, folderId, host) {
  const { share, root } = context;
  const summary = {
    name: root.name,
    type: root.type,
    size: root.size,
    mode: share.mode,
    allowUpload: share.mode !== 'view' && root.type === 'folder',
    expiresAt: share.expires_at,
    hasPassword: Boolean(share.password_hash),
    sharedBy: share.owner_username,
    host,
    note: share.note,
  };
  const rootOnly = { share: summary, node: publicNode(root), path: [{ id: root.id, name: root.name }], items: [] };
  if (share.mode === 'upload_only') {
    if (folderId && folderId !== root.id) throw uploadOnly();
    return rootOnly;
  }
  if (root.type === 'file') {
    nodeInShare(context, folderId);
    return rootOnly;
  }
  const folder = nodeInShare(context, folderId, { type: 'folder' });
  const listing = listChildren(share.user_id, folder.id, { limit: MAX_LISTING });
  const items = listing.items.filter((item) => !item.vaultId);
  return {
    share: summary,
    node: publicNode(folder),
    path: pathInShare(context, folder),
    items: items.map(publicItem),
  };
}

/**
 * Counts one download (a file with ?download=1 or a ZIP) before it is sent.
 * Atomic, so parallel requests can't go past the link's limit.
 */
function consumeDownload(share) {
  const result = db.prepare(`
    UPDATE shares SET download_count = download_count + 1
    WHERE id = ? AND (max_downloads IS NULL OR download_count < max_downloads)
  `).run(share.id);
  if (result.changes === 0) throw exhausted();
}

function uploadLimitReached() {
  return httpError(413, 'This link can’t take more uploads.', 'SHARE_UPLOAD_LIMIT');
}

// A capped link needs the size up front to reserve it.
function requiredLength(contentLength) {
  const text = String(contentLength ?? '').trim();
  const length = Number(text);
  if (!/^\d+$/.test(text) || !Number.isSafeInteger(length)) {
    throw httpError(411, 'This link needs the upload size up front (Content-Length).', 'LENGTH_REQUIRED');
  }
  return length;
}

/**
 * A visitor's upload into the shared folder; never replaces anything. On an
 * upload-only link it goes to the shared folder itself and the answer names
 * only what the visitor sent, not what the file is called now.
 */
async function uploadIntoShare(context, { folderId, name, contentLength, sha256 }, body) {
  const { share, root } = context;
  if (share.mode === 'view' || root.type !== 'folder') {
    throw httpError(403, 'This link does not accept uploads.', 'UPLOAD_NOT_ALLOWED');
  }
  let folder;
  if (share.mode === 'upload_only') {
    if (folderId && folderId !== root.id) throw uploadOnly();
    folder = root;
  } else {
    folder = nodeInShare(context, folderId, { type: 'folder' });
  }
  // A capped link reserves the upload's size before reading a byte, in one
  // conditional UPDATE, so parallel uploads can't together go past the cap;
  // a failed upload gives its reservation back.
  let reserved = null;
  if (share.max_upload_bytes !== null) {
    reserved = requiredLength(contentLength);
    const { changes } = db.prepare(`
      UPDATE shares SET uploaded_bytes = uploaded_bytes + ?
      WHERE id = ? AND uploaded_bytes < max_upload_bytes AND uploaded_bytes + ? <= max_upload_bytes
    `).run(reserved, share.id, reserved);
    if (changes === 0) throw uploadLimitReached();
  }
  let node;
  try {
    node = await uploadStream(share.user_id, {
      parentId: folder.id,
      name,
      conflict: 'rename',
      contentLength,
      sha256,
      cap: reserved === null ? null : { limit: reserved, limitError: uploadLimitReached },
    }, body);
  } catch (err) {
    if (reserved) {
      db.prepare('UPDATE shares SET uploaded_bytes = MAX(0, uploaded_bytes - ?) WHERE id = ?').run(reserved, share.id);
    }
    throw err;
  }
  const counted = node.size - (reserved ?? 0);
  if (counted !== 0) db.prepare('UPDATE shares SET uploaded_bytes = uploaded_bytes + ? WHERE id = ?').run(counted, share.id);
  if (share.mode === 'upload_only') return { name: validateName(name), size: node.size };
  return publicItem(node);
}

module.exports = {
  assertCanView,
  assertDownloadsLeft,
  consumeDownload,
  describeShare,
  nodeInShare,
  openShare,
  unlockShare,
  uploadIntoShare,
};
