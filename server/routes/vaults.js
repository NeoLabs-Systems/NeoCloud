'use strict';

// End-to-end encrypted vault folders: the wrapped key and its parameters.
// Reached with a browser session or a device token.

const express = require('express');
const { requireAuthOrDevice } = require('../middleware/auth');
const vaults = require('../services/files/vaults');
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
}), { status: 201 }));

router.get('/:id', jsonHandler((req) => vaults.getVault(req.userId, req.params.id)));

router.put('/:id/key', jsonHandler((req) => vaults.updateVaultKey(req.userId, req.params.id, {
  salt: req.body?.salt,
  iterations: req.body?.iterations,
  wrappedKey: req.body?.wrappedKey,
})));

module.exports = router;
