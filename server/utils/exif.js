'use strict';

// The few EXIF values NeoCloud reads from a photo: the camera (make, model)
// and when it was taken. Takes the raw EXIF block sharp reports (with or
// without its "Exif\0\0" prefix); anything malformed reads as nothing.

const TAG_MAKE = 0x010f;
const TAG_MODEL = 0x0110;
const TAG_DATE_TIME = 0x0132;
const TAG_EXIF_IFD = 0x8769;
const TAG_DATE_TIME_ORIGINAL = 0x9003;
const TYPE_ASCII = 2;
const TYPE_LONG = 4;
const MAX_ENTRIES = 512;

function readIfd(view, offset, little) {
  const entries = new Map();
  if (offset < 8 || offset + 2 > view.byteLength) return entries;
  const count = Math.min(view.getUint16(offset, little), MAX_ENTRIES);
  for (let index = 0; index < count; index += 1) {
    const at = offset + 2 + index * 12;
    if (at + 12 > view.byteLength) break;
    const tag = view.getUint16(at, little);
    const type = view.getUint16(at + 2, little);
    const length = view.getUint32(at + 4, little);
    if (type === TYPE_LONG) {
      entries.set(tag, view.getUint32(at + 8, little));
    } else if (type === TYPE_ASCII) {
      const start = length <= 4 ? at + 8 : view.getUint32(at + 8, little);
      if (start + length > view.byteLength) continue;
      const bytes = new Uint8Array(view.buffer, view.byteOffset + start, length);
      entries.set(tag, Buffer.from(bytes).toString('latin1').replace(/\0+$/, '').trim());
    }
  }
  return entries;
}

// "2026:03:14 09:26:53" -> "2026-03-14"
function exifDate(value) {
  const match = /^(\d{4}):(\d{2}):(\d{2})/.exec(String(value || ''));
  if (!match || match[1] === '0000') return null;
  return `${match[1]}-${match[2]}-${match[3]}`;
}

/** { make, model, takenAt: "YYYY-MM-DD" } from an EXIF block; null fields when absent. */
function readExif(buffer) {
  const empty = { make: null, model: null, takenAt: null };
  if (!Buffer.isBuffer(buffer) || buffer.length < 14) return empty;
  const tiff = buffer.subarray(0, 6).toString('latin1') === 'Exif\0\0' ? buffer.subarray(6) : buffer;
  const view = new DataView(tiff.buffer, tiff.byteOffset, tiff.byteLength);
  const order = tiff.subarray(0, 2).toString('latin1');
  if (order !== 'II' && order !== 'MM') return empty;
  const little = order === 'II';
  if (view.getUint16(2, little) !== 42) return empty;
  const ifd0 = readIfd(view, view.getUint32(4, little), little);
  const exifOffset = ifd0.get(TAG_EXIF_IFD);
  const exif = typeof exifOffset === 'number' ? readIfd(view, exifOffset, little) : new Map();
  const text = (value) => (typeof value === 'string' && value ? value : null);
  return {
    make: text(ifd0.get(TAG_MAKE)),
    model: text(ifd0.get(TAG_MODEL)),
    takenAt: exifDate(exif.get(TAG_DATE_TIME_ORIGINAL)) || exifDate(ifd0.get(TAG_DATE_TIME)),
  };
}

module.exports = { readExif };
