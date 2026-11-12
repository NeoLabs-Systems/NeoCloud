'use strict';

// Duplicate files of an account, for Review › Duplicates. No AI involved:
// it runs whether AI is on or not.
//
//   exact    files with the same content (the blob's SHA-256, which is
//            also the etag),
//            any type, of the account's own live files (no vault, no trash);
//   similar  photos whose 64-bit perceptual hashes (dHash of a 9×8
//            grayscale thumbnail) differ in at most 6 bits (about 90 %).
//
// The hashes are made in the background when the page asks and kept per
// content (image_hashes, keyed by node id and etag). Photos come from the
// Photos index (photo_items), which already leaves out vaults and the trash.

const sharp = require('sharp');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');
const { LIVE_NODES_CTE, NODE_COLUMNS, serializeNode, withFolderPaths } = require('../files/tree');
const { openBlob } = require('../storage/blob_store');
const { catchUp } = require('../photos/indexer');

const logger = createServiceLogger('Duplicates');

// At most 6 of 64 bits apart: 90 % alike or more. Every photo in a group
// must be that close to every other one (no chains of "A like B like C").
const MAX_DISTANCE = 6;
// A hash with this few (or this many) set bits comes from a nearly flat
// picture (black frame, white page, one colour): such pictures all look
// alike to a dHash, so they are never called similar.
const MIN_DETAIL_BITS = 10;
// Pictures whose shapes differ this much (width/height ratio) aren't
// variations of one photo.
const MAX_ASPECT_DIFFERENCE = 0.1;
const MAX_GROUPS = 500;
const MAX_SOURCE_BYTES = 200 * 1024 * 1024;
const HASH_BATCH = 50;
const MAX_DISMISS_IDS = 1000;
const HASHABLE_MIMES = [
  'image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/avif',
  'image/tiff', 'image/heic', 'image/heif', 'image/bmp',
];

// --- Hashing ----------------------------------------------------------------------------

async function streamToBuffer(stream) {
  const chunks = [];
  for await (const chunk of stream) chunks.push(chunk);
  return Buffer.concat(chunks);
}

/** dHash: each bit says whether a pixel is brighter than its right neighbour. */
async function dHash(buffer) {
  const pixels = await sharp(buffer, { failOn: 'none', limitInputPixels: 100_000_000 })
    .rotate()
    .flatten({ background: '#ffffff' })
    .grayscale()
    .resize(9, 8, { fit: 'fill' })
    .raw()
    .toBuffer();
  let hex = '';
  for (let y = 0; y < 8; y += 1) {
    let byte = 0;
    for (let x = 0; x < 8; x += 1) {
      byte = (byte << 1) | (pixels[y * 9 + x] > pixels[y * 9 + x + 1] ? 1 : 0);
    }
    hex += byte.toString(16).padStart(2, '0');
  }
  return hex;
}

function unhashed(userId, limit) {
  return db.prepare(`
    SELECT n.id, n.etag, n.size, n.blob_id FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.user_id = ? AND p.kind = 'photo' AND n.blob_id = p.blob_id AND n.size <= ?
      AND n.mime IN (SELECT value FROM json_each(?))
      AND NOT EXISTS (SELECT 1 FROM image_hashes h WHERE h.node_id = n.id AND h.etag = n.etag)
    LIMIT ?
  `).all(userId, MAX_SOURCE_BYTES, JSON.stringify(HASHABLE_MIMES), limit);
}

function pendingCount(userId) {
  return db.prepare(`
    SELECT COUNT(*) AS count FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.user_id = ? AND p.kind = 'photo' AND n.blob_id = p.blob_id AND n.size <= ?
      AND n.mime IN (SELECT value FROM json_each(?))
      AND NOT EXISTS (SELECT 1 FROM image_hashes h WHERE h.node_id = n.id AND h.etag = n.etag)
  `).get(userId, MAX_SOURCE_BYTES, JSON.stringify(HASHABLE_MIMES)).count;
}

async function hashOne(userId, row) {
  let hash = null;
  try {
    hash = await dHash(await streamToBuffer(openBlob(userId, row.blob_id, row.size)));
  } catch (err) {
    logger.warn(`Could not read an image for Duplicates: ${err.message}`);
  }
  db.prepare(`
    INSERT INTO image_hashes (node_id, user_id, etag, dhash) VALUES (?, ?, ?, ?)
    ON CONFLICT(node_id) DO UPDATE SET etag = excluded.etag, dhash = excluded.dhash
  `).run(row.id, userId, row.etag, hash);
}

// One background fill per account at a time.
const filling = new Map();

