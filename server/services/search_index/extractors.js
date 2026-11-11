'use strict';

// Plain text out of file contents, for the search index. Everything here
// works on a Buffer in memory; nothing is written to disk. The extractors run
// in the indexer's worker thread (extract_worker.js), so a slow or hostile
// file never blocks the server and can be stopped by its time limit.

const path = require('path');
const yauzl = require('yauzl');

const TEXT_EXTENSIONS = new Set([
  '.txt', '.text', '.md', '.markdown', '.rst', '.adoc', '.org', '.tex', '.log', '.csv', '.tsv',
  '.json', '.jsonl', '.ndjson', '.xml', '.yaml', '.yml', '.toml', '.ini', '.cfg', '.conf', '.properties',
  '.srt', '.vtt', '.ics', '.vcf', '.eml',
  '.js', '.mjs', '.cjs', '.ts', '.tsx', '.jsx', '.vue', '.svelte', '.css', '.scss', '.sass', '.less',
  '.py', '.rb', '.go', '.rs', '.java', '.kt', '.kts', '.swift', '.dart', '.c', '.h', '.cc', '.cpp', '.hpp',
  '.cs', '.php', '.pl', '.lua', '.r', '.scala', '.sh', '.bash', '.zsh', '.fish', '.ps1', '.bat', '.sql',
  '.gradle', '.cmake', '.mk',
]);
const HTML_EXTENSIONS = new Set(['.html', '.htm', '.xhtml']);
const OFFICE_EXTENSIONS = new Set(['.docx', '.xlsx', '.pptx', '.odt', '.ods', '.odp']);

// Above these sizes a file is not read at all.
const MAX_SOURCE_BYTES = {
  text: 10 * 1024 * 1024,
  html: 10 * 1024 * 1024,
  pdf: 100 * 1024 * 1024,
  office: 100 * 1024 * 1024,
};

// Zip limits: entries are checked against their declared sizes, which yauzl
// verifies while inflating, so a zip bomb stops at the cap.
const MAX_ZIP_ENTRIES = 10000;
const MAX_ZIP_ENTRY_BYTES = 32 * 1024 * 1024;
const MAX_ZIP_TOTAL_BYTES = 96 * 1024 * 1024;
const MAX_PDF_PAGES = 2000;

/** 'text' | 'html' | 'pdf' | 'office' for files the index reads, else null. */
function indexableKind(name, mime) {
  const ext = path.extname(String(name || '')).toLowerCase();
  const type = String(mime || '').toLowerCase();
  if (ext === '.pdf' || type === 'application/pdf') return 'pdf';
  if (OFFICE_EXTENSIONS.has(ext)) return 'office';
  if (HTML_EXTENSIONS.has(ext) || type === 'text/html') return 'html';
  if (TEXT_EXTENSIONS.has(ext) || type.startsWith('text/')) return 'text';
  return null;
}

/** Every extension the index reads, for the indexer's SQL filter. */
function indexableExtensions() {
  return ['.pdf', ...OFFICE_EXTENSIONS, ...HTML_EXTENSIONS, ...TEXT_EXTENSIONS];
}

// --- Plain text -------------------------------------------------------------

function tryDecode(label, bytes, fatal = false) {
  try {
    return new TextDecoder(label, { fatal }).decode(bytes);
  } catch {
    return null;
  }
}

// UTF-16 without a byte order mark: mostly zero bytes in every other place.
function sniffUtf16(buffer) {
  const sample = buffer.subarray(0, 4096);
  if (sample.length < 4) return null;
  let evenZeros = 0;
  let oddZeros = 0;
  for (let i = 0; i < sample.length; i += 1) {
    if (sample[i] === 0) {
      if (i % 2 === 0) evenZeros += 1;
      else oddZeros += 1;
    }
  }
  const half = sample.length / 2;
  if (oddZeros > half * 0.3 && evenZeros < half * 0.05) return 'utf-16le';
  if (evenZeros > half * 0.3 && oddZeros < half * 0.05) return 'utf-16be';
  return null;
}

