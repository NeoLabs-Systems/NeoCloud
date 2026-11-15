'use strict';

// Grant tokens: a short-lived stand-in for a party's link, handed to a
// signed-in account that is entitled to the party (the inbox, "sign
// yourself"). The public signing routes accept a grant wherever they accept
// the party's token, with the same permissions; the party's own token never
// leaves the server. Like a link, a grant is 256 random bits of which only
// the SHA-256 is stored. It lives 60 minutes, binds the party to its account
// on first use, and dies when the party finishes or the document ends.

const crypto = require('crypto');
const db = require('../../db/database');
const { EVENT } = require('./constants');
const { appendEvent, clientOf } = require('./events');
const { UUID_PATTERN } = require('./store');
const { generateToken, hashToken } = require('./tokens');

const GRANT_TTL_MS = 60 * 60 * 1000;

const iso = (time) => new Date(time).toISOString();

/** A new grant for the account on the party. Returns the token, shown once. */
function createGrant(userId, partyId, now = Date.now()) {
  const token = generateToken();
  db.prepare(`
    INSERT INTO sign_party_grants (id, party_id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)
  `).run(crypto.randomUUID(), partyId, userId, hashToken(token), iso(now + GRANT_TTL_MS), iso(now));
  return token;
}

/**
 * The party a grant (by the hash of its token) opens, or null when it is
 * unknown, expired, or its party belongs to another account. The first use
 * binds the party to the grant's account and is recorded in the trail.
 */
function resolveGrant(tokenHash, req, now = Date.now()) {
  const grant = db.prepare('SELECT * FROM sign_party_grants WHERE token_hash = ?').get(tokenHash);
  if (!grant || grant.expires_at <= iso(now)) return null;
  const party = db.prepare('SELECT * FROM sign_parties WHERE id = ?').get(grant.party_id);
  if (!party || (party.user_id && party.user_id !== grant.user_id)) return null;
  if (grant.first_used_at) return party;
  const client = clientOf(req);
  return db.transaction(() => {
    db.prepare('UPDATE sign_party_grants SET first_used_at = ? WHERE id = ?').run(iso(now), grant.id);
    if (!party.user_id) {
      db.prepare('UPDATE sign_parties SET user_id = ? WHERE id = ?').run(grant.user_id, party.id);
      appendEvent(party.document_id, { type: EVENT.CLAIMED, partyId: party.id, ...client, data: { via: 'account' } });
    }
    appendEvent(party.document_id, { type: EVENT.GRANT_USED, partyId: party.id, ...client, data: { grantId: grant.id } });
    return db.prepare('SELECT * FROM sign_parties WHERE id = ?').get(party.id);
  })();
}

/** Ends the grants of one party (it signed). */
function revokeForParty(partyId) {
  if (!UUID_PATTERN.test(String(partyId))) return 0;
  return db.prepare('DELETE FROM sign_party_grants WHERE party_id = ?').run(partyId).changes;
}

/** Ends the grants of every party of a document (it was declined, canceled or expired). */
function revokeForDocument(documentId) {
  return db.prepare(`
    DELETE FROM sign_party_grants WHERE party_id IN (SELECT id FROM sign_parties WHERE document_id = ?)
  `).run(documentId).changes;
}

/** Removes grants whose time is over. */
function purgeExpiredGrants(now = Date.now()) {
  return db.prepare('DELETE FROM sign_party_grants WHERE expires_at <= ?').run(iso(now)).changes;
}

module.exports = { GRANT_TTL_MS, createGrant, purgeExpiredGrants, resolveGrant, revokeForDocument, revokeForParty };
