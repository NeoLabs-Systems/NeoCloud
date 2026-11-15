'use strict';

// The field types a document can hold, as data: how each is stored, how big
// a new one is, which preferences it takes and how a value is checked and
// normalized. Adding a type means adding an entry here (and drawing it in the
// PDF engine); no other code branches on the type name.
//
// storage: 'inline'  the value is JSON in sign_values.value_json
//          'blob'    the value is an uploaded image or file (see uploads.js)
//          'static'  the field only shows text and never takes a value

const { httpError } = require('../../utils/http_error');
const { normalizePrefs } = require('./prefs');
const { withoutInvisible } = require('./plain_text');

const MAX_TEXT = 5000;
const MAX_LINE = 500;
const MAX_OPTIONS = 50;

const TEXT_STYLE = {
  fontSize: { kind: 'int', min: 6, max: 72 },
  // The bundled fonts have no italic; offering it would print something else than was chosen.
  fontType: { kind: 'enum', values: ['normal', 'bold'] },
  align: { kind: 'enum', values: ['left', 'center', 'right'] },
  valign: { kind: 'enum', values: ['top', 'center', 'bottom'] },
  color: { kind: 'color' },
};
const PLACEHOLDER = { placeholder: { kind: 'string', max: 100 } };
const OPTIONS = { options: { kind: 'strings', min: 1, maxItems: MAX_OPTIONS, maxLength: 100 } };
const SIGNATURE_METHODS = { methods: { kind: 'strings', min: 1, maxItems: 3, maxLength: 10 } };

function invalid(message) {
  return httpError(400, message, 'INVALID_VALUE');
}

function text(value, label = 'Value') {
  if (typeof value !== 'string') throw invalid(`${label} must be text.`);
  return withoutInvisible(value).replace(/\r\n?/g, '\n').replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '');
}

function validateText(value, prefs) {
  const cleaned = text(value);
  const single = prefs.multiline ? cleaned : cleaned.replace(/\n/g, ' ');
  const trimmed = single.trim();
  if (trimmed.length > (prefs.maxLength ?? (prefs.multiline ? MAX_TEXT : MAX_LINE))) throw invalid('The text is too long.');
  if (trimmed && prefs.minLength && trimmed.length < prefs.minLength) throw invalid(`Enter at least ${prefs.minLength} characters.`);
  if (trimmed && prefs.format === 'email' && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)) throw invalid('Enter a valid email address.');
  return trimmed;
}

function validateNumber(value, prefs) {
  const raw = typeof value === 'number' ? String(value) : text(value).trim();
  if (raw === '') return '';
  if (!/^-?\d{1,15}(\.\d{1,10})?$/.test(raw)) throw invalid('Enter a number.');
  const decimals = (raw.split('.')[1] || '').length;
  if (prefs.decimals !== undefined && decimals > prefs.decimals) throw invalid(`At most ${prefs.decimals} decimal places.`);
  const number = Number(raw);
  if (prefs.min !== undefined && number < prefs.min) throw invalid(`The number must be at least ${prefs.min}.`);
  if (prefs.max !== undefined && number > prefs.max) throw invalid(`The number must be at most ${prefs.max}.`);
  return raw;
}

function validateDate(value) {
  const raw = text(value).trim();
  if (raw === '') return '';
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
  const date = match ? new Date(Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]))) : null;
  if (!date || date.toISOString().slice(0, 10) !== raw) throw invalid('Enter a valid date (YYYY-MM-DD).');
  return raw;
}

function validatePhone(value) {
  const raw = text(value).trim();
  if (raw === '') return '';
  const digits = raw.replace(/\D/g, '').length;
  if (!/^\+?[0-9][0-9 ()./-]{3,24}$/.test(raw) || digits < 5 || digits > 15) throw invalid('Enter a valid phone number.');
  return raw;
}

function validateCheckbox(value) {
  if (typeof value !== 'boolean') throw invalid('A checkbox is either checked or not.');
  return value;
}

function validateChoice(value, prefs) {
  const raw = text(value);
  if (raw === '') return '';
  if (!prefs.options.includes(raw)) throw invalid('That is not one of the options.');
  return raw;
}

