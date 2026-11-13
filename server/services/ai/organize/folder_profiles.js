'use strict';

// Similarity between a file and the person's folders, from the embedding
// role: each folder's profile (its path and up to 15 names inside it) is
// embedded once and again only when it changes; the file's name and
// extracted fields are embedded per decision. Without an embedding model,
// folders are ranked by the words their path shares with the file.

const crypto = require('crypto');
const db = require('../../../db/database');
const ai = require('../client');

const SAMPLE_NAMES = 15;
const EMBED_BATCH = 64;

function profileText(userId, folder) {
  const names = db.prepare(`
    SELECT name FROM nodes WHERE parent_id = ? AND user_id = ? AND trashed_at IS NULL
    ORDER BY modified_at DESC LIMIT ?
  `).all(folder.id, userId, SAMPLE_NAMES).map((row) => row.name);
  return `Folder ${folder.path}\n${names.join('\n')}`;
}

function signatureOf(text) {
  return crypto.createHash('sha256').update(text).digest('hex');
}

function toBlob(vector) {
  return Buffer.from(new Float32Array(vector).buffer);
}

function fromBlob(blob) {
  return Array.from(new Float32Array(blob.buffer, blob.byteOffset, blob.byteLength / 4));
}

function cosine(left, right) {
  if (!left || !right || left.length !== right.length || left.length === 0) return 0;
  let dot = 0;
  let leftNorm = 0;
  let rightNorm = 0;
  for (let index = 0; index < left.length; index += 1) {
    dot += left[index] * right[index];
    leftNorm += left[index] * left[index];
    rightNorm += right[index] * right[index];
  }
  return leftNorm && rightNorm ? dot / Math.sqrt(leftNorm * rightNorm) : 0;
}

/** Vectors for many short texts, in batches; null when there is no embedding model. */
async function embedTexts(userId, texts, signal) {
  const vectors = [];
  let model = null;
  for (let start = 0; start < texts.length; start += EMBED_BATCH) {
    let answer;
    try {
      answer = await ai.embed({ userId, feature: 'embed', texts: texts.slice(start, start + EMBED_BATCH), signal });
    } catch (error) {
      if (error.code === 'AI_MODELS_UNAVAILABLE') return null;
      throw error;
    }
    model = answer.model;
    vectors.push(...answer.vectors);
  }
  return { vectors, model };
}

/** A vector per candidate folder, embedding only profiles that changed (or another model's). */
async function folderVectors(userId, folders, model, signal) {
  const stored = new Map(db.prepare('SELECT folder_id, signature, model, vector FROM ai_folder_profiles WHERE user_id = ?')
    .all(userId).map((row) => [row.folder_id, row]));
  const vectors = new Map();
  const missing = [];
  for (const folder of folders) {
    const text = profileText(userId, folder);
    const signature = signatureOf(text);
    const row = stored.get(folder.id);
    if (row && row.signature === signature && row.model === model) vectors.set(folder.id, fromBlob(row.vector));
    else missing.push({ folder, text, signature });
  }
  const save = db.prepare(`
    INSERT INTO ai_folder_profiles (folder_id, user_id, signature, model, vector, updated_at)
    VALUES (?, ?, ?, ?, ?, datetime('now'))
    ON CONFLICT(folder_id) DO UPDATE SET signature = excluded.signature, model = excluded.model,
      vector = excluded.vector, updated_at = excluded.updated_at
  `);
  for (let start = 0; start < missing.length; start += EMBED_BATCH) {
    const batch = missing.slice(start, start + EMBED_BATCH);
    const answer = await ai.embed({ userId, feature: 'embed', texts: batch.map((entry) => entry.text), signal });
    db.transaction(() => {
      batch.forEach((entry, index) => {
        const vector = answer.vectors[index];
        if (!Array.isArray(vector)) return;
        save.run(entry.folder.id, userId, entry.signature, answer.model, toBlob(vector));
        vectors.set(entry.folder.id, vector);
      });
    })();
  }
  return vectors;
}

function fileText(node, fields) {
  return [node.name, fields.type, fields.issuer, fields.subject, fields.summary, fields.caption, ...(fields.tags || [])]
    .filter(Boolean).join('\n');
}

function words(text) {
  return new Set(String(text || '').toLowerCase().split(/[^\p{L}\p{N}]+/u).filter((word) => word.length > 2));
}

// Without an embedding model: folders whose path shares the most words with the file.
function rankByWords(folders, text) {
  const fileWords = words(text);
  return folders
    .map((folder) => ({ folder, score: [...words(folder.path)].filter((word) => fileWords.has(word)).length }))
    .sort((left, right) => right.score - left.score)
    .map((entry) => ({ folder: entry.folder, score: null }));
}

/** Candidates ranked by similarity: [{ folder, score }], and the embedding model (or null). */
async function rankFolders(userId, node, fields, folders, signal) {
  const text = fileText(node, fields);
  const file = await embedTexts(userId, [text], signal);
  if (!file) return { ranked: rankByWords(folders, text), model: null };
  const vectors = await folderVectors(userId, folders, file.model, signal);
  const ranked = folders
    .map((folder) => ({ folder, score: cosine(file.vectors[0], vectors.get(folder.id)) }))
    .sort((left, right) => right.score - left.score);
  return { ranked, model: file.model };
}

module.exports = { cosine, embedTexts, rankFolders };
