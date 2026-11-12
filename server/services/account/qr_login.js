'use strict';

// QR sign-in: a signed-out browser shows a QR code (neocloud://qr-login?…);
// the signed-in phone app scans it, previews the request and approves or
// declines it; the browser then swaps its poll token for a normal session.
//
// pending → scanned → approved → claimed, or declined / expired on the way.
// The browser holds the poll token, the QR code carries the approve secret;
// only their SHA-256 hashes are stored. A code lives two minutes.

const crypto = require('crypto');
const { EventEmitter } = require('events');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { describeClient, normalizeClientMetadata, trimmedString } = require('../../utils/client_descriptor');
const { sessionHash } = require('./sessions');

const QR_LOGIN_TTL_MS = 2 * 60 * 1000;
const TERMINAL_RETENTION_MS = 60 * 60 * 1000;
const LONG_POLL_MS = 25 * 1000;

// Wakes status requests held open for a challenge, keyed by challenge id.
const challengeChanges = new EventEmitter();
challengeChanges.setMaxListeners(0);

function notifyChallengeChanged(challengeId) {
  challengeChanges.emit(challengeId);
}

function sqliteDateFromMs(ms) {
  return new Date(ms).toISOString().replace('T', ' ').replace(/\.\d{3}Z$/, '');
}

function parseSqliteUtcMs(value) {
  const raw = String(value || '').trim();
  if (!raw) return Number.NaN;
  return Date.parse(`${raw.replace(' ', 'T')}Z`);
}

function tokenHash(token) {
  return crypto.createHash('sha256').update(String(token || '')).digest('hex');
}

