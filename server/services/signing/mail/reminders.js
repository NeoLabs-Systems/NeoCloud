'use strict';

// Reminders and the expiry warning. The owner can remind one party or all of
// them; documents can also remind on their own every N days. Limits that
// hold for both: at least 24 hours between two reminders to a party, at most
// 5 per party. The expiry warning goes out once, 24 hours before the end.

const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { createServiceLogger } = require('../../../utils/logger');
const { isSigningMailEnabled } = require('../../account/service_email');
const { LIVE_STATUSES, PARTY_STATUS } = require('../constants');
const { getOwnedDocument, getParty, listParties } = require('../store');
const {
  DAY_MS, MAX_REMINDERS, MIN_REMINDER_GAP_MS, countedReminders, rowsByParty,
} = require('./history');
const { cleanMessage } = require('./options');
const { assertMayMail, assertVerified } = require('./limits');
const { enqueueMail, enqueueRequest } = require('./outbox');
const { nextReminderAt } = require('./status');

const logger = createServiceLogger('SigningMail');

const EXPIRY_WARNING_MS = DAY_MS;
const ACTIVE_PARTY = [PARTY_STATUS.PENDING, PARTY_STATUS.OPENED];

function notConfigured() {
  return httpError(503, 'Email is not set up on this server.', 'EMAIL_NOT_CONFIGURED');
}

// A party the owner is waiting for: out for signing, its turn, not the owner themselves.
const isWaitedFor = (document, party) => LIVE_STATUSES.includes(document.status) && ACTIVE_PARTY.includes(party.status) && party.is_self === 0;

// ... and one whose link is on and can be mailed.
const isActive = (document, party) => isWaitedFor(document, party) && Boolean(party.token_hash) && Boolean(party.token_enc);

function assertMayRemind(rows, now) {
  const counted = countedReminders(rows);
  if (counted.length >= MAX_REMINDERS) {
    throw httpError(429, `A party can be reminded ${MAX_REMINDERS} times at most.`, 'REMINDER_LIMIT');
  }
  if (counted.length === 0) return;
  const readyAt = Date.parse(counted[counted.length - 1].created_at) + MIN_REMINDER_GAP_MS;
  if (now < readyAt) {
    const error = httpError(429, 'This party was reminded recently. Try again later.', 'REMINDER_TOO_SOON');
    error.retryAfter = Math.ceil((readyAt - now) / 1000);
    throw error;
  }
}

function remind(document, party, rows, { message, now }) {
  const notActive = () => httpError(409, 'This party is not waiting to sign.', 'PARTY_NOT_ACTIVE');
  if (!isWaitedFor(document, party)) throw notActive();
  if (!party.email) throw httpError(400, 'This party has no email address.', 'NO_EMAIL');
  if (!isActive(document, party)) throw notActive();
  assertMayRemind(rows, now);
  assertMayMail(document.owner_id, [{ email: party.email, kind: 'reminder' }], now);
  enqueueMail({
    documentId: document.id, partyId: party.id, kind: 'reminder', toEmail: party.email, payload: message ? { message } : {}, now,
  });
  return { partyId: party.id, status: 'queued' };
}

/** The owner reminds one party now. Resolves with `{ partyId, status: 'queued' }`. */
function remindParty(userId, documentId, partyId, body, { now = Date.now() } = {}) {
  const document = getOwnedDocument(userId, documentId);
  const party = getParty(document.id, partyId);
  if (!isSigningMailEnabled()) throw notConfigured();
  const message = cleanMessage(body?.message);
  assertVerified(userId);
  return remind(document, party, rowsByParty(document.id).get(party.id) || [], { message, now });
}

/** The owner reminds everybody who is waiting to sign. Resolves with `{ results }`, one entry per party that has an address. */
function remindAll(userId, documentId, body, { now = Date.now() } = {}) {
  const document = getOwnedDocument(userId, documentId);
  if (!isSigningMailEnabled()) throw notConfigured();
  const message = cleanMessage(body?.message);
  assertVerified(userId);
  const byParty = rowsByParty(document.id);
  const results = [];
  for (const party of listParties(document.id)) {
    if (!party.email || !ACTIVE_PARTY.includes(party.status) || party.is_self === 1) continue;
    try {
      results.push(remind(document, party, byParty.get(party.id) || [], { message, now }));
    } catch (err) {
      results.push({
        partyId: party.id, status: 'skipped', code: err.code, ...(err.retryAfter ? { retryAfter: err.retryAfter } : {}),
      });
    }
  }
  return { results };
}

function dueAutoReminder(document, party, rows, now) {
  const due = nextReminderAt(document, party, rows);
  if (!due || Date.parse(due) > now) return false;
  const counted = countedReminders(rows);
  const last = counted.length > 0 ? Date.parse(counted[counted.length - 1].created_at) : 0;
  return counted.length < MAX_REMINDERS && now - last >= MIN_REMINDER_GAP_MS;
}

function dueExpiryWarning(document, rows, now) {
  if (!document.expires_at || !document.sent_at) return false;
  const expiresAt = Date.parse(document.expires_at);
  const warnAt = expiresAt - EXPIRY_WARNING_MS;
  // A request that never lasted 24 hours has nothing to warn about.
  if (now < warnAt || now >= expiresAt || Date.parse(document.sent_at) >= warnAt) return false;
  return !rows.some((row) => row.kind === 'expiry_warning');
}

function sweepDocument(document, now) {
  const byParty = rowsByParty(document.id);
  let queued = 0;
  for (const party of listParties(document.id)) {
    if (!party.email || !isActive(document, party)) continue;
    const rows = byParty.get(party.id) || [];
    // The warning also restarts the reminder clock, so the two never arrive together.
    let kind = null;
    if (dueExpiryWarning(document, rows, now)) kind = 'expiry_warning';
    else if (dueAutoReminder(document, party, rows, now)) kind = 'reminder';
    if (kind) {
      if (enqueueRequest({ documentId: document.id, partyId: party.id, kind, toEmail: party.email, now }).queued) queued += 1;
    }
  }
  return queued;
}

/** Queues the automatic reminders and expiry warnings that are due. Returns how many. */
function runReminderPass({ now = Date.now() } = {}) {
  if (!isSigningMailEnabled()) return 0;
  const documents = db.prepare(`
    SELECT * FROM sign_documents WHERE status IN ('sent', 'in_progress') AND deliver_email = 1
  `).all();
  let queued = 0;
  for (const document of documents) {
    try {
      queued += sweepDocument(document, now);
    } catch (err) {
      logger.error(`Reminders for a document failed: ${err.message}`);
    }
  }
  return queued;
}

module.exports = { remindAll, remindParty, runReminderPass };
