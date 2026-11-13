'use strict';

// Before camera upload sends a photo: is an almost identical one (at least
// 95 %, so 64-bit dHashes at most 3 bits apart) already among the account's
// own photos? The phone makes the hash the same way as Duplicates does
// (services/ai/duplicates.js). Only photos hashed already can match; the
// background worker there hashes the rest soon after they are indexed.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { wakeDuplicateHasher } = require('../ai/duplicates');
const { catchUp } = require('./indexer');

const MAX_DISTANCE = 3;
const MAX_HASHES = 200;
const HASH_PATTERN = /^[0-9a-f]{16}$/;

function popcount32(value) {
  let v = value - ((value >>> 1) & 0x55555555);
  v = (v & 0x33333333) + ((v >>> 2) & 0x33333333);
  return (((v + (v >>> 4)) & 0x0f0f0f0f) * 0x01010101) >>> 24;
}

function parseHash(hex) {
  return [parseInt(hex.slice(0, 8), 16) >>> 0, parseInt(hex.slice(8, 16), 16) >>> 0];
}

function distance(a, b) {
  return popcount32((a[0] ^ b[0]) >>> 0) + popcount32((a[1] ^ b[1]) >>> 0);
}

function readHashes(body) {
  const list = Array.isArray(body?.hashes) ? body.hashes : (body?.hash !== undefined ? [body.hash] : null);
  if (!list || list.length === 0) throw httpError(400, 'Send a hash or a list of hashes.', 'INVALID_HASH');
  if (list.length > MAX_HASHES) throw httpError(400, `At most ${MAX_HASHES} hashes at a time.`, 'TOO_MANY_HASHES');
  return [...new Set(list.map((hash) => {
    const hex = typeof hash === 'string' ? hash.trim().toLowerCase() : '';
    if (!HASH_PATTERN.test(hex)) throw httpError(400, 'A hash is 16 hexadecimal characters.', 'INVALID_HASH');
    return hex;
  }))];
}

/** `{ matches: [{ hash, nodeId, similarity }] }`, the closest own photo per asked hash. */
function findSimilar(userId, body) {
  const asked = readHashes(body);
  catchUp(userId);
  wakeDuplicateHasher();
  // Own live photos (the Photos index leaves out vaults and the trash).
  const rows = db.prepare(`
    SELECT n.id, h.dhash FROM photo_items p
    JOIN nodes n ON n.id = p.node_id AND n.blob_id = p.blob_id
    JOIN image_hashes h ON h.node_id = n.id AND h.etag = n.etag
    WHERE p.user_id = ? AND p.kind = 'photo' AND h.dhash IS NOT NULL
      AND n.user_id = p.user_id AND n.vault_id IS NULL AND n.trashed_at IS NULL
    ORDER BY n.created_at, n.id
  `).all(userId);
  const known = rows.map((row) => ({ id: row.id, hash: parseHash(row.dhash) }));
  const matches = [];
  for (const hex of asked) {
    const hash = parseHash(hex);
    let best = null;
    for (const photo of known) {
      const d = distance(hash, photo.hash);
      if (d <= MAX_DISTANCE && (!best || d < best.d)) {
        best = { d, id: photo.id };
        if (d === 0) break;
      }
    }
    if (best) {
      matches.push({ hash: hex, nodeId: best.id, similarity: Math.round(((64 - best.d) / 64) * 1000) / 10 });
    }
  }
  return { matches };
}

module.exports = { findSimilar };