function validateMultiple(value, prefs) {
  if (!Array.isArray(value)) throw invalid('Pick from the options.');
  const picked = new Set();
  for (const entry of value) {
    if (typeof entry !== 'string' || !prefs.options.includes(entry)) throw invalid('That is not one of the options.');
    picked.add(entry);
  }
  // Stored in the order of the options, so the same choice is the same value.
  const result = prefs.options.filter((option) => picked.has(option));
  if (result.length > (prefs.maxSelect ?? MAX_OPTIONS)) throw invalid(`Pick at most ${prefs.maxSelect}.`);
  if (result.length > 0 && prefs.minSelect && result.length < prefs.minSelect) throw invalid(`Pick at least ${prefs.minSelect}.`);
  return result;
}

const CELL_CHARSETS = { digits: /^[0-9]*$/, letters: /^\p{L}*$/u, any: /^[^\s]*$/u };

function validateCells(value, prefs) {
  const raw = text(value).trim();
  if (Array.from(raw).length > prefs.cells) throw invalid(`At most ${prefs.cells} characters.`);
  if (!CELL_CHARSETS[prefs.charset || 'any'].test(raw)) throw invalid('Those characters are not allowed here.');
  return raw;
}

function rejectInline() {
  throw invalid('This field takes an upload, not a typed value.');
}

const notEmpty = (value) => value !== '' && value !== null && value !== undefined;

const TYPES = {
  text: {
    storage: 'inline',
    size: { w: 0.25, h: 0.03 },
    prefs: { ...TEXT_STYLE, ...PLACEHOLDER, multiline: { kind: 'bool' }, minLength: { kind: 'int', min: 1, max: MAX_TEXT }, maxLength: { kind: 'int', min: 1, max: MAX_TEXT }, format: { kind: 'enum', values: ['none', 'email'] } },
    validate: validateText,
    isFilled: notEmpty,
  },
  number: {
    storage: 'inline',
    size: { w: 0.15, h: 0.03 },
    prefs: { ...TEXT_STYLE, ...PLACEHOLDER, min: { kind: 'number' }, max: { kind: 'number' }, decimals: { kind: 'int', min: 0, max: 10 }, format: { kind: 'enum', values: ['none', 'comma', 'dot', 'space', 'usd', 'eur', 'gbp'] } },
    validate: validateNumber,
    isFilled: notEmpty,
  },
  date: {
    storage: 'inline',
    size: { w: 0.18, h: 0.03 },
    prefs: { ...TEXT_STYLE, // Tokens YYYY YY MMMM MMM MM M DD D, e.g. DD.MM.YYYY.
    format: { kind: 'pattern', regex: /^[YMD .,/-]{1,20}$/ }, autoToday: { kind: 'bool' } },
    validate: validateDate,
    isFilled: notEmpty,
  },
  phone: {
    storage: 'inline',
    size: { w: 0.22, h: 0.03 },
    prefs: { ...TEXT_STYLE, ...PLACEHOLDER },
    validate: validatePhone,
    isFilled: notEmpty,
  },
  checkbox: {
    storage: 'inline',
    size: { w: 0.03, h: 0.03 },
    prefs: { color: { kind: 'color' } },
    validate: validateCheckbox,
    // A required checkbox must be checked (an agreement), so false is empty.
    isFilled: (value) => value === true,
  },
  radio: {
    storage: 'inline',
    size: { w: 0.2, h: 0.08 },
    prefs: { ...TEXT_STYLE, ...OPTIONS },
    requiredPrefs: ['options'],
    validate: validateChoice,
    isFilled: notEmpty,
  },
  select: {
    storage: 'inline',
    size: { w: 0.22, h: 0.03 },
    prefs: { ...TEXT_STYLE, ...OPTIONS },
    requiredPrefs: ['options'],
    validate: validateChoice,
    isFilled: notEmpty,
  },
  multiple: {
    storage: 'inline',
    size: { w: 0.22, h: 0.08 },
    prefs: { ...TEXT_STYLE, ...OPTIONS, minSelect: { kind: 'int', min: 1, max: MAX_OPTIONS }, maxSelect: { kind: 'int', min: 1, max: MAX_OPTIONS } },
    requiredPrefs: ['options'],
    validate: validateMultiple,
    isFilled: (value) => Array.isArray(value) && value.length > 0,
  },
  cells: {
    storage: 'inline',
    size: { w: 0.3, h: 0.03 },
    prefs: { ...TEXT_STYLE, cells: { kind: 'int', min: 1, max: 50 }, charset: { kind: 'enum', values: ['any', 'digits', 'letters'] } },
    requiredPrefs: ['cells'],
    validate: validateCells,
    // Required means every cell is filled.
    isFilled: (value, prefs) => typeof value === 'string' && Array.from(value).length === prefs.cells,
  },
  signature: { storage: 'blob', upload: 'image', maxPixels: [1200, 600], size: { w: 0.25, h: 0.06 }, prefs: SIGNATURE_METHODS, validate: rejectInline },
  initials: { storage: 'blob', upload: 'image', maxPixels: [600, 300], size: { w: 0.1, h: 0.04 }, prefs: SIGNATURE_METHODS, validate: rejectInline },
  stamp: { storage: 'blob', upload: 'image', maxPixels: [1200, 1200], size: { w: 0.15, h: 0.08 }, prefs: {}, validate: rejectInline },
  image: { storage: 'blob', upload: 'image', maxPixels: [1600, 1600], size: { w: 0.25, h: 0.15 }, prefs: {}, validate: rejectInline },
  file: { storage: 'blob', upload: 'file', size: { w: 0.25, h: 0.04 }, prefs: {}, validate: rejectInline },
  heading: {
    storage: 'static',
    size: { w: 0.4, h: 0.04 },
    prefs: TEXT_STYLE,
    validate: rejectInline,
  },
};

