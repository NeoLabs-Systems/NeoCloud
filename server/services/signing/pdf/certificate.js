'use strict';

// The audit/certificate pages appended to the final PDF.

const { rgb } = require('pdf-lib');
const { COLORS, CONTENT_WIDTH, Flow } = require('./flow');
const { formatUtc, parseColor, toDate } = require('./normalize');
const { fill } = require('./strings');

const EVENT_COLUMNS = [
  { key: 'colNo', width: 24 },
  { key: 'colTime', width: 112 },
  { key: 'colEvent', width: 128 },
  { key: 'colParty', width: 100 },
  { key: 'colIp', width: 85 },
  { key: 'colHash', width: CONTENT_WIDTH - 449 },
];

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatOffset(minutes) {
  const total = Number(minutes);
  if (!Number.isFinite(total)) return '';
  const sign = total >= 0 ? '+' : '-';
  const abs = Math.abs(total);
  return `UTC${sign}${String(Math.trunc(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
}

function describeTimezone(party) {
  const offset = formatOffset(party.tzOffsetMinutes);
  if (party.timezone && offset) return `${party.timezone} (${offset})`;
  return party.timezone || offset;
}

function describeMethods(strings, methods) {
  const labels = { draw: strings.methodDraw, type: strings.methodType, upload: strings.methodUpload };
  return [...methods].map((method) => labels[method] || strings.methodOther).join(', ');
}

function chronological(events) {
  return events
    .map((event, index) => ({ event, index, time: (toDate(event.at) || new Date(0)).getTime() }))
    .sort((a, b) => a.time - b.time || a.index - b.index)
    .map((entry) => entry.event);
}

function statusLabel(strings, party) {
  if (party.completedAt) return strings.statusCompleted;
  if (party.declinedAt) return strings.statusDeclined;
  return strings.statusOther;
}

function drawTitle(flow, strings) {
  flow.paragraph(strings.title, { size: 20, bold: true });
  flow.paragraph(strings.subtitle, { size: 9.5, color: COLORS.muted });
  flow.gap(4);
}

function drawDocument(flow, strings, data) {
  const { document, originalHash, originalPageCount } = data;
  flow.heading(strings.sectionDocument);
  flow.keyValue(strings.docTitle, document.title);
  flow.keyValue(strings.docId, document.id);
  flow.keyValue(strings.docPages, String(originalPageCount));
  if (document.signingOrder) {
    flow.keyValue(strings.docOrder, document.signingOrder === 'random' ? strings.orderRandom : strings.orderPreserved);
  }
  flow.keyValue(strings.docCreated, formatUtc(document.createdAt));
  flow.keyValue(strings.docSent, formatUtc(document.sentAt));
  flow.keyValue(strings.docCompleted, formatUtc(data.finalizedAt));
  flow.keyValue(strings.originalHash, originalHash);
}

function drawParty(flow, strings, party, index, methods, defaultConsentVersion) {
  flow.ensure(120);
  flow.gap(4);
  const color = parseColor(party.color);
  if (color) flow.dot(rgb(...color), 52);
  flow.paragraph(`${index + 1}. ${party.name}`, { size: 10, bold: true, x: color ? 62 : 48 });
  flow.keyValue(strings.email, party.email);
  flow.keyValue(strings.signerName, party.signerName);
  flow.keyValue(strings.status, statusLabel(strings, party));
  flow.keyValue(strings.ip, party.ip);
  flow.keyValue(strings.userAgent, party.userAgent);
  flow.keyValue(strings.timezone, describeTimezone(party));
  flow.keyValue(strings.opened, formatUtc(party.openedAt));
  const consent = formatUtc(party.consentedAt);
  const versionId = party.consentVersion || defaultConsentVersion;
  const version = versionId ? ` (${fill(strings.consentVersion, { version: versionId })})` : '';
  flow.keyValue(strings.consent, consent ? `${consent}${version}` : '');
  flow.keyValue(strings.signedAt, formatUtc(party.completedAt));
  flow.keyValue(strings.method, party.completedAt ? describeMethods(strings, methods) : '');
  flow.keyValue(strings.declinedAt, formatUtc(party.declinedAt));
  flow.keyValue(strings.declineReason, party.declineReason);
  flow.gap(4);
}

function drawAttachments(flow, strings, attachments) {
  if (attachments.length === 0) return;
  flow.heading(strings.sectionAttachments);
  for (const file of attachments) {
    flow.keyValue(file.fieldName || '-', `${file.filename} (${formatSize(file.size)})\nSHA-256 ${file.sha256}`);
  }
}

function drawEvents(flow, strings, data) {
  const events = chronological(data.events);
  flow.heading(strings.sectionEvents);
  if (events.length === 0) {
    flow.paragraph(strings.noEvents, { color: COLORS.muted });
    return;
  }
  const partyName = new Map(data.parties.map((party) => [party.id, party.name]));
  const rows = events.map((event, index) => [
    String(index + 1),
    formatUtc(event.at),
    strings.events[event.type] || event.type,
    event.partyId ? partyName.get(String(event.partyId)) || '-' : strings.sender,
    event.ip || '-',
    event.hash ? String(event.hash).slice(0, 8) : '-',
  ]);
  flow.table(EVENT_COLUMNS.map((column) => ({ title: strings[column.key], width: column.width })), rows);

  const head = events[events.length - 1].hash;
  if (head) {
    flow.gap(8);
    flow.ensure(40);
    flow.paragraph(`${strings.chainHead} - ${fill(strings.chainCount, { count: events.length })}`, { size: 8.5, color: COLORS.muted });
    flow.paragraph(String(head), { size: 8.5 });
  }
}

function drawVerification(flow, strings) {
  flow.heading(strings.sectionVerify);
  for (const note of strings.verify) {
    flow.paragraph(note, { size: 8.5, color: COLORS.muted });
    flow.gap(3);
  }
}

/**
 * Appends the certificate pages to `doc` and returns the page count added.
 * `data`: { document, parties, events, originalHash, originalPageCount,
 * attachments, methodsByParty, finalizedAt } with everything normalised.
 */
function appendCertificate(doc, fonts, strings, data) {
  const flow = new Flow(doc, fonts, (n, total) => ({
    left: fill(strings.footer, { id: data.document.id }),
    right: fill(strings.page, { n, total }),
  }));
  drawTitle(flow, strings);
  drawDocument(flow, strings, data);
  flow.heading(strings.sectionParties);
  data.parties.forEach((party, index) => {
    drawParty(flow, strings, party, index, data.methodsByParty.get(party.id) || new Set(), data.document.consentVersion);
  });
  drawAttachments(flow, strings, data.attachments);
  drawEvents(flow, strings, data);
  drawVerification(flow, strings);
  flow.finish();
  return flow.pages.length;
}

module.exports = { appendCertificate };