/** Hashes the account's photos that have no hash for their content yet. */
function fillHashes(userId) {
  if (filling.has(userId)) return filling.get(userId);
  const job = (async () => {
    for (;;) {
      const rows = unhashed(userId, HASH_BATCH);
      for (const row of rows) await hashOne(userId, row);
      if (rows.length < HASH_BATCH) return;
      await new Promise((resolve) => setImmediate(resolve));
    }
  })()
    .catch((err) => logger.error('Duplicates hashing failed:', err.message))
    .finally(() => filling.delete(userId));
  filling.set(userId, job);
  return job;
}

// --- Grouping ---------------------------------------------------------------------------

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

// Seven chunks of the 64 bits: two hashes at most 6 bits apart agree on at
// least one chunk, so only hashes sharing a chunk are compared.
const CHUNKS = [[0, 9], [9, 18], [18, 27], [27, 36], [36, 45], [45, 54], [54, 64]];

function chunkOf(hash, [from, to]) {
  let value = 0;
  for (let bit = from; bit < to; bit += 1) {
    const word = bit < 32 ? hash[0] : hash[1];
    value = value * 2 + ((word >>> (31 - (bit % 32))) & 1);
  }
  return value;
}

function exactGroups(userId) {
  // The content hash is the blob's SHA-256 (also the file's etag), as for
  // the Storage page's duplicates.
  const rows = db.prepare(`
    ${LIVE_NODES_CTE},
    live AS (
      SELECT nodes.id, b.sha256 FROM nodes JOIN blobs b ON b.id = nodes.blob_id
      WHERE nodes.user_id = @userId AND nodes.type = 'file' AND nodes.size > 0 AND nodes.vault_id IS NULL
        AND nodes.trashed_at IS NULL AND nodes.id NOT IN (SELECT id FROM hidden)
    ),
    repeated AS (SELECT sha256 FROM live GROUP BY sha256 HAVING COUNT(*) > 1)
    SELECT ${NODE_COLUMNS}, live.sha256 AS sha256, p.taken_at, p.width, p.height
    FROM live JOIN nodes ON nodes.id = live.id
    LEFT JOIN photo_items p ON p.node_id = nodes.id
    WHERE live.sha256 IN (SELECT sha256 FROM repeated)
    ORDER BY live.sha256, nodes.created_at, nodes.id
  `).all({ userId });
  const byHash = new Map();
  for (const row of rows) {
    if (!byHash.has(row.sha256)) byHash.set(row.sha256, []);
    byHash.get(row.sha256).push(row);
  }
  // The oldest copy is the one to keep (rows are oldest first).
  return [...byHash.values()].filter((members) => members.length > 1)
    .map((members) => ({ kind: 'exact', similarity: 100, members, keepId: members[0].id }));
}

