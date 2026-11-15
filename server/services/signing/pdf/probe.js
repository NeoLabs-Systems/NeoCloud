'use strict';

const { PDFArray, PDFDict, PDFName } = require('pdf-lib');
const { loadPdfDocument } = require('./load');
const { pageGeometry } = require('./geometry');

const round2 = (n) => Math.round(n * 100) / 100;

/** True when the catalog has an AcroForm with at least one field. */
function catalogHasFields(doc) {
  const acroForm = doc.catalog.lookupMaybe(PDFName.of('AcroForm'), PDFDict);
  if (!acroForm) return false;
  const fields = acroForm.lookupMaybe(PDFName.of('Fields'), PDFArray);
  return Boolean(fields && fields.size() > 0);
}

/**
 * Cheap structural facts about a PDF: displayed page sizes (points), rotation
 * and whether it carries form fields. Throws PdfError for unusable input.
 */
async function probePdf(bytes) {
  const doc = await loadPdfDocument(bytes);
  const pages = doc.getPages().map((page) => {
    const geo = pageGeometry(page);
    return { width: round2(geo.width), height: round2(geo.height), rotate: geo.rotate };
  });
  return { pageCount: pages.length, pages, hasAcroForm: catalogHasFields(doc), encrypted: false };
}

module.exports = { catalogHasFields, probePdf };
