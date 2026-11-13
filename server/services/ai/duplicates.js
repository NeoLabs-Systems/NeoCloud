'use strict';

// Duplicate files of an account, for Review › Duplicates. No AI involved:
// it runs whether AI is on or not.
//
//   exact    files with the same content (the blob's SHA-256, which is
//            also the etag),
//            any type, of the account's own live files (no vault, no trash);
//   similar  photos whose 64-bit perceptual hashes (dHash of a 9×8
//            grayscale thumbnail) differ in at most 3 bits (about 95 %).
//
// The hashes are made by a low-priority background worker (started with the
// server, for every account, whether the page is open or not) and kept per
// content (image_hashes, keyed by node id and etag). Photos come from the
// Photos index (photo_items), which already leaves out vaults and the trash.
// Exact groups need no hash and are always complete.

const os = require('os');
const { performance } = require('perf_hooks');
const sharp = require('sharp');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');
const { LIVE_NODES_CTE, NODE_COLUMNS, serializeNode, withFolderPaths } = require('../files/tree');
const { openBlob } = require('../storage/blob_store');
const { catchUp } = require('../photos/indexer');

const logger = createServiceLogger('Duplicates');

// At most 3 of 64 bits apart: 95 % alike or more. Every photo in a group
// must be that close to every other one (no chains of "A like B like C").
const MAX_DISTANCE = 3;
// A hash with this few (or this many) set bits comes from a nearly flat
// picture (black frame, white page, one colour): such pictures all look
// alike to a dHash, so they are never called similar.
const MIN_DETAIL_BITS = 10;
// Pictures whose shapes differ this much (width/height ratio) aren't
// variations of one photo.
const MAX_ASPECT_DIFFERENCE = 0.1;
const MAX_GROUPS = 500;
const MAX_SOURCE_BYTES = 200 * 1024 * 1024;
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

