'use strict';

// Filling a party's fields from the signer's profile. `compute` says what
// would go where (never into a field that already has a value, a default of
// the sender, or that is read-only); `apply` writes it, checked by the same
// per-type validation as anything a signer types.

const sharp = require('sharp');
const db = require('../../db/database');
const { remainingBytes } = require('../storage/quota');
const { discard, readBlobBuffer, storeBlob } = require('./blobs');
const { normalizeValue, typeOf } = require('./field_types');
const { fillKind, matchName, valueOfSource } = require('./prefill_match');
const { prefillSource } = require('./profile');
const { candidatesFor } = require('./profile_images');
const { parseJson } = require('./store');
const { saveBlobValue, saveTypedValue } = require('./values');

const SOURCE = 'profile';

// The party's own fields that can still take a value: not read-only, not given a default, nothing entered.
function openFields(documentId, partyId) {
  return db.prepare(`
    SELECT f.* FROM sign_fields f LEFT JOIN sign_values v ON v.field_id = f.id
    WHERE f.document_id = ? AND f.party_id = ? AND f.readonly = 0 AND f.default_value IS NULL AND v.id IS NULL
    ORDER BY f.position
  `).all(documentId, partyId).map((row) => ({ ...row, prefs: parseJson(row.prefs_json, {}) }));
}

const todayUtc = () => new Date().toISOString().slice(0, 10);

// The checked text a field would get, or null when the profile has nothing valid for it.
function textFor(field, kind, details, today) {
  let raw = today;
  if (kind === 'named') {
    const source = matchName(field.name, details.presets);
    raw = source ? valueOfSource(source, details) : '';
  }
  if (!raw) return null;
  try {
    const value = normalizeValue(field, raw);
    return value === '' || value === null ? null : value;
  } catch {
    return null;
  }
}

function imageFor(userId, field) {
  const [best] = candidatesFor(userId, field.type, field.prefs.methods);
  return best || null;
}

/** The profile's answer for each fillable field: [{ fieldId, name, type, source, value? | imageId? }]. */
function computePrefill(userId, document, party, { today = todayUtc() } = {}) {
  const { details, autoFill } = prefillSource(userId);
  if (!autoFill) return [];
  const items = [];
  for (const field of openFields(document.id, party.id)) {
    const kind = fillKind(field);
    const base = { fieldId: field.id, name: field.name, type: field.type, source: SOURCE };
    if (kind === 'image') {
      const image = imageFor(userId, field);
      if (image) items.push({ ...base, imageId: image.id });
    } else if (kind) {
      const value = textFor(field, kind, details, today);
      if (value !== null) items.push({ ...base, value });
    }
  }
  return items;
}

// A saved picture becomes the field's value: its bytes move from the signer's storage to the document owner's.
async function applyImage(userId, document, party, field, imageId) {
  const image = db.prepare('SELECT * FROM sign_profile_images WHERE id = ? AND user_id = ?').get(imageId, userId);
  if (!image) return false;
  if (image.size > remainingBytes(document.owner_id)) return false;
  const bytes = await readBlobBuffer(userId, image.blob_id, image.size);
  const { width, height } = await sharp(bytes).metadata();
  const stored = await storeBlob(document.owner_id, bytes);
  const meta = {
    kind: 'image', mime: 'image/png', width, height, method: image.method, ...(image.typed_text ? { text: image.typed_text } : {}), sha256: stored.blob.sha256,
  };
  try {
    await saveBlobValue(document, party, field, { blob: stored.blob, meta }, { needConsent: false });
    return true;
  } catch (err) {
    await discard(stored.path);
    throw err;
  }
}

/** Writes the items into the party's fields. Resolves with the ids of the fields that were filled; one that fails is skipped. */
async function applyPrefill(userId, document, party, items) {
  const fields = new Map(openFields(document.id, party.id).map((field) => [field.id, field]));
  const applied = [];
  for (const item of items) {
    const field = fields.get(item.fieldId);
    if (!field) continue;
    try {
      if (typeOf(field.type).storage === 'blob') {
        if (await applyImage(userId, document, party, field, item.imageId)) applied.push(field.id);
      } else {
        saveTypedValue(document, party, field, item.value, { needConsent: false });
        applied.push(field.id);
      }
    } catch {
      // The document changed under us (closed, the party finished): nothing more to fill.
    }
  }
  return applied;
}

module.exports = { applyPrefill, computePrefill };
