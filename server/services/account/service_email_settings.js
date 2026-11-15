'use strict';

// The admin's view of the mail settings (docs/email.md): read them from the
// environment, validate an update, and write it to the .env file and to
// process.env so it applies without a restart. The SMTP password is only ever
// written, never returned: `smtpPasswordConfigured` says whether one is set.

const { ENV_FILE, upsertEnvValue } = require('../../../runtime/paths');
const { readEnvBool } = require('../../../runtime/env');
const { httpError } = require('../../utils/http_error');

const STRING_FIELDS = Object.freeze({
  from: 'NEOCLOUD_EMAIL_FROM',
  smtpHost: 'NEOCLOUD_EMAIL_SMTP_HOST',
  smtpUser: 'NEOCLOUD_EMAIL_SMTP_USER',
  replyTo: 'NEOCLOUD_EMAIL_REPLY_TO',
  brandName: 'NEOCLOUD_EMAIL_BRAND_NAME',
  supportUrl: 'NEOCLOUD_EMAIL_SUPPORT_URL',
  publicUrl: 'NEOCLOUD_EMAIL_PUBLIC_URL',
});

const LABELS = Object.freeze({
  from: 'Sender address',
  smtpHost: 'SMTP host',
  smtpUser: 'SMTP username',
  replyTo: 'Reply-to address',
  brandName: 'Brand name',
  supportUrl: 'Support URL',
  publicUrl: 'Public URL',
});

// name → [env key, default]. The two TLS defaults depend on the port and are
// resolved in effectiveDefault().
const BOOLEAN_FIELDS = Object.freeze({
  smtpSecure: ['NEOCLOUD_EMAIL_SMTP_SECURE', false],
  smtpRequireTls: ['NEOCLOUD_EMAIL_SMTP_REQUIRE_TLS', true],
  smtpRejectUnauthorized: ['NEOCLOUD_EMAIL_SMTP_REJECT_UNAUTHORIZED', true],
  requireSignupConfirmation: ['NEOCLOUD_EMAIL_REQUIRE_SIGNUP_CONFIRMATION', true],
  requireEmailChangeConfirmation: ['NEOCLOUD_EMAIL_REQUIRE_EMAIL_CHANGE_CONFIRMATION', true],
  notifyNewLogin: ['NEOCLOUD_EMAIL_NOTIFY_NEW_LOGIN', true],
  notifyAccountChanges: ['NEOCLOUD_EMAIL_NOTIFY_ACCOUNT_CHANGES', true],
  signingMails: ['NEOCLOUD_EMAIL_SIGNING_MAILS', true],
});

