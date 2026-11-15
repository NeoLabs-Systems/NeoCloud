'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { PdfError } = require('./errors');
const { CONVERSION_TIMEOUT_MS, MAX_PDF_BYTES } = require('./limits');
const { loadPdfDocument } = require('./load');
const { toBytes } = require('./normalize');

// Extensions handed to LibreOffice. The name on disk is fixed
// (`document.<ext>`), so nothing user-controlled reaches the command line.
const CONVERTIBLE = new Set(['doc', 'docx', 'odt', 'rtf', 'txt', 'xls', 'xlsx', 'ods', 'csv', 'ppt', 'pptx', 'odp']);

let detected;

/** Candidate LibreOffice executables, most specific first. */
function officeCandidates() {
  const names = process.platform === 'win32' ? ['soffice.exe'] : ['soffice', 'libreoffice'];
  const onPath = (process.env.PATH || '')
    .split(path.delimiter)
    .filter(Boolean)
    .flatMap((dir) => names.map((name) => path.join(dir, name)));
  return [
    process.env.NEOCLOUD_LIBREOFFICE_PATH,
    ...onPath,
    '/Applications/LibreOffice.app/Contents/MacOS/soffice',
    'C:\\Program Files\\LibreOffice\\program\\soffice.exe',
  ].filter(Boolean);
}

/**
 * Whether a LibreOffice executable exists on this host. Looked up once per
 * process and without running anything; docToPdf still verifies the binary
 * when it first converts.
 */
function canConvertDocuments() {
  if (detected === undefined) {
    detected = officeCandidates().some((candidate) => {
      try {
        fs.accessSync(candidate, fs.constants.X_OK);
        return fs.statSync(candidate).isFile();
      } catch {
        return false;
      }
    });
  }
  return detected;
}

// LibreOffice is heavy; convert one document at a time.
let queueTail = Promise.resolve();
function serialized(task) {
  const run = queueTail.then(task, task);
  queueTail = run.catch(() => {});
  return run;
}

/** Converts an office document to PDF with LibreOffice; see CONVERTIBLE. */
async function docToPdf(bytes, filename) {
  const input = toBytes(bytes);
  const ext = path.extname(String(filename || '')).slice(1).toLowerCase();
  if (!input || input.length === 0 || input.length > MAX_PDF_BYTES || !CONVERTIBLE.has(ext)) {
    throw new PdfError('UNSUPPORTED_DOCUMENT', 'This document type cannot be converted.');
  }
  if (!canConvertDocuments()) throw new PdfError('CONVERSION_UNAVAILABLE', 'Document conversion is not available.');
  // Loaded on demand so that importing the engine stays free of side effects.
  const { convertToPdf, findOfficeBinary } = require('../../preview/office_convert');
  const binary = await findOfficeBinary();
  if (!binary) throw new PdfError('CONVERSION_UNAVAILABLE', 'Document conversion is not available.');

  return serialized(async () => {
    const workDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), 'neocloud-sign-'));
    try {
      const source = path.join(workDir, `document.${ext}`);
      await fs.promises.writeFile(source, input, { mode: 0o600 });
      try {
        await convertToPdf({ binary, workDir, source, timeoutMs: CONVERSION_TIMEOUT_MS });
      } catch (err) {
        throw new PdfError('CONVERSION_FAILED', 'The document could not be converted.', { cause: err });
      }
      const output = await fs.promises.readFile(path.join(workDir, 'document.pdf')).catch((err) => {
        throw new PdfError('CONVERSION_FAILED', 'The document could not be converted.', { cause: err });
      });
      // The result must be a sound PDF within the caps.
      await loadPdfDocument(output).catch((err) => {
        throw new PdfError('CONVERSION_FAILED', 'The converted document is not usable.', { cause: err });
      });
      return output;
    } finally {
      await fs.promises.rm(workDir, { recursive: true, force: true });
    }
  });
}

module.exports = { canConvertDocuments, docToPdf };
