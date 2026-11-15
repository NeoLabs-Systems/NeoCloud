'use strict';

// Abuse limits for signing mail. Open registration plus "mail whoever I name"
// would make an instance a mail relay, so a person may only mail from a
// verified address and only so much per day. The numbers are constants on
// purpose (docs/email.md, docs/signing.md); the admin's kill switch
// (NEOCLOUD_EMAIL_SIGNING_MAILS) stops all of it.
//
// The ledger (sign_mail_ledger) outlives the documents: deleting a document
// does not give its mails back. A mail canceled before it left does not count.

const crypto = require('crypto');
const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { LIMITS } = require('../constants');

const MAIL_LIMITS = Object.freeze({
  // Mails asking people to sign (invitation, turn, reminder, expiry warning) per account and 24 hours.
  creatorPerDay: 100,
  // The same for an administrator.
  adminPerDay: 1000,
  // Distinct addresses one document may mail.
  recipientsPerDocument: 20,
  // Invitations (and turns) to one address from the whole instance per 24 hours, whoever asks.
  recipientPerDay: 3,
});

const DAY_MS = 24 * 60 * 60 * 1000;
const COUNTED_KINDS = ['invitation', 'your_turn', 'reminder', 'expiry_warning'];
// The kinds that start a conversation with a stranger.
const INVITING_KINDS = ['invitation', 'your_turn'];

const hashOf = (address) => crypto.createHash('sha256').update(String(address || '').trim().toLowerCase()).digest('hex');
const iso = (time) => new Date(time).toISOString();

/** Remembers a queued mail for the limits. `kind` outside COUNTED_KINDS is ignored. */
function recordInLedger({ userId, toEmail, kind, mailId, now = Date.now() }) {
  if (!COUNTED_KINDS.includes(kind)) return;
  db.prepare('INSERT INTO sign_mail_ledger (user_id, recipient_hash, kind, mail_id, at) VALUES (?, ?, ?, ?, ?)')
    .run(userId, hashOf(toEmail), kind, mailId, iso(now));
}

/** A mail that was canceled before it left no longer counts. */
function removeFromLedger(mailIds) {
  const remove = db.prepare('DELETE FROM sign_mail_ledger WHERE mail_id = ?');
  for (const id of mailIds) remove.run(id);
}

/** Forgets entries older than two days. */
function pruneLedger(now = Date.now()) {
  return db.prepare('DELETE FROM sign_mail_ledger WHERE at < ?').run(iso(now - 2 * DAY_MS)).changes;
}

function verifiedOrNot(userId) {
  const user = db.prepare('SELECT email, email_verified_at, is_admin FROM users WHERE id = ?').get(userId);
  return { verified: Boolean(user?.email && user.email_verified_at), admin: user?.is_admin === 1 };
}

/**
 * Whether the account may have `items` ([{ email, kind }]) mailed now:
 * `null`, or the reason as an error code (`EMAIL_NOT_VERIFIED`,
 * `MAIL_QUOTA_EXCEEDED`).
 */
function mailProblem(userId, items, now = Date.now()) {
  if (items.length === 0) return null;
  const { verified, admin } = verifiedOrNot(userId);
  if (!verified) return 'EMAIL_NOT_VERIFIED';
  const since = iso(now - DAY_MS);
  const used = db.prepare('SELECT COUNT(*) AS count FROM sign_mail_ledger WHERE user_id = ? AND at > ?').get(userId, since).count;
  if (used + items.length > (admin ? MAIL_LIMITS.adminPerDay : MAIL_LIMITS.creatorPerDay)) return 'MAIL_QUOTA_EXCEEDED';
  const perAddress = new Map();
  for (const item of items) {
    if (!INVITING_KINDS.includes(item.kind)) continue;
    const key = hashOf(item.email);
    perAddress.set(key, (perAddress.get(key) || 0) + 1);
  }
  const countAddress = db.prepare(`
    SELECT COUNT(*) AS count FROM sign_mail_ledger WHERE recipient_hash = ? AND at > ? AND kind IN ('invitation', 'your_turn')
  `);
  for (const [key, extra] of perAddress) {
    if (countAddress.get(key, since).count + extra > MAIL_LIMITS.recipientPerDay) return 'MAIL_QUOTA_EXCEEDED';
  }
  return null;
}

const MESSAGES = {
  EMAIL_NOT_VERIFIED: [403, 'Confirm your email address before sending requests by email. You can still copy the links and send them yourself.'],
  MAIL_QUOTA_EXCEEDED: [429, 'Too many signing emails today (or to one of these addresses). Try again tomorrow, or copy the links and send them yourself.'],
};

/** Throws the 403/429 for a problem code. */
function refuse(code) {
  const [status, message] = MESSAGES[code];
  return httpError(status, message, code);
}

/** Throws 403 EMAIL_NOT_VERIFIED unless the account has a verified address. */
function assertVerified(userId) {
  if (!verifiedOrNot(userId).verified) throw refuse('EMAIL_NOT_VERIFIED');
}

/** Throws unless the account may have `items` mailed now. Also bounds the addresses of one document. */
function assertMayMail(userId, items, now = Date.now()) {
  if (new Set(items.map((item) => hashOf(item.email))).size > MAIL_LIMITS.recipientsPerDocument || items.length > LIMITS.maxParties) {
    throw refuse('MAIL_QUOTA_EXCEEDED');
  }
  const code = mailProblem(userId, items, now);
  if (code) throw refuse(code);
}

module.exports = {
  MAIL_LIMITS, assertMayMail, assertVerified, hashOf, mailProblem, pruneLedger, recordInLedger, refuse, removeFromLedger,
};
