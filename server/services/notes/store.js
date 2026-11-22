'use strict';

// Notes are Markdown files under the account's /Notes folder; a subfolder is
// a notebook (docs/notes.md). Built on the agent file services, so every
// change keeps the change journal, versions, quota and trash, and nothing
// inside a vault is reached.

const db = require('../../db/database');
const tree = require('../files/tree');
const agentFiles = require('../agent/files');
const { httpError } = require('../../utils/http_error');
const { numberedName } = require('../../utils/file_names');
const { eachLimit } = require('../../utils/each_limit');
const { cleanTags, normalizeProperties, serializeNote } = require('../../utils/note_markdown');
const cache = require('./cache');

const NOTES_FOLDER = 'Notes';
const MAX_NOTE_BYTES = 1024 * 1024;
const MAX_TITLE_CHARS = 120;
const READ_LANES = 8;

const notFound = () => httpError(404, 'Note not found', 'NOT_FOUND');

/** The live /Notes folder, or null before the first note. */
function notesRoot(userId) {
  const folder = tree.findLiveChildByName(tree.getRoot(userId).id, NOTES_FOLDER);
  if (!folder || folder.type !== 'folder' || folder.vault_id) return null;
  return folder;
}

const isNoteFile = (row) => row.type === 'file' && /\.md$/i.test(row.name) && !row.vault_id && row.size <= MAX_NOTE_BYTES;

function notebookOf(relativePath) {
  const index = relativePath.lastIndexOf('/');
  return index === -1 ? '' : relativePath.slice(0, index);
}

/** Every live note and notebook under /Notes, outside vaults, and the vault folders there. */
function scan(userId) {
  const root = notesRoot(userId);
  if (!root) return { root: null, files: [], notebooks: [], encrypted: [] };
  const rows = tree.liveSubtree(userId, root.id).filter((row) => row.depth > 0);
  // A vault folder hides everything under it: only the app holding its key reads it.
  const hidden = rows.filter((row) => row.vault_id).map((row) => `${row.relative_path}/`);
  const visible = rows.filter((row) => !row.vault_id && !hidden.some((prefix) => row.relative_path.startsWith(prefix)));
  const vaultRoots = new Map(db.prepare('SELECT id, folder_id FROM vaults WHERE user_id = ?').all(userId).map((row) => [row.folder_id, row.id]));
  return {
    root,
    files: visible.filter(isNoteFile),
    notebooks: visible.filter((row) => row.type === 'folder').map((row) => row.relative_path),
    // Encrypted notebooks: vault folders under /Notes (their own name is plaintext).
    encrypted: rows.filter((row) => vaultRoots.has(row.id)).map((row) => ({ vaultId: vaultRoots.get(row.id), folderId: row.id, name: row.relative_path })),
  };
}

/** A note of the account by id: a live .md file under /Notes, else 404. */
function noteNode(userId, id) {
  const { files } = scan(userId);
  const row = files.find((file) => file.id === String(id || ''));
  if (!row) throw notFound();
  return { node: tree.nodeById(row.id), notebook: notebookOf(row.relative_path) };
}

