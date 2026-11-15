'use strict';

// The external storage connections of each account ("mounts"). The settings,
// secrets included, are one JSON document sealed with the account's own data
// key (bound to the account and the mount, so a row cannot be moved to
// another). Reads return them without secrets; only the server side that
// opens a connection ever sees the secrets.

const crypto = require('crypto');
const db = require('../../db/database');
const { recordAccessEvent } = require('../access/audit');
const { getUserDek, openBytes, sealBytes } = require('../crypto/keys');
const { httpError } = require('../../utils/http_error');
const { cleanConfig, providerFor, publicConfig } = require('./config');

const MAX_MOUNTS_PER_USER = 10;
const MAX_NAME_LENGTH = 60;

const aad = (userId, mountId) => `external-mount:${userId}:${mountId}`;

function seal(userId, mountId, config) {
  return sealBytes(getUserDek(userId), Buffer.from(JSON.stringify(config), 'utf8'), aad(userId, mountId)).toString('base64');
}

function readConfig(row) {
  return JSON.parse(openBytes(getUserDek(row.user_id), Buffer.from(row.config_enc, 'base64'), aad(row.user_id, row.id)).toString('utf8'));
}

function getMountRow(userId, mountId) {
  const row = db.prepare('SELECT * FROM external_mounts WHERE id = ? AND user_id = ?').get(String(mountId || ''), userId);
  if (!row) throw httpError(404, 'Cloud storage not found.', 'NOT_FOUND');
  return row;
}

function serializeMount(row) {
  return {
    id: row.id,
    name: row.name,
    provider: row.provider,
    readOnly: row.read_only === 1,
    config: publicConfig(row.provider, readConfig(row)),
    createdAt: row.created_at,
  };
}

function cleanName(value) {
  const name = String(value ?? '').normalize('NFC').trim();
  if (!name || name.length > MAX_NAME_LENGTH || /[\u0000-\u001f\u007f/\\]/.test(name)) {
    throw httpError(400, 'Give the connection a name of up to 60 characters, without slashes.', 'NAME_INVALID');
  }
  return name;
}

function assertNameFree(userId, name, exceptId = null) {
  const clash = db.prepare('SELECT id FROM external_mounts WHERE user_id = ? AND name = ? COLLATE NOCASE AND id != ?').get(userId, name, exceptId || '');
  if (clash) throw httpError(409, 'You already have a connection with that name.', 'NAME_EXISTS');
}

function audit(action, userId, mountId, provider) {
  recordAccessEvent(action, { actorUserId: userId, subjectUserId: userId, detail: { mountId, provider } });
}

function listMounts(userId) {
  return db.prepare('SELECT * FROM external_mounts WHERE user_id = ? ORDER BY name COLLATE NOCASE').all(userId).map(serializeMount);
}

/** The cleaned fields of a new connection, before it has been tested. */
function draftMount(body = {}, previous = null) {
  const provider = previous ? previous.provider : body.provider;
  providerFor(provider);
  return {
    provider,
    name: cleanName(body.name ?? previous?.name),
    config: previous && !body.config ? readConfig(previous) : cleanConfig(provider, body.config, previous ? readConfig(previous) : {}),
    readOnly: body.readOnly === undefined ? previous?.read_only === 1 : body.readOnly === true,
  };
}

function insertMount(userId, draft) {
  const count = db.prepare('SELECT COUNT(*) AS count FROM external_mounts WHERE user_id = ?').get(userId).count;
  if (count >= MAX_MOUNTS_PER_USER) {
    throw httpError(409, `You can connect up to ${MAX_MOUNTS_PER_USER} cloud storages.`, 'EXTERNAL_MOUNT_LIMIT');
  }
  assertNameFree(userId, draft.name);
  const id = crypto.randomUUID();
  db.prepare('INSERT INTO external_mounts (id, user_id, name, provider, config_enc, read_only) VALUES (?, ?, ?, ?, ?, ?)')
    .run(id, userId, draft.name, draft.provider, seal(userId, id, draft.config), draft.readOnly ? 1 : 0);
  audit('external_storage.connect', userId, id, draft.provider);
  return serializeMount(getMountRow(userId, id));
}

function saveMount(userId, row, draft) {
  assertNameFree(userId, draft.name, row.id);
  db.prepare("UPDATE external_mounts SET name = ?, config_enc = ?, read_only = ?, updated_at = datetime('now') WHERE id = ?")
    .run(draft.name, seal(userId, row.id, draft.config), draft.readOnly ? 1 : 0, row.id);
  audit('external_storage.update', userId, row.id, row.provider);
  return serializeMount(getMountRow(userId, row.id));
}

function deleteMount(userId, mountId) {
  const row = getMountRow(userId, mountId);
  db.prepare('DELETE FROM external_mounts WHERE id = ?').run(row.id);
  audit('external_storage.disconnect', userId, row.id, row.provider);
  return { success: true };
}

/** Removes every connection of an account that is erased or disabled. */
function removeMountsOf(userId) {
  db.prepare('DELETE FROM external_mounts WHERE user_id = ?').run(userId);
}

module.exports = {
  deleteMount, draftMount, getMountRow, insertMount, listMounts, readConfig, removeMountsOf, saveMount, serializeMount,
};
