'use strict';

// Reading another server's PROPFIND answer (207 multistatus), for the
// importer and the WebDAV backup target. The body is read with a byte cap
// and parsed by the same linear parser as WebDAV request bodies, so a hostile
// or broken server can neither fill memory nor stall the event loop.

const { DAV, child, isElement, parseXml } = require('./xml');

const DEFAULT_MAX_BYTES = 16 * 1024 * 1024;

function multistatusError(message, code) {
  const error = new Error(message);
  error.code = code;
  return error;
}

/** A fetch Response body as text; stops reading past `limit` bytes. */
async function readCappedText(response, limit = DEFAULT_MAX_BYTES) {
  const tooLarge = () => multistatusError(`The server's answer is larger than ${limit} bytes.`, 'MULTISTATUS_TOO_LARGE');
  if (Number(response.headers.get('content-length')) > limit) {
    await response.body?.cancel().catch(() => {});
    throw tooLarge();
  }
  if (!response.body) return '';
  const reader = response.body.getReader();
  const chunks = [];
  let size = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > limit) {
      await reader.cancel().catch(() => {});
      throw tooLarge();
    }
    chunks.push(Buffer.from(value.buffer, value.byteOffset, value.byteLength));
  }
  return Buffer.concat(chunks).toString('utf8');
}

// The props of every propstat the server answered 2xx for (or without a
// status at all, which some minimal servers do).
function okProps(response) {
  const props = [];
  for (const propstat of response.children.filter((item) => isElement(item, DAV, 'propstat'))) {
    const status = child(propstat, DAV, 'status')?.text.trim();
    if (status && !/ 2\d\d(?:\s|$)/.test(status)) continue;
    props.push(...(child(propstat, DAV, 'prop')?.children || []));
  }
  return props;
}

/**
 * Parses a multistatus document into [{ href, props }]: `href` as the server
 * wrote it, `props` the successfully answered property elements
 * ({ ns, name, children, text }). Throws code MULTISTATUS_INVALID for
 * anything that is not one.
 */
function parseMultistatus(xml) {
  let root;
  try {
    root = parseXml(xml);
  } catch {
    throw multistatusError('The server did not answer with valid WebDAV XML.', 'MULTISTATUS_INVALID');
  }
  if (!isElement(root, DAV, 'multistatus')) {
    throw multistatusError('The server did not answer with a WebDAV multistatus.', 'MULTISTATUS_INVALID');
  }
  return root.children
    .filter((item) => isElement(item, DAV, 'response'))
    .map((response) => ({ href: child(response, DAV, 'href')?.text.trim() || null, props: okProps(response) }));
}

/** A property's text by local name, whatever its namespace; null when absent. */
function propText(props, name) {
  const prop = props.find((item) => item.name === name);
  return prop ? prop.text.trim() : null;
}

function isCollection(props) {
  const type = props.find((item) => isElement(item, DAV, 'resourcetype'));
  return Boolean(type?.children.some((item) => isElement(item, DAV, 'collection')));
}

/** Reads and parses a 207 answer. */
async function readMultistatus(response, { limit = DEFAULT_MAX_BYTES } = {}) {
  return parseMultistatus(await readCappedText(response, limit));
}

module.exports = { isCollection, parseMultistatus, propText, readMultistatus };
