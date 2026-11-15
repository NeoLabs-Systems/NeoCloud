'use strict';

// Short plain-text comments on files and folders. A comment belongs to the
// node, so it stays through rename and move and goes when the node is
// deleted for good. Everyone who can reach the node (its owner, accounts it
// is shared with, read-only members included) reads and writes comments;
// public link visitors never do. Bodies are stored sealed. Authors edit
// their own comments; the author and the node's owner delete them.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { decryptValue, encryptValue } = require('../crypto/keys');
const { emitToUser } = require('../events');
const { getLiveNode } = require('../files/tree');
const { resolveAccess } = require('../members/access');
const { recordActivity } = require('../activity/record');
const { watchers } = require('../activity/watchers');

const MAX_LENGTH = 2000;
const DEFAULT_LIMIT = 50;
const MAX_LIMIT = 100;
// Everything except tab, newline and carriage return.
const CONTROL_CHARACTERS = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/;

function notFound() {
  return httpError(404, 'File or folder not found', 'NOT_FOUND');
}

function commentNotFound() {
  return httpError(404, 'Comment not found', 'COMMENT_NOT_FOUND');
}

function parseBody(raw) {
  if (typeof raw !== 'string') throw httpError(400, 'Write a comment first.', 'COMMENT_EMPTY');
  const body = raw.replace(/\r\n/g, '\n').trim();
  if (body.length === 0) throw httpError(400, 'Write a comment first.', 'COMMENT_EMPTY');
  if (body.length > MAX_LENGTH) {
    throw httpError(400, `A comment can be up to ${MAX_LENGTH} characters.`, 'COMMENT_TOO_LONG');
  }
  if (CONTROL_CHARACTERS.test(body)) throw httpError(400, 'That comment has characters that can’t be saved.', 'COMMENT_INVALID');
  return body;
}

// The node a caller may comment on and whose tree it sits in: another
// account's node, a missing one, a vault node and the root all answer 404.
function commentTarget(userId, nodeId) {
  const access = resolveAccess(userId, nodeId);
  const node = access.shared ? access.node : getLiveNode(userId, nodeId);
  if (node.parent_id === null || node.vault_id) throw notFound();
  return { node, ownerId: access.ownerId };
}

function serialize(row, userId, ownerId) {
  const mine = row.user_id === userId;
  return {
    id: row.id,
    author: row.author_name ?? '',
    mine,
    body: decryptValue(row.body),
    createdAt: `${row.created_at.replace(' ', 'T')}Z`,
    editedAt: row.edited_at ? `${row.edited_at.replace(' ', 'T')}Z` : null,
    canDelete: mine || ownerId === userId,
  };
}

const SELECT_COMMENT = `
  SELECT c.*, COALESCE(NULLIF(u.display_name, ''), u.username) AS author_name
  FROM node_comments c JOIN users u ON u.id = c.user_id
`;

function tellWatchers(ownerId, node) {
  for (const accountId of watchers(ownerId, node.id)) emitToUser(accountId, 'comments:changed', { nodeId: node.id });
}

/** Comments on a node, newest first; `cursor` is the `nextCursor` of the page before. */
function listComments(userId, nodeId, { cursor = null, limit = DEFAULT_LIMIT } = {}) {
  const { node, ownerId } = commentTarget(userId, nodeId);
  const pageSize = Math.min(Math.max(Math.trunc(Number(limit)) || DEFAULT_LIMIT, 1), MAX_LIMIT);
  const before = Number(cursor) > 0 ? Math.trunc(Number(cursor)) : Number.MAX_SAFE_INTEGER;
  const rows = db.prepare(`${SELECT_COMMENT} WHERE c.node_id = ? AND c.id < ? ORDER BY c.id DESC LIMIT ?`)
    .all(node.id, before, pageSize + 1);
  const page = rows.slice(0, pageSize);
  return {
    comments: page.map((row) => serialize(row, userId, ownerId)),
    nextCursor: rows.length > pageSize ? page[page.length - 1].id : null,
  };
}

function addComment(userId, nodeId, rawBody) {
  const body = parseBody(rawBody);
  const { node, ownerId } = commentTarget(userId, nodeId);
  const row = db.transaction(() => {
    const { lastInsertRowid } = db.prepare('INSERT INTO node_comments (node_id, user_id, body) VALUES (?, ?, ?)')
      .run(node.id, userId, encryptValue(body));
    recordActivity(ownerId, 'comment', node, { actorId: userId });
    return db.prepare(`${SELECT_COMMENT} WHERE c.id = ?`).get(lastInsertRowid);
  })();
  tellWatchers(ownerId, node);
  return serialize(row, userId, ownerId);
}

function ownedComment(node, commentId) {
  const row = db.prepare(`${SELECT_COMMENT} WHERE c.id = ? AND c.node_id = ?`).get(commentId, node.id);
  if (!row) throw commentNotFound();
  return row;
}

function editComment(userId, nodeId, commentId, rawBody) {
  const body = parseBody(rawBody);
  const { node, ownerId } = commentTarget(userId, nodeId);
  const row = ownedComment(node, commentId);
  if (row.user_id !== userId) throw httpError(403, 'You can only edit your own comments.', 'COMMENT_NOT_YOURS');
  db.prepare("UPDATE node_comments SET body = ?, edited_at = datetime('now') WHERE id = ?").run(encryptValue(body), row.id);
  tellWatchers(ownerId, node);
  return serialize(ownedComment(node, row.id), userId, ownerId);
}

function deleteComment(userId, nodeId, commentId) {
  const { node, ownerId } = commentTarget(userId, nodeId);
  const row = ownedComment(node, commentId);
  if (row.user_id !== userId && ownerId !== userId) {
    throw httpError(403, 'You can only delete your own comments.', 'COMMENT_NOT_YOURS');
  }
  db.prepare('DELETE FROM node_comments WHERE id = ?').run(row.id);
  tellWatchers(ownerId, node);
  return { success: true };
}

module.exports = { addComment, deleteComment, editComment, listComments };
