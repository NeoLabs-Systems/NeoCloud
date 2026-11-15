'use strict';

// Creating and reading sends (offers). An offer is addressed to one device and
// is visible only to it (its inbox) and to the device that sent it (its
// status); another device of the same account sees neither.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { recordAccessEvent } = require('../access/audit');
const { getLiveNode } = require('../files/tree');
const { assertUploadFits } = require('../storage/quota');
const { describeSender, resolveTarget } = require('./devices');
const { LIMITS } = require('./limits');
const { uniqueFileNames } = require('./names');
const { stageExisting } = require('./staging');

const OPEN_SQL = "'uploading', 'pending', 'accepted'";

function serializeFiles(offerId) {
  return db.prepare('SELECT id, name, mime, size FROM handoff_files WHERE offer_id = ? ORDER BY position')
    .all(offerId)
    .map((file) => ({ id: file.id, name: file.name, mime: file.mime, size: file.size }));
}

function serializeOffer(row, role) {
  return {
    id: row.id,
    role,
    status: row.status,
    from: { id: row.sender_key, name: row.sender_name, platform: row.sender_platform },
    to: { id: row.target_key, name: row.target_name },
    totalSize: row.total_size,
    files: serializeFiles(row.id),
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    expiresAt: row.expires_at,
  };
}

function offerRow(userId, id) {
  return db.prepare('SELECT * FROM handoff_offers WHERE id = ? AND user_id = ?').get(String(id), userId);
}

/** The offer as its sender or its receiver sees it; a 404 for any other device or account. */
function getOffer(userId, key, id) {
  const row = offerRow(userId, id);
  if (row && row.sender_key === key) return serializeOffer(row, 'sender');
  if (row && row.target_key === key && row.status !== 'uploading') return serializeOffer(row, 'receiver');
  throw httpError(404, 'Send not found.', 'NOT_FOUND');
}

/** Row of an offer addressed to `key`, or a 404. */
function receiverRow(userId, key, id) {
  const row = offerRow(userId, id);
  if (!row || row.target_key !== key || row.status === 'uploading') throw httpError(404, 'Send not found.', 'NOT_FOUND');
  return row;
}

/** Row of an offer sent by `key`, or a 404. */
function senderRow(userId, key, id) {
  const row = offerRow(userId, id);
  if (!row || row.sender_key !== key) throw httpError(404, 'Send not found.', 'NOT_FOUND');
  return row;
}

function listSent(userId, key) {
  return db.prepare('SELECT * FROM handoff_offers WHERE user_id = ? AND sender_key = ? ORDER BY created_at DESC, rowid DESC LIMIT 50')
    .all(userId, key).map((row) => serializeOffer(row, 'sender'));
}

function inboxRows(userId, key) {
  return db.prepare(`
    SELECT * FROM handoff_offers WHERE user_id = ? AND target_key = ? AND status IN ('pending', 'accepted')
    ORDER BY created_at, rowid
  `).all(userId, key);
}

function listInbox(userId, key) {
  return inboxRows(userId, key).map((row) => serializeOffer(row, 'receiver'));
}

function cleanDeclaredFiles(files) {
  if (!Array.isArray(files) || files.length === 0) throw httpError(400, 'Choose at least one file.', 'NOTHING_SELECTED');
  if (files.length > LIMITS.maxFiles) throw httpError(400, `Send up to ${LIMITS.maxFiles} files at once.`, 'TOO_MANY_FILES');
  return files.map((file) => {
    const size = Number(file?.size);
    if (!Number.isSafeInteger(size) || size < 0) throw httpError(400, 'Every file needs its size.', 'INVALID_SIZE');
    const mime = typeof file?.mime === 'string' && /^[\w.+-]+\/[\w.+-]+$/.test(file.mime) ? file.mime : 'application/octet-stream';
    return { name: file?.name, size, mime };
  });
}

// Files already in NeoCloud (sent as copies that share their content).
function declaredFromNodes(userId, nodeIds) {
  if (!Array.isArray(nodeIds) || nodeIds.length === 0) return [];
  return [...new Set(nodeIds)].map((id) => {
    const node = getLiveNode(userId, id, { type: 'file' });
    if (node.vault_id) throw httpError(400, 'Files in a vault can’t be sent.', 'VAULT_FILE');
    return { name: node.name, size: node.size, mime: node.mime || 'application/octet-stream', node };
  });
}

