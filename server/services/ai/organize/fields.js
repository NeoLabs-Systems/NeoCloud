'use strict';

// The extraction call's answer made safe and checked: known types only,
// short clean texts, tags normalized, amounts parsed. NeoCloud lowers the
// model's confidence where it can't confirm a value (the issuer and amount
// must appear in the text) and picks the date by precedence (naming.js).
// Pure: no database, no model.

const { chooseDate } = require('./naming');

const DOC_TYPES = [
  'invoice', 'receipt', 'contract', 'payslip', 'letter', 'statement', 'certificate', 'manual',
  'report', 'notes', 'presentation', 'spreadsheet', 'screenshot', 'photo', 'scan', 'other',
];
const CONFIDENCE_FIELDS = ['date', 'issuer', 'subject', 'type', 'amount', 'tags'];
const MAX_TAGS = 8;
// The model's confidence is lowered when NeoCloud can't confirm a value.
const UNCONFIRMED_FACTOR = 0.7;
const DATE_SOURCE_CONFIDENCE = { exif: 0.9, filename: 0.6, modified: 0.4 };

function clamp(value, fallback = 0.5) {
  const number = Number(value);
  if (!Number.isFinite(number)) return fallback;
  return Math.min(Math.max(number, 0), 1);
}

function shortText(value, max) {
  if (typeof value !== 'string') return null;
  const text = value.replace(/\s+/g, ' ').trim();
  return text ? text.slice(0, max) : null;
}

function parseAmount(value) {
  if (typeof value === 'number') return Number.isFinite(value) ? value : null;
  if (typeof value !== 'string') return null;
  let text = value.replace(/[^\d.,-]/g, '');
  // 1.234,56 → 1234.56; 1,234.56 → 1234.56
  if (/,\d{1,2}$/.test(text)) text = text.replace(/\./g, '').replace(',', '.');
  else text = text.replace(/,/g, '');
  const number = Number(text);
  return text && Number.isFinite(number) ? number : null;
}

function normalizeTag(tag) {
  return String(tag ?? '').toLowerCase().replace(/[^\p{L}\p{N} &+-]/gu, '').replace(/\s+/g, ' ').trim().slice(0, 30);
}

function normalizeTags(value) {
  const list = Array.isArray(value) ? value : [];
  return [...new Set(list.map(normalizeTag).filter(Boolean))].slice(0, MAX_TAGS);
}

// Whether the amount appears in the text in a usual spelling.
function amountInText(amount, text) {
  const fixed = amount.toFixed(2);
  const spellings = [fixed, fixed.replace('.', ','), String(amount), String(amount).replace('.', ',')];
  return spellings.some((spelling) => text.includes(spelling));
}

/**
 * The model's answer made safe and checked. `text` is the text it read (null
 * for an image), `context` the node's name and dates and the EXIF date.
 */
function normalizeExtraction(data, { text, node, exif }) {
  const raw = data && typeof data === 'object' ? data : {};
  const confidences = raw.confidence && typeof raw.confidence === 'object' ? raw.confidence : {};
  const confidence = Object.fromEntries(CONFIDENCE_FIELDS.map((field) => [field, clamp(confidences[field])]));
  const type = shortText(raw.type, 40)?.toLowerCase();
  const fields = {
    issuer: shortText(raw.issuer, 80),
    subject: shortText(raw.subject, 100),
    type: DOC_TYPES.includes(type) ? type : (type ? 'other' : null),
    amount: parseAmount(raw.amount),
    currency: /^[A-Za-z]{3}$/.test(String(raw.currency || '').trim()) ? String(raw.currency).trim().toUpperCase() : null,
    tags: normalizeTags(raw.tags),
    language: /^[a-z]{2}$/i.test(String(raw.language || '').trim()) ? String(raw.language).trim().toLowerCase() : null,
    summary: shortText(raw.summary, 400),
    caption: shortText(raw.caption, 200),
  };
  if (typeof text === 'string') {
    const haystack = text.toLowerCase();
    if (fields.issuer && !haystack.includes(fields.issuer.toLowerCase())) confidence.issuer *= UNCONFIRMED_FACTOR;
    if (fields.amount !== null && !amountInText(fields.amount, text)) confidence.amount *= UNCONFIRMED_FACTOR;
  }
  const { date, dateSource } = chooseDate({
    contentDate: raw.date,
    exifDate: exif?.takenAt,
    name: node.name,
    modifiedAt: node.modified_at,
    uploadedAt: node.created_at,
  });
  if (dateSource && dateSource !== 'content') confidence.date = DATE_SOURCE_CONFIDENCE[dateSource];
  const mean = CONFIDENCE_FIELDS.reduce((sum, field) => sum + confidence[field], 0) / CONFIDENCE_FIELDS.length;
  return {
    ...fields,
    date,
    dateSource,
    confidence,
    overall: clamp(raw.overall, mean),
    camera: Boolean(exif?.make || exif?.model),
  };
}

module.exports = { DOC_TYPES, normalizeExtraction, normalizeTag };
