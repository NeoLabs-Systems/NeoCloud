'use strict';

// The signer roles of a document: names, colours, optional email. Edited as
// one list while the document is a draft.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { LIMITS, PARTY_COLORS } = require('./constants');
const { singleLine } = require('./plain_text');
const { initialStatuses } = require('./turns');
const { listParties, nowIso } = require('./store');

const COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function invalid(message) {
  return httpError(400, message, 'INVALID_PARTY');
}

function defaultPartyName(locale, number) {
  return `${locale === 'de' ? 'Partei' : 'Party'} ${number}`;
}

function colorFor(position) {
  return PARTY_COLORS[position % PARTY_COLORS.length];
}

function cleanName(value) {
  const name = typeof value === 'string' ? singleLine(value) : '';
  if (!name || name.length > LIMITS.maxNameLength) throw invalid(`A party needs a name of 1 to ${LIMITS.maxNameLength} characters.`);
  return name;
}

function cleanEmail(value) {
  if (value === undefined || value === null || value === '') return null;
  const email = typeof value === 'string' ? value.trim() : '';
  if (!email || email.length > LIMITS.maxEmailLength || !EMAIL_PATTERN.test(email)) throw invalid('That email address is not valid.');
  return email;
}

function cleanColor(value, position) {
  if (value === undefined || value === null || value === '') return colorFor(position);
  if (typeof value !== 'string' || !COLOR_PATTERN.test(value)) throw invalid('A party colour looks like #1a2b3c.');
  return value.toLowerCase();
}

/** Adds one party at the end (or at `position`). Call inside a transaction. `userId` binds a party the owner signs themselves. */
function insertParty(documentId, {
  name, email = null, color, position, isSelf = false, userId = null,
}, at = nowIso()) {
  const id = crypto.randomUUID();
  db.prepare(`
    INSERT INTO sign_parties (id, document_id, position, name, color, email, status, is_self, user_id, created_at)
    VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)
  `).run(id, documentId, position, name, color ?? colorFor(position), email, isSelf ? 1 : 0, userId, at);
  return id;
}

function cleanSelf(value) {
  if (value === undefined || value === null) return false;
  if (typeof value !== 'boolean') throw invalid('isSelf must be true or false.');
  return value;
}

// The owner's own address, for the party they sign as themselves.
const ownerEmail = (ownerId) => db.prepare('SELECT email FROM users WHERE id = ?').get(ownerId)?.email || null;

/**
 * Replaces the party list. Entries with the id of an existing party keep it
 * (and its fields); new entries are added; parties left out are removed with
 * their fields. At least one party stays. One party may be `isSelf`: the
 * owner signs it, it is bound to the owner's account and its email defaults
 * to the owner's.
 */
function replaceParties(documentId, input, ownerId) {
  if (!Array.isArray(input) || input.length < 1 || input.length > LIMITS.maxParties) {
    throw invalid(`A document has 1 to ${LIMITS.maxParties} parties.`);
  }
  const existing = new Map(listParties(documentId).map((party) => [party.id, party]));
  const seen = new Set();
  const entries = input.map((entry, position) => {
    if (!entry || typeof entry !== 'object') throw invalid('Each party must be an object.');
    const keepId = typeof entry.id === 'string' && existing.has(entry.id) ? entry.id : null;
    if (keepId && seen.has(keepId)) throw invalid('A party is listed twice.');
    if (keepId) seen.add(keepId);
    const isSelf = cleanSelf(entry.isSelf);
    const email = cleanEmail(entry.email) || (isSelf ? ownerEmail(ownerId) : null);
    return {
      id: keepId, name: cleanName(entry.name), email, color: cleanColor(entry.color, position), position, isSelf, userId: isSelf ? ownerId : null,
    };
  });
  if (entries.filter((entry) => entry.isSelf).length > 1) throw invalid('Only one party can be you.');
  db.transaction(() => {
    for (const id of existing.keys()) {
      if (!seen.has(id)) db.prepare('DELETE FROM sign_parties WHERE id = ?').run(id);
    }
    for (const entry of entries) {
      if (entry.id) {
        // A party that stops being "you" is unbound again; one bound by someone's claim is never touched.
        db.prepare(`
          UPDATE sign_parties SET name = ?, email = ?, color = ?, position = ?, is_self = ?,
            user_id = CASE WHEN ? = 1 THEN ? WHEN is_self = 1 THEN NULL ELSE user_id END WHERE id = ?
        `).run(entry.name, entry.email, entry.color, entry.position, entry.isSelf ? 1 : 0, entry.isSelf ? 1 : 0, entry.userId, entry.id);
      } else {
        insertParty(documentId, entry);
      }
    }
  })();
  return listParties(documentId);
}

/** Sets every party's status for the document's signing order (before anyone has opened it). */
function applyInitialStatuses(documentId, order) {
  const parties = listParties(documentId);
  const statuses = initialStatuses(order, parties);
  for (const party of parties) {
    db.prepare('UPDATE sign_parties SET status = ? WHERE id = ?').run(statuses.get(party.id), party.id);
  }
}

module.exports = { applyInitialStatuses, colorFor, defaultPartyName, insertParty, replaceParties };
