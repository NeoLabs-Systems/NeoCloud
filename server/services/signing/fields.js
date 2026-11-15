'use strict';

// The boxes on the pages. Edited as one list (replace-all) while the
// document is a draft or sent and still unopened; every entry is validated
// against its type in field_types.js, nothing the client sends is trusted.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');
const { LIMITS } = require('./constants');
const { isTypeName, normalizeDefault, normalizeFieldPrefs, typeOf } = require('./field_types');
const { singleLine } = require('./plain_text');
const { listFields, listParties, nowIso } = require('./store');
const engine = require('./pdf_engine');

const logger = createServiceLogger('Signing');
const MIN_SIDE = 0.005;
const TOLERANCE = 1e-4;

function invalid(message) {
  return httpError(400, message, 'INVALID_FIELD');
}

const round = (value) => Math.round(value * 100000) / 100000;

// One box: page counted from 0, the rest as fractions of the displayed page.
function normalizeArea(area, pageCount) {
  if (!area || typeof area !== 'object') throw invalid('An area must be an object.');
  const { page, x, y, w, h } = area;
  if (!Number.isInteger(page) || page < 0 || page >= pageCount) throw invalid('An area lies on a page the document does not have.');
  for (const value of [x, y, w, h]) {
    if (typeof value !== 'number' || !Number.isFinite(value)) throw invalid('Area values must be numbers.');
  }
  if (x < 0 || y < 0 || w < MIN_SIDE || h < MIN_SIDE || x + w > 1 + TOLERANCE || y + h > 1 + TOLERANCE) {
    throw invalid('An area must lie inside its page.');
  }
  return { page, x: round(x), y: round(y), w: round(Math.min(w, 1 - x)), h: round(Math.min(h, 1 - y)) };
}

function normalizeAreas(areas, pageCount) {
  if (!Array.isArray(areas) || areas.length < 1 || areas.length > LIMITS.maxAreasPerField) {
    throw invalid(`A field has 1 to ${LIMITS.maxAreasPerField} areas.`);
  }
  return areas.map((area) => normalizeArea(area, pageCount));
}

/** One field as it will be stored, or a 400. `partyIds` are the document's parties. */
function normalizeField(input, { pageCount, partyIds }) {
  if (!input || typeof input !== 'object') throw invalid('Each field must be an object.');
  if (!isTypeName(input.type)) throw invalid(`Unknown field type "${input.type}".`);
  if (!partyIds.has(input.partyId)) throw invalid('A field belongs to a party of this document.');
  const type = typeOf(input.type);
  const name = input.name === undefined || input.name === null ? '' : input.name;
  if (typeof name !== 'string' || name.length > LIMITS.maxNameLength) throw invalid(`A field name has at most ${LIMITS.maxNameLength} characters.`);
  for (const flag of ['required', 'readonly']) {
    if (input[flag] !== undefined && typeof input[flag] !== 'boolean') throw invalid(`${flag} must be true or false.`);
  }
  const prefs = normalizeFieldPrefs(input.type, input.prefs);
  const readonly = type.storage === 'static' ? true : input.readonly === true;
  if (readonly && type.storage === 'blob') throw invalid('A signature, image or file field can’t be read-only.');
  return {
    type: input.type,
    partyId: input.partyId,
    name: singleLine(name),
    // A read-only field is filled in by the sender, so there is nothing to require.
    required: readonly ? false : input.required !== false,
    readonly,
    prefs,
    defaultValue: normalizeDefault(input.type, prefs, input.defaultValue),
    areas: normalizeAreas(input.areas, pageCount),
  };
}

function normalizeAll(input, document) {
  if (!Array.isArray(input) || input.length > LIMITS.maxFields) throw invalid(`A document has at most ${LIMITS.maxFields} fields.`);
  const partyIds = new Set(listParties(document.id).map((party) => party.id));
  return input.map((entry) => ({ ...normalizeField(entry, { pageCount: document.page_count, partyIds }), id: entry?.id }));
}

function writeField(documentId, field, position) {
  const values = [
    field.partyId, field.type, field.name, field.required ? 1 : 0, field.readonly ? 1 : 0,
    field.defaultValue === null ? null : JSON.stringify(field.defaultValue),
    JSON.stringify(field.prefs), JSON.stringify(field.areas), position,
  ];
  if (field.existing) {
    db.prepare(`
      UPDATE sign_fields SET party_id = ?, type = ?, name = ?, required = ?, readonly = ?, default_value = ?,
        prefs_json = ?, areas_json = ?, position = ? WHERE id = ? AND document_id = ?
    `).run(...values, field.id, documentId);
  } else {
    db.prepare(`
      INSERT INTO sign_fields (party_id, type, name, required, readonly, default_value, prefs_json, areas_json, position, id, document_id)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(...values, field.id, documentId);
  }
}

/**
 * Replaces the document's fields with `input`. An entry whose id is one of the
 * document's fields updates it; any other entry becomes a new field with a
 * new id; fields left out are removed.
 */
function replaceFields(document, input) {
  const fields = normalizeAll(input, document);
  const existing = new Set(listFields(document.id).map((field) => field.id));
  const kept = new Set();
  for (const field of fields) {
    field.existing = typeof field.id === 'string' && existing.has(field.id);
    if (field.existing) {
      if (kept.has(field.id)) throw invalid('A field is listed twice.');
      kept.add(field.id);
    } else {
      field.id = crypto.randomUUID();
    }
  }
  db.transaction(() => {
    for (const id of existing) {
      if (!kept.has(id)) db.prepare('DELETE FROM sign_fields WHERE id = ?').run(id);
    }
    fields.forEach((field, position) => writeField(document.id, field, position));
  })();
  return listFields(document.id);
}

/**
 * Adds the PDF's own form fields (AcroForm) as new fields of `partyId`.
 * Entries the engine returns that don't pass validation are skipped.
 */
async function importFields(document, pdfBytes, partyId) {
  const found = await engine.importAcroFields(pdfBytes);
  const partyIds = new Set([partyId]);
  const room = LIMITS.maxFields - listFields(document.id).length;
  const imported = [];
  for (const draft of found) {
    if (imported.length >= room) break;
    try {
      imported.push(normalizeField({ ...draft, name: String(draft.name ?? '').slice(0, LIMITS.maxNameLength), partyId }, { pageCount: document.page_count, partyIds }));
    } catch (err) {
      logger.debug(`Skipped an imported form field: ${err.message}`);
    }
  }
  const start = listFields(document.id).length;
  db.transaction(() => {
    imported.forEach((field, index) => writeField(document.id, { ...field, id: crypto.randomUUID(), existing: false }, start + index));
    db.prepare('UPDATE sign_documents SET updated_at = ? WHERE id = ?').run(nowIso(), document.id);
  })();
  return imported.length;
}

module.exports = { importFields, replaceFields };
