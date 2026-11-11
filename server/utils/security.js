'use strict';

const crypto = require('crypto');
const os = require('os');
const path = require('path');

const HOME = os.homedir();
const PROJECT_ROOT = path.resolve(__dirname, '..', '..');

// An error message with local paths and module locations taken out, safe to
// log or to show an admin.
function sanitizeError(err) {
  if (!err) return 'An unexpected error occurred';
  const raw = typeof err === 'string' ? err : err.message || String(err);
  let msg = raw && raw !== '[object Object]' ? raw : 'An unexpected error occurred';
  if (PROJECT_ROOT) msg = msg.split(PROJECT_ROOT).join('[app]');
  if (HOME) msg = msg.split(HOME).join('~');
  msg = msg.replace(/(?:^|[\s'"(\[])\S*?[\\/]node_modules(?:[\\/]\S*)?/g, (match) => {
    const prefix = /^[\s'"(\[]/.test(match) ? match[0] : '';
    return `${prefix}[module]`;
  });
  return msg;
}

// Constant-time string comparison for secrets of possibly different length.
function safeEqual(a, b) {
  const left = crypto.createHash('sha256').update(String(a ?? '')).digest();
  const right = crypto.createHash('sha256').update(String(b ?? '')).digest();
  return crypto.timingSafeEqual(left, right);
}

module.exports = { sanitizeError, safeEqual };
