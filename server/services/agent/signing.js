'use strict';

// Signature requests made by a connected app (NeoAgent) through /api/v1.
// A request is an ordinary signing document of the account: made from a
// rendered Markdown document or an existing PDF (with a signature page
// appended as a new file beside it), with a signature, printed name and date
// field for each signer, then sent. Signer links are returned once, here.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { splitExtension } = require('../../utils/file_names');
const documents = require('../signing/documents');
const links = require('../signing/links');
const { remindAll } = require('../signing/mail/reminders');
const { LIMITS } = require('../signing/constants');
const { getOwnedDocument } = require('../signing/store');
const agentFiles = require('./files');
const { appendSignaturePage, renderMarkdownPdf } = require('./markdown_pdf');

const DAY_MS = 24 * 60 * 60 * 1000;
const ORDERS = { parallel: 'random', sequential: 'preserved' };
// The status filter of the agent API and the document statuses it covers.
const STATUS_FILTERS = {
  pending: ['sent', 'in_progress', 'finalizing'],
  completed: ['completed'],
  cancelled: ['canceled'],
  expired: ['expired'],
  declined: ['declined'],
  draft: ['draft'],
};
const PUBLIC_STATUS = { in_progress: 'pending', sent: 'pending', finalizing: 'pending', canceled: 'cancelled' };

function invalid(message) {
  return httpError(400, message, 'INVALID_REQUEST');
}

function cleanSigners(input) {
  if (!Array.isArray(input) || input.length < 1 || input.length > LIMITS.maxParties) {
    throw invalid(`signers must list 1 to ${LIMITS.maxParties} people.`);
  }
  return input.map((signer) => {
    const name = typeof signer?.name === 'string' ? signer.name.trim() : '';
    if (!name) throw invalid('Every signer needs a name.');
    const email = typeof signer.email === 'string' && signer.email.trim() ? signer.email.trim() : null;
    return { name, email };
  });
}

function signerView(party, url) {
  return {
    id: party.id,
    name: party.name,
    email: party.email || null,
    status: party.status,
    signedAt: party.completed_at ?? party.completedAt ?? null,
    ...(url ? { url } : {}),
  };
}

function itemOf(row, parties) {
  return {
    id: row.id,
    title: row.title,
    status: PUBLIC_STATUS[row.status] || row.status,
    createdAt: row.created_at,
    signers: parties.map((party) => signerView(party)),
    ...(row.final_node_id ? { finalFileId: row.final_node_id } : {}),
  };
}

const partiesOf = (documentId) => db.prepare('SELECT * FROM sign_parties WHERE document_id = ? ORDER BY position').all(documentId);

function fieldsFor(parties, areas) {
  return parties.flatMap((party, index) => [
    { type: 'signature', partyId: party.id, name: 'Signature', required: true, areas: [areas[index].signature] },
    { type: 'text', partyId: party.id, name: 'Name', readonly: true, defaultValue: party.name, areas: [areas[index].name] },
    { type: 'date', partyId: party.id, name: 'Date', required: true, prefs: { autoToday: true }, areas: [areas[index].date] },
  ]);
}

/** The file to sign, saved in the library: the rendered document, or the PDF with a signature page. */
async function prepareFile(userId, body, signers) {
  if (body.document) {
    const { path, title, markdown } = body.document;
    const rendered = await renderMarkdownPdf({ title: typeof title === 'string' ? title : '', markdown, signers });
    const item = await agentFiles.writeFile(userId, { path, bytes: rendered.bytes });
    return { nodeId: item.id, areas: rendered.signatureAreas, title: typeof title === 'string' && title.trim() ? title.trim() : undefined };
  }
  const { node, bytes } = await agentFiles.fileBytes(userId, body.fileId);
  if (node.size > LIMITS.maxSourceBytes) throw httpError(413, 'This file is too large to sign (50 MB at most).', 'DOCUMENT_TOO_LARGE');
  if (!bytes.subarray(0, 1024).includes('%PDF-')) throw httpError(415, 'Only PDF files can be sent for signing this way.', 'UNSUPPORTED_TYPE');
  const withPage = await appendSignaturePage(bytes, { signers });
  const [base] = splitExtension(node.name);
  const parentPath = agentFiles.getItem(userId, node.parent_id).path;
  const target = `${parentPath === '/' ? '' : parentPath}/${base} (for signature).pdf`;
  const item = await agentFiles.writeFile(userId, { path: target, bytes: withPage.bytes, overwrite: true });
  return { nodeId: item.id, areas: withPage.signatureAreas, title: base };
}