// A charset named in the first bytes: <meta charset>, Content-Type or <?xml encoding?>.
function declaredCharset(buffer) {
  const head = buffer.subarray(0, 2048).toString('latin1');
  const found = /(?:charset|encoding)\s*=\s*["']?([A-Za-z0-9._:-]+)/i.exec(head);
  return found ? found[1].toLowerCase() : null;
}

/**
 * Decodes text in an unknown charset: a byte order mark wins, then UTF-16
 * by its zero bytes, then UTF-8 if the bytes are valid UTF-8, then a declared
 * charset, then Windows-1252 (which never fails). Binary data comes back as
 * an empty string.
 */
function decodeText(buffer) {
  if (buffer[0] === 0xef && buffer[1] === 0xbb && buffer[2] === 0xbf) return tryDecode('utf-8', buffer.subarray(3));
  if (buffer[0] === 0xff && buffer[1] === 0xfe) return tryDecode('utf-16le', buffer.subarray(2));
  if (buffer[0] === 0xfe && buffer[1] === 0xff) return tryDecode('utf-16be', buffer.subarray(2));
  const utf16 = sniffUtf16(buffer);
  if (utf16) return tryDecode(utf16, buffer) ?? '';
  if (buffer.subarray(0, 8192).includes(0)) return '';
  const utf8 = tryDecode('utf-8', buffer, true);
  if (utf8 !== null) return utf8;
  const declared = declaredCharset(buffer);
  return (declared && tryDecode(declared, buffer)) ?? tryDecode('windows-1252', buffer);
}

const NAMED_ENTITIES = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ' };

function decodeEntities(text) {
  return text.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (whole, name) => {
    if (name[0] === '#') {
      const code = name[1] === 'x' || name[1] === 'X' ? Number.parseInt(name.slice(2), 16) : Number(name.slice(1));
      return Number.isInteger(code) && code > 0 && code <= 0x10ffff ? String.fromCodePoint(code) : ' ';
    }
    return NAMED_ENTITIES[name.toLowerCase()] ?? whole;
  });
}

function htmlToText(html) {
  return decodeEntities(html
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/<(script|style|noscript|template)\b[\s\S]*?<\/\1\s*>/gi, ' ')
    .replace(/<\/?(?:p|div|br|li|tr|h[1-6]|section|article|header|footer|table|ul|ol|pre|blockquote)\b[^>]*>/gi, '\n')
    .replace(/<[^>]*>/g, ' '));
}

// --- Office Open XML and OpenDocument -----------------------------------------

// Word, PowerPoint and OpenDocument markup to text: paragraphs and cells
// become line breaks, tabs and spaces stay, all other tags go.
function xmlToText(xml) {
  return decodeEntities(xml
    .replace(/<(?:w:tab|w:br|w:cr|a:br|text:s|text:tab|text:line-break)\b[^>]*\/?>/g, ' ')
    .replace(/<\/(?:w:p|a:p|text:p|text:h|table:table-cell|si)>/g, '\n')
    .replace(/<[^>]*>/g, ''));
}

// A sheet's cells: inline strings and values; shared strings (t="s") are
// only indices into sharedStrings.xml, which is read on its own.
function sheetToText(xml) {
  const values = [];
  const cell = /<c\b([^>]*?)(?:\/>|>([\s\S]*?)<\/c>)/g;
  let found;
  while ((found = cell.exec(xml)) !== null) {
    if (/\bt="s"/.test(found[1]) || !found[2]) continue;
    const inline = /<is>([\s\S]*?)<\/is>/.exec(found[2]);
    const value = /<v>([\s\S]*?)<\/v>/.exec(found[2]);
    if (inline) values.push(xmlToText(inline[1]));
    else if (value) values.push(decodeEntities(value[1]));
  }
  return values.join('\n');
}

const OFFICE_PARTS = {
  '.docx': [/^word\/(document|header\d*|footer\d*|footnotes|endnotes|comments)\.xml$/, xmlToText],
  '.pptx': [/^ppt\/(slides\/slide|notesSlides\/notesSlide)\d+\.xml$/, xmlToText],
  '.xlsx': [/^xl\/(sharedStrings|worksheets\/sheet\d+)\.xml$/, null],
  '.odt': [/^content\.xml$/, xmlToText],
  '.ods': [/^content\.xml$/, xmlToText],
  '.odp': [/^content\.xml$/, xmlToText],
};

