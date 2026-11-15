'use strict';

const { appendCertificate } = require('./certificate');
const { addSignaturePlaceholder, signWithPlaceholder } = require('./digital_signature');
const { PdfError } = require('./errors');
const { embedFonts } = require('./fonts');
const { flattenAcroForm, stripActiveContent } = require('./flatten');
const { loadPdfDocument } = require('./load');
const {
  normalizeDocument,
  normalizeEvent,
  normalizeField,
  normalizeParty,
  normalizeValues,
  pick,
  sha256Hex,
  toBytes,
  toDate,
} = require('./normalize');
const { PagePainter } = require('./painter');
const { renderField } = require('./render_fields');
const { languageOf, stringsFor } = require('./strings');

const SIGNATURE_TYPES = new Set(['signature', 'initials']);

/** The files a `file` field holds: `entry.files` or the entry itself. */
function filesOf(entry) {
  if (!entry) return [];
  const candidates = Array.isArray(entry.files) ? entry.files : [entry];
  return candidates
    .map((file) => ({
      bytes: toBytes(file.bytes),
      filename: String(pick(file, 'filename', 'name') || 'attachment'),
      mime: pick(file, 'mime', 'mimeType') || 'application/octet-stream',
    }))
    .filter((file) => file.bytes && file.bytes.length > 0);
}

function safeFilename(name, used) {
  const base = name.replace(/[\\/:*?"<>|\u0000-\u001f]/g, '_').slice(0, 120) || 'attachment';
  let candidate = base;
  for (let n = 2; used.has(candidate); n += 1) candidate = `${base} (${n})`;
  used.add(candidate);
  return candidate;
}

/** Which signature methods (draw/type/upload) each party used, by party id. */
function collectMethods(fields, valueMap, parties) {
  const methods = new Map(parties.map((party) => [party.id, new Set()]));
  for (const party of parties) {
    if (party.signatureMethod) methods.get(party.id).add(String(party.signatureMethod));
  }
  for (const field of fields) {
    if (!SIGNATURE_TYPES.has(field.type) || field.partyId === undefined) continue;
    const entry = valueMap.get(field.id);
    const method = entry && (entry.method || (entry.value && entry.value.method));
    const set = methods.get(String(field.partyId));
    if (method && set) set.add(String(method));
  }
  return methods;
}

/** One painter per page that has at least one area. */
function createPainters(doc, fields) {
  const painters = new Map();
  const pages = doc.getPages();
  for (const field of fields) {
    for (const area of field.areas) {
      if (area.page < pages.length && !painters.has(area.page)) painters.set(area.page, new PagePainter(pages[area.page]));
    }
  }
  return painters;
}

function setMetadata(doc, document, finalizedAt, language) {
  if (document.title) doc.setTitle(document.title);
  doc.setProducer('NeoCloud');
  doc.setCreator('NeoCloud');
  doc.setModificationDate(finalizedAt);
  doc.setLanguage(language);
}

/**
 * Builds the final signed file from the frozen snapshot, the parties' values
 * and the audit data. Pure: all inputs are passed in; the same inputs with
 * the same `document.finalizedAt` give byte-identical output.
 *
 *   pdfBytes   the frozen original
 *   parties    [{ id, name, email, color, signerName, ip, userAgent, timezone, tzOffsetMinutes,
 *                 openedAt, consentedAt, completedAt, declinedAt, declineReason, position }]
 *   fields     [{ id, partyId, type, name, readonly, defaultValue, prefs, options, areas }]
 *   values     entries keyed by field id (see normalizeValues): { fieldId, value, bytes, method, files }
 *   events     [{ type, at, partyId, ip, hash }]
 *   document   { id, title, signingOrder, createdAt, sentAt, finalizedAt, originalSha256?,
 *                consentTextVersion? }
 *   locale     'en' | 'de'
 *   signer     optional { p12Buffer, passphrase, reason, name, location }: seals the result
 *   certificate  default true; false draws and flattens everything but appends no audit pages
 *              (the seal still applies, over the clean file's own content)
 *
 * snake_case keys are accepted wherever camelCase is shown.
 */
async function buildFinalPdf({
  pdfBytes, parties = [], fields = [], values, events = [], document = {}, locale, signer, certificate = true,
}) {
  const original = toBytes(pdfBytes);
  if (!original) throw new PdfError('PDF_INVALID', 'No PDF given.');
  const meta = normalizeDocument(document);
  const originalHash = sha256Hex(original);
  if (meta.originalSha256 && String(meta.originalSha256).toLowerCase() !== originalHash) {
    throw new PdfError('HASH_MISMATCH', 'The stored original does not match its recorded hash.');
  }
  const finalizedAt = toDate(meta.finalizedAt) || new Date();
  const strings = stringsFor(locale);

  const doc = await loadPdfDocument(original, { maxBytes: Infinity });
  const originalPageCount = doc.getPageCount();
  const fonts = await embedFonts(doc);
  stripActiveContent(doc);
  flattenAcroForm(doc, fonts.regular);

  const normalizedParties = parties.map(normalizeParty).sort((a, b) => a.position - b.position);
  const normalizedFields = fields.map(normalizeField).sort((a, b) => a.position - b.position);
  const valueMap = normalizeValues(values);

  const painters = createPainters(doc, normalizedFields);
  const ctx = { doc, fonts, locale: languageOf(locale), imageCache: new Map() };
  const attachments = [];
  const usedNames = new Set();
  for (const field of normalizedFields) {
    let entry = valueMap.get(field.id);
    if (!entry && field.defaultValue !== undefined && field.defaultValue !== null) entry = { value: field.defaultValue };
    const files = field.type === 'file' ? filesOf(entry) : [];
    await renderField(ctx, painters, field, entry, files);
    for (const file of files) {
      const filename = safeFilename(file.filename, usedNames);
      file.filename = filename;
      doc.attach(file.bytes, filename, {
        mimeType: file.mime,
        description: field.name || filename,
        creationDate: finalizedAt,
        modificationDate: finalizedAt,
      });
      attachments.push({ fieldName: field.name, filename, size: file.bytes.length, sha256: sha256Hex(file.bytes) });
    }
  }
  for (const painter of painters.values()) painter.finish();

  if (certificate !== false) {
    appendCertificate(doc, fonts, strings, {
      document: meta,
      parties: normalizedParties,
      events: events.map(normalizeEvent),
      originalHash,
      originalPageCount,
      attachments,
      methodsByParty: collectMethods(normalizedFields, valueMap, normalizedParties),
      finalizedAt,
    });
  }
  setMetadata(doc, meta, finalizedAt, ctx.locale);

  let bytes;
  if (signer) {
    const p12Buffer = toBytes(signer.p12Buffer);
    if (!p12Buffer) throw new PdfError('SIGNING_FAILED', 'No signing certificate given.');
    addSignaturePlaceholder(doc, { ...signer, p12Buffer, signingTime: finalizedAt });
    const saved = Buffer.from(await doc.save({ useObjectStreams: false }));
    bytes = await signWithPlaceholder(saved, { p12Buffer, passphrase: signer.passphrase, signingTime: finalizedAt });
  } else {
    bytes = Buffer.from(await doc.save());
  }
  return { bytes, sha256: sha256Hex(bytes) };
}

module.exports = { buildFinalPdf };
