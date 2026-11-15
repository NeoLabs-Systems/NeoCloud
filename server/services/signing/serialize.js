'use strict';

// Rows as the API shows them (camelCase). Tokens and token hashes never
// appear here.

const { parseJson } = require('./store');

function documentView(row) {
  return {
    id: row.id,
    title: row.title,
    status: row.status,
    signingOrder: row.signing_order,
    locale: row.locale,
    expiresAt: row.expires_at,
    pageCount: row.page_count,
    pages: parseJson(row.pages_json, []),
    sourceNodeId: row.source_node_id,
    sourceName: row.source_name,
    finalNodeId: row.final_node_id,
    snapshotSha256: row.snapshot_sha256,
    finalSha256: row.final_sha256,
    consentVersion: row.consent_text_version,
    message: row.message,
    reminderDays: row.reminder_days,
    finalizeError: row.status === 'finalizing' ? row.finalize_error : null,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    sentAt: row.sent_at,
    completedAt: row.completed_at,
    canceledAt: row.canceled_at,
  };
}

// The few columns the list shows for each party.
function partySummary(row) {
  return { id: row.id, name: row.name, color: row.color, status: row.status };
}

// `delivery`: the party's mail (mail/status.js); the caller supplies it, it needs the outbox.
function partyView(row, { delivery = null } = {}) {
  return {
    id: row.id,
    position: row.position,
    name: row.name,
    color: row.color,
    email: row.email,
    isSelf: row.is_self === 1,
    status: row.status,
    hasLink: Boolean(row.token_hash),
    linkHint: row.token_hash ? row.token_hint : null,
    openedAt: row.opened_at,
    consentedAt: row.consented_at,
    completedAt: row.completed_at,
    declinedAt: row.declined_at,
    declineReason: row.decline_reason,
    signerName: row.signer_name,
    createdAt: row.created_at,
    ...(delivery ? { delivery } : {}),
  };
}

function fieldView(row) {
  return {
    id: row.id,
    partyId: row.party_id,
    type: row.type,
    name: row.name,
    required: row.required === 1,
    readonly: row.readonly === 1,
    defaultValue: parseJson(row.default_value, null),
    prefs: parseJson(row.prefs_json, {}),
    areas: parseJson(row.areas_json, []),
    position: row.position,
  };
}

module.exports = { documentView, fieldView, partySummary, partyView };
