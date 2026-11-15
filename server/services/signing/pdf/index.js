'use strict';

// PDF engine of the signing feature: pure functions over bytes, no database
// and no HTTP. All functions are async unless noted. See docs/signing-pdf.md.

const { importAcroFields } = require('./acroform');
const { canConvertDocuments, docToPdf } = require('./doc_to_pdf');
const { signPdf, verifySignedPdf } = require('./digital_signature');
const { PdfError } = require('./errors');
const { buildFinalPdf } = require('./final_pdf');
const { imageToPdf } = require('./image_to_pdf');
const { generateSelfSignedP12 } = require('./p12');
const { probePdf } = require('./probe');

module.exports = {
  PdfError,
  buildFinalPdf,
  canConvertDocuments, // sync
  docToPdf,
  generateSelfSignedP12, // sync
  importAcroFields,
  imageToPdf,
  probePdf,
  signPdf,
  verifySignedPdf,
};
