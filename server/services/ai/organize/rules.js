'use strict';

// The owner's sorting rules (deterministic, checked before any model), and
// rule proposals: after three accepted moves of the same kind of document
// from the same issuer into one folder, NeoCloud offers a rule for it.

const db = require('../../../db/database');
const { httpError } = require('../../../utils/http_error');
const { splitExtension } = require('../../../utils/file_names');
const { assertFilingFolder, folderPath } = require('./targets');

const MAX_RULES = 200;
const PROPOSAL_THRESHOLD = 3;

function optionalText(value, field, max) {
  if (value === undefined || value === null || value === '') return null;
  if (typeof value !== 'string' || value.trim().length === 0 || value.length > max) {
    throw httpError(400, `${field} must be text of at most ${max} characters.`, 'INVALID_RULE');
  }
  return value.trim();
}

function globRegex(pattern) {
  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*').replace(/\?/g, '.');
  return new RegExp(`^${escaped}$`, 'i');
}

function extensionOf(name) {
  return splitExtension(String(name || ''))[1].replace(/^\./, '').toLowerCase();
}

function serializeRule(userId, row) {
  return {
    id: row.id,
    name: row.name,
    match: {
      type: row.match_type,
      issuer: row.match_issuer,
      extension: row.match_extension,
      namePattern: row.match_name_pattern,
    },
    targetFolderId: row.target_folder_id,
    targetPath: folderPath(userId, row.target_folder_id),
    enabled: row.enabled === 1,
    createdAt: row.created_at,
  };
}

function ruleRow(userId, id) {
  const row = db.prepare('SELECT * FROM ai_rules WHERE id = ? AND user_id = ?').get(Number(id) || 0, userId);
  if (!row) throw httpError(404, 'Rule not found', 'NOT_FOUND');
  return row;
}

// The conditions of a rule body; at least one must be set.
function parseMatch(match) {
  const source = match && typeof match === 'object' && !Array.isArray(match) ? match : {};
  const parsed = {
    type: optionalText(source.type, 'match.type', 40)?.toLowerCase() ?? null,
    issuer: optionalText(source.issuer, 'match.issuer', 80),
    extension: optionalText(source.extension, 'match.extension', 16)?.replace(/^\./, '').toLowerCase() ?? null,
    namePattern: optionalText(source.namePattern, 'match.namePattern', 120),
  };
  if (!Object.values(parsed).some((value) => value !== null)) {
    throw httpError(400, 'A rule needs at least one condition.', 'INVALID_RULE');
  }
  return parsed;
}

function parseName(value) {
  const name = optionalText(value, 'name', 80);
  if (!name) throw httpError(400, 'A rule needs a name.', 'INVALID_RULE');
  return name;
}

// A rule's target must be a folder AI may file into (no vault, not excluded).
function checkTarget(userId, folderId) {
  return assertFilingFolder(userId, folderId).id;
}

function listRules(userId) {
  const rows = db.prepare('SELECT * FROM ai_rules WHERE user_id = ? ORDER BY id').all(userId);
  return { rules: rows.map((row) => serializeRule(userId, row)) };
}

function createRule(userId, body = {}) {
  const name = parseName(body.name);
  const match = parseMatch(body.match);
  const target = checkTarget(userId, body.targetFolderId);
  const { count } = db.prepare('SELECT COUNT(*) AS count FROM ai_rules WHERE user_id = ?').get(userId);
  if (count >= MAX_RULES) throw httpError(400, `At most ${MAX_RULES} rules.`, 'TOO_MANY_RULES');
  const row = db.prepare(`
    INSERT INTO ai_rules (user_id, name, match_type, match_issuer, match_extension, match_name_pattern, target_folder_id, enabled)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING *
  `).get(userId, name, match.type, match.issuer, match.extension, match.namePattern, target, body.enabled === false ? 0 : 1);
  return { rule: serializeRule(userId, row) };
}

function updateRule(userId, id, body = {}) {
  const row = ruleRow(userId, id);
  const name = body.name === undefined ? row.name : parseName(body.name);
  const match = body.match === undefined
    ? { type: row.match_type, issuer: row.match_issuer, extension: row.match_extension, namePattern: row.match_name_pattern }
    : parseMatch(body.match);
  const target = body.targetFolderId === undefined ? row.target_folder_id : checkTarget(userId, body.targetFolderId);
  if (body.enabled !== undefined && typeof body.enabled !== 'boolean') {
    throw httpError(400, 'enabled must be true or false.', 'INVALID_RULE');
  }
  const enabled = body.enabled === undefined ? row.enabled : Number(body.enabled);
  const updated = db.prepare(`
    UPDATE ai_rules SET name = ?, match_type = ?, match_issuer = ?, match_extension = ?, match_name_pattern = ?,
      target_folder_id = ?, enabled = ?
    WHERE id = ? RETURNING *
  `).get(name, match.type, match.issuer, match.extension, match.namePattern, target, enabled, row.id);
  return { rule: serializeRule(userId, updated) };
}

