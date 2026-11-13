'use strict';

// Moves into folders that don't exist yet (only with `newFolders` on).
//
// A proposed name is first checked against the folders the person has: the
// same name in any case, accent, plural, spelling or language
// (folder_names.js), or a name the embedding model finds close enough,
// means the existing folder is used instead. A name that survives is put in
// the person's language for known kinds of folders.
//
// A new folder is never proposed for one file. Each file's vote is held
// (table ai_new_folder_votes, not shown anywhere) until MIN_FILES files of
// the same job would go into the same new folder; then they all become
// suggestions at once. A job proposes at most MAX_PER_BATCH new folders.
// Nothing is created here: the folder is made when the person accepts
// (review.js), in the same transaction as the move.

const db = require('../../../db/database');
const { cosine, embedTexts } = require('./folder_profiles');
const { cleanNewFolderName, folderKey, localizedConceptName } = require('./folder_names');

const MIN_FILES = 3;
const MAX_PER_BATCH = 3;
// Uploads have no job of their own: their new folders are counted per week.
const UPLOAD_BATCH = 'upload';
const UPLOAD_WINDOW = '-7 days';
const KEEP_VOTES = '-7 days';
// How alike two folder names must be (cosine of their embeddings) to count
// as the same folder.
const SAME_NAME_SIMILARITY = 0.86;
const MAX_NAMES_TO_EMBED = 300;

// The folders below `parentId` of any kind (also ones AI may not file
// into), so a new folder never duplicates a sibling it can't see as a candidate.
function siblingNames(userId, parentId) {
  return db.prepare(`
    SELECT name FROM nodes WHERE user_id = ? AND parent_id = ? AND type = 'folder' AND trashed_at IS NULL
  `).all(userId, parentId).map((row) => row.name);
}

async function similarFolder(userId, name, folders, signal) {
  const named = folders.filter((folder) => folder.path !== '/');
  const names = [...new Set(named.map((folder) => folder.name))].slice(0, MAX_NAMES_TO_EMBED);
  if (names.length === 0) return null;
  const answer = await embedTexts(userId, [name, ...names], signal);
  if (!answer) return null;
  const [proposed, ...vectors] = answer.vectors;
  let best = null;
  names.forEach((candidate, index) => {
    const score = cosine(proposed, vectors[index]);
    if (score >= SAME_NAME_SIMILARITY && (!best || score > best.score)) best = { name: candidate, score };
  });
  return best ? named.find((folder) => folder.name === best.name) : null;
}

/**
 * Where a proposed new folder really goes: { existing: folder } when one of
 * `folders` (where this file may go) means the same, { name, key } for a
 * new folder (in the person's language when it is a known kind), or null
 * when the name is not usable: too specific, or the same as a folder
 * elsewhere (`everywhere`) or a sibling AI can't file into.
 */
async function resolveNewFolder(userId, { name: rawName, parent }, folders, { everywhere = folders, language, signal } = {}) {
  const name = cleanNewFolderName(rawName);
  if (!name) return null;
  const key = folderKey(name);
  const same = folders.filter((folder) => folder.path !== '/' && folderKey(folder.name) === key);
  if (same.length > 0) return { existing: same.find((folder) => folder.parentId === parent.id) || same[0] };
  if (everywhere.some((folder) => folder.path !== '/' && folderKey(folder.name) === key)) return null;
  if (siblingNames(userId, parent.id).some((sibling) => folderKey(sibling) === key)) return null;
  const similar = await similarFolder(userId, name, folders, signal);
  if (similar) return { existing: similar };
  return { name: localizedConceptName(name, language) || name, key };
}

function liveUnchanged(userId, vote, draft) {
  const node = db.prepare('SELECT * FROM nodes WHERE id = ? AND user_id = ? AND trashed_at IS NULL').get(vote.node_id, userId);
  if (!node || node.etag !== vote.basis_etag || node.parent_id !== draft.from.id) return null;
  return node;
}

function newFoldersSoFar(userId, batchKey) {
  const since = batchKey === UPLOAD_BATCH ? `AND created_at >= datetime('now', '${UPLOAD_WINDOW}')` : '';
  return db.prepare(`
    SELECT COUNT(*) AS count FROM (
      SELECT DISTINCT parent_id, folder_key FROM ai_new_folder_votes
      WHERE user_id = ? AND batch_key = ? AND released = 1 ${since}
    )
  `).get(userId, batchKey).count;
}

/**
 * Holds one file's move into a new folder (`draft.to` = { id: null,
 * parentId, name, path, key }). Returns the moves that are ready to become
 * suggestions, [{ node, draft }]: none while fewer than MIN_FILES files of
 * the job would go there (or the job reached MAX_PER_BATCH new folders),
 * then every held one at once, all under the first file's folder name.
 */
function holdNewFolderMove(userId, node, draft, { batchKey } = {}) {
  const batch = batchKey || UPLOAD_BATCH;
  const { key, ...to } = draft.to;
  return db.transaction(() => {
    db.prepare(`
      INSERT INTO ai_new_folder_votes (user_id, node_id, batch_key, parent_id, folder_key, folder_name, draft_json, basis_etag)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?)
      ON CONFLICT(user_id, node_id, batch_key) DO UPDATE SET parent_id = excluded.parent_id,
        folder_key = excluded.folder_key, folder_name = excluded.folder_name, draft_json = excluded.draft_json,
        basis_etag = excluded.basis_etag, released = 0, created_at = datetime('now')
    `).run(userId, node.id, batch, to.parentId, key, to.name, JSON.stringify({ ...draft, to }), node.etag);
    const votes = db.prepare(`
      SELECT * FROM ai_new_folder_votes
      WHERE user_id = ? AND batch_key = ? AND parent_id = ? AND folder_key = ? ORDER BY id
    `).all(userId, batch, to.parentId, key);
    const releasedBefore = votes.some((vote) => vote.released === 1);
    const ready = votes
      .filter((vote) => vote.released === 0)
      .map((vote) => {
        const held = JSON.parse(vote.draft_json);
        const live = liveUnchanged(userId, vote, held);
        return live ? { vote, node: live, draft: held } : null;
      })
      .filter(Boolean);
    if (!releasedBefore) {
      if (ready.length < MIN_FILES) return [];
      if (newFoldersSoFar(userId, batch) >= MAX_PER_BATCH) return [];
    }
    const name = votes[0].folder_name;
    const markReleased = db.prepare('UPDATE ai_new_folder_votes SET released = 1 WHERE id = ?');
    return ready.map(({ vote, node: live, draft: held }) => {
      markReleased.run(vote.id);
      const path = held.to.path.slice(0, held.to.path.length - held.to.name.length) + name;
      return { node: live, draft: { ...held, to: { ...held.to, name, path } } };
    });
  })();
}

/** Forgets held votes older than a week. */
function dropOldVotes() {
  db.prepare("DELETE FROM ai_new_folder_votes WHERE created_at < datetime('now', ?)").run(KEEP_VOTES);
}

module.exports = { MIN_FILES, dropOldVotes, holdNewFolderMove, resolveNewFolder };
