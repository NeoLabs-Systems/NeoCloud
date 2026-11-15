'use strict';

// Turns an outbox row into the message that is sent: the template of its
// kind, filled from the document, the party and the account as they are NOW
// (a renewed link, an edited title). Links come from configuration, never
// from a request. A party's link is decrypted from its stored copy only
// here, only to put it in that party's own mail.

const db = require('../../../db/database');
const { decryptValue } = require('../../crypto/keys');
const { getEmailConfig } = require('../../account/service_email');
const { readBlobBuffer } = require('../blobs');
const { LIVE_STATUSES, OWNER_PAGE_PATH, PARTY_STATUS, SIGN_PAGE_PATH } = require('../constants');
const { liveFinalNode } = require('../final_node');
const { finalFileName } = require('../final_name');
const { getDocumentById } = require('../store');
const { matchesHash } = require('../tokens');
const { singleLine } = require('../../account/email_templates');
const { tr } = require('./strings');
const { renderKind } = require('./templates');

const MAIL_ATTACHMENT_MAX_BYTES = 10 * 1024 * 1024;
const REQUEST_KINDS = ['invitation', 'reminder', 'your_turn', 'expiry_warning'];

let attachmentLimit = MAIL_ATTACHMENT_MAX_BYTES;

/** Tests lower the attachment limit to try the "too large" path; no argument restores it. */
function setAttachmentLimitForTests(bytes) {
  attachmentLimit = bytes || MAIL_ATTACHMENT_MAX_BYTES;
}

// A failure no retry can fix: the outbox marks the mail failed at once.
function permanent(message) {
  const error = new Error(message);
  error.permanent = true;
  return error;
}

/** The page of a party's own link, or null when its stored link is missing or no longer the live one. */
function partyPageUrl(party, baseUrl) {
  if (!party?.token_enc || !party.token_hash) return null;
  try {
    const token = decryptValue(party.token_enc);
    return matchesHash(token, party.token_hash) ? `${baseUrl}${SIGN_PAGE_PATH}${token}` : null;
  } catch {
    return null;
  }
}

const isOpen = (document, party) => LIVE_STATUSES.includes(document.status)
  && party.status !== PARTY_STATUS.COMPLETED && party.status !== PARTY_STATUS.DECLINED;

function requestMail(row, payload, context) {
  const { document, party, baseUrl } = context;
  if (!party || !isOpen(document, party)) return null;
  const url = partyPageUrl(party, baseUrl);
  if (!url) throw permanent('The signing link is no longer available.');
  const firstContact = row.kind === 'invitation' || row.kind === 'your_turn';
  const message = payload.message ?? (firstContact ? document.message : null);
  return renderKind(row.kind, { ...context.common, url, message });
}

// The signed file as an attachment when it fits, else only its size is known.
async function finalAttachment(document) {
  const node = liveFinalNode(document);
  if (!node) return { unavailable: true };
  if (node.size > attachmentLimit) return { attached: false };
  const content = await readBlobBuffer(document.owner_id, node.blob_id, node.size);
  return {
    attached: true,
    attachments: [{ filename: finalFileName(document), content, contentType: 'application/pdf' }],
  };
}

async function completedMail(row, context) {
  const { document, party, baseUrl } = context;
  const ownerView = !row.party_id;
  const url = ownerView ? `${baseUrl}${OWNER_PAGE_PATH}${document.id}` : partyPageUrl(party, baseUrl);
  const file = await finalAttachment(document);
  const message = renderKind('completed', {
    ...context.common, url, ownerView, attached: file.attached === true, unavailable: file.unavailable === true,
  });
  return file.attachments ? { ...message, attachments: file.attachments } : message;
}

function noticeMail(row, payload, context) {
  const url = `${context.baseUrl}${OWNER_PAGE_PATH}${context.document.id}`;
  return renderKind(row.kind, {
    ...context.common, url, name: payload.name, reason: payload.reason, done: payload.done, total: payload.total,
  });
}

// Where somebody can report a mail they did not expect: the support URL, else the reply-to or sender address.
function reportContact(config) {
  if (config.supportUrl) return config.supportUrl;
  const address = (config.replyTo || config.from).match(/<([^<>]+)>\s*$/)?.[1] || config.replyTo || config.from;
  return address ? `mailto:${address.trim()}` : '';
}

// Mail to a party says who asked for it, that it can be ignored and where to report it.
function withRequesterFooter(message, { lang, creatorName, config }) {
  const contact = reportContact(config);
  const creator = singleLine(creatorName).slice(0, 100) || tr(lang, 'creatorFallback');
  const report = contact ? tr(lang, 'footer.report', { contact }) : '';
  return { ...message, footer: tr(lang, 'footer.requested', { creator, brand: config.brandName, report }) };
}

/**
 * `{ to, message }` for sendServiceEmail, or null when the mail is no longer
 * wanted (its party already signed, the document ended). Throws an error with
 * `permanent: true` when it can never be built.
 */
async function composeMail(row, payload) {
  const document = getDocumentById(row.document_id);
  if (!document) throw permanent('The document no longer exists.');
  const owner = db.prepare('SELECT display_name, username FROM users WHERE id = ?').get(document.owner_id);
  const party = row.party_id ? db.prepare('SELECT * FROM sign_parties WHERE id = ?').get(row.party_id) : null;
  if (row.party_id && !party) throw permanent('The party no longer exists.');
  const config = getEmailConfig();
  const context = {
    document,
    party,
    baseUrl: config.publicUrl,
    common: {
      lang: document.locale,
      creatorName: owner?.display_name || owner?.username || '',
      title: document.title,
      expiresAt: document.expires_at,
    },
  };
  let message;
  if (REQUEST_KINDS.includes(row.kind)) message = requestMail(row, payload, context);
  else if (row.kind === 'completed') message = await completedMail(row, context);
  else message = noticeMail(row, payload, context);
  if (!message) return null;
  const toParty = Boolean(row.party_id) && (REQUEST_KINDS.includes(row.kind) || row.kind === 'completed');
  const finished = toParty ? withRequesterFooter(message, { lang: document.locale, creatorName: context.common.creatorName, config }) : message;
  return { to: row.to_email, message: finished };
}

module.exports = { composeMail, partyPageUrl, setAttachmentLimitForTests };
