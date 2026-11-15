'use strict';

// Text people type that is later shown in lists and printed on the
// certificate. Direction overrides (a name that reads reversed), line and
// paragraph separators and zero-width characters can make a name look like
// something else, so they never get stored.

const INVISIBLE = /[\u200b-\u200f\u2028-\u202e\u2060-\u2069\ufeff]/g;
const CONTROLS = /[\u0000-\u001f\u007f-\u009f]/g;

/** The text without direction overrides, separators and zero-width characters; line breaks stay. */
function withoutInvisible(value) {
  return String(value).replace(INVISIBLE, '');
}

/** One line: invisible characters dropped, every control character (a line break too) turned into a space. */
function singleLine(value) {
  return withoutInvisible(value).replace(CONTROLS, ' ').replace(/ {2,}/g, ' ').trim();
}

module.exports = { singleLine, withoutInvisible };
