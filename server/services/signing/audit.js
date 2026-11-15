'use strict';

// The owner's view of what happened: the event trail with its chain check,
// and the whole record as one JSON file.

const { listEvents, verifyChain } = require('./events');
const { documentView, fieldView, partyView } = require('./serialize');
const { getOwnedDocument, listFields, listParties, listValues, parseJson } = require('./store');

function eventsOf(userId, documentId) {
  const document = getOwnedDocument(userId, documentId);
  const chain = verifyChain(document.id);
  return {
    events: listEvents(document.id),
    verify: { chainValid: chain.valid, count: chain.count, head: chain.head, ...(chain.brokenAt ? { brokenAt: chain.brokenAt } : {}) },
  };
}

// What each party signed with: details a signer's page never shows the others.
function partyRecord(row) {
  return {
    ...partyView(row),
    ip: row.ip,
    userAgent: row.user_agent,
    timezone: row.timezone,
    tzOffsetMinutes: row.tz_offset_minutes,
  };
}

/** The audit record of a document: facts, parties, values and the hash-chained trail. */
function auditRecord(userId, documentId) {
  const document = getOwnedDocument(userId, documentId);
  const { events, verify } = eventsOf(userId, document.id);
  return {
    format: 1,
    kind: 'neocloud-signing-audit',
    exportedAt: new Date().toISOString(),
    document: documentView(document),
    parties: listParties(document.id).map(partyRecord),
    fields: listFields(document.id).map(fieldView),
    values: listValues(document.id).map((row) => ({
      fieldId: row.field_id,
      partyId: row.party_id,
      value: parseJson(row.value_json, null),
      updatedAt: row.updated_at,
    })),
    events,
    verify,
  };
}

module.exports = { auditRecord, eventsOf };
