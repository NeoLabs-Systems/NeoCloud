'use strict';

// What the holder of a signing link can see and do. The token finds the
// party; everything else (document, fields, turn) follows from the party's
// rows, never from ids the client sends. Unknown, malformed, revoked and
// replaced tokens are all the same 404.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { nodeById } = require('../files/tree');
const { DOCUMENT_STATUS, EVENT, LIMITS, PARTY_STATUS } = require('./constants');
const { CONSENT_VERSION, consentText } = require('./consent');
const { appendEvent, clientOf } = require('./events');
const { expireIfDue } = require('./expiry');
const { typeOf } = require('./field_types');
const { notifyOwner } = require('./notify');
const { singleLine } = require('./plain_text');
const { fieldView } = require('./serialize');
const {
  UUID_PATTERN, getDocumentById, listFields, listParties, listValues, nowIso, parseJson, updateDocument,
} = require('./store');
const { hashToken, isWellFormed, matchesHash } = require('./tokens');
const { publicState, waitingCount } = require('./turns');
const { clearBlobValue, saveTypedValue, valueOf } = require('./values');
const { uploadFieldValue } = require('./uploads');

const CLOSED_STATES = new Set(['canceled', 'expired', 'declined']);

const notFound = () => httpError(404, 'This link does not exist.', 'NOT_FOUND');

/** The party behind a token, or the uniform 404. */
function partyByToken(token) {
  if (!isWellFormed(token)) throw notFound();
  const party = db.prepare('SELECT * FROM sign_parties WHERE token_hash = ?').get(hashToken(token));
  if (!party || !matchesHash(token, party.token_hash)) throw notFound();
  return party;
}

/** { document, party, parties, state } for a token. Expires the document first when its time is up. */
function openContext(token) {
  const party = partyByToken(token);
  let document = getDocumentById(party.document_id);
  const owner = db.prepare('SELECT disabled_at FROM users WHERE id = ?').get(document?.owner_id);
  if (!document || !owner || owner.disabled_at || document.status === DOCUMENT_STATUS.DRAFT) throw notFound();
  if (expireIfDue(document)) document = getDocumentById(document.id);
  return { document, party, parties: listParties(document.id), state: publicState(document, party) };
}

// Why a signer cannot act in this state.
function refusal(state) {
  if (state === 'waiting') return httpError(409, 'It is not your turn yet.', 'NOT_YOUR_TURN');
  if (state === 'completed') return httpError(409, 'You have already signed this document.', 'ALREADY_COMPLETED');
  return httpError(409, 'This document is closed.', 'DOCUMENT_CLOSED');
}

function assertReady(context) {
  if (context.state !== 'ready') throw refusal(context.state);
}

/** The first time a party's turn is seen: it counts as opened. */
function recordOpened({ document, party }, req) {
  if (party.opened_at) return;
  const at = nowIso();
  db.transaction(() => {
    db.prepare(`UPDATE sign_parties SET opened_at = ?, status = CASE WHEN status = 'pending' THEN 'opened' ELSE status END WHERE id = ?`)
      .run(at, party.id);
    if (document.status === DOCUMENT_STATUS.SENT) updateDocument(document.id, { status: DOCUMENT_STATUS.IN_PROGRESS }, at);
    appendEvent(document.id, { type: EVENT.OPENED, partyId: party.id, at, ...clientOf(req) });
  })();
  notifyOwner(document.owner_id, document.id, DOCUMENT_STATUS.IN_PROGRESS);
}

const partyBrief = (party) => ({ id: party.id, name: party.name, color: party.color, status: party.status });

// A field as the signer's screen draws it. Other parties' fields carry their value.
function publicField(row, { own, value }) {
  const view = fieldView(row);
  return {
    id: view.id,
    partyId: view.partyId,
    type: view.type,
    name: view.name,
    required: view.required,
    readonly: own ? view.readonly : true,
    defaultValue: own ? view.defaultValue : null,
    prefs: view.prefs,
    areas: view.areas,
    own,
    ...(own ? {} : { value }),
  };
}

// The value a field shows: what was entered, else the sender's default.
function effectiveValue(row, valueRow) {
  if (valueRow) return valueOf(valueRow);
  return parseJson(row.default_value, null);
}

function fieldsFor({ document, party, parties }) {
  const rows = listFields(document.id);
  const values = new Map(listValues(document.id).map((row) => [row.field_id, row]));
  const done = new Set(parties.filter((other) => other.status === PARTY_STATUS.COMPLETED).map((other) => other.id));
  const fields = [];
  const myValues = {};
  for (const row of rows) {
    if (row.party_id === party.id) {
      fields.push(publicField(row, { own: true }));
      const value = effectiveValue(row, values.get(row.id));
      if (value !== null) myValues[row.id] = value;
    } else if (typeOf(row.type).storage === 'static' || row.readonly === 1 || done.has(row.party_id)) {
      const value = effectiveValue(row, values.get(row.id));
      if (value !== null) fields.push(publicField(row, { own: false, value }));
    }
  }
  return { fields, myValues };
}

/** The final file while it is still the one that was made: moved or renamed is fine, replaced, trashed or removed is not. */
function liveFinalNode(document) {
  if (document.status !== DOCUMENT_STATUS.COMPLETED || !document.final_node_id) return null;
  const node = nodeById(document.final_node_id);
  return node && !node.trashed_at && node.etag === document.final_sha256 ? node : null;
}

function finalAvailable(document) {
  return liveFinalNode(document) !== null;
}

