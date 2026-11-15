'use strict';

// The name of a signed file, in the document's language.

const { splitExtension } = require('../../utils/file_names');

const FINAL_SUFFIX = { en: 'signed', de: 'signiert' };
const MAX_BASE_NAME = 200;

// "<base> (signed).pdf", or "<base> (signed, no certificate).pdf" for the variant without audit pages.
function finalFileName(document, { certificate = true } = {}) {
  const base = splitExtension(document.source_name)[0] || document.title;
  const german = document.locale === 'de';
  const word = FINAL_SUFFIX[german ? 'de' : 'en'];
  const extra = certificate ? '' : `, ${german ? 'ohne Protokoll' : 'no certificate'}`;
  return `${base.slice(0, MAX_BASE_NAME)} (${word}${extra}).pdf`;
}

module.exports = { finalFileName };
