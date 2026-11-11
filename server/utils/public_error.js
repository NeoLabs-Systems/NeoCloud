'use strict';

// What an account without admin rights may read about a failure: messages
// written for people survive, exception text does not.

const GENERIC_ERROR_MESSAGE = 'Something went wrong. Please try again.';
const MAX_READABLE_LENGTH = 300;

const TECHNICAL_PATTERNS = [
  /(?:^|\n)\s*(?:at\s+\S|#\d+\s)/,
  /\b\w*(?:Error|Exception):/,
  /\b(?:ECONN\w+|ENOTFOUND|ETIMEDOUT|EPIPE|ENOENT|EACCES|EPERM|ENOSPC|EADDR\w+|ERR_[A-Z0-9_]+|SQLITE_[A-Z0-9_]+)\b/,
  /\[(?:module|app)\]|node_modules|(?:^|[\s'"(])(?:~\/|\/Users\/|\/home\/|\/var\/|\/tmp\/|\/etc\/|[A-Za-z]:\\)/,
  /stack trace|cannot read propert|is not a function|is not defined|unexpected token|constraint failed|no such (?:table|column)|\bsqlite\b/i,
];

function looksTechnical(text) {
  const value = String(text || '').trim();
  if (!value) return false;
  if (value.includes('\n') || value.length > MAX_READABLE_LENGTH) return true;
  return TECHNICAL_PATTERNS.some((pattern) => pattern.test(value));
}

// A JSON response as a non-admin may see it. A 5xx says nothing about its
// cause unless it carries an application code (507 DISK_FULL); any other
// response keeps its text unless that text is technical.
function redactResponseBody(body, status) {
  if (!body || typeof body !== 'object' || Array.isArray(body)) return body;
  if (typeof body.error !== 'string') return body;
  if ((status >= 500 && !body.code) || looksTechnical(body.error)) {
    const { message, detail, stack, ...rest } = body;
    return { ...rest, error: GENERIC_ERROR_MESSAGE };
  }
  return body;
}

module.exports = { GENERIC_ERROR_MESSAGE, looksTechnical, redactResponseBody };
