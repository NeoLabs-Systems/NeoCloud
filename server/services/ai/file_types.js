'use strict';

// File types a person can leave out of every AI feature (setting
// `ai_skip_types`): whole categories, or single formats by extension.
// "other" is every file whose extension is in no category (or that has
// none). Pure: no database. The apps read the categories from
// GET /api/ai/status, so the list lives here only.

const FILE_TYPE_CATEGORIES = Object.freeze([
  {
    id: 'images',
    extensions: ['jpg', 'jpeg', 'png', 'heic', 'heif', 'webp', 'gif', 'tiff', 'tif', 'bmp', 'avif', 'svg',
      'raw', 'dng', 'cr2', 'cr3', 'nef', 'arw', 'orf', 'rw2', 'raf'],
  },
  { id: 'videos', extensions: ['mp4', 'mov', 'm4v', 'avi', 'mkv', 'webm', 'wmv', '3gp', 'mts'] },
  { id: 'documents', extensions: ['pdf', 'docx', 'doc', 'odt', 'rtf', 'txt', 'md', 'pages', 'epub'] },
  { id: 'spreadsheets', extensions: ['xlsx', 'xls', 'ods', 'csv', 'tsv', 'numbers'] },
  { id: 'presentations', extensions: ['pptx', 'ppt', 'odp', 'key'] },
  { id: 'archives', extensions: ['zip', 'rar', '7z', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'dmg', 'iso'] },
  { id: 'audio', extensions: ['mp3', 'm4a', 'wav', 'flac', 'aac', 'ogg', 'opus', 'aiff', 'wma'] },
  {
    id: 'code',
    extensions: ['js', 'ts', 'jsx', 'tsx', 'py', 'java', 'kt', 'swift', 'go', 'rs', 'c', 'h', 'cpp', 'cs', 'rb',
      'php', 'sh', 'dart', 'html', 'css', 'json', 'yaml', 'yml', 'xml', 'sql', 'toml'],
  },
  { id: 'other', extensions: [] },
]);

const CATEGORY_IDS = new Set(FILE_TYPE_CATEGORIES.map((category) => category.id));
const CATEGORY_OF = new Map(FILE_TYPE_CATEGORIES.flatMap((category) => category.extensions.map((ext) => [ext, category.id])));
const MAX_SKIPPED_EXTENSIONS = 200;
const EXTENSION = /^[a-z0-9]{1,10}$/;

const SKIP_TYPES_DEFAULT = Object.freeze({ categories: [], extensions: [] });

/** Valid `ai_skip_types`: { categories: [known ids], extensions: [lowercase extensions] }. */
function isSkipTypes(value) {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false;
  const keys = Object.keys(value);
  if (keys.some((key) => key !== 'categories' && key !== 'extensions')) return false;
  const { categories = [], extensions = [] } = value;
  if (!Array.isArray(categories) || !Array.isArray(extensions)) return false;
  if (categories.length > CATEGORY_IDS.size || extensions.length > MAX_SKIPPED_EXTENSIONS) return false;
  return categories.every((id) => typeof id === 'string' && CATEGORY_IDS.has(id))
    && extensions.every((ext) => typeof ext === 'string' && EXTENSION.test(ext));
}

function extensionOf(name) {
  const match = /\.([^./\\]+)$/.exec(String(name || ''));
  return match ? match[1].toLowerCase() : '';
}

function categoryOf(name) {
  return CATEGORY_OF.get(extensionOf(name)) || 'other';
}

/** Whether a file of this name is left out of AI under `skip` (an `ai_skip_types` value). */
function isSkippedType(name, skip) {
  if (!skip || typeof skip !== 'object') return false;
  const categories = Array.isArray(skip.categories) ? skip.categories : [];
  const extensions = Array.isArray(skip.extensions) ? skip.extensions : [];
  if (categories.length === 0 && extensions.length === 0) return false;
  const ext = extensionOf(name);
  if (ext && extensions.includes(ext)) return true;
  return categories.includes(categoryOf(name));
}

module.exports = { FILE_TYPE_CATEGORIES, SKIP_TYPES_DEFAULT, isSkipTypes, isSkippedType };
