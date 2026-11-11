'use strict';

const UNITS = { B: 1, KB: 1024, MB: 1024 ** 2, GB: 1024 ** 3, TB: 1024 ** 4 };

// "10GB", "512 MB", "1.5tb" or a plain byte count. Binary units throughout,
// matching what file managers show. Returns null for anything unparseable.
function parseByteSize(value) {
  if (typeof value === 'number') {
    return Number.isSafeInteger(value) && value >= 0 ? value : null;
  }
  const match = String(value ?? '').trim().toUpperCase().match(/^(\d+(?:\.\d+)?)\s*(B|KB|MB|GB|TB|K|M|G|T)?I?B?$/);
  if (!match) return null;
  const unit = match[2] ? (match[2].length === 1 && match[2] !== 'B' ? `${match[2]}B` : match[2]) : 'B';
  const bytes = Math.floor(Number(match[1]) * UNITS[unit]);
  return Number.isSafeInteger(bytes) ? bytes : null;
}

function formatBytes(bytes) {
  const value = Number(bytes) || 0;
  if (value < 1024) return `${value} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let scaled = value;
  let unit = 'B';
  for (const next of units) {
    if (scaled < 1024) break;
    scaled /= 1024;
    unit = next;
  }
  return `${scaled.toFixed(scaled >= 10 ? 0 : 1)} ${unit}`;
}

module.exports = { formatBytes, parseByteSize };