function textQuery(value) {
  return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

/**
 * The account's notes as cards, pinned first, then the newest. Filters:
 * `q` (title, text, tags), `tag`, `notebook` ('' for none), `archived`.
 */
async function listNotes(userId, { q, tag, notebook, archived } = {}) {
  const { root, files, notebooks, encrypted } = scan(userId);
  const cards = [];
  await eachLimit(files, READ_LANES, async (row) => {
    cards.push(await cache.cardFor(userId, row, notebookOf(row.relative_path)));
  });
  const wantArchived = archived === true || archived === 'true';
  const tagCounts = new Map();
  for (const card of cards) {
    if (card.archived) continue;
    for (const name of card.tags) tagCounts.set(name, (tagCounts.get(name) || 0) + 1);
  }
  const query = textQuery(q);
  const wantTag = textQuery(tag);
  const notes = cards.filter((card) => {
    if (card.archived !== wantArchived) return false;
    if (wantTag && !card.tags.some((name) => name.toLowerCase() === wantTag)) return false;
    if (typeof notebook === 'string' && card.notebook !== notebook) return false;
    if (!query) return true;
    return [card.title, card.snippet, ...card.tags].some((text) => text.toLowerCase().includes(query));
  });
  notes.sort((a, b) => Number(b.pinned) - Number(a.pinned) || String(b.modifiedAt).localeCompare(String(a.modifiedAt)));
  return {
    rootId: root ? root.id : null,
    notes,
    notebooks: notebooks.sort((a, b) => a.localeCompare(b)),
    encryptedNotebooks: encrypted.sort((a, b) => a.name.localeCompare(b.name)),
    tags: [...tagCounts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([name, count]) => ({ name, count })),
  };
}

/** One note with its Markdown body. */
async function getNote(userId, id) {
  const { node, notebook } = noteNode(userId, id);
  const parsed = await cache.readNote(userId, node);
  const card = cache.cardOf(node, notebook, parsed);
  cache.remember(userId, node, card);
  return { ...card, body: parsed.body };
}

/** A file name for a title: no path separators or control characters, not too long. */
function fileTitle(raw) {
  const title = String(raw ?? '')
    .normalize('NFC')
    .replace(/[\u0000-\u001f\u007f/\\]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/^\.+/, '')
    .slice(0, MAX_TITLE_CHARS)
    .trim();
  return title;
}

// "Untitled" when nothing else says what it is: the first line of the text.
function defaultTitle(body) {
  const first = String(body ?? '').split(/\r?\n/).map((line) => line.replace(/^[#>\-*+\s]+|\[[ xX]\]\s*/g, '').trim()).find(Boolean);
  return fileTitle(first ? first.slice(0, 60) : '') || `Note ${new Date().toISOString().slice(0, 16).replace('T', ' ').replace(':', '.')}`;
}

function notebookParts(value) {
  if (value === undefined || value === null || value === '') return [];
  if (typeof value !== 'string') throw httpError(400, 'notebook must be text.', 'INVALID_REQUEST');
  const parts = value.split('/').map((part) => fileTitle(part)).filter(Boolean);
  if (parts.some((part) => part === '.' || part === '..')) throw httpError(400, 'That notebook name is not allowed.', 'INVALID_NOTEBOOK');
  return parts;
}

const notePath = (parts, name) => `/${[NOTES_FOLDER, ...parts, name].join('/')}`;

// "Title.md", or "Title (2).md" when that is taken in the folder.
function freeName(userId, parts, title, exceptId = null) {
  const base = `${title}.md`;
  let folder = tree.findLiveChildByName(tree.getRoot(userId).id, NOTES_FOLDER);
  for (const part of parts) folder = folder ? tree.findLiveChildByName(folder.id, part) : null;
  if (!folder) return base;
  let name = base;
  for (let number = 2; number < 1000; number += 1) {
    const clash = tree.findLiveChildByName(folder.id, name);
    if (!clash || clash.id === exceptId) return name;
    name = numberedName(base, number);
  }
  throw httpError(409, 'Too many notes with this title.', 'ALREADY_EXISTS');
}

function bodyText(value) {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw httpError(400, 'body must be text.', 'INVALID_REQUEST');
  return value;
}

function propertiesFrom(input, base = normalizeProperties({})) {
  const next = { ...base };
  if (input.tags !== undefined) {
    if (!Array.isArray(input.tags)) throw httpError(400, 'tags must be a list.', 'INVALID_REQUEST');
    next.tags = cleanTags(input.tags);
  }
  for (const key of ['pinned', 'archived']) {
    if (input[key] === undefined) continue;
    if (typeof input[key] !== 'boolean') throw httpError(400, `${key} must be true or false.`, 'INVALID_REQUEST');
    next[key] = input[key];
  }
  if (input.color !== undefined) next.color = input.color;
  return normalizeProperties(next);
}

function assertFits(text) {
  if (Buffer.byteLength(text, 'utf8') > MAX_NOTE_BYTES) throw httpError(413, 'This note is too long (1 MB at most).', 'TOO_LARGE');
}

/** A new note: `{ title?, body?, notebook?, tags?, pinned?, color? }`. */
async function createNote(userId, input = {}) {
  const body = bodyText(input.body);
  const parts = notebookParts(input.notebook);
  const title = fileTitle(input.title) || defaultTitle(body);
  const text = serializeNote({ properties: propertiesFrom(input), body });
  assertFits(text);
  const item = await agentFiles.writeFile(userId, { path: notePath(parts, freeName(userId, parts, title)), bytes: Buffer.from(text, 'utf8') });
  return getNote(userId, item.id);
}

/**
 * Changes a note: `body`, the properties, `title` (renames the file) and
 * `notebook` (moves it). `baseRevision` refuses the change (409 CONFLICT)
 * when the note changed since it was read.
 */
async function updateNote(userId, id, input = {}) {
  const { node, notebook } = noteNode(userId, id);
  const touchesText = input.body !== undefined || ['tags', 'pinned', 'archived', 'color'].some((key) => input[key] !== undefined);
  if (touchesText) {
    const current = await cache.readNote(userId, node);
    const text = serializeNote({
      properties: propertiesFrom(input, current.properties),
      extra: current.extra,
      body: input.body === undefined ? current.body : bodyText(input.body),
    });
    assertFits(text);
    await agentFiles.updateContent(userId, node.id, { content: text, baseRevision: input.baseRevision ?? undefined });
  } else if (input.baseRevision && input.baseRevision !== node.etag) {
    throw httpError(409, 'The file changed since that revision.', 'CONFLICT');
  }
  const parts = input.notebook === undefined ? notebookParts(notebook) : notebookParts(input.notebook);
  const title = input.title === undefined ? cache.titleOf(node.name) : (fileTitle(input.title) || cache.titleOf(node.name));
  const moved = input.notebook !== undefined && parts.join('/') !== notebook;
  if (moved || title !== cache.titleOf(node.name)) {
    if (moved) agentFiles.createFolder(userId, { path: `/${[NOTES_FOLDER, ...parts].join('/')}` });
    await agentFiles.moveOrRename(userId, node.id, {
      name: freeName(userId, parts, title, moved ? null : node.id),
      ...(moved ? { parentPath: `/${[NOTES_FOLDER, ...parts].join('/')}` } : {}),
    });
  }
  return getNote(userId, node.id);
}

/** Moves a note to the trash (restorable there). */
function deleteNote(userId, id) {
  const { node } = noteNode(userId, id);
  return agentFiles.trashItem(userId, node.id);
}

/** The /Notes folder, made when missing: where the app creates an encrypted notebook (a vault). */
function ensureRoot(userId) {
  const { item } = agentFiles.createFolder(userId, { path: `/${NOTES_FOLDER}` });
  return { id: item.id };
}

/** A notebook (a folder under /Notes); idempotent. */
function createNotebook(userId, input = {}) {
  const parts = notebookParts(input.name);
  if (parts.length === 0) throw httpError(400, 'Give the notebook a name.', 'INVALID_NOTEBOOK');
  const { created } = agentFiles.createFolder(userId, { path: `/${[NOTES_FOLDER, ...parts].join('/')}` });
  return { name: parts.join('/'), created };
}

module.exports = { NOTES_FOLDER, createNote, createNotebook, deleteNote, ensureRoot, getNote, listNotes, noteNode, updateNote };
