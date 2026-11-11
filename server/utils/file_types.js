'use strict';

// Coarse kinds of files for search filters and the storage breakdown. A file
// belongs to the first group whose MIME type or extension matches; anything
// else is "other". Folders are their own kind.

const { splitExtension } = require('./file_names');

const FILE_GROUPS = Object.freeze({
  image: {
    mimePrefixes: ['image/'],
    mimes: [],
    extensions: ['.cr2', '.cr3', '.nef', '.arw', '.dng', '.raf', '.orf', '.rw2', '.psd', '.ico'],
  },
  video: {
    mimePrefixes: ['video/'],
    mimes: [],
    extensions: ['.3gp', '.mts', '.m2ts', '.wmv', '.flv', '.mpg', '.mpeg', '.vob'],
  },
  audio: {
    mimePrefixes: ['audio/'],
    mimes: [],
    extensions: ['.aac', '.aif', '.aiff', '.wma', '.mid', '.midi', '.alac'],
  },
  pdf: {
    mimePrefixes: [],
    mimes: ['application/pdf'],
    extensions: [],
  },
  document: {
    mimePrefixes: ['text/', 'application/vnd.openxmlformats-officedocument.', 'application/vnd.oasis.opendocument.'],
    mimes: ['application/msword', 'application/vnd.ms-excel', 'application/vnd.ms-powerpoint', 'application/rtf', 'application/json', 'application/xml'],
    extensions: ['.pages', '.numbers', '.key', '.epub', '.tex'],
  },
  archive: {
    mimePrefixes: [],
    mimes: ['application/zip', 'application/gzip', 'application/x-tar', 'application/x-7z-compressed', 'application/vnd.rar'],
    extensions: ['.bz2', '.xz', '.tgz', '.zst', '.dmg', '.iso'],
  },
});

const SEARCH_TYPES = Object.freeze([...Object.keys(FILE_GROUPS), 'folder']);

/** The group of a file: one of FILE_GROUPS' keys, or "other". */
function fileGroup(mime, name) {
  const type = String(mime || '').toLowerCase();
  const [, extension] = splitExtension(String(name || ''));
  const ext = extension.toLowerCase();
  for (const [group, spec] of Object.entries(FILE_GROUPS)) {
    if (spec.mimePrefixes.some((prefix) => type.startsWith(prefix))) return group;
    if (spec.mimes.includes(type)) return group;
    if (ext && spec.extensions.includes(ext)) return group;
  }
  return 'other';
}

/**
 * The SQL condition for one search type over a `nodes` row, with its named
 * parameters. The SQL text is built from the constants above only; every
 * value is bound.
 */
function typeCondition(type, prefix) {
  if (type === 'folder') return { sql: "type = 'folder'", params: {} };
  const spec = FILE_GROUPS[type];
  const parts = [];
  const params = {};
  const add = (sql, value) => {
    const name = `${prefix}${Object.keys(params).length}`;
    params[name] = value;
    parts.push(sql.replace('?', `@${name}`));
  };
  for (const value of spec.mimePrefixes) add('mime LIKE ?', `${value}%`);
  for (const value of spec.mimes) add('mime = ?', value);
  for (const value of spec.extensions) add('lower(name) LIKE ?', `%${value}`);
  return { sql: `(type = 'file' AND (${parts.join(' OR ')}))`, params };
}

module.exports = { FILE_GROUPS, SEARCH_TYPES, fileGroup, typeCondition };
