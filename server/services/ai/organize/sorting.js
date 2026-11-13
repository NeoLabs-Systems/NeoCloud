'use strict';

// Where a file belongs. Restraint first: "keep it where it is" is the
// default answer, a move must be clearly better.
//
// Which folders are considered at all:
//   - A file at the top level or in an inbox-like folder (Downloads,
//     Unsorted, Scans, …) may go into any folder it may move to (targets.js).
//   - A file in a folder the person made stays in that folder's subtree:
//     AI never suggests moving it out of their structure.
//   - Photos and videos only ever go into the existing photo tree (Photos,
//     Fotos, Camera Uploads, …), by year and month when the person already
//     sorts it that way, and never into a new folder. Photos already in a
//     folder of the person's stay there.
//   - Documents are never filed into the photo tree.
//
// Then, cheapest signal first:
//   1. the owner's rules (no model at all),
//   2. similarity between the file and each folder's profile from the
//      embedding role; a clear winner decides on its own,
//   3. otherwise the text model picks among the ten closest folders, or
//      keeps the file where it is (prompts.js). Below MIN_CONFIDENCE it
//      is not suggested.
// A new folder is proposed only when the person allows it, and is first
// matched against existing folders in any language (new_folders.js).

const ai = require('../client');
const { readSettings } = require('../../settings/store');
const { matchingRule } = require('./rules');
const { candidateFolders } = require('./targets');
const { rankFolders } = require('./folder_profiles');
const { resolveNewFolder } = require('./new_folders');
const { sortMessages } = require('./prompts');
const names = require('./folder_names');

const TOP_CANDIDATES = 10;
// A similarity this high, this far ahead of the next folder, needs no model.
const DECISIVE_SCORE = 0.92;
const DECISIVE_MARGIN = 0.05;
// Below this the model's pick is not worth asking the person about.
const MIN_CONFIDENCE = 0.6;
const MIN_NEW_FOLDER_CONFIDENCE = 0.7;
const PHOTO_CONFIDENCE = 0.8;
// Kinds of pictures that are documents (a photographed receipt), filed like documents.
const DOCUMENT_TYPES = new Set([
  'invoice', 'receipt', 'contract', 'payslip', 'letter', 'statement', 'certificate', 'manual',
  'report', 'notes', 'presentation', 'spreadsheet', 'scan',
]);

function isImageOrVideo(node) {
  const mime = String(node.mime || '');
  return /^(image|video)\//.test(mime) && mime !== 'image/svg+xml';
}

function isMedia(node, fields) {
  return isImageOrVideo(node) && !DOCUMENT_TYPES.has(fields.type);
}

function segments(path) {
  return path.split('/').filter(Boolean);
}

function inMediaTree(folder) {
  return segments(folder.path).some(names.isMediaName);
}

function within(folder, ancestor) {
  return folder.id === ancestor.id || folder.path.startsWith(`${ancestor.path === '/' ? '' : ancestor.path}/`);
}

// --- Photos and videos ----------------------------------------------------------------------

