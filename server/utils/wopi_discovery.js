'use strict';

// Reads a WOPI discovery document (what Collabora Online and ONLYOFFICE answer
// at /hosting/discovery): which editor page opens which file extension.
// Pure: text in, plain data out.

const ACTION_NAMES = new Set(['edit', 'view']);
const EXTENSION = /^[a-z0-9]{1,10}$/;

function decodeXml(text) {
  return text
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&quot;', '"')
    .replaceAll('&apos;', "'")
    .replaceAll('&amp;', '&');
}

function attributes(tag) {
  const found = {};
  for (const match of tag.matchAll(/([A-Za-z][\w:-]*)\s*=\s*"([^"]*)"/g)) found[match[1]] = decodeXml(match[2]);
  return found;
}

function isHttpUrl(value) {
  try {
    return ['http:', 'https:'].includes(new URL(value.replace(/<[^>]*>/g, '')).protocol);
  } catch {
    return false;
  }
}

/**
 * { docx: { edit: 'https://…/cool.html?', view: '…' }, … }. The first action
 * listed for an extension wins. Throws when the text is not a discovery
 * document.
 */
function parseDiscovery(xml) {
  const text = String(xml || '');
  if (!/<wopi-discovery[\s>]/.test(text)) throw new Error('This is not a WOPI discovery document.');
  const actions = Object.create(null);
  for (const [tag] of text.matchAll(/<action\b[^>]*>/g)) {
    const { name, ext, urlsrc } = attributes(tag);
    const extension = String(ext || '').toLowerCase();
    const action = String(name || '').toLowerCase();
    if (!ACTION_NAMES.has(action) || !EXTENSION.test(extension) || !urlsrc || !isHttpUrl(urlsrc)) continue;
    actions[extension] ||= {};
    actions[extension][action] ||= urlsrc;
  }
  return actions;
}

/** The editor page URL with `WOPISrc` added; urlsrc may carry placeholders like <ui=UI_LLCC&>. */
function editorPageUrl(urlsrc, wopiSrc) {
  const base = urlsrc.replace(/<[^>]*>/g, '');
  const joiner = base.includes('?') ? (/[?&]$/.test(base) ? '' : '&') : '?';
  return `${base}${joiner}WOPISrc=${encodeURIComponent(wopiSrc)}`;
}

module.exports = { editorPageUrl, parseDiscovery };
