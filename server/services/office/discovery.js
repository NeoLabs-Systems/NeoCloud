'use strict';

// The editor's WOPI discovery, fetched from the address the admin configured
// and cached: which extensions it opens and at which page. A fetch has a
// timeout and a size cap and never follows a redirect (the admin enters the
// final address); a failure becomes a short, friendly reason.

const { parseDiscovery } = require('../../utils/wopi_discovery');
const { readOfficeSettings } = require('./settings');

const FETCH_TIMEOUT_MS = 8000;
const MAX_DISCOVERY_BYTES = 1024 * 1024;
const REFRESH_MS = 60 * 60 * 1000;
const RETRY_MS = 60 * 1000;
// The formats offered for editing. Text and CSV stay with the built-in text editor.
const EDITABLE = ['docx', 'xlsx', 'pptx', 'odt', 'ods', 'odp'];

// { url, actions, error, checkedAt }; one entry for the configured address.
let cache = null;

function failure(reason) {
  return Object.assign(new Error(reason), { discovery: true });
}

async function readLimited(response) {
  const chunks = [];
  let total = 0;
  for await (const chunk of response.body) {
    total += chunk.length;
    if (total > MAX_DISCOVERY_BYTES) throw failure('The editor’s answer is far larger than a discovery document.');
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

async function fetchActions(baseUrl, fetchImpl = fetch) {
  let response;
  try {
    response = await fetchImpl(`${baseUrl}/hosting/discovery`, {
      redirect: 'manual',
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
      headers: { Accept: 'text/xml, application/xml' },
    });
  } catch (err) {
    const timedOut = err?.name === 'TimeoutError' || err?.name === 'AbortError';
    throw failure(timedOut
      ? 'The editor did not answer in time. Is it running and reachable from this server?'
      : 'This server could not connect to the editor. Check the address and that the editor is running.');
  }
  if (response.status >= 300 && response.status < 400) {
    throw failure('The address redirects somewhere else. Enter the final address (for example the https one).');
  }
  if (!response.ok) throw failure(`The editor answered with status ${response.status}. Check the address.`);
  let actions;
  try {
    actions = parseDiscovery(await readLimited(response));
  } catch (err) {
    throw err.discovery ? err : failure('That address is not a Collabora Online or ONLYOFFICE editor (no WOPI discovery).');
  }
  if (!EDITABLE.some((extension) => actions[extension])) {
    throw failure('The editor does not offer any office formats.');
  }
  return actions;
}

/** Fetches (no cache) and remembers the answer for `url`. Throws a friendly error. */
async function checkEditor(url, fetchImpl) {
  try {
    const actions = await fetchActions(url, fetchImpl);
    cache = { url, actions, error: null, checkedAt: Date.now() };
    return actions;
  } catch (err) {
    cache = { url, actions: null, error: err.message, checkedAt: Date.now() };
    throw err;
  }
}

function forgetDiscovery() {
  cache = null;
}

function cachedFor(url) {
  return cache && cache.url === url ? cache : null;
}

/** The cached actions, refreshed when stale; null when the editor is off or unreachable. */
async function currentActions() {
  const settings = readOfficeSettings();
  if (!settings.enabled) return null;
  const known = cachedFor(settings.url);
  const age = known ? Date.now() - known.checkedAt : Infinity;
  if (known?.actions && age < REFRESH_MS) return known.actions;
  if (known && !known.actions && age < RETRY_MS) return null;
  try {
    return await checkEditor(settings.url);
  } catch {
    return known?.actions || null;
  }
}

/** What the admin sees: whether the editor works and why not. */
function editorStatus() {
  const settings = readOfficeSettings();
  if (!settings.url) return { state: 'unconfigured' };
  const known = cachedFor(settings.url);
  if (!known) return { state: settings.enabled ? 'unchecked' : 'off' };
  if (known.error) return { state: 'error', message: known.error, checkedAt: new Date(known.checkedAt).toISOString() };
  return {
    state: settings.enabled ? 'ok' : 'off',
    checkedAt: new Date(known.checkedAt).toISOString(),
    extensions: EDITABLE.filter((extension) => known.actions[extension]),
  };
}

/** Origins pages may load the editor from: the configured address and where its actions point. */
function editorOrigins() {
  const settings = readOfficeSettings();
  if (!settings.enabled) return [];
  const origins = new Set([new URL(settings.url).origin]);
  const known = cachedFor(settings.url);
  for (const action of Object.values(known?.actions || {})) {
    for (const urlsrc of Object.values(action)) origins.add(new URL(urlsrc.replace(/<[^>]*>/g, '')).origin);
  }
  return [...origins];
}

module.exports = { EDITABLE, checkEditor, currentActions, editorOrigins, editorStatus, forgetDiscovery };
