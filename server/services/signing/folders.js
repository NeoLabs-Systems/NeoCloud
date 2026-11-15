'use strict';

// Folders the signing feature may put files in: live, the account's own and
// never a vault (a vault's files are not for a background job to write).

const db = require('../../db/database');
const { getLiveNode } = require('../files/tree');

/** The folder when it can take a signed file, else null. */
function usableFolder(userId, folderId) {
  if (!folderId) return null;
  try {
    const folder = getLiveNode(userId, folderId, { type: 'folder' });
    const isVault = folder.vault_id || db.prepare('SELECT 1 FROM vaults WHERE folder_id = ?').get(folder.id);
    return isVault ? null : folder;
  } catch {
    return null;
  }
}

module.exports = { usableFolder };
