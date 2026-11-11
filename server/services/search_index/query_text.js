'use strict';

// Turning what a user typed into an FTS5 query, and finding the matches again
// in the decrypted text for snippets. Pure: no database, no I/O.
//
// Words and phrases are split into tokens the way FTS5's unicode61 tokenizer
// with remove_diacritics 2 does (letters, digits and private-use characters;
// case and accents folded). Every token goes into the FTS query as a quoted
// string, so nothing the user types is ever read as FTS syntax.
//
// The index is detail=column, which can't answer phrase queries; a phrase
// (quoted, or a word like "e-mail" that splits into several tokens) is
// searched as all of its tokens and then checked in the text (`matchesAll`).

const MAX_TERMS = 8;
const MAX_TOKENS = 16;
const SNIPPET_BEFORE = 60;
const SNIPPET_LENGTH = 220;
const MARK_START = '\u0002';
const MARK_END = '\u0003';

const TOKEN_PATTERN = /[\p{L}\p{N}\p{Co}\p{M}]+/gu;

function fold(text) {
  return text.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
}

function tokensOf(text) {
  return fold(text).match(TOKEN_PATTERN) || [];
}

/**
 * The terms of a query: `"quoted text"` is a phrase of exact tokens; any
 * other word is a prefix (its last token may continue). An unclosed quote
 * runs to the end. Returns [{ tokens, prefix }].
 */
function parseQuery(query) {
  const terms = [];
  const text = String(query ?? '').slice(0, 200);
  const pattern = /"([^"]*)"?|([^\s"]+)/g;
  let found;
  while ((found = pattern.exec(text)) !== null && terms.length < MAX_TERMS) {
    const phrase = found[1] !== undefined;
    const tokens = tokensOf(phrase ? found[1] : found[2]);
    if (tokens.length > 0) terms.push({ tokens, prefix: !phrase });
  }
  let budget = MAX_TOKENS;
  return terms
    .map((term) => {
      const tokens = term.tokens.slice(0, Math.max(budget, 0));
      budget -= tokens.length;
      return { ...term, tokens };
    })
    .filter((term) => term.tokens.length > 0);
}

/** The FTS5 MATCH expression for parsed terms; null when there is nothing to match. */
function matchExpression(terms) {
  const parts = [];
  for (const term of terms) {
    term.tokens.forEach((token, index) => {
      const quoted = `"${token.replace(/"/g, '""')}"`;
      parts.push(term.prefix && index === term.tokens.length - 1 ? `${quoted}*` : quoted);
    });
  }
  return parts.length > 0 ? parts.join(' AND ') : null;
}

function needsVerification(terms) {
  return terms.some((term) => term.tokens.length > 1);
}

// The text's tokens with their positions in the original string.
function* scanTokens(text) {
  TOKEN_PATTERN.lastIndex = 0;
  let found;
  while ((found = TOKEN_PATTERN.exec(text)) !== null) {
    const folded = fold(found[0]);
    if (folded) yield { start: found.index, end: found.index + found[0].length, folded };
  }
}

function tokenMatches(term, index, folded) {
  const wanted = term.tokens[index];
  const last = index === term.tokens.length - 1;
  return term.prefix && last ? folded.startsWith(wanted) : folded === wanted;
}

/**
 * Every place a term occurs, in text order: [{ start, end, term }]. With
 * `firstOnly`, stops at the first complete match.
 */
function findMatches(text, terms, { firstOnly = false } = {}) {
  const matches = [];
  // Partial phrase matches still in progress: { term, next, start }.
  let open = [];
  for (const token of scanTokens(text)) {
    if (firstOnly && matches.length > 0) break;
    const still = [];
    for (const partial of open) {
      if (!tokenMatches(partial.term, partial.next, token.folded)) continue;
      if (partial.next === partial.term.tokens.length - 1) {
        matches.push({ start: partial.start, end: token.end, term: partial.term });
      } else {
        still.push({ ...partial, next: partial.next + 1 });
      }
    }
    for (const term of terms) {
      if (!tokenMatches(term, 0, token.folded)) continue;
      if (term.tokens.length === 1) {
        matches.push({ start: token.start, end: token.end, term });
      } else {
        still.push({ term, next: 1, start: token.start });
      }
    }
    open = still;
  }
  return matches.sort((a, b) => a.start - b.start);
}

/** Whether every term (phrases included) occurs in the text. */
function matchesAll(text, terms) {
  const seen = new Set(findMatches(text, terms).map((match) => match.term));
  return terms.every((term) => seen.has(term));
}

function clean(text) {
  return text.replace(/[\u0000-\u0008\u000b-\u001f\u007f]/g, ' ').replace(/\s+/g, ' ');
}

/**
 * A short excerpt around the first match, matches wrapped in \u0002 … \u0003,
 * with "…" where the text was cut. The start of the text when nothing matches.
 */
function makeSnippet(text, terms) {
  const source = String(text || '');
  const [first] = findMatches(source, terms, { firstOnly: true });
  let start = first ? Math.max(first.start - SNIPPET_BEFORE, 0) : 0;
  // Begin at a word boundary.
  if (start > 0) {
    const space = source.lastIndexOf(' ', start);
    start = space > first.start - SNIPPET_BEFORE * 2 && space >= 0 ? space + 1 : start;
  }
  let end = Math.min(start + SNIPPET_LENGTH, source.length);
  if (end < source.length) {
    const space = source.indexOf(' ', end);
    if (space !== -1 && space - end < 30) end = space;
  }
  const window = source.slice(start, end);
  const inWindow = findMatches(window, terms);
  let out = '';
  let cursor = 0;
  for (const match of inWindow) {
    if (match.start < cursor) continue;
    out += clean(window.slice(cursor, match.start)) + MARK_START + clean(window.slice(match.start, match.end)) + MARK_END;
    cursor = match.end;
  }
  out += clean(window.slice(cursor));
  out = out.trim();
  return `${start > 0 ? '… ' : ''}${out}${end < source.length ? ' …' : ''}`;
}

module.exports = {
  MARK_END,
  MARK_START,
  makeSnippet,
  matchExpression,
  matchesAll,
  needsVerification,
  parseQuery,
};