async function createRequest(userId, body, { req, origin, canWriteFiles }) {
  const input = body || {};
  const hasFile = typeof input.fileId === 'string' && input.fileId;
  const hasDocument = input.document && typeof input.document === 'object';
  if (Boolean(hasFile) === Boolean(hasDocument)) throw invalid('Send exactly one of fileId or document.');
  if (hasDocument && !canWriteFiles) {
    throw httpError(403, 'This connection may not create files (files:write).', 'INSUFFICIENT_SCOPE');
  }
  const signers = cleanSigners(input.signers);
  if (input.signingOrder !== undefined && !ORDERS[input.signingOrder]) throw invalid('signingOrder is parallel or sequential.');
  if (input.sendEmail !== undefined && typeof input.sendEmail !== 'boolean') throw invalid('sendEmail must be true or false.');
  let expiresAt;
  if (input.expiresInDays !== undefined) {
    const days = Number(input.expiresInDays);
    if (!Number.isInteger(days) || days < 1 || days > 365) throw invalid('expiresInDays is a whole number from 1 to 365.');
    expiresAt = new Date(Date.now() + days * DAY_MS).toISOString();
  }

  const file = await prepareFile(userId, input, signers);
  const title = typeof input.title === 'string' && input.title.trim() ? input.title.trim() : file.title;
  const created = await documents.createDocument(userId, { nodeId: file.nodeId, title }, req);
  const documentId = created.document.id;
  try {
    const saved = documents.saveParties(userId, documentId, signers);
    documents.saveFields(userId, documentId, fieldsFor(saved.parties, file.areas), req);
    documents.updateSettings(userId, documentId, {
      signingOrder: ORDERS[input.signingOrder || 'parallel'],
      ...(expiresAt ? { expiresAt } : {}),
    }, req);
    const sent = await links.sendDocument(userId, documentId, req, origin, {
      message: input.message ?? undefined,
      deliverEmail: input.sendEmail === true,
    });
    const urls = new Map(sent.links.map((link) => [link.partyId, link.url]));
    return {
      id: documentId,
      title: sent.document.title,
      status: PUBLIC_STATUS[sent.document.status] || sent.document.status,
      signers: partiesOf(documentId).map((party) => signerView(party, urls.get(party.id))),
    };
  } catch (err) {
    // Nothing went out: the half-made draft goes again (the file stays in the library).
    await documents.deleteDocument(userId, documentId).catch(() => {});
    throw err;
  }
}

function listRequests(userId, { status, limit } = {}) {
  let statuses = null;
  if (status !== undefined && status !== '') {
    statuses = STATUS_FILTERS[status];
    if (!statuses) throw invalid(`status is one of: ${Object.keys(STATUS_FILTERS).join(', ')}.`);
  }
  const pageSize = Math.min(Math.max(Math.trunc(Number(limit)) || 50, 1), 100);
  const rows = db.prepare(`
    SELECT * FROM sign_documents
    WHERE owner_id = @userId AND (@statuses IS NULL OR status IN (SELECT value FROM json_each(@statuses)))
    ORDER BY updated_at DESC, id DESC LIMIT @limit
  `).all({ userId, statuses: statuses ? JSON.stringify(statuses) : null, limit: pageSize });
  return { items: rows.map((row) => itemOf(row, partiesOf(row.id))) };
}

function getRequest(userId, id) {
  const row = getOwnedDocument(userId, id);
  return itemOf(row, partiesOf(row.id));
}

function remindSigners(userId, id) {
  getOwnedDocument(userId, id);
  let result;
  try {
    result = remindAll(userId, id, {});
  } catch (err) {
    if (err.code === 'EMAIL_NOT_CONFIGURED') throw httpError(409, 'Email is not set up on this NeoCloud.', 'EMAIL_NOT_CONFIGURED');
    throw err;
  }
  const queued = result.results.filter((entry) => entry.status === 'queued');
  if (queued.length === 0) {
    const reason = result.results[0]?.code || 'NO_EMAIL';
    throw httpError(409, 'Nobody could be reminded (signers need an email address and must still be waiting).', reason);
  }
  return { reminded: queued.map((entry) => entry.partyId) };
}

function newSignerLink(userId, id, signerId, { req, origin }) {
  const { url } = links.regenerateLink(userId, id, String(signerId), req, origin);
  return { url };
}

function cancelRequest(userId, id, req) {
  documents.cancelDocument(userId, id, req);
  return getRequest(userId, id);
}

module.exports = { cancelRequest, createRequest, getRequest, listRequests, newSignerLink, remindSigners };
