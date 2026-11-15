'use strict';

// The audit trail of a document: an append-only list whose entries are
// chained by hash. Each hash covers the previous one and the canonical JSON
// of the entry, so an edited, removed or reordered entry shows up in verify().

const crypto = require('crypto');
const db = require('../../db/database');
const { canonicalJson } = require('../../utils/canonical_json');

const GENESIS_HASH = '0'.repeat(64);
const MAX_USER_AGENT = 300;

function parseData(text) {
  try {
    return JSON.parse(text || '{}');
  } catch {
    return {};
  }
}

// The part of an entry the hash covers.
function hashedPayload(documentId, entry) {
  return {
    documentId,
    partyId: entry.partyId ?? null,
    type: entry.type,
    at: entry.at,
    ip: entry.ip ?? null,
    userAgent: entry.userAgent ?? null,
    data: entry.data ?? {},
  };
}

function chainHash(prevHash, documentId, entry) {
  return crypto.createHash('sha256')
    .update(prevHash)
    .update(canonicalJson(hashedPayload(documentId, entry)))
    .digest('hex');
}

function truncateUserAgent(value) {
  const text = String(value || '').replace(/[\u0000-\u001f\u007f]/g, ' ').trim();
  return text ? text.slice(0, MAX_USER_AGENT) : null;
}

/** Who and where a request came from: `{ ip, userAgent }`, for event records. */
function clientOf(req) {
  return { ip: req?.ip || null, userAgent: truncateUserAgent(req?.get?.('user-agent')) };
}

/**
 * Appends one entry. Call inside the transaction that makes the change it
 * records, so the trail and the state never disagree.
 */
function appendEvent(documentId, { type, partyId = null, ip = null, userAgent = null, data = {}, at = new Date().toISOString() }) {
  const last = db.prepare('SELECT hash FROM sign_events WHERE document_id = ? ORDER BY id DESC LIMIT 1').get(documentId);
  const prevHash = last ? last.hash : GENESIS_HASH;
  const entry = { type, partyId, at, ip, userAgent: truncateUserAgent(userAgent), data };
  const hash = chainHash(prevHash, documentId, entry);
  db.prepare(`
    INSERT INTO sign_events (document_id, party_id, type, at, ip, user_agent, data_json, prev_hash, hash)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
  `).run(documentId, partyId, type, at, ip, entry.userAgent, JSON.stringify(data), prevHash, hash);
  return hash;
}

function listEvents(documentId) {
  return db.prepare('SELECT * FROM sign_events WHERE document_id = ? ORDER BY id').all(documentId).map((row) => ({
    id: row.id,
    type: row.type,
    partyId: row.party_id,
    at: row.at,
    ip: row.ip,
    userAgent: row.user_agent,
    data: parseData(row.data_json),
    prevHash: row.prev_hash,
    hash: row.hash,
  }));
}

/**
 * Recomputes the chain. `{ valid, count, head }`; when broken, `brokenAt` is
 * the id of the first entry that does not match.
 */
function verifyChain(documentId) {
  const events = listEvents(documentId);
  let prevHash = GENESIS_HASH;
  for (const event of events) {
    const expected = chainHash(prevHash, documentId, event);
    if (event.prevHash !== prevHash || event.hash !== expected) {
      return { valid: false, count: events.length, head: prevHash, brokenAt: event.id };
    }
    prevHash = event.hash;
  }
  return { valid: true, count: events.length, head: prevHash };
}

module.exports = { GENESIS_HASH, appendEvent, clientOf, listEvents, verifyChain };
