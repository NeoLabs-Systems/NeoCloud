'use strict';

// Service mail: the SMTP settings (NEOCLOUD_EMAIL_*, read on every send so an
// admin's change applies without a restart), the transport and the one send
// function every feature calls. Mail exists only when an admin configured it
// (host and sender); until then isServiceEmailConfigured() is false and
// NeoCloud behaves as if it had no mail at all.
//
// Other features use:
//   isServiceEmailConfigured(), getEmailConfig()
//   sendServiceEmail(to, message)   message: see email_templates.js, plus
//                                   { subject, lang?, attachments? }
//   sendBackground(label, task)     fire and forget; failures are logged
//   renderEmail / renderText        the layout, for previews

const nodemailer = require('nodemailer');
const { readEnvBool } = require('../../../runtime/env');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger, redactSecrets } = require('../../utils/logger');
const { t } = require('./email_i18n');
const { renderEmail, renderText, singleLine } = require('./email_templates');

const log = createServiceLogger('Email');

const DEFAULT_PORT = 587;
const DEFAULT_TOKEN_TTL_HOURS = 24;
const DEFAULT_BRAND = 'NeoCloud';
const SMTP_TIMEOUT_MS = 15 * 1000;
const MAX_ERROR_TEXT = 300;
// A bare address: no display name, comments, groups or lists, which are what
// address-parser tricks hide in.
const PLAIN_ADDRESS = /^[^\s@<>,;:"()[\]\\]+@[^\s@<>,;:"()[\]\\]+\.[^\s@<>,;:"()[\]\\]+$/;

function envText(name) {
  return String(process.env[name] || '').trim();
}

function envPositiveNumber(name, fallback) {
  const value = Number(envText(name));
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

// Links in mails come from configuration, never from the request's Host
// header, which anyone can forge.
function publicBaseUrl() {
  const configured = envText('NEOCLOUD_EMAIL_PUBLIC_URL') || envText('PUBLIC_URL');
  return (configured || `http://localhost:${envText('PORT') || '3344'}`).replace(/\/+$/, '');
}

function getEmailConfig() {
  const host = envText('NEOCLOUD_EMAIL_SMTP_HOST');
  const from = envText('NEOCLOUD_EMAIL_FROM');
  const port = envPositiveNumber('NEOCLOUD_EMAIL_SMTP_PORT', DEFAULT_PORT);
  const secure = readEnvBool('NEOCLOUD_EMAIL_SMTP_SECURE', port === 465);
  const missing = [];
  if (!host) missing.push('NEOCLOUD_EMAIL_SMTP_HOST');
  if (!from) missing.push('NEOCLOUD_EMAIL_FROM');
  return {
    configured: missing.length === 0,
    missing,
    host,
    port,
    secure,
    requireTls: readEnvBool('NEOCLOUD_EMAIL_SMTP_REQUIRE_TLS', !secure),
    rejectUnauthorized: readEnvBool('NEOCLOUD_EMAIL_SMTP_REJECT_UNAUTHORIZED', true),
    user: envText('NEOCLOUD_EMAIL_SMTP_USER'),
    pass: envText('NEOCLOUD_EMAIL_SMTP_PASS'),
    from,
    replyTo: envText('NEOCLOUD_EMAIL_REPLY_TO'),
    brandName: envText('NEOCLOUD_EMAIL_BRAND_NAME') || DEFAULT_BRAND,
    supportUrl: envText('NEOCLOUD_EMAIL_SUPPORT_URL'),
    publicUrl: publicBaseUrl(),
    tokenTtlHours: envPositiveNumber('NEOCLOUD_EMAIL_TOKEN_TTL_HOURS', DEFAULT_TOKEN_TTL_HOURS),
    requireSignupConfirmation: readEnvBool('NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION', true),
    requireEmailChangeConfirmation: readEnvBool('NEOCLOUD_EMAIL_REQUIRE_EMAIL_CHANGE_CONFIRMATION', true),
    notifyNewLogin: readEnvBool('NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN', true),
    notifyAccountChanges: readEnvBool('NEOCLOUD_EMAIL_NOTIFY_ACCOUNT_CHANGES', true),
    // The admin's kill switch for mail to other people (signing requests, reminders, completion).
    signingMails: readEnvBool('NEOCLOUD_EMAIL_SIGNING_MAILS', true),
  };
}

function isServiceEmailConfigured() {
  return getEmailConfig().configured;
}

/** Signing mail (to people who are not the account) is on: mail is configured and the admin did not switch it off. */
function isSigningMailEnabled(config = getEmailConfig()) {
  return config.configured && config.signingMails;
}

/** New accounts must confirm their address before they can sign in. */
function requireSignupEmailConfirmation(config = getEmailConfig()) {
  return config.configured && config.requireSignupConfirmation;
}

/** A new address replaces the old one only after it is confirmed. */
function requireEmailChangeConfirmation(config = getEmailConfig()) {
  return config.configured && config.requireEmailChangeConfirmation;
}

function defaultTransportFactory(config) {
  return nodemailer.createTransport({
    host: config.host,
    port: config.port,
    secure: config.secure,
    requireTLS: config.requireTls,
    auth: config.user || config.pass ? { user: config.user, pass: config.pass } : undefined,
    tls: { rejectUnauthorized: config.rejectUnauthorized },
    connectionTimeout: SMTP_TIMEOUT_MS,
    greetingTimeout: SMTP_TIMEOUT_MS,
    socketTimeout: SMTP_TIMEOUT_MS,
    // A message names attachments by content only: never a file path or URL
    // the server would then read on someone's behalf.
    disableFileAccess: true,
    disableUrlAccess: true,
  });
}

let transportFactory = defaultTransportFactory;

/** Tests swap the SMTP transport for one that records mail; no argument restores it. */
function setTransportFactoryForTests(factory) {
  transportFactory = factory || defaultTransportFactory;
}

function assertPlainAddress(value, what) {
  if (typeof value !== 'string' || !PLAIN_ADDRESS.test(value) || value.length > 254) {
    throw httpError(400, `${what} is not a plain email address.`, 'INVALID_EMAIL');
  }
  return value;
}

/** The error as an admin may read it: one line, no password, no URL secrets. */
function describeSendError(err, config = getEmailConfig()) {
  let text = singleLine(err?.response || err?.message || 'Sending failed');
  if (config.pass) text = text.split(config.pass).join('[hidden]');
  text = redactSecrets(text);
  const code = typeof err?.code === 'string' ? ` (${err.code})` : '';
  return `${text.slice(0, MAX_ERROR_TEXT)}${code}`;
}

function footerFor(config, lang) {
  return config.supportUrl
    ? t(lang, 'mail.footerSupport', { brand: config.brandName, url: config.supportUrl })
    : t(lang, 'mail.footer', { brand: config.brandName });
}

/**
 * Sends one mail. Resolves { skipped: true } when mail is not configured,
 * else { messageId }. Throws the transport's error when SMTP refuses.
 * `message.subject` and the sender are held to one line (header injection),
 * the recipient to a bare address.
 */
async function sendServiceEmail(to, message) {
  const config = getEmailConfig();
  if (!config.configured) return { skipped: true, reason: 'disabled' };
  const lang = message.lang || 'en';
  const themed = {
    ...message,
    lang,
    brandName: config.brandName,
    footer: message.footer || footerFor(config, lang),
  };
  if (/[\r\n]/.test(config.from) || /[\r\n]/.test(config.replyTo)) {
    throw httpError(500, 'The sender address of service email must be a single line.', 'EMAIL_CONFIG_INVALID');
  }
  const recipients = (Array.isArray(to) ? to : [to]).map((address) => assertPlainAddress(address, 'The recipient'));
  const transport = transportFactory(config);
  try {
    const info = await transport.sendMail({
      from: config.from,
      to: recipients,
      replyTo: config.replyTo || undefined,
      subject: singleLine(message.subject),
      text: renderText(themed),
      html: renderEmail(themed),
      attachments: message.attachments,
      // Tells auto-responders and out-of-office rules to stay quiet.
      // `Precedence: bulk` keeps it out of vacation replies; mail to people who did not ask for it says so.
      headers: {
        'Auto-Submitted': 'auto-generated', 'X-Auto-Response-Suppress': 'All', Precedence: 'bulk', ...(message.headers || {}),
      },
    });
    return { messageId: info?.messageId || null };
  } finally {
    transport.close?.();
  }
}

/**
 * Starts a send (or any async task around one) without making the caller
 * wait for SMTP. A failure is logged, without secrets, never thrown; the
 * returned promise never rejects, so tests can await it.
 */
function sendBackground(label, task) {
  return Promise.resolve()
    .then(task)
    .catch((err) => {
      log.warn(`${label} failed: ${describeSendError(err)}`);
    });
}

module.exports = {
  describeSendError,
  getEmailConfig,
  isServiceEmailConfigured,
  isSigningMailEnabled,
  renderEmail,
  renderText,
  requireEmailChangeConfirmation,
  requireSignupEmailConfirmation,
  sendBackground,
  sendServiceEmail,
  setTransportFactoryForTests,
};
