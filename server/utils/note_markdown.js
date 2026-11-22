'use strict';

// A note is a Markdown file with optional YAML frontmatter (docs/notes.md).
// This reads and writes the small YAML subset notes use (scalars and lists of
// scalars) and keeps any other key it finds as it was, so a note written in
// another app (Obsidian) keeps its own properties.

const COLORS = Object.freeze(['default', 'red', 'orange', 'amber', 'green', 'teal', 'blue', 'purple', 'gray']);
const MAX_TAGS = 20;
const MAX_TAG_LENGTH = 40;
const SNIPPET_CHARS = 280;

const FRONTMATTER = /^---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)/;

function unquote(text) {
  const value = text.trim();
  if (value.length >= 2 && ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))) {
    return value.slice(1, -1);
  }
  return value;
}

function scalar(text) {
  const value = unquote(text);
  if (text.trim() === 'true') return true;
  if (text.trim() === 'false') return false;
  return value;
}

// "[a, b]" → ['a', 'b']
function inlineList(text) {
  const inner = text.trim().slice(1, -1).trim();
  return inner ? inner.split(',').map((part) => unquote(part)).filter(Boolean) : [];
}

/**
 * Splits a note into `{ properties, extra, body }`: the keys notes know
 * (tags, pinned, archived, color) as values, every other frontmatter line
 * kept verbatim in `extra`, and the Markdown body.
 */
function parseNote(text) {
  const source = String(text ?? '').replace(/^﻿/, '');
  const match = FRONTMATTER.exec(source);
  if (!match) return { properties: normalizeProperties({}), extra: [], body: source };
  const properties = {};
  const extra = [];
  const lines = match[1].split(/\r?\n/);
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const key = /^([A-Za-z_][\w-]*):(.*)$/.exec(line);
    if (!key) {
      extra.push(line);
      continue;
    }
    const [, name, rest] = key;
    // A key's block: its own line and the indented or "- " lines under it.
    const block = [line];
    while (index + 1 < lines.length && /^(\s+|-\s)/.test(lines[index + 1])) {
      index += 1;
      block.push(lines[index]);
    }
    if (!['tags', 'pinned', 'archived', 'color'].includes(name)) {
      extra.push(...block);
      continue;
    }
    if (rest.trim().startsWith('[')) properties[name] = inlineList(rest);
    else if (block.length > 1) properties[name] = block.slice(1).map((item) => unquote(item.replace(/^\s*-\s?/, ''))).filter(Boolean);
    else properties[name] = scalar(rest);
  }
  return { properties: normalizeProperties(properties), extra, body: source.slice(match[0].length) };
}

function cleanTags(value) {
  const list = Array.isArray(value) ? value : (typeof value === 'string' && value ? [value] : []);
  const seen = new Set();
  const tags = [];
  for (const raw of list) {
    const tag = String(raw).trim().replace(/^#/, '').replace(/[\r\n]/g, ' ').slice(0, MAX_TAG_LENGTH).trim();
    if (!tag || seen.has(tag.toLowerCase())) continue;
    seen.add(tag.toLowerCase());
    tags.push(tag);
    if (tags.length >= MAX_TAGS) break;
  }
  return tags;
}

function normalizeProperties(input) {
  return {
    tags: cleanTags(input.tags),
    pinned: input.pinned === true,
    archived: input.archived === true,
    color: COLORS.includes(input.color) ? input.color : 'default',
  };
}

function quoteIfNeeded(text) {
  return /^[\w][\w .\-/&+]*$/u.test(text) && !['true', 'false', 'null'].includes(text) ? text : JSON.stringify(text);
}

/** The file text for `properties`, kept `extra` lines and `body`; no frontmatter when there is nothing to say. */
function serializeNote({ properties, extra = [], body }) {
  const props = normalizeProperties(properties || {});
  const lines = [];
  if (props.tags.length > 0) {
    lines.push('tags:');
    for (const tag of props.tags) lines.push(`  - ${quoteIfNeeded(tag)}`);
  }
  if (props.pinned) lines.push('pinned: true');
  if (props.archived) lines.push('archived: true');
  if (props.color !== 'default') lines.push(`color: ${props.color}`);
  const kept = extra.filter((line) => line.trim().length > 0);
  lines.push(...kept);
  const text = String(body ?? '');
  if (lines.length === 0) return text;
  return `---\n${lines.join('\n')}\n---\n${text}`;
}

/** What a card shows: a plain snippet and the checklist's progress. */
function summarizeBody(body) {
  let total = 0;
  let done = 0;
  const plain = [];
  for (const line of String(body ?? '').split(/\r?\n/)) {
    const task = /^\s*[-*+]\s+\[([ xX])\]\s?(.*)$/.exec(line);
    if (task) {
      total += 1;
      if (task[1] !== ' ') done += 1;
    }
    const text = (task ? task[2] : line)
      .replace(/^\s*(#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+)/, '')
      .replace(/(\*\*|__|\*|_|~~|`)/g, '')
      .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
      .trim();
    if (text && !/^(-{3,}|\*{3,}|```.*)$/.test(line.trim())) plain.push(text);
  }
  const snippet = plain.join(' · ');
  return {
    snippet: snippet.length > SNIPPET_CHARS ? `${snippet.slice(0, SNIPPET_CHARS - 1)}…` : snippet,
    checklist: { done, total },
  };
}

module.exports = { COLORS, cleanTags, normalizeProperties, parseNote, serializeNote, summarizeBody };
