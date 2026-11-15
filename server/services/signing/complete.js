'use strict';

// A signer finishing or declining. Completing is one transaction: the
// required fields are checked against what is stored, the party is marked
// complete, the next party's turn opens (or, after the last one, the document
// moves to finalizing). Calling it again for a completed party answers the
// same way and changes nothing.

const crypto = require('crypto');
const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const {
  DOCUMENT_STATUS, EVENT, LIMITS, PARTY_STATUS,
} = require('./constants');
const { appendEvent, clientOf } = require('./events');
const { isFilled, typeOf } = require('./field_types');
const { revokeForDocument, revokeForParty } = require('./grants');
const { onPartyCompleted, onPartyDeclined } = require('./mail/triggers');
const { scheduleFinalize } = require('./finalize');
const { notifyOwner } = require('./notify');
const { withoutInvisible } = require('./plain_text');
const { assertReady, finalAvailable } = require('./public');
const { getDocumentById, listParties, nowIso, parseJson, updateDocument } = require('./store');
const { allCompleted, nextToUnlock } = require('./turns');
const { assertWritable, valueOf } = require('./values');

// The party's fillable fields with what is stored for them.
function ownFields(documentId, partyId) {
  return db.prepare(`
    SELECT f.*, v.id AS value_id, v.value_json, v.blob_ref FROM sign_fields f
    LEFT JOIN sign_values v ON v.field_id = f.id
    WHERE f.document_id = ? AND f.party_id = ? ORDER BY f.position
  `).all(documentId, partyId).filter((row) => typeOf(row.type).storage !== 'static');
}

/**
 * A field the sender gave a value to (a default) and the signer left alone
 * keeps that value: it is stored now, so the document's final form is
 * made of stored values only.
 */
function materializeDefault(document, party, row) {
  db.prepare(`
    INSERT INTO sign_values (id, document_id, field_id, party_id, value_json, updated_at) VALUES (?, ?, ?, ?, ?, ?)
  `).run(crypto.randomUUID(), document.id, row.id, party.id, row.default_value, nowIso());
  return { ...row, value_json: row.default_value };
}

function missingRequired(rows) {
  return rows.filter((row) => {
    if (row.required !== 1) return false;
    const field = { type: row.type, prefs: parseJson(row.prefs_json, {}) };
    return !isFilled(field, valueOf(row));
  }).map((row) => row.id);
}

function incomplete(fieldIds) {
  const error = httpError(400, 'Some required fields are still empty.', 'FIELDS_INCOMPLETE');
  error.fieldIds = fieldIds;
  return error;
}

function finish(document, party, req) {
  const at = nowIso();
  const client = clientOf(req);
  let finalizing = false;
  let unlockedPartyId = null;
  db.transaction(() => {
    assertWritable(document.id, party.id);
    const rows = ownFields(document.id, party.id).map((row) => (
      !row.value_id && row.default_value !== null ? materializeDefault(document, party, row) : row
    ));
    const missing = missingRequired(rows);
    if (missing.length > 0) throw incomplete(missing);

    db.prepare(`
      UPDATE sign_parties SET status = 'completed', completed_at = ?, ip = ?, user_agent = ? WHERE id = ?
    `).run(at, client.ip, client.userAgent, party.id);
    appendEvent(document.id, {
      type: EVENT.PARTY_COMPLETED, partyId: party.id, at, ...client, data: { fields: rows.length },
    });
    // A signed party's account links stop working; its inbox opens a new one to read the result.
    revokeForParty(party.id);

    const parties = listParties(document.id);
    if (allCompleted(parties)) {
      finalizing = true;
      updateDocument(document.id, { status: DOCUMENT_STATUS.FINALIZING, finalize_next_at: at }, at);
      appendEvent(document.id, { type: EVENT.COMPLETED, at, data: { parties: parties.length } });
      return;
    }
    const next = nextToUnlock(parties);
    if (document.signing_order === 'preserved' && next) {
      db.prepare('UPDATE sign_parties SET status = ? WHERE id = ?').run(PARTY_STATUS.PENDING, next.id);
      unlockedPartyId = next.id;
    }
    updateDocument(document.id, {}, at);
  })();
  return { finalizing, unlockedPartyId };
}

/** The signer finishes. Resolves with the state the page shows next. */
function completeParty(context, req) {
  if (context.state === 'completed') {
    return { state: 'completed', finalAvailable: finalAvailable(context.document) };
  }
  assertReady(context);
  const { document, party } = context;
  const { finalizing, unlockedPartyId } = finish(document, party, req);
  onPartyCompleted(document.id, party.id, { unlockedPartyId, finalizing });
  notifyOwner(document.owner_id, document.id, finalizing ? DOCUMENT_STATUS.FINALIZING : getDocumentById(document.id).status);
  if (finalizing) scheduleFinalize(document.id);
  return { state: 'completed', finalAvailable: false };
}

/** The signer refuses. The document ends as declined and every link shows that. */
function declineParty(context, reason, req) {
  assertReady(context);
  const text = typeof reason === 'string' ? withoutInvisible(reason).replace(/[\u0000-\u0008\u000b-\u001f\u007f]/g, '').trim() : '';
  if (text.length > LIMITS.maxDeclineReason) {
    throw httpError(400, `A reason has at most ${LIMITS.maxDeclineReason} characters.`, 'INVALID_REQUEST');
  }
  const { document, party } = context;
  const at = nowIso();
  db.transaction(() => {
    assertWritable(document.id, party.id, { needConsent: false });
    db.prepare(`
      UPDATE sign_parties SET status = 'declined', declined_at = ?, decline_reason = ? WHERE id = ?
    `).run(at, text || null, party.id);
    updateDocument(document.id, { status: DOCUMENT_STATUS.DECLINED }, at);
    appendEvent(document.id, {
      type: EVENT.PARTY_DECLINED, partyId: party.id, at, ...clientOf(req), data: { reason: text || null },
    });
    revokeForDocument(document.id);
  })();
  onPartyDeclined(document.id, party.id, text);
  notifyOwner(document.owner_id, document.id, DOCUMENT_STATUS.DECLINED);
  return { state: 'declined' };
}

module.exports = { completeParty, declineParty };