const PORT_KEY = 'NEOCLOUD_EMAIL_SMTP_PORT';
const TTL_KEY = 'NEOCLOUD_EMAIL_TOKEN_TTL_HOURS';
const PASSWORD_KEY = 'NEOCLOUD_EMAIL_SMTP_PASS';
const MAX_VALUE_LENGTH = 300;
const ADDRESS = /^[^\s@<>,;"()]+@[^\s@<>,;"()]+\.[^\s@<>,;"()]+$/;
const HOSTNAME = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$|^\[[0-9A-Fa-f:.]+\]$/;

function envString(env, key) {
  return String(env[key] || '').trim();
}

function envPositiveInteger(env, key, fallback) {
  const value = Number(envString(env, key));
  return Number.isInteger(value) && value > 0 ? value : fallback;
}

function cleanSingleLine(value, label) {
  if (typeof value !== 'string') throw httpError(400, `${label} must be text.`, 'INVALID_VALUE');
  if (/[\u0000-\u001f\u007f]/.test(value)) throw httpError(400, `${label} must be a single line.`, 'INVALID_VALUE');
  const cleaned = value.trim();
  if (cleaned.length > MAX_VALUE_LENGTH) throw httpError(400, `${label} is too long.`, 'INVALID_VALUE');
  return cleaned;
}

function requireBoolean(value, label) {
  if (typeof value !== 'boolean') throw httpError(400, `${label} must be true or false.`, 'INVALID_VALUE');
  return value;
}

function requireInteger(value, label, { min, max }) {
  const number = typeof value === 'string' && value.trim() !== '' ? Number(value) : value;
  if (!Number.isInteger(number) || number < min || number > max) {
    throw httpError(400, `${label} must be an integer from ${min} to ${max}.`, 'INVALID_VALUE');
  }
  return number;
}

function requireHttpUrl(value, label) {
  const cleaned = cleanSingleLine(value, label);
  if (!cleaned) return '';
  let valid = false;
  try {
    const url = new URL(cleaned);
    valid = url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    valid = false;
  }
  if (!valid) throw httpError(400, `${label} must be an HTTP or HTTPS URL.`, 'INVALID_VALUE');
  return cleaned.replace(/\/+$/, '');
}

// "Name <a@b.example>" or "a@b.example".
function requireMailbox(value, label) {
  const cleaned = cleanSingleLine(value, label);
  if (!cleaned) return '';
  const inner = cleaned.match(/<([^<>]+)>\s*$/)?.[1] ?? cleaned;
  if (!ADDRESS.test(inner.trim()) || /[<>]/.test(cleaned.replace(/<[^<>]+>\s*$/, ''))) {
    throw httpError(400, `${label} must be an email address, optionally as "Name <address>".`, 'INVALID_VALUE');
  }
  return cleaned;
}

function requireHost(value, label) {
  const cleaned = cleanSingleLine(value, label);
  if (cleaned && !HOSTNAME.test(cleaned)) throw httpError(400, `${label} must be a host name or address.`, 'INVALID_VALUE');
  return cleaned;
}

/**
 * A .env line the server's own loader (dotenv) reads back as exactly `value`.
 * dotenv does not unescape quotes or backslashes, so the quote character is
 * chosen so that neither appears inside: double quotes for ordinary values,
 * else single quotes, else backticks. A value that contains all three kinds
 * of quote cannot be stored in a .env file at all.
 */
function encodeEnvValue(value) {
  const text = String(value);
  if (!text.includes('"') && !text.includes('\\')) return `"${text}"`;
  if (!text.includes("'")) return `'${text}'`;
  if (!text.includes('`')) return `\`${text}\``;
  throw httpError(400, 'The value may not contain all of \', " and ` together.', 'INVALID_VALUE');
}

function persistValue(envFile, env, key, value) {
  upsertEnvValue(envFile, key, value === '' ? '' : encodeEnvValue(value));
  if (value === '') {
    delete env[key];
  } else {
    env[key] = String(value);
  }
}

function effectiveDefault(name, fallback, { smtpPort, smtpSecure }) {
  if (name === 'smtpSecure') return smtpPort === 465;
  if (name === 'smtpRequireTls') return !smtpSecure;
  return fallback;
}

function getAdminEmailSettings({ env = process.env } = {}) {
  const smtpPort = envPositiveInteger(env, PORT_KEY, 587);
  const settings = {
    smtpPort,
    tokenTtlHours: envPositiveInteger(env, TTL_KEY, 24),
    smtpPasswordConfigured: Boolean(envString(env, PASSWORD_KEY)),
  };
  for (const [name, key] of Object.entries(STRING_FIELDS)) settings[name] = envString(env, key);
  for (const [name, [key, fallback]] of Object.entries(BOOLEAN_FIELDS)) {
    const value = envString(env, key).toLowerCase();
    settings[name] = value
      ? ['1', 'true', 'yes', 'on'].includes(value)
      : effectiveDefault(name, fallback, { smtpPort, smtpSecure: settings.smtpSecure });
  }
  // The same rule the sender uses (service_email.js), kept in step by a test.
  const missing = [];
  if (!settings.from) missing.push(LABELS.from);
  if (!settings.smtpHost) missing.push(LABELS.smtpHost);
  return { configured: missing.length === 0, missing, settings };
}

function validateString(name, value) {
  if (name === 'from' || name === 'replyTo') return requireMailbox(value, LABELS[name]);
  if (name === 'smtpHost') return requireHost(value, LABELS[name]);
  if (name === 'supportUrl' || name === 'publicUrl') return requireHttpUrl(value, LABELS[name]);
  return cleanSingleLine(value, LABELS[name]);
}

/**
 * Applies an update. Every field is optional: one that is absent keeps its
 * value, one that is present is validated like NeoAgent's. The password is
 * written only when non-empty, or removed by `clearSmtpPassword: true`.
 * Nothing is written unless everything validates.
 */
function updateAdminEmailSettings(body, { env = process.env, envFile = ENV_FILE } = {}) {
  if (!body || typeof body !== 'object' || Array.isArray(body)) {
    throw httpError(400, 'Email settings must be an object.', 'INVALID_VALUE');
  }
  const writes = [];
  for (const [name, key] of Object.entries(STRING_FIELDS)) {
    if (body[name] !== undefined) writes.push([key, validateString(name, body[name])]);
  }
  if (body.smtpPort !== undefined) writes.push([PORT_KEY, requireInteger(body.smtpPort, 'SMTP port', { min: 1, max: 65535 })]);
  if (body.tokenTtlHours !== undefined) writes.push([TTL_KEY, requireInteger(body.tokenTtlHours, 'Token lifetime', { min: 1, max: 8760 })]);
  for (const [name, [key]] of Object.entries(BOOLEAN_FIELDS)) {
    if (body[name] !== undefined) writes.push([key, requireBoolean(body[name], name)]);
  }
  const clear = body.clearSmtpPassword === true;
  const password = body.smtpPassword === undefined ? '' : cleanSingleLine(body.smtpPassword, 'SMTP password');
  if (password || clear) writes.push([PASSWORD_KEY, clear ? '' : password]);
  // Encoding can still refuse a value, so nothing is persisted before it is checked.
  for (const [, value] of writes) if (value !== '') encodeEnvValue(value);

  for (const [key, value] of writes) persistValue(envFile, env, key, value);
  return getAdminEmailSettings({ env });
}

module.exports = { encodeEnvValue, getAdminEmailSettings, updateAdminEmailSettings };
