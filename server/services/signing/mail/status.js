'use strict';

// A party's mail as the owner's screen shows it, worked out from the outbox:
//   { status, lastSentAt, attempts, errorCode, lastError, remindersSent, nextReminderAt }
// errorCode is one of errors.js' codes and lastError a short sentence for it; the server's own text never shows.
// status: queued | sent | skipped (nothing was or will be sent) | no_email | failed.

const { LIVE_STATUSES, PARTY_STATUS } = require('../constants');
const { friendlyMailError, normalizeCode } = require('./errors');
const {
  DAY_MS, MAX_REMINDERS, countedReminders, lastRequestTime, requestRows, rowsByParty,
} = require('./history');

const STATUS_OF_ROW = { queued: 'queued', sending: 'queued', sent: 'sent', failed: 'failed' };

// When the automatic reminder is due next, or null (off, finished, limit reached, nothing sent yet).
function nextReminderAt(document, party, rows) {
  if (!document.reminder_days || !document.deliver_email || !party.email || party.is_self) return null;
  if (!LIVE_STATUSES.includes(document.status)) return null;
  if (party.status === PARTY_STATUS.COMPLETED || party.status === PARTY_STATUS.DECLINED || party.status === PARTY_STATUS.WAITING) return null;
  if (countedReminders(rows).length >= MAX_REMINDERS) return null;
  const base = lastRequestTime(rows);
  return base === null ? null : new Date(base + document.reminder_days * DAY_MS).toISOString();
}

function describeParty(document, party, rows) {
  const requests = requestRows(rows);
  const latest = [...requests].reverse().find((row) => row.status !== 'canceled');
  const sent = requests.filter((row) => row.sent_at).map((row) => row.sent_at).sort();
  let status = 'skipped';
  if (!party.email) status = 'no_email';
  else if (latest) status = STATUS_OF_ROW[latest.status];
  const failing = latest && latest.status !== 'sent' && latest.last_error ? normalizeCode(latest.last_error) : null;
  return {
    status,
    lastSentAt: sent.length > 0 ? sent[sent.length - 1] : null,
    attempts: latest ? latest.attempts : 0,
    errorCode: failing,
    lastError: failing ? friendlyMailError(failing, document.locale) : null,
    remindersSent: rows.filter((row) => row.kind === 'reminder' && row.status === 'sent').length,
    nextReminderAt: nextReminderAt(document, party, rows),
  };
}

/** partyId -> delivery object, for all parties of a document. */
function deliveryByParty(document, parties) {
  const byParty = rowsByParty(document.id);
  return new Map(parties.map((party) => [party.id, describeParty(document, party, byParty.get(party.id) || [])]));
}

module.exports = { deliveryByParty, nextReminderAt };
