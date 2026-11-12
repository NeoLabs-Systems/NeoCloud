'use strict';

// The little NeoCloud reads from an MP4 / QuickTime movie: when it was
// recorded, how long it runs and how big the picture is. Works on the box
// headers and the `moov` box only, so the caller reads a few kilobytes of a
// file that may be gigabytes long. Anything malformed reads as nothing.

// Seconds between 1904-01-01 (the QuickTime epoch) and 1970-01-01.
const QUICKTIME_EPOCH_OFFSET = 2082844800;
const MAX_BOXES = 4096;
const CONTAINERS = new Set(['moov', 'trak', 'mdia', 'minf', 'stbl', 'edts', 'udta']);

/**
 * The header of the box at `offset` in `buffer`: { type, size, headerSize },
 * or null when it doesn't fit. `fileSize` resolves a size of 0 ("to the end").
 */
function readBoxHeader(buffer, offset = 0, fileSize = buffer.length) {
  if (offset + 8 > buffer.length) return null;
  let size = buffer.readUInt32BE(offset);
  const type = buffer.toString('latin1', offset + 4, offset + 8);
  let headerSize = 8;
  if (size === 1) {
    if (offset + 16 > buffer.length) return null;
    size = Number(buffer.readBigUInt64BE(offset + 8));
    headerSize = 16;
  } else if (size === 0) {
    size = fileSize - offset;
  }
  if (size < headerSize || !/^[\x20-\x7e]{4}$/.test(type)) return null;
  return { type, size, headerSize };
}

// Every box below `moov` that NeoCloud reads, in file order: [{ type, body }].
function walkBoxes(buffer, start, end, found = [], depth = 0) {
  let offset = start;
  while (offset < end && found.length < MAX_BOXES) {
    const header = readBoxHeader(buffer, offset, end);
    if (!header || offset + header.size > end) break;
    const bodyStart = offset + header.headerSize;
    const bodyEnd = offset + header.size;
    found.push({ type: header.type, body: buffer.subarray(bodyStart, bodyEnd) });
    if (CONTAINERS.has(header.type) && depth < 8) walkBoxes(buffer, bodyStart, bodyEnd, found, depth + 1);
    offset = bodyEnd;
  }
  return found;
}

function quickTimeDate(seconds) {
  if (!Number.isFinite(seconds) || seconds <= QUICKTIME_EPOCH_OFFSET) return null;
  const date = new Date((seconds - QUICKTIME_EPOCH_OFFSET) * 1000);
  const year = date.getUTCFullYear();
  if (year < 1971 || year > 2200) return null;
  return date.toISOString().slice(0, 19);
}

// mvhd: version, flags, creation and modification time, timescale, duration.
function readMovieHeader(body) {
  if (body.length < 20) return null;
  const version = body[0];
  if (version === 1) {
    if (body.length < 32) return null;
    return {
      created: Number(body.readBigUInt64BE(4)),
      timescale: body.readUInt32BE(20),
      duration: Number(body.readBigUInt64BE(24)),
    };
  }
  return { created: body.readUInt32BE(4), timescale: body.readUInt32BE(12), duration: body.readUInt32BE(16) };
}

// tkhd ends with the 3x3 matrix and the width and height as 16.16 numbers.
function readTrackSize(body) {
  if (body.length < 84) return null;
  const width = Math.round(body.readUInt32BE(body.length - 8) / 65536);
  const height = Math.round(body.readUInt32BE(body.length - 4) / 65536);
  if (width <= 0 || height <= 0) return null;
  // The matrix (nine values) sits before them; its second value is
  // non-zero for a 90° or 270° turn.
  const turned = body.readInt32BE(body.length - 8 - 36 + 4) !== 0;
  return turned ? { width: height, height: width } : { width, height };
}

/**
 * { takenAt: "YYYY-MM-DDTHH:MM:SS" (UTC), durationSeconds, width, height }
 * from the bytes of a `moov` box (header included); null fields when absent.
 */
function readMovie(moov) {
  const empty = { takenAt: null, durationSeconds: null, width: null, height: null };
  if (!Buffer.isBuffer(moov)) return empty;
  const header = readBoxHeader(moov, 0);
  if (!header || header.type !== 'moov') return empty;
  const boxes = walkBoxes(moov, header.headerSize, Math.min(header.size, moov.length));
  const result = { ...empty };
  const mvhd = boxes.find((box) => box.type === 'mvhd');
  const movie = mvhd ? readMovieHeader(mvhd.body) : null;
  if (movie) {
    result.takenAt = quickTimeDate(movie.created);
    if (movie.timescale > 0 && movie.duration > 0) {
      result.durationSeconds = Math.round((movie.duration / movie.timescale) * 10) / 10;
    }
  }
  for (const box of boxes) {
    if (box.type !== 'tkhd') continue;
    const size = readTrackSize(box.body);
    if (size) {
      result.width = size.width;
      result.height = size.height;
      break;
    }
  }
  return result;
}

module.exports = { readBoxHeader, readMovie };