function mediaRoot(node, fields, folders) {
  let wanted = ['photos'];
  if (fields.type === 'screenshot') wanted = ['screenshots', 'photos'];
  else if (/^video\//.test(String(node.mime))) wanted = ['videos', 'photos'];
  // Shallowest first (candidateFolders is ordered by depth).
  const roots = folders.filter((folder) => folder.path !== '/' && names.isMediaName(folder.name)
    && !segments(folder.path).slice(0, -1).some(names.isMediaName));
  for (const concept of wanted) {
    const root = roots.find((folder) => names.conceptOf(folder.name) === concept);
    if (root) return root;
  }
  return null;
}

// The year folder (and month folder in it) below the photo root, when the person has them.
function datedFolder(folders, root, date) {
  if (!/^\d{4}-\d{2}/.test(String(date || ''))) return root;
  const year = Number(date.slice(0, 4));
  const month = Number(date.slice(5, 7));
  const yearFolder = folders.find((folder) => folder.parentId === root.id && names.isYearName(folder.name, year));
  if (!yearFolder) return root;
  return folders.find((folder) => folder.parentId === yearFolder.id && names.isMonthName(folder.name, month)) || yearFolder;
}

function photoMove(node, fields, folders) {
  const root = mediaRoot(node, fields, folders);
  if (!root) return null;
  const target = datedFolder(folders, root, fields.date);
  if (target.id === node.parent_id) return null;
  return {
    to: { id: target.id, path: target.path },
    confidence: PHOTO_CONFIDENCE,
    reason: `Photos and videos are kept in ${root.path}`,
    source: 'ai',
    ruleId: null,
    model: null,
  };
}

// --- The model's pick -----------------------------------------------------------------------

function parseChoice(value, count) {
  const number = Number(value);
  return Number.isInteger(number) && number >= 1 && number <= count ? number - 1 : -1;
}

function clamp(value) {
  const number = Number(value);
  return Number.isFinite(number) ? Math.min(Math.max(number, 0), 1) : 0.5;
}

function userLanguage(userId, folders, fields) {
  return names.folderLanguage(folders.map((folder) => folder.name)) || readSettings(userId).language || fields.language || 'en';
}

async function newFolderMove(userId, node, answer, context) {
  const { candidates, scoped, folders, current, open, language, signal } = context;
  const confidence = clamp(answer.data.confidence);
  if (confidence < MIN_NEW_FOLDER_CONFIDENCE) return null;
  const parentIndex = parseChoice(answer.data.parent, candidates.length);
  let parent = parentIndex >= 0 ? candidates[parentIndex].folder : null;
  if (!parent || !scoped.includes(parent)) parent = open ? folders[0] : current;
  const resolved = await resolveNewFolder(userId, { name: answer.data.newFolder, parent }, scoped, { everywhere: folders, language, signal });
  if (!resolved) return null;
  const base = { confidence, reason: context.reason, source: 'ai', ruleId: null, model: answer.model };
  if (resolved.existing) {
    if (resolved.existing.id === node.parent_id) return null;
    return { ...base, to: { id: resolved.existing.id, path: resolved.existing.path } };
  }
  const path = parent.path === '/' ? `/${resolved.name}` : `${parent.path}/${resolved.name}`;
  return { ...base, to: { id: null, parentId: parent.id, name: resolved.name, path, key: resolved.key } };
}

/**
 * A move for a file node with its extraction, or null when it stays where
 * it is. Returns { to: { id, path } or, for a new folder, { id: null,
 * parentId, name, path, key }, confidence, reason, source, ruleId, model }.
 */
async function decideMove(userId, node, fields, { allowNewFolders = false, signal } = {}) {
  const folders = candidateFolders(userId, node.parent_id);
  const allowed = new Map(folders.map((folder) => [folder.id, folder]));

  const rule = matchingRule(userId, node, fields);
  if (rule && allowed.has(rule.target_folder_id)) {
    if (rule.target_folder_id === node.parent_id) return null;
    const folder = allowed.get(rule.target_folder_id);
    return { to: { id: folder.id, path: folder.path }, confidence: 1, reason: `Rule: ${rule.name}`, source: 'rule', ruleId: rule.id, model: null };
  }

  const current = allowed.get(node.parent_id);
  if (!current) return null;
  const open = current.path === '/' || names.isInboxName(current.name);
  if (isMedia(node, fields)) return open ? photoMove(node, fields, folders) : null;

  const scoped = folders.filter((folder) => !inMediaTree(folder) && (open || within(folder, current)));
  const allowNew = allowNewFolders && !isImageOrVideo(node);
  if (scoped.length === 0 || (scoped.length < 2 && !allowNew)) return null;

  const { ranked, model: embedModel } = await rankFolders(userId, node, fields, scoped, signal);
  const [best, next] = ranked;
  if (next && best.score !== null && best.score >= DECISIVE_SCORE && best.score - (next.score ?? 0) >= DECISIVE_MARGIN) {
    if (best.folder.id === node.parent_id) return null;
    return {
      to: { id: best.folder.id, path: best.folder.path },
      confidence: best.score,
      reason: `Similar to the files in ${best.folder.path}`,
      source: 'ai',
      ruleId: null,
      model: embedModel,
    };
  }

  const candidates = ranked.slice(0, TOP_CANDIDATES);
  const language = userLanguage(userId, folders, fields);
  const answer = await ai.chat({
    userId,
    feature: 'sort',
    role: 'text',
    json: true,
    messages: sortMessages({ node, fields, candidates, allowNew, language }),
    signal,
  });
  const reason = typeof answer.data.reason === 'string' ? answer.data.reason.replace(/\s+/g, ' ').trim().slice(0, 200) : '';
  const confidence = clamp(answer.data.confidence);
  const index = parseChoice(answer.data.choice, candidates.length);
  if (index >= 0) {
    const folder = candidates[index].folder;
    if (folder.id === node.parent_id || confidence < MIN_CONFIDENCE) return null;
    return { to: { id: folder.id, path: folder.path }, confidence, reason, source: 'ai', ruleId: null, model: answer.model };
  }
  if (!allowNew || typeof answer.data.newFolder !== 'string') return null;
  return newFolderMove(userId, node, answer, { candidates, scoped, folders, current, open, language, reason, signal });
}

module.exports = { decideMove };