// slide2.xml before slide10.xml.
function partOrder(name) {
  return name.replace(/\d+(?=\.xml$)/, (digits) => digits.padStart(8, '0'));
}

function openZip(buffer) {
  return new Promise((resolve, reject) => {
    yauzl.fromBuffer(buffer, { lazyEntries: true, validateEntrySizes: true, strictFileNames: false }, (err, zip) => {
      if (err) reject(err);
      else resolve(zip);
    });
  });
}

function readEntry(zip, entry) {
  return new Promise((resolve, reject) => {
    zip.openReadStream(entry, (err, stream) => {
      if (err) return reject(err);
      const chunks = [];
      stream.on('data', (chunk) => chunks.push(chunk));
      stream.on('error', reject);
      stream.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
      return undefined;
    });
  });
}

/** The XML parts of an Office or OpenDocument file that hold its text. */
async function readOfficeParts(buffer, pattern) {
  const zip = await openZip(buffer);
  const parts = [];
  let seen = 0;
  let total = 0;
  try {
    await new Promise((resolve, reject) => {
      zip.on('error', reject);
      zip.on('end', resolve);
      zip.on('entry', (entry) => {
        seen += 1;
        if (seen > MAX_ZIP_ENTRIES) return reject(new Error('Too many entries in the archive.'));
        const wanted = pattern.test(entry.fileName)
          && entry.uncompressedSize <= MAX_ZIP_ENTRY_BYTES
          && total + entry.uncompressedSize <= MAX_ZIP_TOTAL_BYTES;
        if (!wanted) return zip.readEntry();
        total += entry.uncompressedSize;
        return readEntry(zip, entry).then((xml) => {
          parts.push({ name: entry.fileName, xml });
          zip.readEntry();
        }, reject);
      });
      zip.readEntry();
    });
  } finally {
    zip.close();
  }
  return parts.sort((a, b) => (partOrder(a.name) < partOrder(b.name) ? -1 : 1));
}

async function officeToText(buffer, ext) {
  const [pattern, convert] = OFFICE_PARTS[ext];
  const parts = await readOfficeParts(buffer, pattern);
  return parts.map((part) => {
    if (convert) return convert(part.xml);
    return part.name.endsWith('sharedStrings.xml') ? xmlToText(part.xml) : sheetToText(part.xml);
  }).join('\n');
}

// --- PDF (the bundled extractor; pdftotext is used instead when installed) ----

let pdfModule;

async function pdfToText(buffer, maxChars) {
  pdfModule ??= import('unpdf');
  const { getDocumentProxy } = await pdfModule;
  const pdf = await getDocumentProxy(new Uint8Array(buffer), { isEvalSupported: false, disableFontFace: true });
  try {
    const pages = [];
    let length = 0;
    for (let number = 1; number <= Math.min(pdf.numPages, MAX_PDF_PAGES) && length < maxChars; number += 1) {
      const page = await pdf.getPage(number);
      const content = await page.getTextContent();
      const text = content.items.map((item) => `${item.str ?? ''}${item.hasEOL ? '\n' : ''}`).join('');
      pages.push(text);
      length += text.length;
      page.cleanup();
    }
    return pages.join('\n');
  } finally {
    await pdf.loadingTask.destroy();
  }
}

/** The text of a file of `kind` (see indexableKind). */
async function extractText(buffer, kind, name, { maxChars }) {
  const ext = path.extname(String(name || '')).toLowerCase();
  if (kind === 'text') return decodeText(buffer);
  if (kind === 'html') return htmlToText(decodeText(buffer));
  if (kind === 'office') return officeToText(buffer, ext);
  if (kind === 'pdf') return pdfToText(buffer, maxChars);
  throw new Error(`Unknown kind ${kind}`);
}

module.exports = {
  MAX_SOURCE_BYTES,
  extractText,
  indexableExtensions,
  indexableKind,
};
