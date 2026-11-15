'use strict';

// What the outbox holds about a document's parties: the facts the delivery
// status, the reminder limits and the automatic reminders are all worked out
// from. Pure reads.

const db = require('../../../db/database');

const DAY_MS = 24 * 60 * 60 * 1000;
const MAX_REMINDERS = 5;
const MIN_REMINDER_GAP_MS = DAY_MS;
// The mails that ask a party to sign (the creator's notices and the completion mail are not).
const REQUEST_KINDS = ['invitation', 'your_turn', 'reminder', 'expiry_warning'];
const LIVE_MAIL = ['queued', 'sending', 'sent'];

/** partyId -> its outbox rows, oldest first. */
function rowsByParty(documentId) {
  const rows = db.prepare(`
    SELECT id, party_id, kind, status, attempts, last_error, created_at, sent_at, next_attempt_at
    FROM sign_mail_outbox WHERE document_id = ? AND party_id IS NOT NULL ORDER BY id
  `).all(documentId);
  const byParty = new Map();
  for (const row of rows) {
    if (!byParty.has(row.party_id)) byParty.set(row.party_id, []);
    byParty.get(row.party_id).push(row);
  }
  return byParty;
}

const requestRows = (rows) => rows.filter((row) => REQUEST_KINDS.includes(row.kind));

/** Reminders that count against the limits: queued, being sent or sent (not canceled or failed). */
const countedReminders = (rows) => rows.filter((row) => row.kind === 'reminder' && LIVE_MAIL.includes(row.status));

/** When the last request mail (invitation, turn, reminder or expiry warning) was queued, as a time, or null. */
function lastRequestTime(rows) {
  const live = requestRows(rows).filter((row) => LIVE_MAIL.includes(row.status));
  return live.length === 0 ? null : Date.parse(live[live.length - 1].created_at);
}

module.exports = {
  DAY_MS,
  LIVE_MAIL,
  MAX_REMINDERS,
  MIN_REMINDER_GAP_MS,
  countedReminders,
  lastRequestTime,
  requestRows,
  rowsByParty,
};
