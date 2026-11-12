'use strict';

// File names for auto-title, all local and deterministic:
//   - which names are meaningless (camera, screenshot, scanner, download
//     and random names) and so worth a suggestion,
//   - which date a name gets (content, then EXIF, then the old name, then the
//     modification time), with a plausibility check,
//   - the name itself, rendered from the person's template out of the fields
//     the model extracted. The model never writes a file name.

const { splitExtension } = require('../../../utils/file_names');

const DEFAULT_TEMPLATE = '{date} {issuer} – {subject}';
const PLACEHOLDERS = ['date', 'year', 'month', 'issuer', 'subject', 'type', 'amount', 'currency', 'original'];
const MAX_NAME_CHARS = 120;
const MAX_NAME_BYTES = 255;

// --- Which names qualify ---------------------------------------------------------

const CAMERA_PATTERNS = [
  /^(img|pxl|dsc|dscn|dscf|dji|gopr|gp|mvimg|vid|mov|sam|p)[_-]?\d{3,}/i,
  /^\d{8}[_-]\d{6}/,
  /^whatsapp (image|video) \d{4}-\d{2}-\d{2}/i,
  /^signal-\d{4}-\d{2}-\d{2}/i,
  /^photo[_ -]?\d+/i,
];
const SCREENSHOT_PATTERNS = [
  /^screenshot/i,
  /^screen ?shot/i,
  /^bildschirmfoto/i,
  /^bildschirmaufnahme/i,
  /^capture d.[ée]cran/i,
  /^screen ?recording/i,
];
const SCAN_PATTERNS = [
  /^scan(ned)?([ _-]?\d.*)?$/i,
  /^scn[_-]?\d+/i,
  /^doc[_-]?\d+/i,
  /^image[_-]?\d+$/i,
  /^\d{4}[_-]?\d{2}[_-]?\d{2}[_-]?scan/i,
];
// Names that say nothing, alone or numbered: "download (3)", "Unbenannt-1".
const GENERIC_WORDS = [
  'download', 'downloads', 'document', 'dokument', 'doc', 'file', 'datei', 'untitled', 'untitled document',
  'unbenannt', 'unnamed', 'new document', 'neues dokument', 'image', 'bild', 'foto', 'photo', 'attachment',
  'anhang', 'invoice', 'rechnung', 'receipt', 'beleg', 'scan', 'page', 'seite', 'export', 'print', 'output',
  'temp', 'tmp', 'test', 'neu', 'new', 'copy', 'kopie',
];
const GENERIC_PATTERN = new RegExp(`^(${GENERIC_WORDS.join('|')})( ?\\(\\d+\\)|[ _-]?\\d+)?( ?\\(\\d+\\))?$`, 'i');
const RANDOM_PATTERNS = [
  /^[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}$/i,
  /^[0-9a-f]{12,}$/i,
  // Timestamps: 20260312, 20260312_142233, 2026-03-12 14.22.33, 1710252153123.
  /^\d{8}([ _T-]?\d{4,6})?$/,
  /^\d{4}-\d{2}-\d{2}([ _T]\d{2}[.:-]?\d{2}([.:-]?\d{2})?)?$/,
  /^\d{9,13}$/,
  // Only digits and separators.
  /^[\d\W_]+$/,
];

// A long run of letters and digits mixed, with no separator (CDN and app ids).
function looksRandom(stem) {
  if (!/^[A-Za-z0-9]{16,}$/.test(stem)) return false;
  const digits = (stem.match(/\d/g) || []).length;
  const letters = (stem.match(/[A-Za-z]/g) || []).length;
  return digits >= 3 && letters >= 3;
}

/**
 * What a name says about the file: { meaningless, camera, screenshot, scan }.
 * Only meaningless names are renamed unless the person asks for a file.
 */
function classifyName(name) {
  const [rawStem] = splitExtension(String(name || '').trim());
  const stem = rawStem.trim();
  const camera = CAMERA_PATTERNS.some((pattern) => pattern.test(stem));
  const screenshot = SCREENSHOT_PATTERNS.some((pattern) => pattern.test(stem));
  const scan = SCAN_PATTERNS.some((pattern) => pattern.test(stem));
  const generic = GENERIC_PATTERN.test(stem);
  const random = stem.length === 0 || RANDOM_PATTERNS.some((pattern) => pattern.test(stem)) || looksRandom(stem);
  return { meaningless: camera || screenshot || scan || generic || random, camera, screenshot, scan };
}

// --- Dates -------------------------------------------------------------------------

function isoDate(year, month, day) {
  const date = new Date(Date.UTC(year, month - 1, day));
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return null;
  return date.toISOString().slice(0, 10);
}

/** A YYYY-MM-DD date out of any of the usual spellings in a value, else null. */
function parseDate(value) {
  const text = String(value ?? '').trim();
  let match = /^(\d{4})-(\d{1,2})-(\d{1,2})/.exec(text);
  if (match) return isoDate(Number(match[1]), Number(match[2]), Number(match[3]));
  match = /^(\d{1,2})\.(\d{1,2})\.(\d{4})/.exec(text);
  if (match) return isoDate(Number(match[3]), Number(match[2]), Number(match[1]));
  return null;
}

/** The first date written in a file name (2026-03-12, 2026_03_12, 20260312), else null. */
function dateFromName(name) {
  const text = String(name || '');
  const separated = /(?:^|\D)((?:19|20)\d{2})[-_.](\d{2})[-_.](\d{2})(?:\D|$)/.exec(text);
  if (separated) return isoDate(Number(separated[1]), Number(separated[2]), Number(separated[3]));
  const compact = /(?:^|\D)((?:19|20)\d{2})(\d{2})(\d{2})(?:\D|$)/.exec(text);
  if (compact) return isoDate(Number(compact[1]), Number(compact[2]), Number(compact[3]));
  return null;
}

