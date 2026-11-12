'use strict';

// Key hierarchy:
//   NEOCLOUD_MASTER_KEY (env, 32 bytes hex)
//     ├─ HKDF "neocloud:dek-wrap"  → wraps each account's data key (DEK)
//     ├─ HKDF "neocloud:secrets"   → encrypts small server-side secrets (2FA seeds)
//     └─ HKDF "neocloud:backup"    → encrypts database snapshots sent to backup targets
//   DEK (32 random bytes per account) → encrypts that account's blobs
//
// Wrapped values are "v1:" + base64(iv | tag | ciphertext), AES-256-GCM.

const crypto = require('crypto');
const db = require('../../db/database');
const { isValidMasterKey } = require('../../../runtime/paths');

const WRAP_PREFIX = 'v1:';
const SECRET_PREFIX = 'enc:v1:';
const IV_LENGTH = 12;
const TAG_LENGTH = 16;

const derivedKeys = new Map();

function masterKey() {
  const hex = String(process.env.NEOCLOUD_MASTER_KEY || '').trim();
  if (!isValidMasterKey(hex)) {
    throw new Error('NEOCLOUD_MASTER_KEY is missing or malformed.');
  }
  return Buffer.from(hex, 'hex');
}

function deriveKey(purpose) {
  const hex = String(process.env.NEOCLOUD_MASTER_KEY || '').trim();
  const cacheKey = `${hex}:${purpose}`;
  if (!derivedKeys.has(cacheKey)) {
    const key = Buffer.from(crypto.hkdfSync('sha256', masterKey(), Buffer.alloc(0), purpose, 32));
    derivedKeys.set(cacheKey, key);
  }
  return derivedKeys.get(cacheKey);
}

// iv | tag | ciphertext, AES-256-GCM, as bytes.
function sealBytes(key, plaintext, aad) {
  const iv = crypto.randomBytes(IV_LENGTH);
  const cipher = crypto.createCipheriv('aes-256-gcm', key, iv);
  if (aad) cipher.setAAD(Buffer.from(aad));
  const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
  return Buffer.concat([iv, cipher.getAuthTag(), ciphertext]);
}

function openBytes(key, raw, aad) {
  const iv = raw.subarray(0, IV_LENGTH);
  const tag = raw.subarray(IV_LENGTH, IV_LENGTH + TAG_LENGTH);
  const ciphertext = raw.subarray(IV_LENGTH + TAG_LENGTH);
  const decipher = crypto.createDecipheriv('aes-256-gcm', key, iv);
  if (aad) decipher.setAAD(Buffer.from(aad));
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(ciphertext), decipher.final()]);
}

function seal(key, plaintext, aad) {
  return sealBytes(key, plaintext, aad).toString('base64');
}

function open(key, sealed, aad) {
  return openBytes(key, Buffer.from(sealed, 'base64'), aad);
}

function wrapDek(userId, dek) {
  return WRAP_PREFIX + seal(deriveKey('neocloud:dek-wrap'), dek, `user:${userId}`);
}

function unwrapDek(userId, wrapped) {
  if (!String(wrapped || '').startsWith(WRAP_PREFIX)) {
    throw new Error(`Data key of account ${userId} has an unknown format.`);
  }
  return open(deriveKey('neocloud:dek-wrap'), wrapped.slice(WRAP_PREFIX.length), `user:${userId}`);
}

/**
 * The account's data key, created on first use. The wrapped key is bound to
 * the account id, so it cannot be moved onto another account's row.
 */
function getUserDek(userId) {
  const row = db.prepare('SELECT wrapped_dek FROM users WHERE id = ?').get(userId);
  if (!row) throw new Error(`Account ${userId} does not exist.`);
  if (row.wrapped_dek) return unwrapDek(userId, row.wrapped_dek);

  const dek = crypto.randomBytes(32);
  const claimed = db.prepare('UPDATE users SET wrapped_dek = ? WHERE id = ? AND wrapped_dek IS NULL')
    .run(wrapDek(userId, dek), userId);
  if (claimed.changes === 1) return dek;
  // Another request created it first; use theirs.
  return getUserDek(userId);
}

function encryptValue(value) {
  return SECRET_PREFIX + seal(deriveKey('neocloud:secrets'), Buffer.from(String(value), 'utf8'));
}

function decryptValue(value) {
  const text = String(value || '');
  if (!text.startsWith(SECRET_PREFIX)) throw new Error('Value is not encrypted.');
  return open(deriveKey('neocloud:secrets'), text.slice(SECRET_PREFIX.length)).toString('utf8');
}

/**
 * Stops a start with a master key that is well-formed but not the one this
 * install's data keys were wrapped with (a typo, another server's .env, a
 * key replaced by hand). Running on would fail every download and give new
 * accounts keys the original master key can't open.
 */
function assertMasterKeyMatches() {
  const row = db.prepare('SELECT id, wrapped_dek FROM users WHERE wrapped_dek IS NOT NULL ORDER BY id LIMIT 1').get();
  if (!row) return;
  try {
    unwrapDek(row.id, row.wrapped_dek);
  } catch {
    throw new Error(
      'NEOCLOUD_MASTER_KEY does not match the key this data was encrypted with. Put the original key back '
      + '(from `neocloud keys export` or your copy of .env); nothing was changed.',
    );
  }
}

/** The key database snapshots are encrypted with before leaving the server. */
function getBackupKey() {
  return deriveKey('neocloud:backup');
}

module.exports = {
  assertMasterKeyMatches,
  decryptValue,
  encryptValue,
  getBackupKey,
  getUserDek,
  openBytes,
  sealBytes,
  unwrapDek,
  wrapDek,
};
