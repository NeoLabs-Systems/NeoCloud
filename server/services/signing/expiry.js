'use strict';

// Documents that ran out of time. A link answers "expired" the moment its
// document's time is up (public.js asks expireIfDue on every request); the
// periodic sweep records it for documents nobody opened since.

const db = require('../../db/database');
const { DOCUMENT_STATUS, EVENT, LIVE_STATUSES } = require('./constants');
const { appendEvent } = require('./events');
const { notifyOwner } = require('./notify');
const { nowIso, updateDocument } = require('./store');

const isDue = (document, now = Date.now()) => Boolean(document.expires_at) && Date.parse(document.expires_at) <= now
  && LIVE_STATUSES.includes(document.status);

/** Marks one live document expired if its time is up. Returns whether it did. */
function expireIfDue(document) {
  if (!isDue(document)) return false;
  const at = nowIso();
  const changed = db.transaction(() => {
    // Re-read: another request may have finished or canceled it just now.
    const current = db.prepare('SELECT status FROM sign_documents WHERE id = ?').get(document.id);
    if (!current || !LIVE_STATUSES.includes(current.status)) return false;
    updateDocument(document.id, { status: DOCUMENT_STATUS.EXPIRED }, at);
    appendEvent(document.id, { type: EVENT.EXPIRED, at });
    return true;
  })();
  if (changed) notifyOwner(document.owner_id, document.id, DOCUMENT_STATUS.EXPIRED);
  return changed;
}

/** Expires every live document that is past its time. Returns how many. */
function expireDueDocuments() {
  const due = db.prepare(`
    SELECT * FROM sign_documents WHERE status IN ('sent', 'in_progress') AND expires_at IS NOT NULL AND expires_at <= ?
  `).all(nowIso());
  return due.filter(expireIfDue).length;
}

module.exports = { expireDueDocuments, expireIfDue };
