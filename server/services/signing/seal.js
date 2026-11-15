'use strict';

// The certificate that seals finished PDFs. It is generated once, on the
// first finalization, as a self-signed bundle kept in the database encrypted
// with the master key. Viewers show it as an unknown signer, but any change
// to a sealed file breaks the signature: it proves integrity, not identity.
// NEOCLOUD_SIGNING_SEAL=false turns sealing off.

const db = require('../../db/database');
const { readEnvBool } = require('../../../runtime/env');
const { decryptValue, encryptValue } = require('../crypto/keys');
const { nowIso } = require('./store');

const REASON = 'Signed with NeoCloud';

function createKey() {
  const { generateSelfSignedP12 } = require('./pdf');
  const { p12Buffer, passphrase } = generateSelfSignedP12({ commonName: 'NeoCloud document seal' });
  // Two requests may race to create it; the first row wins.
  db.prepare('INSERT OR IGNORE INTO sign_seal_keys (id, p12_enc, passphrase_enc, created_at) VALUES (1, ?, ?, ?)')
    .run(encryptValue(p12Buffer.toString('base64')), encryptValue(passphrase), nowIso());
  return db.prepare('SELECT * FROM sign_seal_keys WHERE id = 1').get();
}

/** The `signer` option of buildFinalPdf, or null when sealing is off. */
function getSealSigner() {
  if (!readEnvBool('NEOCLOUD_SIGNING_SEAL', true)) return null;
  const row = db.prepare('SELECT * FROM sign_seal_keys WHERE id = 1').get() || createKey();
  return {
    p12Buffer: Buffer.from(decryptValue(row.p12_enc), 'base64'),
    passphrase: decryptValue(row.passphrase_enc),
    reason: REASON,
    name: 'NeoCloud',
  };
}

module.exports = { getSealSigner };
