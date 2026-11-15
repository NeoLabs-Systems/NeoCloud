'use strict';

// A signed-in account takes a signing link as its own: the party is bound to
// the account (so the signed file can be saved to its cloud and the request
// shows in its inbox) and, when the account's profile says so, the fields it
// can answer are prefilled. Only someone who holds the link can claim; the
// public routes themselves never read a session.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { EVENT } = require('./constants');
const { appendEvent, clientOf } = require('./events');
const { applyPrefill, computePrefill } = require('./prefill');
const { openContext } = require('./public');

const TODAY = /^\d{4}-\d{2}-\d{2}$/;

function alreadyClaimed() {
  return httpError(409, 'This request belongs to another account.', 'ALREADY_CLAIMED');
}

// Binds the party to the account unless another account holds it. Idempotent for the same account.
function bindParty(userId, { document, party }, req) {
  if (party.user_id === userId) return;
  if (party.user_id) throw alreadyClaimed();
  db.transaction(() => {
    const changed = db.prepare('UPDATE sign_parties SET user_id = ? WHERE id = ? AND user_id IS NULL').run(userId, party.id).changes;
    if (changed !== 1) throw alreadyClaimed();
    appendEvent(document.id, { type: EVENT.CLAIMED, partyId: party.id, ...clientOf(req), data: { via: 'link' } });
  })();
}

/**
 * `token`: the party's link token (a grant token works too). With `apply`
 * the prefill is written to the party's empty fields; with `preview` it is
 * only listed (nothing is bound or written). Resolves with
 * `{ partyId, documentTitle, prefill, canPrefill, applied }`.
 */
async function claimParty(userId, body, req) {
  if (typeof body?.token !== 'string') throw httpError(400, 'The body needs the link token.', 'INVALID_REQUEST');
  const today = typeof body.today === 'string' && TODAY.test(body.today) ? body.today : undefined;
  const context = openContext(body.token, req);
  if (body.preview === true) {
    // What "use my details" would fill in, shown before the signer agrees: nothing is bound or written.
    if (context.party.user_id && context.party.user_id !== userId) throw alreadyClaimed();
    const items = context.state === 'ready' ? computePrefill(userId, context.document, context.party, { today }) : [];
    return {
      partyId: context.party.id, documentTitle: context.document.title, prefill: items, canPrefill: items.length > 0, applied: [],
    };
  }
  bindParty(userId, context, req);
  const party = db.prepare('SELECT * FROM sign_parties WHERE id = ?').get(context.party.id);
  const { document } = context;
  const prefill = context.state === 'ready' ? computePrefill(userId, document, party, { today }) : [];
  const applied = body.apply === true && prefill.length > 0 ? await applyPrefill(userId, document, party, prefill) : [];
  return {
    partyId: party.id,
    documentTitle: document.title,
    prefill,
    canPrefill: prefill.length > 0,
    applied,
  };
}

module.exports = { claimParty };
