'use strict';

// A mount's connection settings: validated against the provider's field
// list, and shown to the client without any secret. Saving a blank secret
// keeps the stored one, so a form can be edited without retyping passwords.

const { httpError } = require('../../utils/http_error');
const { PROVIDERS } = require('./adapters');

function invalid(message) {
  return httpError(400, message, 'EXTERNAL_CONFIG_INVALID');
}

function providerFor(type) {
  const provider = PROVIDERS[type];
  if (!provider) throw httpError(400, `provider must be one of ${Object.keys(PROVIDERS).join(', ')}.`, 'EXTERNAL_PROVIDER_INVALID');
  return provider;
}

function cleanValue(field, value) {
  if (field.kind === 'boolean') return value === true || value === 'true';
  if (field.kind === 'number') return Number(value);
  if (typeof value !== 'string') throw invalid(`${field.name} must be text.`);
  return field.secret ? value : value.trim();
}

/** The settings to store for `type`; `previous` supplies the secrets (and the host key) left blank. */
function cleanConfig(type, incoming, previous = {}) {
  const provider = providerFor(type);
  if (incoming !== undefined && incoming !== null && (typeof incoming !== 'object' || Array.isArray(incoming))) throw invalid('config must be an object.');
  const given = incoming || {};
  const config = {};
  for (const field of provider.fields) {
    let value = field.internal ? undefined : given[field.name];
    if (field.secret && (value === undefined || value === '')) value = previous[field.name];
    if (value === undefined || value === null || value === '') continue;
    value = cleanValue(field, value);
    if (typeof value === 'string' && value.length > (field.max || 1024)) throw invalid(`${field.name} is too long.`);
    if (typeof value === 'number' && !Number.isFinite(value)) throw invalid(`${field.name} must be a number.`);
    config[field.name] = value;
  }
  for (const field of provider.fields) {
    if (field.required && config[field.name] === undefined) throw invalid(`${field.name} is required.`);
  }
  // A server whose address changed has a different identity.
  const hostOf = (settings) => {
    try {
      return provider.hostOf(settings);
    } catch {
      return null;
    }
  };
  const sameServer = hostOf(config) === hostOf(previous) && config.port === previous.port;
  if (previous.hostKey && sameServer) config.hostKey = previous.hostKey;
  provider.validate?.(config);
  return config;
}

/** What the client may see: every setting but the secrets, which show only whether they are set. */
function publicConfig(type, config) {
  const out = {};
  for (const field of providerFor(type).fields) {
    if (field.internal) continue;
    if (field.secret) out[`${field.name}Set`] = Boolean(config[field.name]);
    else if (config[field.name] !== undefined) out[field.name] = config[field.name];
  }
  return out;
}

module.exports = { cleanConfig, providerFor, publicConfig };
