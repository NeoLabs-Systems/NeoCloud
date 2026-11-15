'use strict';

// Admin use cases for the document editor (docs/office-editing.md): read the
// settings with the editor's state, and change them. Turning the editor on
// first asks it for its WOPI discovery, so a wrong address is refused here
// with a reason instead of failing later for everyone.

const { recordAccessEvent } = require('../access/audit');
const { checkEditor, editorStatus, forgetDiscovery } = require('../office/discovery');
const { mergeOfficeSettings, readOfficeSettings, writeOfficeSettings } = require('../office/settings');
const { httpError } = require('../../utils/http_error');

function getOfficeSettings() {
  const { enabled, url, hostUrl } = readOfficeSettings();
  return { enabled, url, hostUrl, status: editorStatus() };
}

async function updateOfficeSettings(body, { actorUserId }) {
  const next = mergeOfficeSettings(body);
  if (next.enabled) await verify(next.url);
  else if (next.url !== readOfficeSettings().url) forgetDiscovery();
  writeOfficeSettings(next);
  recordAccessEvent('office.config', { actorUserId, detail: { enabled: next.enabled } });
  return getOfficeSettings();
}

async function verify(url) {
  try {
    await checkEditor(url);
  } catch (err) {
    throw httpError(422, err.message, 'EDITOR_UNREACHABLE');
  }
}

/** Asks the configured editor again and reports what it says. */
async function recheckOffice() {
  const { url } = readOfficeSettings();
  if (!url) throw httpError(409, 'Enter the editor’s address first.', 'EDITOR_URL_REQUIRED');
  await verify(url);
  return getOfficeSettings();
}

module.exports = { getOfficeSettings, recheckOffice, updateOfficeSettings };
