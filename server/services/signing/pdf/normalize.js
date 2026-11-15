'use strict';

// The API layer stores snake_case rows; the clients speak camelCase. The
// engine accepts either so the caller can hand over rows as they are.
// Everything below is pure.

const crypto = require('crypto');

function pick(obj, ...names) {
  if (!obj) return undefined;
  for (const name of names) {
    if (obj[name] !== undefined && obj[name] !== null) return obj[name];
  }
  return undefined;
}

function toBytes(input) {
  if (Buffer.isBuffer(input)) return input;
  if (input instanceof Uint8Array) return Buffer.from(input.buffer, input.byteOffset, input.byteLength);
  if (input instanceof ArrayBuffer) return Buffer.from(input);
  return null;
}

function sha256Hex(bytes) {
  return crypto.createHash('sha256').update(bytes).digest('hex');
}

/** Date | ISO string | epoch ms | SQLite "YYYY-MM-DD HH:MM:SS" (UTC) -> Date, or null. */
function toDate(input) {
  if (input === undefined || input === null || input === '') return null;
  if (input instanceof Date) return Number.isNaN(input.getTime()) ? null : input;
  if (typeof input === 'number') return new Date(input);
  const text = String(input).trim();
  const sqlite = /^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})(\.\d+)?$/.exec(text);
  const date = new Date(sqlite ? `${sqlite[1]}T${sqlite[2]}${sqlite[3] || ''}Z` : text);
  return Number.isNaN(date.getTime()) ? null : date;
}

/** "2026-11-15 14:03:22 UTC", or '' when unknown. */
function formatUtc(input) {
  const date = toDate(input);
  if (!date) return '';
  return `${date.toISOString().slice(0, 19).replace('T', ' ')} UTC`;
}

