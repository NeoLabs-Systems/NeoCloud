'use strict';

const { httpError } = require('../../utils/http_error');

/** An http(s) address without credentials in it (those have their own fields). */
function assertHttpUrl(value, label) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw httpError(400, `${label} must be a full address like https://example.com.`, 'EXTERNAL_CONFIG_INVALID');
  }
  if (!['http:', 'https:'].includes(url.protocol)) throw httpError(400, `${label} must start with https:// or http://.`, 'EXTERNAL_CONFIG_INVALID');
  if (url.username || url.password) throw httpError(400, 'Put the user name and password in their own fields.', 'EXTERNAL_CONFIG_INVALID');
}

module.exports = { assertHttpUrl };
