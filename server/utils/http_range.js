'use strict';

/**
 * Parses a Range header against a file of `size` bytes. Returns null to send
 * the whole file (no header, or several ranges), { start, end } for one
 * range, or { unsatisfiable: true }.
 */
function parseRange(header, size) {
  const value = String(header || '').trim();
  const match = value.match(/^bytes=(\d*)-(\d*)$/);
  if (!value || !match || (match[1] === '' && match[2] === '')) return null;
  let start;
  let end;
  if (match[1] === '') {
    const suffix = Number(match[2]);
    if (suffix === 0) return { unsatisfiable: true };
    start = Math.max(0, size - suffix);
    end = size - 1;
  } else {
    start = Number(match[1]);
    end = match[2] === '' ? size - 1 : Math.min(Number(match[2]), size - 1);
  }
  if (start >= size || start > end) return { unsatisfiable: true };
  return { start, end };
}

module.exports = { parseRange };
