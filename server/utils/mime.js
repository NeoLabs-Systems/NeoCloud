'use strict';

const { splitExtension } = require('./file_names');

const TYPES = {
  '.txt': 'text/plain', '.md': 'text/markdown', '.csv': 'text/csv', '.log': 'text/plain',
  '.json': 'application/json', '.xml': 'application/xml', '.yaml': 'text/yaml', '.yml': 'text/yaml',
  '.html': 'text/html', '.htm': 'text/html', '.css': 'text/css', '.js': 'text/javascript',
  '.ts': 'text/plain', '.dart': 'text/plain', '.py': 'text/x-python', '.sh': 'text/x-shellscript',
  '.pdf': 'application/pdf',
  '.doc': 'application/msword',
  '.docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  '.xls': 'application/vnd.ms-excel',
  '.xlsx': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  '.ppt': 'application/vnd.ms-powerpoint',
  '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  '.odt': 'application/vnd.oasis.opendocument.text',
  '.ods': 'application/vnd.oasis.opendocument.spreadsheet',
  '.odp': 'application/vnd.oasis.opendocument.presentation',
  '.rtf': 'application/rtf',
  '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.png': 'image/png', '.gif': 'image/gif',
  '.webp': 'image/webp', '.svg': 'image/svg+xml', '.heic': 'image/heic', '.heif': 'image/heif',
  '.bmp': 'image/bmp', '.tif': 'image/tiff', '.tiff': 'image/tiff', '.avif': 'image/avif',
  '.mp3': 'audio/mpeg', '.m4a': 'audio/mp4', '.wav': 'audio/wav', '.flac': 'audio/flac',
  '.ogg': 'audio/ogg', '.opus': 'audio/opus',
  '.mp4': 'video/mp4', '.m4v': 'video/mp4', '.mov': 'video/quicktime', '.webm': 'video/webm',
  '.mkv': 'video/x-matroska', '.avi': 'video/x-msvideo',
  '.zip': 'application/zip', '.gz': 'application/gzip', '.tar': 'application/x-tar',
  '.7z': 'application/x-7z-compressed', '.rar': 'application/vnd.rar',
};

// Types a browser would run or render as a page. They are always downloaded,
// never shown inline, so a stored file can't script the app's origin.
const ACTIVE_TYPES = new Set(['text/html', 'image/svg+xml', 'application/xml', 'text/javascript']);

function mimeForName(name) {
  const [, extension] = splitExtension(String(name || ''));
  return TYPES[extension.toLowerCase()] || 'application/octet-stream';
}

function isActiveContent(mime) {
  return ACTIVE_TYPES.has(String(mime || '').toLowerCase());
}

module.exports = { isActiveContent, mimeForName };
