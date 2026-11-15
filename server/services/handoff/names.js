'use strict';

// File names of a send. They end up on another device's disk, so they are
// made safe here for every receiver: no path parts, no control or bidi
// characters, nothing Windows refuses, unique within the send.

const { numberedName, splitExtension } = require('../../utils/file_names');

const MAX_NAME_BYTES = 200;
// Control characters, DEL, and the bidirectional overrides and isolates that
// make "report‮fdp.exe" read as a PDF.
const STRIPPED = /[\u0000-\u001f\u007f-\u009f؜​-‏‪-‮⁦-⁩﻿]/g;
const SEPARATORS = /[/\\]/g;
const WINDOWS_RESERVED = /^(con|prn|aux|nul|com[0-9]|lpt[0-9])$/i;

function truncateBytes(text, maxBytes) {
  let result = text;
  while (Buffer.byteLength(result, 'utf8') > maxBytes) result = result.slice(0, -1);
  return result;
}

/** One name that is safe to create as a plain file in any folder. */
function sanitiseFileName(raw) {
  let name = String(raw ?? '').normalize('NFC').replace(STRIPPED, '').replace(SEPARATORS, '_').trim();
  // A trailing dot or space is dropped by Windows; leading dots would hide the file.
  name = name.replace(/[. ]+$/, '').replace(/^\.+/, '');
  if (!name) return 'file';
  const [stem, extension] = splitExtension(name);
  const shortExtension = truncateBytes(extension, 20);
  const shortStem = truncateBytes(stem, MAX_NAME_BYTES - Buffer.byteLength(shortExtension, 'utf8'));
  const safeStem = WINDOWS_RESERVED.test(shortStem) ? `_${shortStem}` : shortStem;
  return `${safeStem}${shortExtension}`;
}

/** Sanitised names, made different (case-insensitively) with "name (1).ext". */
function uniqueFileNames(rawNames) {
  const used = new Set();
  return rawNames.map((raw) => {
    const clean = sanitiseFileName(raw);
    let name = clean;
    for (let n = 1; used.has(name.toLowerCase()); n += 1) name = numberedName(clean, n);
    used.add(name.toLowerCase());
    return name;
  });
}

module.exports = { sanitiseFileName, uniqueFileNames };