function parseJsonObject(input) {
  if (!input) return {};
  if (typeof input === 'object') return input;
  try {
    const parsed = JSON.parse(input);
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

/** Field preferences under their canonical camelCase names (snake_case accepted). */
function normalizePrefs(raw) {
  const prefs = parseJsonObject(raw);
  const out = {
    fontSize: Number(pick(prefs, 'fontSize', 'font_size')) || 0,
    fontType: String(pick(prefs, 'fontType', 'font_type') || '').toLowerCase(),
    color: pick(prefs, 'color'),
    background: pick(prefs, 'background'),
    align: String(pick(prefs, 'align') || 'left').toLowerCase(),
    valign: String(pick(prefs, 'valign') || '').toLowerCase(),
    format: pick(prefs, 'format'),
    multiline: Boolean(pick(prefs, 'multiline')),
    length: Number(pick(prefs, 'length', 'cells', 'cellCount', 'cell_count')) || 0,
    options: pick(prefs, 'options'),
  };
  return out;
}

/** "#RGB" / "#RRGGBB" -> [r,g,b] in 0..1, or null. */
function parseColor(input) {
  if (typeof input !== 'string') return null;
  let hex = input.trim().replace(/^#/, '');
  if (/^[0-9a-f]{3}$/i.test(hex)) hex = hex.replace(/./g, (c) => c + c);
  if (!/^[0-9a-f]{6}$/i.test(hex)) return null;
  return [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
}

/** Options as [{ value, label }] from strings or { value, label } objects. */
function normalizeOptions(raw) {
  if (!Array.isArray(raw)) return [];
  return raw
    .map((option) => {
      if (option && typeof option === 'object') {
        const value = String(pick(option, 'value', 'label') ?? '');
        return { value, label: String(pick(option, 'label', 'value') ?? value) };
      }
      return { value: String(option ?? ''), label: String(option ?? '') };
    })
    .filter((option) => option.value !== '' || option.label !== '');
}

function normalizeArea(area) {
  const out = {
    page: Math.max(0, Math.trunc(Number(area.page) || 0)),
    x: Number(area.x),
    y: Number(area.y),
    w: Number(area.w),
    h: Number(area.h),
    option: pick(area, 'option'),
  };
  if (![out.x, out.y, out.w, out.h].every(Number.isFinite) || out.w <= 0 || out.h <= 0) return null;
  return out;
}

function normalizeField(raw, index) {
  const prefs = normalizePrefs(pick(raw, 'prefs', 'prefsJson', 'prefs_json'));
  const rawPrefs = parseJsonObject(pick(raw, 'prefs', 'prefsJson', 'prefs_json'));
  const areas = parseJsonArray(pick(raw, 'areas', 'areasJson', 'areas_json'))
    .map(normalizeArea)
    .filter(Boolean);
  return {
    id: String(pick(raw, 'id') ?? `field-${index}`),
    partyId: pick(raw, 'partyId', 'party_id'),
    type: String(pick(raw, 'type') || 'text'),
    name: String(pick(raw, 'name') ?? ''),
    readonly: Boolean(pick(raw, 'readonly')),
    defaultValue: pick(raw, 'defaultValue', 'default_value'),
    position: Number(pick(raw, 'position')) || index,
    prefs,
    options: normalizeOptions(pick(raw, 'options') ?? prefs.options),
    // Free-form extras such as a heading's text.
    text: pick(rawPrefs, 'text'),
    areas,
  };
}

function parseJsonArray(input) {
  if (Array.isArray(input)) return input;
  if (typeof input !== 'string') return [];
  try {
    const parsed = JSON.parse(input);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

/**
 * Entries of `values` keyed by field id. Accepts an array of
 * `{ fieldId, value, bytes, filename, mime, method, files }`, a Map or a plain
 * object whose values are such entries or bare values.
 */
function normalizeValues(values) {
  const out = new Map();
  const add = (fieldId, entry) => {
    if (fieldId === undefined || fieldId === null) return;
    const isEntry =
      entry !== null &&
      typeof entry === 'object' &&
      !Array.isArray(entry) &&
      !toBytes(entry) &&
      ['value', 'bytes', 'files'].some((key) => key in entry);
    out.set(String(fieldId), isEntry ? entry : { value: entry });
  };
  if (Array.isArray(values)) {
    for (const entry of values) add(pick(entry, 'fieldId', 'field_id'), entry);
  } else if (values instanceof Map) {
    for (const [fieldId, entry] of values) add(fieldId, entry);
  } else if (values && typeof values === 'object') {
    for (const [fieldId, entry] of Object.entries(values)) add(fieldId, entry);
  }
  return out;
}

function normalizeParty(raw, index) {
  return {
    id: String(pick(raw, 'id') ?? `party-${index}`),
    name: String(pick(raw, 'name') ?? ''),
    position: Number(pick(raw, 'position')) || index,
    email: pick(raw, 'email') || '',
    color: pick(raw, 'color') || '',
    status: pick(raw, 'status') || '',
    signerName: pick(raw, 'signerName', 'signer_name') || '',
    ip: pick(raw, 'ip') || '',
    userAgent: pick(raw, 'userAgent', 'user_agent') || '',
    timezone: pick(raw, 'timezone', 'timeZone') || '',
    tzOffsetMinutes: pick(raw, 'tzOffsetMinutes', 'tz_offset_minutes', 'tzOffset'),
    openedAt: pick(raw, 'openedAt', 'opened_at'),
    consentedAt: pick(raw, 'consentedAt', 'consented_at'),
    consentVersion: pick(raw, 'consentVersion', 'consent_version', 'consentTextVersion'),
    completedAt: pick(raw, 'completedAt', 'completed_at'),
    declinedAt: pick(raw, 'declinedAt', 'declined_at'),
    declineReason: pick(raw, 'declineReason', 'decline_reason') || '',
    signatureMethod: pick(raw, 'signatureMethod', 'signature_method') || '',
  };
}

function normalizeDocument(raw) {
  return {
    id: String(pick(raw, 'id') ?? ''),
    title: String(pick(raw, 'title') ?? ''),
    signingOrder: pick(raw, 'signingOrder', 'signing_order'),
    createdAt: pick(raw, 'createdAt', 'created_at'),
    sentAt: pick(raw, 'sentAt', 'sent_at'),
    finalizedAt: pick(raw, 'finalizedAt', 'finalized_at', 'completedAt', 'completed_at'),
    originalSha256: pick(raw, 'originalSha256', 'snapshotSha256', 'snapshot_sha256'),
    consentVersion: pick(raw, 'consentTextVersion', 'consent_text_version', 'consentVersion'),
  };
}

function normalizeEvent(raw) {
  return {
    type: String(pick(raw, 'type') || ''),
    at: pick(raw, 'at', 'createdAt', 'created_at'),
    partyId: pick(raw, 'partyId', 'party_id'),
    ip: pick(raw, 'ip') || '',
    hash: pick(raw, 'hash') || '',
  };
}

module.exports = {
  formatUtc,
  normalizeDocument,
  normalizeEvent,
  normalizeField,
  normalizeParty,
  normalizeValues,
  parseColor,
  pick,
  sha256Hex,
  toBytes,
  toDate,
};