function assertRoom(userId, totalSize) {
  if (totalSize > LIMITS.maxTotalBytes) throw httpError(413, 'That is more than can be sent at once.', 'SEND_TOO_LARGE');
  const open = db.prepare(`
    SELECT COUNT(*) AS offers, COALESCE(SUM(total_size), 0) AS bytes FROM handoff_offers
    WHERE user_id = ? AND status IN (${OPEN_SQL})
  `).get(userId);
  if (open.offers >= LIMITS.maxOpenOffers || open.bytes + totalSize > LIMITS.maxOpenBytes) {
    throw httpError(429, 'Too many sends are waiting. Wait for them to arrive or cancel one.', 'TOO_MANY_OPEN_SENDS');
  }
}

/**
 * Creates a send to `targetId`. `files` ({ name, size, mime }) are uploaded by
 * the sender afterwards (the send is "uploading" until the last one arrives);
 * `nodeIds` name files already in NeoCloud and make the send ready at once.
 * Returns { offer, ready } where ready says it is waiting for its receiver.
 */
function createOffer(userId, senderKey, { targetId, files, nodeIds, userAgent = '' }) {
  const target = resolveTarget(userId, targetId);
  if (target.id === senderKey) throw httpError(400, 'Choose another device.', 'SAME_DEVICE');
  const fromNodes = declaredFromNodes(userId, nodeIds);
  const declared = [...fromNodes, ...(files === undefined ? [] : cleanDeclaredFiles(files))];
  if (declared.length === 0) throw httpError(400, 'Choose at least one file.', 'NOTHING_SELECTED');
  if (declared.length > LIMITS.maxFiles) throw httpError(400, `Send up to ${LIMITS.maxFiles} files at once.`, 'TOO_MANY_FILES');
  const totalSize = declared.reduce((sum, file) => sum + file.size, 0);
  assertRoom(userId, totalSize);
  const uploads = declared.filter((file) => !file.node).reduce((sum, file) => sum + file.size, 0);
  if (uploads > 0) assertUploadFits(userId, uploads);

  const sender = describeSender(userId, senderKey, userAgent);
  const names = uniqueFileNames(declared.map((file) => file.name));
  const id = crypto.randomUUID();
  const ready = declared.every((file) => file.node);
  const expiry = ready ? `+${LIMITS.ttlHours} hours` : `+${LIMITS.uploadWindowMinutes} minutes`;
  db.transaction(() => {
    db.prepare(`
      INSERT INTO handoff_offers (id, user_id, sender_key, sender_name, sender_platform, target_key, target_name,
        status, total_size, expires_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now', ?))
    `).run(id, userId, senderKey, sender.name, sender.platform, target.id, target.name, ready ? 'pending' : 'uploading', totalSize, expiry);
    declared.forEach((file, position) => {
      const row = db.prepare(`
        INSERT INTO handoff_files (offer_id, position, name, mime, size, source_node_id) VALUES (?, ?, ?, ?, ?, ?)
      `).run(id, position, names[position], file.mime, file.size, file.node?.id || null);
      if (file.node) stageExisting(row.lastInsertRowid, file.node);
    });
  })();
  recordAccessEvent('handoff.sent', {
    actorUserId: userId,
    subjectUserId: userId,
    detail: { files: declared.length, bytes: totalSize, to: target.kind },
  });
  return { offer: serializeOffer(offerRow(userId, id), 'sender'), ready };
}

/** The staged file `fileId` of a send still collecting files from `senderKey`, or a 404. */
function uploadTarget(userId, senderKey, offerId, fileId) {
  const offer = senderRow(userId, senderKey, offerId);
  const file = db.prepare('SELECT * FROM handoff_files WHERE id = ? AND offer_id = ?').get(Number(fileId), offer.id);
  if (!file || file.source_node_id) throw httpError(404, 'File not found.', 'NOT_FOUND');
  if (offer.status !== 'uploading') throw httpError(409, 'This send is no longer waiting for files.', 'OFFER_CLOSED');
  return file;
}

module.exports = { createOffer, getOffer, inboxRows, listInbox, listSent, offerRow, receiverRow, senderRow, serializeOffer, uploadTarget };
