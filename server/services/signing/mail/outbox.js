'use strict';

// The queue of signing mail. A mail is first a row (sign_mail_outbox): a
// slow or broken SMTP server never loses an invitation, and a restart picks
// up where it stopped. Rows hold the parameters of a template, never a link
// or a token; compose.js renders them at the moment they are sent, from the
// party's current link.
//
// Lifecycle: queued -> sending -> sent, or back to queued with a longer wait
// after a failure (1, 5, 30, 120, 120 minutes), or failed after 6 attempts.
// A mail that is no longer wanted (the party signed, the document ended) is
// canceled while it is still queued. At most CONCURRENCY mails are sent at
// once.

const db = require('../../../db/database');
const { createServiceLogger } = require('../../../utils/logger');
const { describeSendError, isServiceEmailConfigured, isSigningMailEnabled, sendServiceEmail } = require('../../account/service_email');
const { EVENT } = require('../constants');
const { appendEvent } = require('../events');
const { notifyOwner } = require('../notify');
const { getDocumentById, parseJson } = require('../store');
const { maskAddressesIn, maskEmail } = require('./address');
const { composeMail } = require('./compose');
const { classifyMailError } = require('./errors');
const { mailProblem, recordInLedger, removeFromLedger } = require('./limits');

const logger = createServiceLogger('SigningMail');

const BACKOFF_MINUTES = [1, 5, 30, 120, 120];
const MAX_ATTEMPTS = BACKOFF_MINUTES.length + 1;
const CONCURRENCY = 2;
// A mail "sending" for longer than this was interrupted (a crash): it is tried again.
const STUCK_AFTER_MS = 10 * 60 * 1000;
const MAX_ERROR_TEXT = 300;

const MINUTE_MS = 60 * 1000;
const iso = (time) => new Date(time).toISOString();

/** Queues one mail and records it in the document's trail. Returns the row id. */
function enqueueMail({ documentId, partyId = null, kind, toEmail, payload = {}, now = Date.now() }) {
  const at = iso(now);
  const id = db.transaction(() => {
    const row = db.prepare(`
      INSERT INTO sign_mail_outbox (document_id, party_id, kind, to_email, payload_json, status, next_attempt_at, created_at)
      VALUES (?, ?, ?, ?, ?, 'queued', ?, ?)
    `).run(documentId, partyId, kind, toEmail, JSON.stringify(payload), at, at);
    appendEvent(documentId, {
      type: EVENT.MAIL_QUEUED, partyId, at, data: { kind, to: maskEmail(toEmail), mailId: Number(row.lastInsertRowid) },
    });
    const owner = db.prepare('SELECT owner_id FROM sign_documents WHERE id = ?').get(documentId);
    if (owner) recordInLedger({ userId: owner.owner_id, toEmail, kind, mailId: Number(row.lastInsertRowid), now });
    return Number(row.lastInsertRowid);
  })();
  kick();
  return id;
}

/**
 * Queues a mail that asks somebody to sign, unless the account is over its
 * limits (limits.js): then the mail is recorded as failed ("rate limited") so
 * the owner sees it and can copy the link instead. `{ queued, code? }`.
 */
function enqueueRequest(args) {
  const owner = db.prepare('SELECT owner_id FROM sign_documents WHERE id = ?').get(args.documentId);
  const problem = owner ? mailProblem(owner.owner_id, [{ email: args.toEmail, kind: args.kind }], args.now) : 'UNKNOWN';
  if (!problem) return { queued: true, id: enqueueMail(args) };
  const at = iso(args.now || Date.now());
  db.transaction(() => {
    db.prepare(`
      INSERT INTO sign_mail_outbox (document_id, party_id, kind, to_email, payload_json, status, attempts, last_error, next_attempt_at, created_at)
      VALUES (?, ?, ?, ?, '{}', 'failed', 0, ?, ?, ?)
    `).run(args.documentId, args.partyId || null, args.kind, args.toEmail, problem === 'MAIL_QUOTA_EXCEEDED' ? 'RATE_LIMITED' : 'UNKNOWN', at, at);
    appendEvent(args.documentId, {
      type: EVENT.MAIL_FAILED, partyId: args.partyId || null, at, data: { kind: args.kind, to: maskEmail(args.toEmail), reason: problem },
    });
  })();
  return { queued: false, code: problem };
}

/** Cancels the mails of these kinds that are still waiting (all kinds when none are named). */
function cancelQueued({ documentId, partyId = null, kinds = null }) {
  const rows = db.prepare(`
    SELECT id, kind FROM sign_mail_outbox WHERE document_id = ? AND status = 'queued' AND (? IS NULL OR party_id = ?)
  `).all(documentId, partyId, partyId);
  const cancel = db.prepare("UPDATE sign_mail_outbox SET status = 'canceled' WHERE id = ? AND status = 'queued'");
  let canceled = 0;
  for (const row of rows) {
    if (kinds && !kinds.includes(row.kind)) continue;
    if (cancel.run(row.id).changes > 0) {
      canceled += 1;
      removeFromLedger([row.id]);
    }
  }
  return canceled;
}

