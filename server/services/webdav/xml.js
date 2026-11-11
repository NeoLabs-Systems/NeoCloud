'use strict';

// The little XML WebDAV needs: a namespace-aware parser for request bodies
// (PROPFIND, PROPPATCH, LOCK) and helpers to write multistatus responses.
// Bodies are small and simple, so this reads elements and text only;
// DOCTYPE and entity declarations are refused outright (no XXE, no billion
// laughs). Every step is a single forward scan (no backtracking regex over
// input), so the cost stays linear in the body size; the importer and the
// WebDAV backup target read other servers' multistatus answers with it too.

const { httpError } = require('../../utils/http_error');

const DAV = 'DAV:';
const OC = 'http://owncloud.org/ns';
const NC = 'http://nextcloud.org/ns';
const SABRE = 'http://sabredav.org/ns';
const MAX_DEPTH = 64;
const MAX_TAG_LENGTH = 8 * 1024;

const NAME = '[A-Za-z_\\u00C0-\\uFFFF][\\w.\\-\\u00B7-\\uFFFF]*';
const QNAME_PATTERN = new RegExp(`^(?:(${NAME}):)?(${NAME})$`);
const ENTITIES = { lt: '<', gt: '>', amp: '&', quot: '"', apos: "'" };

function badXml(message = 'The request body is not valid XML.') {
  return httpError(400, message, 'XML_INVALID');
}

function decodeEntities(text) {
  return text.replace(/&(#x[0-9a-fA-F]+|#\d+|[a-z]+);/g, (match, entity) => {
    if (entity[0] !== '#') {
      if (!Object.hasOwn(ENTITIES, entity)) throw badXml();
      return ENTITIES[entity];
    }
    const code = entity[1] === 'x' ? Number.parseInt(entity.slice(2), 16) : Number.parseInt(entity.slice(1), 10);
    if (!Number.isInteger(code) || code > 0x10ffff) throw badXml();
    return String.fromCodePoint(code);
  });
}

function resolveName(qname, scopes) {
  const match = QNAME_PATTERN.exec(qname);
  if (!match) throw badXml();
  const [, prefix = '', local] = match;
  for (let index = scopes.length - 1; index >= 0; index -= 1) {
    if (Object.hasOwn(scopes[index], prefix)) return { ns: scopes[index][prefix], name: local };
  }
  if (prefix) throw badXml(`Undeclared namespace prefix "${prefix}".`);
  return { ns: '', name: local };
}

// The index of the '>' that ends the tag starting at `start`. Tags longer
// than MAX_TAG_LENGTH are refused, which also bounds the attribute scan.
function readTag(text, start) {
  let quote = null;
  const limit = Math.min(text.length, start + MAX_TAG_LENGTH);
  for (let index = start; index < limit; index += 1) {
    const char = text[index];
    if (quote) {
      if (char === quote) quote = null;
    } else if (char === '"' || char === "'") {
      quote = char;
    } else if (char === '>') {
      return index;
    }
  }
  throw badXml();
}

function isSpace(char) {
  return char === ' ' || char === '\t' || char === '\n' || char === '\r';
}

/** `name="value"` pairs of a start tag (the part after its name), in one pass. */
function readAttributes(text) {
  const attributes = [];
  let index = 0;
  for (;;) {
    while (index < text.length && isSpace(text[index])) index += 1;
    if (index >= text.length) return attributes;
    const nameStart = index;
    while (index < text.length && !isSpace(text[index]) && text[index] !== '=') index += 1;
    const name = text.slice(nameStart, index);
    while (index < text.length && isSpace(text[index])) index += 1;
    if (!name || text[index] !== '=') throw badXml();
    index += 1;
    while (index < text.length && isSpace(text[index])) index += 1;
    const quote = text[index];
    if (quote !== '"' && quote !== "'") throw badXml();
    const end = text.indexOf(quote, index + 1);
    if (end === -1) throw badXml();
    attributes.push([name, text.slice(index + 1, end)]);
    index = end + 1;
    if (index < text.length && !isSpace(text[index])) throw badXml();
  }
}

