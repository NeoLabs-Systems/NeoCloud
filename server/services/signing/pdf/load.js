'use strict';

const { PDFDocument, EncryptedPDFError, ParseSpeeds } = require('pdf-lib');
require('./decode_guard');
const { PdfError } = require('./errors');
const { MAX_PAGES, MAX_PDF_BYTES } = require('./limits');
const { toBytes } = require('./normalize');

const HEADER = Buffer.from('%PDF-');
const ENCRYPT_KEY = Buffer.from('/Encrypt');

/**
 * Parses a PDF into a pdf-lib document, translating every failure into a
 * PdfError. `maxBytes` defaults to the upload cap; pass Infinity for files the
 * server itself produced.
 */
async function loadPdfDocument(input, { maxBytes = MAX_PDF_BYTES } = {}) {
  const bytes = toBytes(input);
  if (!bytes || bytes.length === 0) throw new PdfError('PDF_INVALID', 'Not a PDF file.');
  if (bytes.length > maxBytes) throw new PdfError('PDF_TOO_LARGE', 'The PDF is too large.');
  // The header may be preceded by a little junk, but not by much.
  if (bytes.subarray(0, 1024).indexOf(HEADER) === -1) throw new PdfError('PDF_INVALID', 'Not a PDF file.');

  let doc;
  try {
    doc = await PDFDocument.load(bytes, {
      updateMetadata: false,
      parseSpeed: ParseSpeeds.Fast,
      throwOnInvalidObject: false,
    });
  } catch (err) {
    // Encrypted files often fail while parsing garbled objects, before
    // pdf-lib gets to its own EncryptedPDFError; the trailer tells the truth.
    const looksEncrypted = err instanceof EncryptedPDFError || bytes.subarray(-4096).indexOf(ENCRYPT_KEY) !== -1;
    if (looksEncrypted) throw new PdfError('PDF_ENCRYPTED', 'Encrypted PDFs are not supported.', { cause: err });
    throw new PdfError('PDF_INVALID', 'The PDF could not be read.', { cause: err });
  }
  let pageCount;
  try {
    pageCount = doc.getPageCount();
  } catch (err) {
    // A header and a trailer, but no page tree behind them.
    throw new PdfError('PDF_INVALID', 'The PDF has no readable pages.', { cause: err });
  }
  if (pageCount > MAX_PAGES) throw new PdfError('PDF_TOO_MANY_PAGES', `The PDF has more than ${MAX_PAGES} pages.`);
  if (pageCount === 0) throw new PdfError('PDF_INVALID', 'The PDF has no pages.');
  return doc;
}

module.exports = { loadPdfDocument };
