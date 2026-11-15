'use strict';

// The account's activity log and comments for "export account data": the
// entries about its own tree (at most the retention window), the comments on
// its files by anyone and its own comments on other accounts' files.

const db = require('../../db/database');
const { decryptValue } = require('../crypto/keys');
const { parseDetail } = require('./record');

const rows = (sql, ...params) => db.prepare(sql).all(...params);

function exportActivity(userId) {
  const entries = rows(`
    SELECT a.*, COALESCE(NULLIF(u.display_name, ''), u.username) AS actor_name
    FROM activity a JOIN users u ON u.id = a.actor_id
    WHERE a.owner_id = ? ORDER BY a.id
  `, userId).map((row) => ({
    kind: row.kind,
    at: row.at,
    byMe: row.actor_id === userId,
    actor: row.actor_name,
    nodeId: row.node_id,
    nodeType: row.node_type,
    name: row.name,
    count: row.count,
    ...parseDetail(row.detail),
  }));
  const comments = rows(`
    SELECT c.id, c.node_id, c.user_id, c.body, c.created_at, c.edited_at, n.name AS node_name, n.user_id AS owner_id,
           COALESCE(NULLIF(u.display_name, ''), u.username) AS author
    FROM node_comments c JOIN nodes n ON n.id = c.node_id JOIN users u ON u.id = c.user_id
    WHERE n.user_id = @id OR c.user_id = @id ORDER BY c.id
  `, { id: userId }).map((row) => ({
    id: row.id,
    nodeId: row.node_id,
    // The name of someone else's file may no longer be mine to know.
    nodeName: row.owner_id === userId ? row.node_name : null,
    onMyFile: row.owner_id === userId,
    byMe: row.user_id === userId,
    author: row.author,
    body: decryptValue(row.body),
    createdAt: row.created_at,
    editedAt: row.edited_at,
  }));
  return { entries, comments };
}

module.exports = { exportActivity };