/**
 * Parses a document into { ns, name, children, text } elements (text is the
 * concatenated character data directly inside an element). Returns the root.
 */
function parseXml(input) {
  const text = String(input).replace(/^﻿/, '');
  const stack = [];
  const scopes = [];
  let root = null;
  let index = 0;
  while (index < text.length) {
    const open = text.indexOf('<', index);
    const chunk = open === -1 ? text.slice(index) : text.slice(index, open);
    if (chunk.trim()) {
      if (stack.length === 0) throw badXml();
      stack[stack.length - 1].text += decodeEntities(chunk);
    }
    if (open === -1) break;
    if (text.startsWith('<?', open)) {
      const end = text.indexOf('?>', open);
      if (end === -1) throw badXml();
      index = end + 2;
    } else if (text.startsWith('<!--', open)) {
      const end = text.indexOf('-->', open);
      if (end === -1) throw badXml();
      index = end + 3;
    } else if (text.startsWith('<![CDATA[', open)) {
      const end = text.indexOf(']]>', open);
      if (end === -1 || stack.length === 0) throw badXml();
      stack[stack.length - 1].text += text.slice(open + 9, end);
      index = end + 3;
    } else if (text.startsWith('<!', open)) {
      throw badXml('DOCTYPE and entity declarations are not accepted.');
    } else if (text.startsWith('</', open)) {
      const end = readTag(text, open);
      const qname = text.slice(open + 2, end).trim();
      const element = stack.pop();
      const closing = resolveName(qname, scopes);
      scopes.pop();
      if (!element || element.ns !== closing.ns || element.name !== closing.name) throw badXml();
      index = end + 1;
    } else {
      const end = readTag(text, open);
      let body = text.slice(open + 1, end);
      const selfClosing = body.endsWith('/');
      if (selfClosing) body = body.slice(0, -1);
      const qname = body.match(/^[^\s]+/)?.[0] || '';
      const scope = {};
      for (const [key, raw] of readAttributes(body.slice(qname.length))) {
        if (key === 'xmlns') scope[''] = decodeEntities(raw);
        else if (key.startsWith('xmlns:')) scope[key.slice(6)] = decodeEntities(raw);
      }
      scopes.push(scope);
      const element = { ...resolveName(qname, scopes), children: [], text: '' };
      if (stack.length > 0) stack[stack.length - 1].children.push(element);
      else if (root) throw badXml();
      else root = element;
      if (stack.length >= MAX_DEPTH) throw badXml();
      if (selfClosing) scopes.pop();
      else stack.push(element);
      index = end + 1;
    }
  }
  if (!root || stack.length > 0) throw badXml();
  return root;
}

function isElement(element, ns, name) {
  return Boolean(element) && element.ns === ns && element.name === name;
}

function child(element, ns, name) {
  return element?.children.find((item) => isElement(item, ns, name)) || null;
}

function escapeXml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

const PREFIXES = { [DAV]: 'd', [OC]: 'oc', [NC]: 'nc', [SABRE]: 's' };
const ROOT_NAMESPACES = Object.entries(PREFIXES)
  .map(([ns, prefix]) => `xmlns:${prefix}="${ns}"`)
  .join(' ');

/**
 * One element as text. Namespaces outside the four declared on the root get
 * their own declaration, so any property a client asked for can be echoed.
 */
function element(ns, name, content = null) {
  const prefix = PREFIXES[ns];
  const tag = prefix ? `${prefix}:${name}` : `x:${name}`;
  const declaration = prefix ? '' : ` xmlns:x="${escapeXml(ns)}"`;
  if (content === null || content === '') return `<${tag}${declaration}/>`;
  return `<${tag}${declaration}>${content}</${tag}>`;
}

/** A complete document whose root element declares the common namespaces. */
function document(ns, name, content) {
  const prefix = PREFIXES[ns];
  return `<?xml version="1.0" encoding="utf-8"?>\n<${prefix}:${name} ${ROOT_NAMESPACES}>${content}</${prefix}:${name}>\n`;
}

module.exports = {
  DAV,
  NC,
  OC,
  SABRE,
  child,
  document,
  element,
  escapeXml,
  isElement,
  parseXml,
};