/** The JSON the signing page starts from. Records the party's first look at its turn. */
function describe(context, req) {
  const { document, party, parties, state } = context;
  const brief = partyBrief(party);
  if (CLOSED_STATES.has(state)) return { state, document: { locale: document.locale }, party: brief };
  if (state === 'ready') recordOpened(context, req);
  const current = state === 'ready' ? { ...party, status: party.status === PARTY_STATUS.PENDING ? PARTY_STATUS.OPENED : party.status } : party;
  const base = {
    state,
    document: {
      title: document.title,
      pageCount: document.page_count,
      pages: parseJson(document.pages_json, []),
      locale: document.locale,
      expiresAt: document.expires_at,
      consentText: consentText(document.locale),
      consentVersion: document.consent_text_version || CONSENT_VERSION,
    },
    party: { ...partyBrief(current), consentedAt: party.consented_at, signerName: party.signer_name },
    // Counts only: a signer never learns the names, emails or colours of the other parties.
    progress: {
      done: parties.filter((other) => other.status === PARTY_STATUS.COMPLETED).length,
      total: parties.length,
    },
  };
  if (state === 'waiting') {
    return { ...base, fields: [], myValues: {}, turn: { canSign: false, waitingCount: waitingCount(party, parties) } };
  }
  return {
    ...base,
    ...fieldsFor(context),
    turn: { canSign: state === 'ready' },
    ...(state === 'completed' ? { result: { finalAvailable: finalAvailable(document) } } : {}),
  };
}

function signerName(value) {
  const name = typeof value === 'string' ? singleLine(value) : '';
  if (!name || name.length > LIMITS.maxSignerName) {
    throw httpError(400, `Enter your name (up to ${LIMITS.maxSignerName} characters).`, 'INVALID_REQUEST');
  }
  return name;
}

// The signer's time zone as the browser reports it; both parts are optional.
function timeZone(body) {
  const name = typeof body.timezone === 'string' ? body.timezone.trim() : '';
  const offset = body.tzOffsetMinutes;
  return {
    timezone: /^[A-Za-z0-9_+\-/]{1,64}$/.test(name) ? name : null,
    tzOffsetMinutes: Number.isInteger(offset) && Math.abs(offset) <= 14 * 60 ? offset : null,
  };
}

/** Records consent and the name the signer will sign with. */
function recordConsent(context, body, req) {
  assertReady(context);
  if (body?.accepted !== true) throw httpError(400, 'Consent is needed to sign.', 'CONSENT_REQUIRED');
  const name = signerName(body.signerName);
  const zone = timeZone(body);
  const { document, party } = context;
  const at = nowIso();
  const client = clientOf(req);
  db.transaction(() => {
    db.prepare(`
      UPDATE sign_parties SET consented_at = COALESCE(consented_at, ?), signer_name = ?, timezone = ?, tz_offset_minutes = ?,
        ip = ?, user_agent = ? WHERE id = ?
    `).run(at, name, zone.timezone, zone.tzOffsetMinutes, client.ip, client.userAgent, party.id);
    appendEvent(document.id, {
      type: EVENT.CONSENTED, partyId: party.id, at, ...client,
      data: { signerName: name, consentVersion: document.consent_text_version || CONSENT_VERSION, ...zone },
    });
  })();
  return { consentedAt: party.consented_at || at, signerName: name };
}

// One of the party's own fields that it may write: not read-only, takes input.
function writableField(context, fieldId) {
  const row = UUID_PATTERN.test(String(fieldId || ''))
    ? db.prepare('SELECT * FROM sign_fields WHERE id = ? AND document_id = ? AND party_id = ?')
      .get(fieldId, context.document.id, context.party.id)
    : null;
  if (!row || row.readonly === 1 || typeOf(row.type).storage === 'static') throw httpError(404, 'Field not found', 'NOT_FOUND');
  return { ...row, prefs: parseJson(row.prefs_json, {}) };
}

/** Saves one typed value; `null` clears the field. Resolves with the value as stored. */
async function saveFieldValue(context, fieldId, value) {
  assertReady(context);
  const field = writableField(context, fieldId);
  if (typeOf(field.type).storage === 'blob') {
    if (value !== null && value !== undefined) throw httpError(400, 'This field takes an upload.', 'INVALID_VALUE');
    await clearBlobValue(context.document, context.party, field);
    return { value: null };
  }
  return { value: saveTypedValue(context.document, context.party, field, value) };
}

/** Saves an upload (signature image or attachment) as the field's value. */
async function uploadFieldFile(context, fieldId, req) {
  assertReady(context);
  const field = writableField(context, fieldId);
  return { value: await uploadFieldValue(context.document, context.party, field, req) };
}

/**
 * The stored image of a field the party may see: its own, or one of a party
 * that has completed. Resolves with `{ row, size }` for serve.sendValueImage.
 */
function imageValue(context, fieldId) {
  if (context.state !== 'ready' && context.state !== 'completed') throw refusal(context.state);
  const row = UUID_PATTERN.test(String(fieldId || ''))
    ? db.prepare(`
      SELECT v.*, f.party_id AS owner_party, b.size AS size, p.status AS owner_status
      FROM sign_values v
      JOIN sign_fields f ON f.id = v.field_id
      JOIN sign_parties p ON p.id = f.party_id
      JOIN blobs b ON b.id = v.blob_ref
      WHERE v.field_id = ? AND v.document_id = ?
    `).get(fieldId, context.document.id)
    : null;
  const visible = row && (row.owner_party === context.party.id || row.owner_status === PARTY_STATUS.COMPLETED);
  if (!visible || valueOf(row)?.kind !== 'image') throw httpError(404, 'Not found', 'NOT_FOUND');
  return { row, size: row.size };
}

module.exports = {
  assertReady,
  describe,
  finalAvailable,
  imageValue,
  liveFinalNode,
  openContext,
  recordConsent,
  refusal,
  saveFieldValue,
  uploadFieldFile,
};
