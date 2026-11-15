'use strict';

// Copying a file between NeoCloud and a mount (a download from one side
// streamed into an upload on the other). Folders are not copied in this
// version. Vault files stay out: they are encrypted in the browser and make
// no sense at a provider.

const { httpError } = require('../../utils/http_error');
const { validateName } = require('../../utils/file_names');
const { getLiveNode } = require('../files/tree');
const { openBlob } = require('../storage/blob_store');
const { uploadStream } = require('../transfer/uploads');
const { freeNameIn, openFile, withMount } = require('./browse');
const { joinPath, normalizePath } = require('./paths');

function vaultRefused() {
  return httpError(409, 'Items can’t be moved or copied into or out of a vault.', 'VAULT_BOUNDARY');
}

/** Copies the NeoCloud file `nodeId` into the mount's folder `folder`. The copy is renamed if the name is taken. */
async function copyToMount(userId, mountId, { nodeId, folder, name }) {
  const node = getLiveNode(userId, nodeId);
  if (node.type !== 'file') throw httpError(400, 'Only files can be copied to cloud storage.', 'NOT_A_FILE');
  if (node.vault_id) throw vaultRefused();
  const folderPath = normalizePath(folder);
  const fileName = validateName(name ?? node.name);
  return withMount(userId, mountId, { write: true }, async (adapter) => {
    const target = joinPath(folderPath, await freeNameIn(adapter, folderPath, fileName));
    const source = openBlob(userId, node.blob_id, node.size);
    try {
      await adapter.write(target, source, { size: node.size });
    } finally {
      source.destroy();
    }
    return { path: target };
  });
}

/** Copies the mount's file `path` into the NeoCloud folder `parentId`, counting against the account's quota. */
async function copyFromMount(userId, mountId, { path, parentId }) {
  const parent = getLiveNode(userId, parentId, { type: 'folder' });
  if (parent.vault_id) throw vaultRefused();
  const { info, stream, release } = await openFile(userId, mountId, path);
  try {
    return await uploadStream(userId, { parentId: parent.id, name: info.name, conflict: 'rename', contentLength: info.size }, stream);
  } finally {
    stream.destroy();
    await release();
  }
}

module.exports = { copyFromMount, copyToMount };
