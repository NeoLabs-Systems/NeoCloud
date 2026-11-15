'use strict';

// Who is looking at a node and so wants to hear when it changes: its owner
// and every account with a live grant on it or a folder above it.

const db = require('../../db/database');
const { ancestry } = require('../files/tree');
const { activeGrant } = require('../members/access');

function watchers(ownerId, nodeId) {
  const chain = ancestry(ownerId, nodeId).map((node) => node.id);
  const members = db.prepare(`
    SELECT DISTINCT grantee_id FROM user_shares
    WHERE owner_id = ? AND node_id IN (SELECT value FROM json_each(?)) AND ${activeGrant()}
  `).all(ownerId, JSON.stringify(chain));
  return [ownerId, ...members.map((row) => row.grantee_id)];
}

module.exports = { watchers };
