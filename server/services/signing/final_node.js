'use strict';

// The final file of a completed document while it is still the one that was
// made: moved or renamed is fine, replaced, trashed or removed is not.

const { nodeById } = require('../files/tree');
const { DOCUMENT_STATUS } = require('./constants');

function liveFinalNode(document) {
  if (document.status !== DOCUMENT_STATUS.COMPLETED || !document.final_node_id) return null;
  const node = nodeById(document.final_node_id);
  return node && !node.trashed_at && node.etag === document.final_sha256 ? node : null;
}

module.exports = { liveFinalNode };
