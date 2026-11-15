'use strict';

// The signing mails as data for the shared layout (renderEmail): one function
// per kind, each from a plain context to a message. Every value that came from
// a person (names, titles, the sender's note) is held to one line here where
// it ends up in a subject, and escaped by the layout everywhere else.
//
// Context: { lang, creatorName, title, url, expiresAt, message, name, done,
//   total, reason, attached, ownerView }. A party's `url` is its own signing
// page; no template ever receives another party's link.

const { singleLine } = require('../../account/email_templates');
const { tr } = require('./strings');

const MAX_SUBJECT_PART = 100;

const short = (value, max = MAX_SUBJECT_PART) => {
  const text = singleLine(value);
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
};

/** "15 March 2026, 14:30 UTC": mail has no viewer time zone, so the time says which one it is. */
function formatWhen(lang, iso) {
  const date = new Date(iso);
  if (!iso || Number.isNaN(date.getTime())) return '';
  const text = new Intl.DateTimeFormat(lang === 'de' ? 'de-DE' : 'en-GB', {
    dateStyle: 'long', timeStyle: 'short', timeZone: 'UTC',
  }).format(date);
  return `${text} UTC`;
}

function names(ctx) {
  const { lang } = ctx;
  return {
    creator: short(ctx.creatorName) || tr(lang, 'creatorFallback'),
    title: short(ctx.title),
    name: short(ctx.name),
  };
}

function documentDetails(ctx, { withExpiry = true } = {}) {
  const details = [{ label: tr(ctx.lang, 'label.document'), value: singleLine(ctx.title) }];
  if (withExpiry && ctx.expiresAt) details.push({ label: tr(ctx.lang, 'label.expires'), value: formatWhen(ctx.lang, ctx.expiresAt) });
  return details;
}

// A request to sign, in its four flavours; they differ in words only.
function request(prefix) {
  return (ctx) => {
    const { lang } = ctx;
    const who = names(ctx);
    const note = singleLine(ctx.message) ? String(ctx.message).trim() : '';
    return {
      lang,
      subject: tr(lang, `${prefix}.subject`, who),
      title: tr(lang, `${prefix}.title`),
      intro: tr(lang, `${prefix}.intro`, { ...who, when: formatWhen(lang, ctx.expiresAt) }),
      paragraphs: [tr(lang, 'noAccount'), tr(lang, 'privacy')],
      quote: note ? { label: tr(lang, 'whoMessage', who), text: note } : undefined,
      actionUrl: ctx.url,
      actionLabel: tr(lang, `${prefix}.button`),
      details: documentDetails(ctx),
      eyebrow: tr(lang, 'eyebrow'),
    };
  };
}

function completed(ctx) {
  const { lang } = ctx;
  const who = names(ctx);
  const closing = ctx.attached ? tr(lang, 'completed.attached') : tr(lang, 'completed.tooLarge');
  const clean = tr(lang, ctx.ownerView ? 'completed.cleanOwner' : 'completed.cleanSigner');
  // The signed file may be gone from the library: then there is nothing to attach or to point to.
  const paragraphs = ctx.unavailable ? [] : [closing, clean];
  return {
    lang,
    subject: tr(lang, 'completed.subject', who),
    title: tr(lang, 'completed.title'),
    intro: tr(lang, 'completed.intro', who),
    paragraphs,
    actionUrl: ctx.url,
    actionLabel: tr(lang, ctx.ownerView ? 'completed.buttonOwner' : 'completed.buttonSigner'),
    details: documentDetails(ctx, { withExpiry: false }),
    eyebrow: tr(lang, 'eyebrow'),
  };
}

function progressDetails(ctx) {
  const details = documentDetails(ctx, { withExpiry: false });
  if (Number.isInteger(ctx.total) && ctx.total > 0) {
    details.push({ label: tr(ctx.lang, 'label.progress'), value: tr(ctx.lang, 'progress', { done: ctx.done, total: ctx.total }) });
  }
  return details;
}

function creatorNotice(prefix) {
  return (ctx) => {
    const { lang } = ctx;
    const who = names(ctx);
    const reason = singleLine(ctx.reason) ? String(ctx.reason).trim() : '';
    return {
      lang,
      subject: tr(lang, `${prefix}.subject`, who),
      title: tr(lang, `${prefix}.title`, who),
      intro: tr(lang, `${prefix}.intro`, who),
      quote: reason ? { label: tr(lang, 'creatorDeclined.reasonLabel'), text: reason } : undefined,
      actionUrl: ctx.url,
      actionLabel: tr(lang, `${prefix}.button`),
      details: progressDetails(ctx),
      eyebrow: tr(lang, 'eyebrow'),
    };
  };
}

const TEMPLATES = {
  invitation: request('invitation'),
  reminder: request('reminder'),
  your_turn: request('turn'),
  expiry_warning: request('expiry'),
  completed,
  creator_signed: creatorNotice('creatorSigned'),
  creator_declined: creatorNotice('creatorDeclined'),
  creator_expired: creatorNotice('creatorExpired'),
};

const MAIL_KINDS = Object.freeze(Object.keys(TEMPLATES));

/** The message (for sendServiceEmail) of one kind. */
function renderKind(kind, context) {
  return TEMPLATES[kind](context);
}

module.exports = { MAIL_KINDS, formatWhen, renderKind };
