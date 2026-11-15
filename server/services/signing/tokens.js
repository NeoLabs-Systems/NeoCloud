'use strict';

// Signing links. A token is 256 random bits (base64url, 43 characters); only
// its SHA-256 is stored, so a copy of the database holds no usable link.

const crypto = require('crypto');
const { safeEqual } = require('../../utils/security');

const TOKEN_PATTERN = /^[A-Za-z0-9_-]{43}$/;
const HINT_LENGTH = 4;

function generateToken() {
  return crypto.randomBytes(32).toString('base64url');
}

function hashToken(token) {
  return crypto.createHash('sha256').update(String(token)).digest('hex');
}

// Shape check before any database work: anything else cannot be a token.
function isWellFormed(token) {
  return typeof token === 'string' && TOKEN_PATTERN.test(token);
}

// What the owner's screen may show to tell links apart.
function tokenHint(token) {
  return token.slice(-HINT_LENGTH);
}

// The stored hash is found by index; the comparison that follows is
// constant-time so a lookup is never the only thing between a guess and a hit.
function matchesHash(token, storedHash) {
  return Boolean(storedHash) && safeEqual(hashToken(token), storedHash);
}

module.exports = { generateToken, hashToken, isWellFormed, matchesHash, tokenHint };
