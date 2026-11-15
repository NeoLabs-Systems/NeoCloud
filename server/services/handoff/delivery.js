'use strict';

// The life of a send after it exists:
//   uploading -> pending -> accepted -> delivered
// and from any open state to declined (receiver), cancelled (sender, or a
// revoked device) or expired. Every end releases the staged files at once.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');
const { deleteBlobFiles } = require('../storage/blob_store');
const { LIMITS } = require('./limits');
const { offerArrived, offerChanged } = require('./notify');
const { offerRow, receiverRow, senderRow, serializeOffer } = require('./offers');
const { releaseOfferFiles } = require('./staging');

const logger = createServiceLogger('Handoff');

// Ends an open offer: new status, staged files released. Returns the row, or
// null when it was not in one of `from` (someone else ended it first).
async function finish(userId, offerId, status, from) {
  const marks = from.map(() => '?').join(', ');
  const ended = db.transaction(() => {
    const changed = db.prepare(`
      UPDATE handoff_offers SET status = ?, updated_at = datetime('now') WHERE id = ? AND status IN (${marks})
    `).run(status, offerId, ...from);
    return changed.changes === 1 ? releaseOfferFiles(offerId) : null;
  })();
  if (ended === null) return null;
  await deleteBlobFiles(userId, ended);
  const row = offerRow(userId, offerId);
  offerChanged(userId, row);
  return row;
}

function stillOpen(row) {
  if (new Date(`${row.expires_at.replace(' ', 'T')}Z`).getTime() <= Date.now()) {
    throw httpError(410, 'This send has expired.', 'OFFER_EXPIRED');
  }
}

/** Called after each uploaded file: the last one makes the send ready for its receiver. */
function markReadyWhenComplete(userId, offerId) {
  const missing = db.prepare('SELECT COUNT(*) AS n FROM handoff_files WHERE offer_id = ? AND blob_id IS NULL').get(offerId).n;
  if (missing > 0) return false;
  const changed = db.prepare(`
    UPDATE handoff_offers SET status = 'pending', updated_at = datetime('now'), expires_at = datetime('now', ?)
    WHERE id = ? AND status = 'uploading'
  `).run(`+${LIMITS.ttlHours} hours`, offerId);
  if (changed.changes === 0) return false;
  const row = offerRow(userId, offerId);
  offerArrived(userId, row);
  offerChanged(userId, row);
  return true;
}

/** Receiver takes the send: the files may now be downloaded. Asking again is fine (a resumed download). */
function accept(userId, key, id) {
  const row = receiverRow(userId, key, id);
  if (row.status === 'accepted') return serializeOffer(row, 'receiver');
  if (row.status !== 'pending') throw httpError(409, 'This send is no longer available.', 'OFFER_CLOSED');
  stillOpen(row);
  db.prepare("UPDATE handoff_offers SET status = 'accepted', updated_at = datetime('now') WHERE id = ? AND status = 'pending'").run(row.id);
  const accepted = offerRow(userId, row.id);
  offerChanged(userId, accepted);
  return serializeOffer(accepted, 'receiver');
}

async function decline(userId, key, id) {
  const row = receiverRow(userId, key, id);
  const ended = await finish(userId, row.id, 'declined', ['pending', 'accepted']);
  if (!ended) throw httpError(409, 'This send is no longer available.', 'OFFER_CLOSED');
  return serializeOffer(ended, 'receiver');
}

/** Receiver has saved everything: the staged files are deleted. */
async function complete(userId, key, id) {
  const row = receiverRow(userId, key, id);
  if (row.status === 'delivered') return serializeOffer(row, 'receiver');
  const ended = await finish(userId, row.id, 'delivered', ['accepted']);
  if (!ended) throw httpError(409, 'Accept the send first.', 'NOT_ACCEPTED');
  return serializeOffer(ended, 'receiver');
}

/**
 * A browser saves a file with its own download and never says it is done, so
 * the server notices: once every file was streamed to its last byte, the
 * send counts as delivered and the staged files go.
 */
async function deliverWhenFetched(userId, offerId, fileIds) {
  const mark = db.prepare("UPDATE handoff_files SET fetched_at = datetime('now') WHERE id = ? AND offer_id = ?");
  for (const id of fileIds) mark.run(id, offerId);
  const unfetched = db.prepare('SELECT COUNT(*) AS n FROM handoff_files WHERE offer_id = ? AND fetched_at IS NULL').get(offerId).n;
  if (unfetched === 0) await finish(userId, offerId, 'delivered', ['accepted']);
}

async function cancel(userId, key, id) {
  const row = senderRow(userId, key, id);
  if (!['uploading', 'pending', 'accepted'].includes(row.status)) return serializeOffer(row, 'sender');
  const ended = await finish(userId, row.id, 'cancelled', ['uploading', 'pending', 'accepted']);
  return serializeOffer(ended || offerRow(userId, row.id), 'sender');
}

/** A revoked device: every open send to or from it is cancelled. */
async function cancelForDevice(userId, key) {
  const open = db.prepare(`
    SELECT id FROM handoff_offers
    WHERE user_id = ? AND (sender_key = ? OR target_key = ?) AND status IN ('uploading', 'pending', 'accepted')
  `).all(userId, key, key);
  for (const { id } of open) await finish(userId, id, 'cancelled', ['uploading', 'pending', 'accepted']);
}

/** Sends past their time end as expired; finished ones are forgotten after a few days. */
async function purgeExpired() {
  const due = db.prepare(`
    SELECT id, user_id FROM handoff_offers
    WHERE status IN ('uploading', 'pending', 'accepted') AND datetime(expires_at) <= datetime('now')
  `).all();
  for (const { id, user_id: userId } of due) await finish(userId, id, 'expired', ['uploading', 'pending', 'accepted']);
  const forgotten = db.prepare(`
    DELETE FROM handoff_offers
    WHERE status NOT IN ('uploading', 'pending', 'accepted') AND datetime(updated_at) < datetime('now', ?)
  `).run(`-${LIMITS.keepFinishedDays} days`).changes;
  if (due.length || forgotten) logger.info(`Send to device: ${due.length} expired, ${forgotten} old record(s) removed.`);
  return due.length;
}

module.exports = { accept, cancel, cancelForDevice, complete, decline, deliverWhenFetched, markReadyWhenComplete, purgeExpired };
