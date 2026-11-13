'use strict';

// What the Photos timeline needs to know about one photo or video: when it
// was taken, its size in pixels, and for a video how long it runs. Reads as
// little of the (encrypted) file as it can: the head of a JPEG, the box
// headers and `moov` box of a movie.

const sharp = require('sharp');
const { openBlob } = require('../storage/blob_store');
const { readExif } = require('../../utils/exif');
const { readBoxHeader, readMovie } = require('../../utils/mp4');

const JPEG_HEAD_BYTES = 256 * 1024;
const MAX_IMAGE_BYTES = 64 * 1024 * 1024;
const MAX_MOOV_BYTES = 32 * 1024 * 1024;
const MAX_TOP_LEVEL_BOXES = 64;
const MOVIE_MIMES = new Set(['video/mp4', 'video/quicktime', 'video/3gpp', 'video/3gpp2', 'video/x-m4v']);
// The EXIF orientations 5 to 8 turn the picture by 90°.
const TURNED_ORIENTATION = 5;

async function readRange(userId, node, start, end) {
  const chunks = [];
  for await (const chunk of openBlob(userId, node.blob_id, node.size, { start, end })) chunks.push(chunk);
  return Buffer.concat(chunks);
}

function isPlausible(dateTime) {
  if (!dateTime || dateTime < '1900-01-01') return false;
  const limit = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 19);
  return dateTime <= limit;
}

/**
 * When a camera or phone named the file: IMG_20260312_101010.jpg,
 * PXL_20260312_101010123.mp4, Screenshot_2026-03-12-10-10-10.png,
 * 2026-03-12 10.10.10.jpg. "YYYY-MM-DDTHH:MM:SS", midnight when the name
 * has only a date, or null.
 */
function takenFromName(name) {
  const match = /(?:^|\D)((?:19|20)\d{2})([-_.]?)(0[1-9]|1[0-2])\2(0[1-9]|[12]\d|3[01])(?:[ _T-]?([01]\d|2[0-3])[-_.:]?([0-5]\d)[-_.:]?([0-5]\d))?/
    .exec(String(name || ''));
  if (!match) return null;
  const [, year, , month, day, hour = '00', minute = '00', second = '00'] = match;
  const date = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day)));
  if (date.getUTCDate() !== Number(day)) return null;
  const taken = `${year}-${month}-${day}T${hour}:${minute}:${second}`;
  return isPlausible(taken) ? taken : null;
}

/** When the file reached the server, as nodes.modified_at has it: a last resort. */
function uploadedAt(node) {
  return String(node.modified_at).replace(' ', 'T').slice(0, 19);
}

/**
 * The best date known for a photo or video, and where it came from: the
 * file's own (EXIF or the video's header, `takenAt`), the device's capture
 * or creation date, the file name, the device's modification date, else
 * the upload time (source "upload": the date is unknown).
 * `node`: name, modified_at, client_created_at, client_modified_at.
 */
function resolveTakenAt(node, takenAt = null) {
  if (takenAt) return { takenAt, source: 'exif' };
  if (node.client_created_at) return { takenAt: node.client_created_at, source: 'client' };
  const named = takenFromName(node.name);
  if (named) return { takenAt: named, source: 'filename' };
  if (node.client_modified_at) return { takenAt: node.client_modified_at, source: 'mtime' };
  return { takenAt: uploadedAt(node), source: 'upload' };
}

async function probePhoto(userId, node) {
  let metadata;
  if (node.mime === 'image/jpeg' && node.size > JPEG_HEAD_BYTES) {
    try {
      metadata = await sharp(await readRange(userId, node, 0, JPEG_HEAD_BYTES - 1)).metadata();
    } catch {
      metadata = null;
    }
  }
  if (!metadata?.width) {
    if (node.size > MAX_IMAGE_BYTES) return {};
    metadata = await sharp(await readRange(userId, node, 0, node.size - 1), { failOn: 'none' }).metadata();
  }
  const turned = (metadata.orientation || 1) >= TURNED_ORIENTATION;
  const taken = readExif(metadata.exif).takenDateTime;
  return {
    takenAt: isPlausible(taken) ? taken : null,
    width: (turned ? metadata.height : metadata.width) || null,
    height: (turned ? metadata.width : metadata.height) || null,
  };
}

// Walks the top-level boxes to the `moov` box and reads only that.
async function probeMovie(userId, node) {
  let offset = 0;
  for (let count = 0; count < MAX_TOP_LEVEL_BOXES && offset + 8 <= node.size; count += 1) {
    const head = await readRange(userId, node, offset, Math.min(offset + 15, node.size - 1));
    const header = readBoxHeader(head, 0, node.size - offset);
    if (!header) return {};
    if (header.type === 'moov') {
      if (header.size > MAX_MOOV_BYTES) return {};
      const movie = readMovie(await readRange(userId, node, offset, offset + header.size - 1));
      return { ...movie, takenAt: isPlausible(movie.takenAt) ? movie.takenAt : null };
    }
    offset += header.size;
  }
  return {};
}

/**
 * { takenAt?, width?, height?, durationSeconds? } of a photo or video node
 * (id, name, mime, size, blob_id); fields it couldn't find are left out or
 * null. Throws when the file can't be read.
 */
async function probeMedia(userId, node, kind) {
  if (node.size === 0) return {};
  if (kind === 'photo') return probePhoto(userId, node);
  if (MOVIE_MIMES.has(node.mime)) return probeMovie(userId, node);
  return {};
}

module.exports = { probeMedia, resolveTakenAt, takenFromName, uploadedAt };
