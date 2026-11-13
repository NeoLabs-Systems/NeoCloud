'use strict';

// End-to-end encrypted vault folders: the wrapped key and its parameters.
// Reached with a browser session or a device token.

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const vaults = require('../services/files/vaults');
const conversion = require('../services/files/vault_conversion');
const { ROOT_ALIAS } = require('../services/files/tree');
const { jsonHandler } = require('./_helpers/http');

const router = express.Router();
router.use(requireAuthOrDevice);

router.post('/', jsonHandler((req) => vaults.createVault(req.userId, {
  parentId: req.body?.parentId ?? ROOT_ALIAS,
  name: req.body?.name,
  salt: req.body?.salt,
  iterations: req.body?.iterations,
  wrappedKey: req.body?.wrappedKey,
  conflict: req.body?.conflict,
}), { status: 201 }));

// Converting a folder into a vault (the client encrypts and uploads).
router.get('/conversions/:folderId', jsonHandler((req) => conversion.conversionCheck(req.userId, req.params.folderId)));

router.post('/:id/replace', jsonHandler((req) => conversion.replaceFolderWithVault(req.userId, req.params.id, {
  folderId: req.body?.folderId,
  cursor: req.body?.cursor,
})));

router.get('/:id', jsonHandler((req) => vaults.getVault(req.userId, req.params.id)));

router.put('/:id/key', jsonHandler((req) => vaults.updateVaultKey(req.userId, req.params.id, {
  salt: req.body?.salt,
  iterations: req.body?.iterations,
  wrappedKey: req.body?.wrappedKey,
})));

module.exports = router;
