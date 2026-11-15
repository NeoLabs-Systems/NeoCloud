'use strict';

// The admin's document-editor settings (docs/office-editing.md): the editor's
// address, the switch, and an optional address the editor uses to reach this
// server. Kept in the .env file like the other admin settings.

const { readEnvBool } = require('../../../runtime/env');
const { httpError } = require('../../utils/http_error');
const { persistEnv } = require('../admin/env_config');

const URL_KEY = 'NEOCLOUD_OFFICE_EDITOR_URL';
const ENABLED_KEY = 'NEOCLOUD_OFFICE_EDITOR_ENABLED';
const HOST_URL_KEY = 'NEOCLOUD_OFFICE_HOST_URL';

/** Returns the origin and path of a http(s) address without trailing slash, or throws 400. */
function cleanAddress(value, label) {
  const text = String(value ?? '').trim();
  if (text === '') return '';
  let parsed;
  try {
    parsed = new URL(text);
  } catch {
    throw httpError(400, `${label} must be a web address like https://office.example.com.`, 'INVALID_VALUE');
  }
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password || parsed.search || parsed.hash) {
    throw httpError(400, `${label} must be a plain http(s) address without login or parameters.`, 'INVALID_VALUE');
  }
  return `${parsed.origin}${parsed.pathname}`.replace(/\/+$/, '');
}

function readOfficeSettings(env = process.env) {
  return {
    enabled: readEnvBool(ENABLED_KEY, false) && String(env[URL_KEY] || '').trim() !== '',
    url: String(env[URL_KEY] || '').trim(),
    hostUrl: String(env[HOST_URL_KEY] || '').trim(),
  };
}

/** The values an update would result in, validated; nothing is written. */
function mergeOfficeSettings(body = {}) {
  const current = readOfficeSettings();
  const next = { ...current, enabled: readEnvBool(ENABLED_KEY, false) };
  if (body.url !== undefined) next.url = cleanAddress(body.url, 'The editor address');
  if (body.hostUrl !== undefined) next.hostUrl = cleanAddress(body.hostUrl, 'The address of this server');
  if (body.enabled !== undefined) {
    if (typeof body.enabled !== 'boolean') throw httpError(400, 'enabled must be true or false.', 'INVALID_VALUE');
    next.enabled = body.enabled;
  }
  if (next.enabled && !next.url) throw httpError(400, 'Enter the editor’s address first.', 'EDITOR_URL_REQUIRED');
  return next;
}

function writeOfficeSettings({ enabled, url, hostUrl }) {
  persistEnv(URL_KEY, url);
  persistEnv(HOST_URL_KEY, hostUrl);
  persistEnv(ENABLED_KEY, enabled ? 'true' : 'false');
}

module.exports = { mergeOfficeSettings, readOfficeSettings, writeOfficeSettings };