function deleteRule(userId, id) {
  const row = ruleRow(userId, id);
  db.prepare('DELETE FROM ai_rules WHERE id = ?').run(row.id);
  return { success: true };
}

function ruleMatches(row, node, fields) {
  const equal = (left, right) => String(left || '').toLowerCase() === String(right || '').toLowerCase();
  if (row.match_type && !equal(row.match_type, fields.type)) return false;
  if (row.match_issuer && !equal(row.match_issuer, fields.issuer)) return false;
  if (row.match_extension && row.match_extension !== extensionOf(node.name)) return false;
  if (row.match_name_pattern && !globRegex(row.match_name_pattern).test(node.name)) return false;
  return true;
}

/** The first enabled rule (oldest first) matching a file and its extracted fields, or null. */
function matchingRule(userId, node, fields) {
  const rows = db.prepare('SELECT * FROM ai_rules WHERE user_id = ? AND enabled = 1 ORDER BY id').all(userId);
  return rows.find((row) => ruleMatches(row, node, fields)) || null;
}

// --- Proposals -----------------------------------------------------------------------

/** Counts an accepted move of a (type, issuer) document into a folder (-1 when undone). */
function recordAcceptedMove(userId, { type, issuer }, folderId, delta = 1) {
  if (!type || !issuer || !folderId) return;
  db.prepare(`
    INSERT INTO ai_move_stats (user_id, doc_type, issuer, target_folder_id, accepts) VALUES (?, ?, ?, ?, MAX(?, 0))
    ON CONFLICT(user_id, doc_type, issuer, target_folder_id) DO UPDATE SET accepts = MAX(accepts + ?, 0)
  `).run(userId, type.toLowerCase(), issuer, folderId, delta, delta);
}

const OPEN_PROPOSALS = `
  FROM ai_move_stats p
  WHERE p.user_id = ? AND p.accepts >= ${PROPOSAL_THRESHOLD} AND p.dismissed = 0
    AND NOT EXISTS (
      SELECT 1 FROM ai_rules r WHERE r.user_id = p.user_id AND r.target_folder_id = p.target_folder_id
        AND lower(COALESCE(r.match_type, '')) = p.doc_type AND lower(COALESCE(r.match_issuer, '')) = lower(p.issuer)
    )
`;

function proposalCount(userId) {
  return db.prepare(`SELECT COUNT(*) AS count ${OPEN_PROPOSALS}`).get(userId).count;
}

function listProposals(userId) {
  const rows = db.prepare(`SELECT p.* ${OPEN_PROPOSALS} ORDER BY p.accepts DESC, p.id`).all(userId);
  return {
    proposals: rows.map((row) => ({
      id: row.id,
      type: row.doc_type,
      issuer: row.issuer,
      targetFolderId: row.target_folder_id,
      targetPath: folderPath(userId, row.target_folder_id),
      accepts: row.accepts,
      name: `${row.issuer} ${row.doc_type}`,
    })),
  };
}

function proposalRow(userId, id) {
  const row = db.prepare(`SELECT p.* ${OPEN_PROPOSALS} AND p.id = ?`).get(userId, Number(id) || 0);
  if (!row) throw httpError(404, 'Proposal not found', 'NOT_FOUND');
  return row;
}

/** Turns a proposal into a rule; `name` may replace the suggested one. */
function acceptProposal(userId, id, { name } = {}) {
  const row = proposalRow(userId, id);
  return createRule(userId, {
    name: name ?? `${row.issuer} ${row.doc_type}`,
    match: { type: row.doc_type, issuer: row.issuer },
    targetFolderId: row.target_folder_id,
  });
}

function dismissProposal(userId, id) {
  const row = proposalRow(userId, id);
  db.prepare('UPDATE ai_move_stats SET dismissed = 1 WHERE id = ?').run(row.id);
  return { success: true };
}

module.exports = {
  acceptProposal,
  createRule,
  deleteRule,
  dismissProposal,
  listProposals,
  listRules,
  matchingRule,
  proposalCount,
  recordAcceptedMove,
  updateRule,
};