/** Puts mails an earlier run left in "sending" back in the queue. */
function recoverInterrupted(now = Date.now(), { all = false } = {}) {
  const limit = all ? iso(now + 1) : iso(now - STUCK_AFTER_MS);
  return db.prepare(`
    UPDATE sign_mail_outbox SET status = 'queued', next_attempt_at = ? WHERE status = 'sending' AND next_attempt_at <= ?
  `).run(iso(now), limit).changes;
}

function claimNext(now) {
  return db.prepare(`
    UPDATE sign_mail_outbox SET status = 'sending', attempts = attempts + 1, next_attempt_at = ?
    WHERE id = (SELECT id FROM sign_mail_outbox WHERE status = 'queued' AND next_attempt_at <= ? ORDER BY next_attempt_at, id LIMIT 1)
    RETURNING *
  `).get(iso(now), iso(now));
}

// What the server log may say about a failure: one line, no password, no address.
// The outbox row itself keeps only a stable code (errors.js).
function safeError(err) {
  if (err?.permanent) return String(err.message).slice(0, MAX_ERROR_TEXT);
  return maskAddressesIn(describeSendError(err)).slice(0, MAX_ERROR_TEXT);
}

function markSent(row, now) {
  const at = iso(now);
  db.transaction(() => {
    db.prepare("UPDATE sign_mail_outbox SET status = 'sent', sent_at = ?, last_error = NULL WHERE id = ?").run(at, row.id);
    const data = { kind: row.kind, to: maskEmail(row.to_email), mailId: row.id };
    appendEvent(row.document_id, { type: EVENT.MAIL_SENT, partyId: row.party_id, at, data });
    if (row.kind === 'reminder') appendEvent(row.document_id, { type: EVENT.REMINDER_SENT, partyId: row.party_id, at, data });
  })();
}

function markFailed(row, err, now) {
  const at = iso(now);
  const permanent = Boolean(err?.permanent) || err?.code === 'INVALID_EMAIL' || row.attempts >= MAX_ATTEMPTS;
  const message = classifyMailError(err);
  if (!permanent) {
    const wait = BACKOFF_MINUTES[row.attempts - 1] * MINUTE_MS;
    db.prepare("UPDATE sign_mail_outbox SET status = 'queued', next_attempt_at = ?, last_error = ? WHERE id = ?")
      .run(iso(now + wait), message, row.id);
    return;
  }
  db.transaction(() => {
    db.prepare("UPDATE sign_mail_outbox SET status = 'failed', last_error = ? WHERE id = ?").run(message, row.id);
    appendEvent(row.document_id, {
      type: EVENT.MAIL_FAILED, partyId: row.party_id, at, data: { kind: row.kind, to: maskEmail(row.to_email), mailId: row.id },
    });
  })();
}

async function deliver(row, now) {
  try {
    const composed = await composeMail(row, parseJson(row.payload_json, {}));
    const result = composed ? await sendServiceEmail(composed.to, composed.message) : { skipped: 'unneeded' };
    if (result.skipped) {
      const reason = composed ? 'Email is not configured.' : 'Not needed any more.';
      db.prepare("UPDATE sign_mail_outbox SET status = 'canceled', last_error = ? WHERE id = ?").run(reason, row.id);
      removeFromLedger([row.id]);
    } else {
      markSent(row, now);
    }
  } catch (err) {
    markFailed(row, err, now);
    logger.warn(`A ${row.kind} mail failed (attempt ${row.attempts}): ${safeError(err)}`);
  }
  const document = getDocumentById(row.document_id);
  if (document) notifyOwner(document.owner_id, document.id, document.status);
}

async function drain(now) {
  let handled = 0;
  const lane = async () => {
    for (;;) {
      const row = claimNext(now);
      if (!row) return;
      handled += 1;
      await deliver(row, now);
    }
  };
  await Promise.all(Array.from({ length: CONCURRENCY }, lane));
  return handled;
}

function dropQueued(reason) {
  const ids = db.prepare("SELECT id FROM sign_mail_outbox WHERE status = 'queued'").all().map((row) => row.id);
  removeFromLedger(ids);
  return db.prepare("UPDATE sign_mail_outbox SET status = 'canceled', last_error = ? WHERE status = 'queued'").run(reason).changes;
}

let draining = null;

/**
 * Sends every mail that is due (two at a time). Resolves with how many it
 * handled. A call while another drain runs waits for it, then looks again.
 */
async function processDue({ now = Date.now() } = {}) {
  if (!isSigningMailEnabled()) {
    // The admin's kill switch: what waits is dropped, not sent when the switch comes back.
    if (isServiceEmailConfigured()) dropQueued('Signing mail is switched off.');
    return 0;
  }
  while (draining) await draining;
  draining = drain(now).finally(() => {
    draining = null;
  });
  return draining;
}

/** Starts sending in the background; the caller does not wait. */
function kick() {
  setImmediate(() => {
    processDue().catch((err) => logger.error(`Sending mail stopped unexpectedly: ${safeError(err)}`));
  });
}

module.exports = {
  BACKOFF_MINUTES,
  MAX_ATTEMPTS,
  cancelQueued,
  enqueueMail,
  enqueueRequest,
  kick,
  processDue,
  recoverInterrupted,
};