/**
 * Whether a date can be the file's: a real calendar date from 1900 on, and
 * not after the file arrived (`uploadedAt`, a day's grace for time zones).
 */
function isPlausibleDate(date, uploadedAt) {
  if (!date || date < '1900-01-01') return false;
  const limit = new Date((Date.parse(uploadedAt) || Date.now()) + 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
  return date <= limit;
}

/**
 * The date a name uses and where it came from:
 * content → EXIF → the old file name → the modification time.
 * Each candidate must be plausible; returns { date, dateSource }.
 */
function chooseDate({ contentDate, exifDate, name, modifiedAt, uploadedAt }) {
  const candidates = [
    ['content', parseDate(contentDate)],
    ['exif', parseDate(exifDate)],
    ['filename', dateFromName(name)],
    ['modified', parseDate(String(modifiedAt || '').slice(0, 10))],
  ];
  for (const [source, date] of candidates) {
    if (isPlausibleDate(date, uploadedAt || modifiedAt)) return { date, dateSource: source };
  }
  return { date: null, dateSource: null };
}

// --- Sanitizing ---------------------------------------------------------------------

const WINDOWS_RESERVED = /^(con|prn|aux|nul|com\d|lpt\d)$/i;

// A value or a whole stem made safe on every OS: path separators and colons
// become dashes, the other characters Windows refuses go, whitespace collapses.
function cleanText(value) {
  return String(value ?? '')
    .normalize('NFC')
    .replace(/[\u0000-\u001f\u007f]/g, ' ')
    .replace(/[/\\:]/g, '-')
    .replace(/[*?"<>|]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

function capStem(stem, extension) {
  let capped = stem;
  const room = MAX_NAME_CHARS - extension.length;
  if ([...capped].length > room) capped = [...capped].slice(0, Math.max(room, 1)).join('').trim();
  while (Buffer.byteLength(capped + extension, 'utf8') > MAX_NAME_BYTES) capped = [...capped].slice(0, -1).join('');
  return capped;
}

/** A file name valid on Windows, macOS and Linux, at most 120 characters, extension kept. */
function sanitizeFileName(stem, extension = '') {
  let clean = cleanText(stem).replace(/^[\s.]+/, '').replace(/[\s.]+$/, '');
  if (WINDOWS_RESERVED.test(clean)) clean = `${clean}_`;
  clean = capStem(clean, extension).replace(/[\s.]+$/, '');
  return clean ? `${clean}${extension}` : null;
}

// --- Templates ----------------------------------------------------------------------

function formatAmount(amount) {
  const value = Number(amount);
  if (amount === null || amount === undefined || amount === '' || !Number.isFinite(value)) return '';
  return Number.isInteger(value) ? String(value) : value.toFixed(2);
}

const SEPARATORS = '–—\\-_,;.·|';

// What is left of a rendered template once missing values are gone:
// no empty brackets, no doubled or dangling separators.
function tidy(text) {
  let result = text.replace(/\s+/g, ' ');
  for (let pass = 0; pass < 3; pass += 1) {
    result = result
      .replace(/\(\s*\)|\[\s*\]|\{\s*\}/g, '')
      .replace(new RegExp(`\\s+([${SEPARATORS}])(\\s*[${SEPARATORS}])+(?=\\s)`, 'g'), ' $1')
      .replace(new RegExp(`^[\\s${SEPARATORS}]+|[\\s${SEPARATORS}]+$`, 'g'), '')
      .replace(/\s+/g, ' ');
  }
  return result.trim();
}

/** Whether a template names at least one placeholder this module knows. */
function isUsableTemplate(template) {
  return PLACEHOLDERS.some((name) => String(template || '').includes(`{${name}}`));
}

/**
 * The file name a template gives for extracted fields:
 *   fields   { date, issuer, subject, type, amount, currency }
 *   original the current name (for {original}; its extension is kept)
 * Missing values drop out with their separators. Returns null when nothing
 * but separators would be left.
 */
function renderName(template, fields, original) {
  const [originalStem, extension] = splitExtension(String(original || ''));
  const date = parseDate(fields.date);
  const values = {
    date: date || '',
    year: date ? date.slice(0, 4) : '',
    month: date ? date.slice(5, 7) : '',
    issuer: cleanText(fields.issuer),
    subject: cleanText(fields.subject),
    type: cleanText(fields.type),
    amount: formatAmount(fields.amount),
    currency: cleanText(fields.currency).toUpperCase(),
    original: cleanText(originalStem),
  };
  const chosen = isUsableTemplate(template) ? template : DEFAULT_TEMPLATE;
  const rendered = chosen.replace(/\{([a-z]+)\}/g, (match, key) => (Object.hasOwn(values, key) ? values[key] : match));
  const stem = tidy(rendered);
  if (!stem || !/[\p{L}\p{N}]/u.test(stem)) return null;
  return sanitizeFileName(stem, extension);
}

/** The placeholders a template uses, for the confidence of the name. */
function templateFields(template) {
  const chosen = isUsableTemplate(template) ? template : DEFAULT_TEMPLATE;
  return PLACEHOLDERS.filter((name) => chosen.includes(`{${name}}`));
}

module.exports = {
  chooseDate,
  classifyName,
  dateFromName,
  renderName,
  sanitizeFileName,
  templateFields,
};
