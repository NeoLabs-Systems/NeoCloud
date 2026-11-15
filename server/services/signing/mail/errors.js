'use strict';

// What a person is told when a signing mail fails. The mail server's own text
// can hold host names, addresses or hints about the setup, so it stays in the
// server log (and in the admin's test message); the outbox keeps only one of
// these stable codes and the owner's screen gets a short sentence by code.

const ERROR_CODES = ['SMTP_UNREACHABLE', 'SMTP_REJECTED', 'INVALID_ADDRESS', 'RATE_LIMITED', 'UNKNOWN'];

const UNREACHABLE = new Set([
  'ECONNECTION', 'ETIMEDOUT', 'ESOCKET', 'EDNS', 'ECONNREFUSED', 'ECONNRESET', 'ENOTFOUND', 'EHOSTUNREACH',
  'ENETUNREACH', 'EAI_AGAIN', 'EPIPE', 'ETLS',
]);
const REJECTED = new Set(['EAUTH', 'EENVELOPE', 'EMESSAGE', 'EPROTOCOL', 'ESTREAM']);

const SENTENCES = {
  en: {
    SMTP_UNREACHABLE: 'The mail server could not be reached.',
    SMTP_REJECTED: 'The mail server refused the message.',
    INVALID_ADDRESS: 'The address was not accepted.',
    RATE_LIMITED: 'The mail server is limiting how much can be sent right now.',
    UNKNOWN: 'The email could not be sent.',
  },
  de: {
    SMTP_UNREACHABLE: 'Der Mailserver ist nicht erreichbar.',
    SMTP_REJECTED: 'Der Mailserver hat die Nachricht abgelehnt.',
    INVALID_ADDRESS: 'Die Adresse wurde nicht akzeptiert.',
    RATE_LIMITED: 'Der Mailserver bremst das Senden gerade.',
    UNKNOWN: 'Die E-Mail konnte nicht gesendet werden.',
  },
};

/** One of ERROR_CODES for an error thrown while sending. */
function classifyMailError(err) {
  if (!err || err.permanent) return 'UNKNOWN';
  if (err.code === 'INVALID_EMAIL') return 'INVALID_ADDRESS';
  const text = String(err.response || err.message || '');
  const status = Number(err.responseCode) || Number(text.match(/^\s*(\d{3})[ -]/)?.[1]) || 0;
  if (UNREACHABLE.has(err.code)) return 'SMTP_UNREACHABLE';
  if ([421, 450, 451, 452, 454].includes(status) || /rate limit|too many|throttl|quota|exceeded/i.test(text)) return 'RATE_LIMITED';
  if (status >= 500 && status < 600 && /recipient|mailbox|no such user|user unknown|unknown user|address|does not exist|invalid/i.test(text)) {
    return 'INVALID_ADDRESS';
  }
  if (REJECTED.has(err.code) || status >= 400) return 'SMTP_REJECTED';
  return 'UNKNOWN';
}

/** A code from the outbox (an older row may hold text: that is UNKNOWN). */
const normalizeCode = (value) => (ERROR_CODES.includes(value) ? value : 'UNKNOWN');

/** The short sentence for a code, in the document's language. */
function friendlyMailError(code, locale) {
  const table = locale === 'de' ? SENTENCES.de : SENTENCES.en;
  return table[normalizeCode(code)];
}

module.exports = { ERROR_CODES, classifyMailError, friendlyMailError, normalizeCode };
