'use strict';

// The seam to the PDF engine (./pdf): everything that reads, converts or
// draws PDFs goes through here, so the rest of the feature does not depend on
// how the engine works and its refusals become API errors in one place. The
// engine is loaded on first use.

const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Signing');

// The engine's PdfError codes as API errors. Anything else is the server's
// own failure and is thrown as it is.
const REFUSALS = {
  PDF_INVALID: [400, 'This file could not be read as a PDF.', 'INVALID_PDF'],
  PDF_ENCRYPTED: [400, 'This PDF is password-protected. Remove the protection first.', 'PDF_ENCRYPTED'],
  PDF_TOO_LARGE: [413, 'This file is too large to sign (50 MB at most).', 'DOCUMENT_TOO_LARGE'],
  PDF_TOO_MANY_PAGES: [400, 'This PDF has too many pages.', 'TOO_MANY_PAGES'],
  IMAGE_INVALID: [400, 'This image could not be used.', 'INVALID_IMAGE'],
  CONVERSION_UNAVAILABLE: [400, 'This server cannot convert documents. Convert the file to PDF first.', 'CONVERSION_UNAVAILABLE'],
  CONVERSION_FAILED: [400, 'This document could not be converted to PDF. Convert it to PDF first.', 'CONVERSION_FAILED'],
  UNSUPPORTED_DOCUMENT: [415, 'Only PDF files, images and office documents can be signed.', 'UNSUPPORTED_TYPE'],
};

let engine = null;
function loadEngine() {
  if (!engine) engine = require('./pdf');
  return engine;
}

async function refusing(action) {
  try {
    return await action(loadEngine());
  } catch (err) {
    const refusal = REFUSALS[err?.code];
    if (!refusal) throw err;
    logger.warn(`The PDF engine refused a file: ${err.code}`);
    throw httpError(...refusal);
  }
}

/** What the engine says about a PDF: { pageCount, pages: [{ width, height, rotate }], hasAcroForm, encrypted }. */
const probePdf = (bytes) => refusing((pdf) => pdf.probePdf(bytes));

/** The AcroForm widgets of a PDF as field drafts (type, name, required, readonly, prefs, areas, defaultValue). */
const importAcroFields = (bytes) => refusing((pdf) => pdf.importAcroFields(bytes));

const imageToPdf = (bytes) => refusing((pdf) => pdf.imageToPdf(bytes));

const docToPdf = (bytes, name) => refusing((pdf) => pdf.docToPdf(bytes, name));

/** Whether this server can turn office documents into PDF. */
const canConvertDocuments = () => loadEngine().canConvertDocuments();

// Failures of the build are the server's, not the request's: finalization retries.
const buildFinalPdf = (input) => loadEngine().buildFinalPdf(input);

module.exports = { buildFinalPdf, canConvertDocuments, docToPdf, importAcroFields, imageToPdf, probePdf };