const METHODS = ['draw', 'type', 'upload'];

function typeOf(name) {
  const type = TYPES[name];
  if (!type) throw httpError(400, `Unknown field type "${name}".`, 'INVALID_FIELD');
  return type;
}

/** Preferences checked against the type's schema, with the ones the type cannot work without. */
function normalizeFieldPrefs(typeName, input) {
  const type = typeOf(typeName);
  const prefs = normalizePrefs(type.prefs, input);
  for (const name of type.requiredPrefs || []) {
    if (prefs[name] === undefined) throw httpError(400, `A ${typeName} field needs "${name}".`, 'INVALID_FIELD');
  }
  if (prefs.minLength && prefs.maxLength && prefs.minLength > prefs.maxLength) throw httpError(400, 'minLength is above maxLength.', 'INVALID_FIELD');
  if (prefs.min !== undefined && prefs.max !== undefined && prefs.min > prefs.max) throw httpError(400, 'min is above max.', 'INVALID_FIELD');
  if (prefs.minSelect && prefs.maxSelect && prefs.minSelect > prefs.maxSelect) throw httpError(400, 'minSelect is above maxSelect.', 'INVALID_FIELD');
  if (prefs.methods && prefs.methods.some((method) => !METHODS.includes(method))) {
    throw httpError(400, `methods must be from: ${METHODS.join(', ')}.`, 'INVALID_FIELD');
  }
  return prefs;
}

/**
 * A value typed or picked by a signer, checked and normalized. null clears
 * the field. Throws 400 INVALID_VALUE.
 */
function normalizeValue(field, value) {
  if (value === null || value === undefined) return null;
  return typeOf(field.type).validate(value, field.prefs);
}

/** The default of a field as the editor sent it, checked like a signer's value. Blob fields take none. */
function normalizeDefault(typeName, prefs, value) {
  const type = typeOf(typeName);
  if (value === null || value === undefined || value === '') return null;
  if (type.storage === 'static') {
    if (typeof value !== 'string' || value.length > 2000) throw httpError(400, 'The heading text is too long.', 'INVALID_FIELD');
    return value;
  }
  if (type.storage === 'blob') throw httpError(400, 'This type has no default value.', 'INVALID_FIELD');
  try {
    return type.validate(value, prefs);
  } catch (err) {
    throw httpError(400, `Default value: ${err.message}`, 'INVALID_FIELD');
  }
}

/** True when the stored value counts as filled in for a required field. */
function isFilled(field, value) {
  const type = typeOf(field.type);
  if (type.storage === 'static') return true;
  if (type.storage === 'blob') return value !== null && value !== undefined;
  return type.isFilled(value, field.prefs);
}

const isTypeName = (name) => Object.hasOwn(TYPES, name);

module.exports = { METHODS, TYPES, isFilled, isTypeName, normalizeDefault, normalizeFieldPrefs, normalizeValue, typeOf };
