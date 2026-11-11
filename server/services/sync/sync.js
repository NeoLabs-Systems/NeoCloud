'use strict';

// What sync clients start from: a full snapshot of the live tree plus the
// journal cursor it corresponds to. After that they follow listChanges().

const db = require('../../db/database');
const { currentCursor } = require('../files/changes');
const { getRoot, liveSubtree, serializeNode } = require('../files/tree');

function treeSnapshot(userId) {
  return db.transaction(() => {
    const root = getRoot(userId);
    const cursor = currentCursor(userId);
    const nodes = liveSubtree(userId, root.id).map((row) => ({ ...serializeNode(row), path: row.relative_path }));
    return { cursor, rootId: root.id, nodes };
  })();
}

module.exports = { treeSnapshot };