/** The account's photos that can be compared, and how many of them have a hash. */
function hashProgress(userId) {
  const row = db.prepare(`
    SELECT COUNT(*) AS total, COALESCE(SUM(EXISTS (
      SELECT 1 FROM image_hashes h WHERE h.node_id = n.id AND h.etag = n.etag
    )), 0) AS hashed
    FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.user_id = ? AND p.kind = 'photo' AND n.blob_id = p.blob_id AND n.size <= ?
      AND n.mime IN (SELECT value FROM json_each(?))
  `).get(userId, MAX_SOURCE_BYTES, JSON.stringify(HASHABLE_MIMES));
  return { total: row.total, hashed: row.hashed, pending: row.total - row.hashed };
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

// --- The background worker ------------------------------------------------------------
//
// Sweeps photo_items in node-id order, a few files at a time, and hashes the
// photos without a hash for their current content. Between batches it
// yields and, while the server is busy (the event loop is mostly occupied
// with requests, or the machine's load is above its CPU count), waits. After
// a sweep it idles and starts the next one when the Photos index changed
// (a new, changed or freshly read photo), or after a while regardless.

const WORKER_BATCH = 20;
const WORKER_CONCURRENCY = Math.max(1, Math.min(2, Math.floor((os.cpus().length || 1) / 4)));
const FIRST_PASS_MS = 15 * 1000;
const IDLE_MS = 10 * 1000;
const BATCH_GAP_MS = 250;
const BUSY_WAIT_MS = 5 * 1000;
const FULL_SWEEP_MS = 10 * 60 * 1000;
const BUSY_LOOP_UTILIZATION = 0.5;

const HASHABLE_JSON = JSON.stringify(HASHABLE_MIMES);

function unhashedAfter(afterId, limit) {
  return db.prepare(`
    SELECT p.user_id, n.id, n.etag, n.size, n.blob_id FROM photo_items p JOIN nodes n ON n.id = p.node_id
    WHERE p.node_id > ? AND p.kind = 'photo' AND n.blob_id = p.blob_id AND n.size <= ?
      AND n.mime IN (SELECT value FROM json_each(?))
      AND NOT EXISTS (SELECT 1 FROM image_hashes h WHERE h.node_id = n.id AND h.etag = n.etag)
    ORDER BY p.node_id
    LIMIT ?
  `).all(afterId, MAX_SOURCE_BYTES, HASHABLE_JSON, limit);
}

/** Changes whenever photos are added, changed or read by the Photos index. */
function indexSignature() {
  const row = db.prepare(`
    SELECT (SELECT COALESCE(MAX(seq), 0) FROM changes) AS seq,
      (SELECT COUNT(*) FROM photo_items) AS items,
      (SELECT COUNT(*) FROM photo_items WHERE probed = 0) AS unprobed
  `).get();
  return `${row.seq}:${row.items}:${row.unprobed}`;
}

const sleep = (ms) => new Promise((resolve) => { setTimeout(resolve, ms).unref(); });

/** Busy: the last moments' event loop mostly ran requests, or the CPUs are full. */
async function serverBusy() {
  const before = performance.eventLoopUtilization();
  await sleep(BATCH_GAP_MS);
  const loop = performance.eventLoopUtilization(before).utilization;
  const load = os.loadavg()[0] / (os.cpus().length || 1);
  return loop > BUSY_LOOP_UTILIZATION || load > 1;
}

const hasher = {
  stopped: true,
  stopping: false,
  ticking: false,
  timer: null,
  cursor: '',
  sweeping: false,
  signature: null,
  sweptAt: 0,
  current: Promise.resolve(0),
};

async function hashBatch(rows) {
  let next = 0;
  const lane = async () => {
    while (next < rows.length && !hasher.stopping) {
      const row = rows[next];
      next += 1;
      await hashOne(row.user_id, row);
    }
  };
  await Promise.all(Array.from({ length: Math.min(WORKER_CONCURRENCY, rows.length) }, lane));
}

/**
 * One batch of the sweep: hashes up to WORKER_BATCH photos; returns how many
 * (0 when the sweep reached the end).
 */
async function hashPass() {
  if (!hasher.sweeping) {
    hasher.signature = indexSignature();
    hasher.cursor = '';
    hasher.sweeping = true;
  }
  const rows = unhashedAfter(hasher.cursor, WORKER_BATCH);
  if (rows.length === 0) {
    hasher.sweeping = false;
    hasher.sweptAt = Date.now();
    return 0;
  }
  hasher.cursor = rows[rows.length - 1].id;
  await hashBatch(rows);
  return rows.length;
}

function runHashPass() {
  hasher.current = hasher.current.catch(() => 0).then(hashPass);
  return hasher.current;
}

/** Hashes every photo still without a hash; returns how many it hashed (tests, tools). */
async function runDuplicateHashingUntilIdle() {
  hasher.sweeping = false;
  let total = 0;
  for (;;) {
    const done = await runHashPass();
    total += done;
    if (done === 0) return total;
  }
}

function scheduleHasher(ms) {
  if (hasher.stopped) return;
  clearTimeout(hasher.timer);
  hasher.timer = setTimeout(hasherTick, ms);
  hasher.timer.unref();
}

async function hasherTick() {
  // A wake while a tick runs: the running tick schedules the next one.
  if (hasher.stopped || hasher.ticking) return;
  hasher.ticking = true;
  try {
    await hasherStep();
  } finally {
    hasher.ticking = false;
  }
}

async function hasherStep() {
  // Idle between sweeps: only look again when the Photos index moved.
  if (!hasher.sweeping && hasher.signature !== null && Date.now() - hasher.sweptAt < FULL_SWEEP_MS) {
    let signature = null;
    try {
      signature = indexSignature();
    } catch (err) {
      logger.error('Duplicates hashing check failed:', err.message);
    }
    if (signature === hasher.signature) {
      scheduleHasher(IDLE_MS);
      return;
    }
  }
  if (await serverBusy()) {
    scheduleHasher(BUSY_WAIT_MS);
    return;
  }
  if (hasher.stopped) return;
  let done = 0;
  try {
    done = await runHashPass();
  } catch (err) {
    hasher.sweeping = false;
    logger.error('Duplicates hashing failed:', err.message);
  }
  // serverBusy() already waited BATCH_GAP_MS: that is the yield between batches.
  scheduleHasher(done > 0 ? 0 : IDLE_MS);
}

/** Starts the background hashing; returns a function that stops it. */
function startDuplicateHasher() {
  hasher.stopped = false;
  hasher.stopping = false;
  hasher.signature = null;
  hasher.sweeping = false;
  scheduleHasher(FIRST_PASS_MS);
  return stopDuplicateHasher;
}

/** Stops the background hashing and waits for the file it is on. */
async function stopDuplicateHasher() {
  hasher.stopped = true;
  hasher.stopping = true;
  clearTimeout(hasher.timer);
  hasher.timer = null;
  await hasher.current.catch(() => 0);
  hasher.stopping = false;
}

/** Looks for new photos now instead of after the idle wait (still yields to a busy server). */
function wakeDuplicateHasher() {
  // The tick compares the Photos index first, so this is cheap to call often.
  if (hasher.stopped || hasher.sweeping || hasher.ticking) return;
  scheduleHasher(0);
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

// Seven chunks of the 64 bits: two hashes at most 3 bits apart agree on at
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
  // Candidate pairs: only hashes sharing a chunk can be this few bits apart.
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
 * The account's duplicate groups, the most space to win first, from what is
 * known now. `pending`: photos not compared yet (the background worker is
 * hashing them); `hashed` of `total` photos are.
 */
function listDuplicates(userId) {
  catchUp(userId);
  const { pending, hashed, total } = hashProgress(userId);
  if (pending > 0) wakeDuplicateHasher();
  const dismissed = dismissedSets(userId);
  const groups = [...exactGroups(userId), ...similarGroups(userId)]
    .filter((group) => !dismissed.some((set) => group.members.every((row) => set.has(row.id))))
    .map((group) => ({
      ...group,
      wasted: group.members.filter((row) => row.id !== group.keepId).reduce((sum, row) => sum + row.size, 0),
    }))
    .sort((a, b) => b.wasted - a.wasted)
    .slice(0, MAX_GROUPS);
  return { groups: serializeGroups(userId, groups), pending, hashed, total };
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

module.exports = {
  dHash,
  dismissDuplicates,
  listDuplicates,
  runDuplicateHashingUntilIdle,
  startDuplicateHasher,
  stopDuplicateHasher,
  wakeDuplicateHasher,
};
