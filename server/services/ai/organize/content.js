'use strict';

// What a model gets to see of a file. Text first: the search index's
// extracted text when it has read this content (through its own store, which
// decrypts with the owner's key), else the same extractor run in memory.
// Images go to the vision role, downscaled, unless the index has text for
// them (OCR). Nothing here is written to disk.

const sharp = require('sharp');
const { openBlob } = require('../../storage/blob_store');
const { readIndexedText } = require('../../search_index/store');
const { ExtractError, extractNodeText } = require('../../search_index/extract');
const { indexableKind } = require('../../search_index/extractors');
const { isIndexEnabled } = require('../../search_index/indexer');
const { readExif } = require('../../../utils/exif');

// About 4k tokens of text is enough to name, tag and place a document.
const MAX_PROMPT_CHARS = 12000;
const MAX_IMAGE_SOURCE_BYTES = 40 * 1024 * 1024;
const VISION_EDGE = 1024;
// How long an upload waits for the indexer before reading the file itself.
const INDEX_WAIT_MS = 2 * 60 * 1000;

async function readBlob(userId, node) {
  const chunks = [];
  for await (const chunk of openBlob(userId, node.blob_id, node.size)) chunks.push(chunk);
  return Buffer.concat(chunks);
}

function isImage(node) {
  return /^image\//.test(String(node.mime || '')) && node.mime !== 'image/svg+xml';
}

function promptText(text) {
  const clean = String(text || '').replace(/\s+/g, ' ').trim();
  return clean.length > MAX_PROMPT_CHARS ? clean.slice(0, MAX_PROMPT_CHARS) : clean;
}

async function readImage(userId, node) {
  if (node.size > MAX_IMAGE_SOURCE_BYTES) return { kind: 'none', reason: 'TOO_LARGE' };
  const source = await readBlob(userId, node);
  try {
    const picture = sharp(source, { failOn: 'none', limitInputPixels: 100_000_000 });
    const metadata = await picture.metadata();
    const data = await picture
      .rotate()
      .resize(VISION_EDGE, VISION_EDGE, { fit: 'inside', withoutEnlargement: true })
      .jpeg({ quality: 80 })
      .toBuffer();
    return { kind: 'image', image: { data, mimeType: 'image/jpeg' }, exif: readExif(metadata.exif) };
  } catch {
    return { kind: 'none', reason: 'UNREADABLE' };
  }
}

/** The EXIF camera and date of an image node, without decoding the picture. */
async function readImageExif(userId, node) {
  if (!isImage(node) || node.size > MAX_IMAGE_SOURCE_BYTES) return readExif(null);
  try {
    const metadata = await sharp(await readBlob(userId, node), { failOn: 'none' }).metadata();
    return readExif(metadata.exif);
  } catch {
    return readExif(null);
  }
}

/**
 * The input for one file node (a row with id, name, mime, size, blob_id,
 * created_at):
 *   { kind: 'text', text, exif }   text for the text role
 *   { kind: 'image', image, exif } a downscaled JPEG for the vision role
 *   { kind: 'wait' }               the indexer will read it soon (uploads only)
 *   { kind: 'none', reason }       nothing a model can use
 */
async function readFileInput(userId, node, { waitForIndex = false } = {}) {
  if (!node.blob_id) return { kind: 'none', reason: 'EMPTY' };
  const indexed = readIndexedText(userId, node);
  if (indexed?.status === 'indexed' && indexed.text.trim()) {
    return { kind: 'text', text: promptText(indexed.text), exif: await readImageExif(userId, node) };
  }
  if (isImage(node)) return readImage(userId, node);
  const exif = readExif(null);
  if (!indexableKind(node.name, node.mime)) return { kind: 'none', reason: 'UNSUPPORTED' };
  if (indexed?.status === 'failed') return { kind: 'none', reason: 'UNREADABLE' };
  const fresh = Date.now() - Date.parse(`${String(node.created_at).replace(' ', 'T')}Z`) < INDEX_WAIT_MS;
  if (!indexed && waitForIndex && isIndexEnabled() && fresh) return { kind: 'wait' };
  try {
    const text = promptText(await extractNodeText(userId, node));
    return text ? { kind: 'text', text, exif } : { kind: 'none', reason: 'NO_TEXT' };
  } catch (error) {
    if (error instanceof ExtractError) return { kind: 'none', reason: 'UNREADABLE' };
    throw error;
  }
}

module.exports = { isImage, readFileInput };