function similarGroups(userId) {
  const rows = db.prepare(`
    SELECT ${NODE_COLUMNS}, p.taken_at, p.width, p.height, h.dhash FROM nodes
    JOIN photo_items p ON p.node_id = nodes.id AND p.blob_id = nodes.blob_id
    JOIN image_hashes h ON h.node_id = nodes.id AND h.etag = nodes.etag
    WHERE p.user_id = ? AND p.kind = 'photo' AND h.dhash IS NOT NULL
    ORDER BY nodes.created_at, nodes.id
  `).all(userId);
  // One photo per content: exact copies are their own group already.
  const seen = new Set();
  const photos = [];
  for (const row of rows) {
    if (seen.has(row.etag)) continue;
    seen.add(row.etag);
    photos.push({ row, hash: parseHash(row.dhash) });
  }
  const detailed = photos.filter((photo) => {
    const bits = popcount32(photo.hash[0]) + popcount32(photo.hash[1]);
    return bits >= MIN_DETAIL_BITS && bits <= 64 - MIN_DETAIL_BITS;
  });
  const aspect = (row) => (row.width && row.height ? row.width / row.height : null);
  const alike = (a, b) => {
    if (distance(a.hash, b.hash) > MAX_DISTANCE) return false;
    const ra = aspect(a.row);
    const rb = aspect(b.row);
    return ra == null || rb == null || Math.abs(ra - rb) / Math.max(ra, rb) <= MAX_ASPECT_DIFFERENCE;
  };
  // Candidate pairs: only hashes sharing a chunk can be 6 bits apart.
  const neighbours = detailed.map(() => new Set());
  for (const chunk of CHUNKS) {
    const buckets = new Map();
    detailed.forEach((photo, index) => {
      const key = chunkOf(photo.hash, chunk);
      if (!buckets.has(key)) buckets.set(key, []);
      buckets.get(key).push(index);
    });
    for (const bucket of buckets.values()) {
      for (let i = 0; i < bucket.length; i += 1) {
        for (let j = i + 1; j < bucket.length; j += 1) {
          if (alike(detailed[bucket[i]], detailed[bucket[j]])) {
            neighbours[bucket[i]].add(bucket[j]);
            neighbours[bucket[j]].add(bucket[i]);
          }
        }
      }
    }
  }
  // Groups where every photo is alike to every other (complete linkage),
  // built greedily from the photo with the most close matches.
  const assigned = new Set();
  const order = detailed.map((_, index) => index).sort((a, b) => neighbours[b].size - neighbours[a].size);
  const byRoot = new Map();
  for (const seed of order) {
    if (assigned.has(seed) || neighbours[seed].size === 0) continue;
    const members = [seed];
    for (const candidate of neighbours[seed]) {
      if (assigned.has(candidate)) continue;
      if (members.every((member) => neighbours[candidate].has(member))) members.push(candidate);
    }
    if (members.length < 2) continue;
    for (const member of members) assigned.add(member);
    byRoot.set(seed, members.map((index) => detailed[index]));
  }
  const groups = [];
  for (const members of byRoot.values()) {
    if (members.length < 2) continue;
    // The largest picture is the one to keep; then the bigger file, then the older.
    const keep = [...members].sort((a, b) => (
      ((b.row.width || 0) * (b.row.height || 0)) - ((a.row.width || 0) * (a.row.height || 0))
      || b.row.size - a.row.size
    ))[0];
    const worst = Math.max(...members.map((member) => distance(member.hash, keep.hash)));
    groups.push({
      kind: 'similar',
      similarity: Math.round(((64 - worst) / 64) * 100),
      members: members.map((member) => member.row),
      keepId: keep.row.id,
    });
  }
  return groups;
}

function dismissedSets(userId) {
  return db.prepare('SELECT node_ids_json FROM duplicate_dismissals WHERE user_id = ?').all(userId)
    .map((row) => new Set(JSON.parse(row.node_ids_json)));
}

function serializeGroups(userId, groups) {
  const nodes = withFolderPaths(userId, groups.flatMap((group) => group.members.map(serializeNode)));
  const byId = new Map(nodes.map((node) => [node.id, node]));
  return groups.map((group) => ({
    kind: group.kind,
    similarity: group.similarity,
    items: group.members.map((row) => ({
      node: byId.get(row.id),
      size: row.size,
      ...(row.taken_at ? { takenAt: row.taken_at } : {}),
      ...(row.width && row.height ? { width: row.width, height: row.height } : {}),
      keep: row.id === group.keepId,
    })),
  }));
}

/**
 * The account's duplicate groups, the most space to win first. `pending`:
 * photos not compared yet (their hashes are being made in the background).
 */
function listDuplicates(userId) {
  catchUp(userId);
  const pending = pendingCount(userId);
  if (pending > 0) void fillHashes(userId);
  const dismissed = dismissedSets(userId);
  const groups = [...exactGroups(userId), ...similarGroups(userId)]
    .filter((group) => !dismissed.some((set) => group.members.every((row) => set.has(row.id))))
    .map((group) => ({
      ...group,
      wasted: group.members.filter((row) => row.id !== group.keepId).reduce((sum, row) => sum + row.size, 0),
    }))
    .sort((a, b) => b.wasted - a.wasted)
    .slice(0, MAX_GROUPS);
  return { groups: serializeGroups(userId, groups), pending };
}

/** "Not duplicates": the group of these files is not shown again. */
function dismissDuplicates(userId, nodeIds) {
  if (!Array.isArray(nodeIds) || nodeIds.length < 2 || nodeIds.length > MAX_DISMISS_IDS
    || !nodeIds.every((id) => typeof id === 'string' && id)) {
    throw httpError(400, 'nodeIds must be a list of 2–1000 file ids.', 'INVALID_REQUEST');
  }
  const ids = [...new Set(nodeIds)];
  const owned = db.prepare(`
    SELECT COUNT(*) AS count FROM nodes WHERE user_id = ? AND id IN (SELECT value FROM json_each(?))
  `).get(userId, JSON.stringify(ids)).count;
  if (owned !== ids.length) throw httpError(404, 'File or folder not found', 'NOT_FOUND');
  db.prepare('INSERT INTO duplicate_dismissals (user_id, node_ids_json) VALUES (?, ?)').run(userId, JSON.stringify(ids));
  return { success: true };
}

module.exports = { dHash, dismissDuplicates, fillHashes, listDuplicates };
