'use strict';

// Saved signatures and initials of an account: PNGs (re-encoded with sharp
// like every signing upload) in the account's own encrypted blob storage, up
// to 5 per kind. Only the owner reads them.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { releaseBlobs } = require('../storage/blob_refs');
const { deleteBlobFiles } = require('../storage/blob_store');
const { remainingBytes } = require('../storage/quota');
const { adoptBlob, discard, storeBlob } = require('./blobs');
const { LIMITS } = require('./constants');
const { METHODS, typeOf } = require('./field_types');
const { singleLine } = require('./plain_text');
const { nowIso } = require('./store');
const { readBody, toPng } = require('./uploads');

const KINDS = ['signature', 'initials'];
const MAX_PER_KIND = 5;
const MAX_LABEL = 60;

function invalid(message) {
  return httpError(400, message, 'INVALID_IMAGE');
}

function view(row) {
  return {
    id: row.id, kind: row.kind, method: row.method, label: row.label, isDefault: row.is_default === 1, createdAt: row.created_at,
  };
}

/** The account's saved images, signatures first, oldest first. */
function signaturesOf(userId) {
  return db.prepare('SELECT * FROM sign_profile_images WHERE user_id = ? ORDER BY kind DESC, created_at, id').all(userId).map(view);
}

/** The account's own image row, or 404 (another account's id answers the same). */
function imageRow(userId, imageId) {
  const row = db.prepare('SELECT * FROM sign_profile_images WHERE id = ? AND user_id = ?').get(String(imageId || ''), userId);
  if (!row) throw httpError(404, 'Image not found', 'NOT_FOUND');
  return row;
}

function cleanLabel(value) {
  const label = typeof value === 'string' ? singleLine(value) : '';
  if (label.length > MAX_LABEL) throw invalid(`A label has at most ${MAX_LABEL} characters.`);
  return label;
}

function cleanQuery(query) {
  const kind = String(query.kind || '');
  if (!KINDS.includes(kind)) throw invalid('kind is signature or initials.');
  const method = query.method === undefined ? 'draw' : String(query.method);
  if (!METHODS.includes(method)) throw invalid(`method is one of: ${METHODS.join(', ')}.`);
  const typed = method === 'type' ? singleLine(String(query.text || '')).slice(0, LIMITS.maxNameLength) : '';
  return {
    kind, method, typed: typed || null, label: cleanLabel(query.label || ''), makeDefault: query.default === '1',
  };
}

function makeDefault(userId, kind, imageId) {
  db.prepare('UPDATE sign_profile_images SET is_default = (id = ?) WHERE user_id = ? AND kind = ?').run(imageId, userId, kind);
}

/** Saves the request body (a picture) as a new signature or initials. Resolves with the image as listed. */
async function addImage(userId, req) {
  const options = cleanQuery(req.query);
  const count = db.prepare('SELECT COUNT(*) AS count FROM sign_profile_images WHERE user_id = ? AND kind = ?').get(userId, options.kind).count;
  if (count >= MAX_PER_KIND) throw httpError(409, `You can save ${MAX_PER_KIND} ${options.kind === 'initials' ? 'initials' : 'signatures'}.`, 'IMAGE_LIMIT');
  const png = await toPng(await readBody(req, LIMITS.maxImageBytes), typeOf(options.kind).maxPixels);
  if (png.data.length > remainingBytes(userId)) throw httpError(413, 'Not enough storage left for this.', 'QUOTA_EXCEEDED');
  const stored = await storeBlob(userId, png.data);
  const id = crypto.randomUUID();
  try {
    db.transaction(() => {
      adoptBlob(userId, stored.blob);
      db.prepare(`
        INSERT INTO sign_profile_images (id, user_id, kind, method, label, typed_text, is_default, blob_id, size, created_at)
        VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?)
      `).run(id, userId, options.kind, options.method, options.label, options.typed, stored.blob.blobId, png.data.length, nowIso());
      if (options.makeDefault || count === 0) makeDefault(userId, options.kind, id);
    })();
  } catch (err) {
    await discard(stored.path);
    throw err;
  }
  return view(imageRow(userId, id));
}

/** Renames an image or makes it the default of its kind. */
function updateImage(userId, imageId, body) {
  const row = imageRow(userId, imageId);
  const input = body && typeof body === 'object' ? body : {};
  if (input.isDefault !== undefined && typeof input.isDefault !== 'boolean') throw invalid('isDefault must be true or false.');
  db.transaction(() => {
    if (input.label !== undefined) {
      db.prepare('UPDATE sign_profile_images SET label = ? WHERE id = ?').run(cleanLabel(input.label), row.id);
    }
    if (input.isDefault === true) makeDefault(userId, row.kind, row.id);
    if (input.isDefault === false) db.prepare('UPDATE sign_profile_images SET is_default = 0 WHERE id = ?').run(row.id);
  })();
  return view(imageRow(userId, row.id));
}

/** Removes an image; the newest of its kind becomes the default when it was one. */
async function deleteImage(userId, imageId) {
  const row = imageRow(userId, imageId);
  const orphaned = db.transaction(() => {
    db.prepare('DELETE FROM sign_profile_images WHERE id = ?').run(row.id);
    if (row.is_default === 1) {
      const next = db.prepare('SELECT id FROM sign_profile_images WHERE user_id = ? AND kind = ? ORDER BY created_at DESC, id DESC LIMIT 1')
        .get(userId, row.kind);
      if (next) makeDefault(userId, row.kind, next.id);
    }
    return releaseBlobs([row.blob_id]);
  })();
  await deleteBlobFiles(userId, orphaned);
  return { success: true };
}

/**
 * The images of a kind that suit a signature field, best first: the default,
 * then the newest. `methods` limits to the ways the field allows.
 */
function candidatesFor(userId, kind, methods) {
  const rows = db.prepare(`
    SELECT * FROM sign_profile_images WHERE user_id = ? AND kind = ? ORDER BY is_default DESC, created_at DESC, id DESC
  `).all(userId, kind);
  return methods ? rows.filter((row) => methods.includes(row.method)) : rows;
}

module.exports = { MAX_PER_KIND, addImage, candidatesFor, deleteImage, imageRow, signaturesOf, updateImage };