function parseJsonObject(value) {
  try {
    const parsed = JSON.parse(value || '{}');
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

function isExpired(row, nowMs = Date.now()) {
  const expiresAtMs = parseSqliteUtcMs(row?.expires_at);
  return !Number.isFinite(expiresAtMs) || expiresAtMs <= nowMs;
}

function notFound() {
  return httpError(404, 'This sign-in request was not found or has expired.', 'NOT_FOUND');
}

function stateError(message, code) {
  return httpError(409, message, code);
}

function pruneChallenges() {
  db.prepare(`
    UPDATE user_qr_login_challenges SET status = 'expired'
    WHERE status IN ('pending', 'scanned', 'approved') AND datetime(expires_at) <= datetime('now')
  `).run();
  db.prepare(`
    DELETE FROM user_qr_login_challenges
    WHERE status IN ('expired', 'claimed', 'declined')
      AND datetime(COALESCE(claimed_at, expires_at, created_at)) <= datetime(?)
  `).run(sqliteDateFromMs(Date.now() - TERMINAL_RETENTION_MS));
}

function rowBySecret(challengeId, secret) {
  pruneChallenges();
  return db.prepare('SELECT * FROM user_qr_login_challenges WHERE id = ? AND approve_secret_hash = ?')
    .get(String(challengeId || '').trim(), tokenHash(secret));
}

function rowByPollToken(challengeId, pollToken) {
  pruneChallenges();
  return db.prepare('SELECT * FROM user_qr_login_challenges WHERE id = ? AND poll_token_hash = ?')
    .get(String(challengeId || '').trim(), tokenHash(pollToken));
}

function liveRowBySecret(challengeId, secret) {
  const row = rowBySecret(challengeId, secret);
  if (!row || row.status === 'expired' || isExpired(row)) throw notFound();
  return row;
}

/** The scanning device as the signing-in screen may show it. */
function scannerDescriptor(metadata) {
  return {
    label: trimmedString(metadata.deviceLabel || metadata.platformLabel, 120) || 'Phone',
    platformLabel: trimmedString(metadata.platformLabel, 60) || null,
    deviceClass: trimmedString(metadata.deviceClass, 24) || null,
  };
}

// What the approving phone sees. NeoCloud has no geo lookup, so the location
// is the address the request came from.
function serializeChallenge(row) {
  const requestMetadata = parseJsonObject(row.request_metadata_json);
  const descriptor = describeClient(row.request_user_agent, requestMetadata);
  return {
    challengeId: row.id,
    status: row.status || 'pending',
    requestedAt: row.created_at || null,
    expiresAt: row.expires_at || null,
    scannedAt: row.scanned_at || null,
    approvedAt: row.approved_at || null,
    claimedAt: row.claimed_at || null,
    scannedBy: row.scanned_at ? scannerDescriptor(parseJsonObject(row.scanned_metadata_json)) : null,
    requestedDevice: {
      ...descriptor,
      userAgent: row.request_user_agent || '',
      metadata: requestMetadata,
    },
    requestLocation: {
      label: row.request_ip_address || 'Unknown',
      ipAddress: row.request_ip_address || null,
    },
  };
}

function createChallenge({ userAgent, ipAddress, requestMetadata }) {
  pruneChallenges();
  const challengeId = crypto.randomUUID();
  const pollToken = crypto.randomBytes(24).toString('base64url');
  const approveSecret = crypto.randomBytes(24).toString('base64url');
  const expiresAt = sqliteDateFromMs(Date.now() + QR_LOGIN_TTL_MS);
  db.prepare(`
    INSERT INTO user_qr_login_challenges (
      id, poll_token_hash, approve_secret_hash, status,
      request_user_agent, request_ip_address, request_metadata_json, expires_at
    ) VALUES (?, ?, ?, 'pending', ?, ?, ?, ?)
  `).run(
    challengeId,
    tokenHash(pollToken),
    tokenHash(approveSecret),
    trimmedString(userAgent, 500),
    trimmedString(ipAddress, 64) || null,
    JSON.stringify(normalizeClientMetadata(requestMetadata)),
    expiresAt,
  );
  return { challengeId, pollToken, approveSecret, expiresAt, status: 'pending' };
}

/**
 * A signed-in phone opened the approval preview. The signing-in screen sees
 * this on its next poll and waits for the decision instead of showing the code.
 */
function markChallengeScanned({ challengeId, secret, scannerMetadata }) {
  const row = liveRowBySecret(challengeId, secret);
  if (row.status === 'declined') throw stateError('This sign-in request was cancelled.', 'QR_LOGIN_DECLINED');
  if (row.status === 'pending') {
    db.prepare(`
      UPDATE user_qr_login_challenges SET status = 'scanned', scanned_at = ?, scanned_metadata_json = ?
      WHERE id = ? AND status = 'pending'
    `).run(sqliteDateFromMs(Date.now()), JSON.stringify(normalizeClientMetadata(scannerMetadata)), row.id);
    notifyChallengeChanged(row.id);
  }
  const preview = serializeChallenge(liveRowBySecret(challengeId, secret));
  // To the phone a scanned request is still undecided.
  if (preview.status === 'scanned') preview.status = 'pending';
  return preview;
}

function declineRow(row) {
  if (!row || row.status === 'expired' || isExpired(row)) throw notFound();
  if (row.status === 'declined') return;
  if (row.status !== 'pending' && row.status !== 'scanned') {
    throw stateError('This sign-in request was already approved.', 'QR_LOGIN_APPROVED');
  }
  db.prepare(`
    UPDATE user_qr_login_challenges SET status = 'declined'
    WHERE id = ? AND status IN ('pending', 'scanned')
  `).run(row.id);
  notifyChallengeChanged(row.id);
}

/** The phone turned the request down. */
function declineChallenge({ challengeId, secret }) {
  declineRow(rowBySecret(challengeId, secret));
}

/** The signing-in screen gave up on this code, so a late approval fails. */
function cancelChallenge({ challengeId, pollToken }) {
  declineRow(rowByPollToken(challengeId, pollToken));
}

/** The phone approved; the approver is a browser session or a device token. */
function approveChallenge({ challengeId, secret, userId, sessionId, deviceId, approvalMetadata }) {
  const row = liveRowBySecret(challengeId, secret);
  if (row.status === 'claimed') throw stateError('This sign-in request was already used.', 'QR_LOGIN_USED');
  if (row.status === 'declined') throw stateError('This sign-in request was cancelled.', 'QR_LOGIN_DECLINED');
  if (row.status === 'approved' && Number(row.approved_by_user_id) !== Number(userId)) {
    throw stateError('This sign-in request was already approved.', 'QR_LOGIN_APPROVED');
  }
  db.prepare(`
    UPDATE user_qr_login_challenges
    SET status = 'approved', approved_by_user_id = ?, approved_session_hash = ?, approved_device_id = ?,
        approved_metadata_json = ?, approved_at = ?
    WHERE id = ? AND status IN ('pending', 'scanned', 'approved')
  `).run(
    userId,
    sessionId ? sessionHash(sessionId) : null,
    deviceId || null,
    JSON.stringify(normalizeClientMetadata(approvalMetadata)),
    sqliteDateFromMs(Date.now()),
    row.id,
  );
  notifyChallengeChanged(row.id);
  return serializeChallenge(liveRowBySecret(challengeId, secret));
}

function getChallengeStatusForPoll({ challengeId, pollToken }) {
  const row = rowByPollToken(challengeId, pollToken);
  if (!row || row.status === 'expired' || isExpired(row)) {
    return { challengeId: String(challengeId || '').trim(), status: 'expired', expiresAt: row?.expires_at || null };
  }
  const result = {
    challengeId: row.id,
    status: row.status,
    expiresAt: row.expires_at,
    approvedAt: row.approved_at || null,
    claimedAt: row.claimed_at || null,
  };
  if (row.status === 'scanned') {
    // Only what this screen already knows about itself, plus which device of
    // the account holder is deciding; the account stays unnamed until approval.
    const challenge = serializeChallenge(row);
    result.scannedAt = challenge.scannedAt;
    result.scannedBy = challenge.scannedBy;
    result.requestedDevice = { label: challenge.requestedDevice.label };
    result.requestLocation = { label: challenge.requestLocation.label };
  }
  return result;
}

/**
 * Answers once the challenge leaves [knownStatus]: right away if it already
 * has, else when the phone scans, approves or declines, the code expires, or
 * after [maxWaitMs]. [signal] ends the wait when the client goes away.
 */
function waitForChallengeStatusChange({ challengeId, pollToken, knownStatus, signal, maxWaitMs = LONG_POLL_MS }) {
  const read = () => ({ ...getChallengeStatusForPoll({ challengeId, pollToken }), longPoll: true });
  const current = read();
  if (current.status !== knownStatus || signal?.aborted) return Promise.resolve(current);
  const untilExpiry = parseSqliteUtcMs(current.expiresAt) - Date.now();
  const waitMs = Math.max(0, Math.min(maxWaitMs, untilExpiry + 250));
  return new Promise((resolve) => {
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      challengeChanges.off(current.challengeId, finish);
      signal?.removeEventListener('abort', finish);
      resolve(read());
    };
    const timer = setTimeout(finish, waitMs);
    challengeChanges.on(current.challengeId, finish);
    signal?.addEventListener('abort', finish, { once: true });
  });
}

/** The browser swaps its approved poll token for the account to sign in. */
function claimApprovedChallenge({ challengeId, pollToken }) {
  const row = rowByPollToken(challengeId, pollToken);
  if (!row || row.status === 'expired' || isExpired(row)) throw notFound();
  if (row.status === 'claimed') throw stateError('This sign-in request was already used.', 'QR_LOGIN_USED');
  if (row.status !== 'approved' || !row.approved_by_user_id) {
    throw stateError('This sign-in request is not approved yet.', 'QR_LOGIN_NOT_APPROVED');
  }
  const claimed = db.prepare(`
    UPDATE user_qr_login_challenges SET status = 'claimed', claimed_at = ?
    WHERE id = ? AND poll_token_hash = ? AND status = 'approved'
  `).run(sqliteDateFromMs(Date.now()), row.id, tokenHash(pollToken));
  if (claimed.changes !== 1) throw stateError('This sign-in request was already used.', 'QR_LOGIN_USED');
  notifyChallengeChanged(row.id);
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(row.approved_by_user_id);
  if (!user) throw notFound();
  return user;
}

module.exports = {
  approveChallenge,
  cancelChallenge,
  claimApprovedChallenge,
  createChallenge,
  declineChallenge,
  getChallengeStatusForPoll,
  markChallengeScanned,
  waitForChallengeStatusChange,
};
