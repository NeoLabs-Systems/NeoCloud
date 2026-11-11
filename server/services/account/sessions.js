'use strict';

// Signed-in browser sessions, as the account sees them in settings. The
// session store keeps the cookie data; user_sessions keeps a keyed hash of the
// session id plus where it was used, so a session can be listed and revoked
// without storing its id in readable form.

const crypto = require('crypto');
const db = require('../../db/database');
const { getSessionSecret } = require('./session_secret');
const { disconnectSessions, disconnectUser } = require('../events');
const { httpError } = require('../../utils/http_error');

function sessionHash(sessionId) {
  return crypto.createHmac('sha256', getSessionSecret()).update(String(sessionId || '')).digest('hex');
}

function sessionExpiresAt(req) {
  const expires = req.session?.cookie?.expires;
  return expires ? new Date(expires).toISOString() : null;
}

function recordCurrentSession(req, userId) {
  if (!req?.sessionID || !userId) return;
  db.prepare(`
    INSERT INTO user_sessions (user_id, session_hash, ip_address, user_agent, expires_at)
    VALUES (?, ?, ?, ?, ?)
    ON CONFLICT(session_hash) DO UPDATE SET
      ip_address = excluded.ip_address,
      user_agent = excluded.user_agent,
      last_seen_at = datetime('now'),
      expires_at = excluded.expires_at
  `).run(
    userId,
    sessionHash(req.sessionID),
    req.ip || null,
    String(req.get?.('user-agent') || '').slice(0, 500),
    sessionExpiresAt(req),
  );
}

function listSessions(req, userId) {
  recordCurrentSession(req, userId);
  const currentHash = req?.sessionID ? sessionHash(req.sessionID) : null;
  return db.prepare(`
    SELECT id, session_hash, ip_address, user_agent, created_at, last_seen_at, expires_at
    FROM user_sessions
    WHERE user_id = ? AND revoked_at IS NULL
      AND (expires_at IS NULL OR datetime(expires_at) > datetime('now'))
    ORDER BY datetime(last_seen_at) DESC, id DESC
  `).all(userId).map((row) => ({
    id: row.id,
    current: row.session_hash === currentHash,
    ipAddress: row.ip_address,
    userAgent: row.user_agent || '',
    createdAt: row.created_at,
    lastSeenAt: row.last_seen_at,
    expiresAt: row.expires_at,
  }));
}

function deleteStoredSessions(hashes) {
  if (hashes.size === 0) return;
  const remove = db.prepare('DELETE FROM sessions WHERE sid = ?');
  for (const { sid } of db.prepare('SELECT sid FROM sessions').all()) {
    if (hashes.has(sessionHash(sid))) remove.run(sid);
  }
}

function revokeSession(req, userId, sessionRecordId) {
  const row = db.prepare(`
    SELECT id, session_hash FROM user_sessions
    WHERE id = ? AND user_id = ? AND revoked_at IS NULL
  `).get(sessionRecordId, userId);
  if (!row) throw httpError(404, 'Session not found', 'NOT_FOUND');
  if (req?.sessionID && row.session_hash === sessionHash(req.sessionID)) {
    throw httpError(400, 'Use sign out to end the current session', 'CURRENT_SESSION');
  }
  deleteStoredSessions(new Set([row.session_hash]));
  db.prepare("UPDATE user_sessions SET revoked_at = datetime('now') WHERE id = ?").run(row.id);
  disconnectSessions([row.session_hash]);
  return { success: true };
}

function markRevoked(hash) {
  db.prepare("UPDATE user_sessions SET revoked_at = datetime('now') WHERE session_hash = ? AND revoked_at IS NULL")
    .run(hash);
}

/**
 * Ends the account's browser sessions and their live sockets. With
 * `exceptSessionId` the request's own session stays signed in.
 */
function revokeAllSessionsForUser(userId, { exceptSessionId = null } = {}) {
  const keep = exceptSessionId ? sessionHash(exceptSessionId) : null;
  const hashes = db.prepare('SELECT session_hash FROM user_sessions WHERE user_id = ? AND revoked_at IS NULL')
    .all(userId)
    .map((row) => row.session_hash)
    .filter((hash) => hash !== keep);
  deleteStoredSessions(new Set(hashes));
  db.transaction(() => hashes.forEach(markRevoked))();
  if (keep) disconnectSessions(hashes);
  else disconnectUser(userId);
  return { success: true, revoked: hashes.length };
}

/** Sign-out: the session's record and live sockets end with it. */
function endCurrentSession(req) {
  if (!req?.sessionID) return;
  const hash = sessionHash(req.sessionID);
  markRevoked(hash);
  disconnectSessions([hash]);
}

/**
 * Gives the signed-in request a fresh session id (after a password change),
 * so a copy of the old cookie no longer works. Resolves once saved.
 */
function regenerateCurrentSession(req) {
  const { userId, username } = req.session;
  const previousHash = sessionHash(req.sessionID);
  return new Promise((resolve, reject) => {
    req.session.regenerate((regenerateError) => {
      if (regenerateError) return reject(regenerateError);
      req.session.userId = userId;
      req.session.username = username;
      return req.session.save((saveError) => {
        if (saveError) return reject(saveError);
        markRevoked(previousHash);
        disconnectSessions([previousHash]);
        recordCurrentSession(req, userId);
        return resolve();
      });
    });
  });
}

module.exports = {
  endCurrentSession,
  listSessions,
  recordCurrentSession,
  regenerateCurrentSession,
  revokeAllSessionsForUser,
  revokeSession,
  sessionHash,
};
