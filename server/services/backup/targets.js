'use strict';

// Backup targets as the admin configures them. Connection settings are kept
// encrypted; secrets (passwords, keys) never leave the server again -- reads
// return them blanked, and saving a blank keeps the stored one.

const db = require('../../db/database');
const { decryptValue, encryptValue } = require('../crypto/keys');
const { httpError } = require('../../utils/http_error');
const { readState } = require('../state_store');
const { BACKUP_TYPES, createAdapter } = require('./adapters');

const SCHEDULES = new Set(['manual', 'daily', 'weekly']);
const SECRET_FIELDS = new Set(['password', 'privateKey', 'passphrase', 'secretAccessKey']);
const FIELDS = {
  local: ['path'],
  sftp: ['host', 'port', 'username', 'password', 'privateKey', 'passphrase', 'path'],
  s3: ['endpoint', 'region', 'bucket', 'accessKeyId', 'secretAccessKey', 'prefix', 'forcePathStyle'],
  webdav: ['url', 'username', 'password'],
};

function readConfig(row) {
  return JSON.parse(decryptValue(row.config_enc));
}

function publicConfig(type, config) {
  const out = {};
  for (const field of FIELDS[type]) {
    if (SECRET_FIELDS.has(field)) {
      out[`${field}Set`] = Boolean(config[field]);
    } else if (config[field] !== undefined) {
      out[field] = config[field];
    }
  }
  return out;
}

function lastRun(targetId) {
  return db.prepare('SELECT * FROM backup_runs WHERE target_id = ? ORDER BY id DESC LIMIT 1').get(targetId) || null;
}

function serializeRun(row) {
  if (!row) return null;
  return {
    id: row.id,
    targetId: row.target_id,
    status: row.status,
    snapshot: row.snapshot,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    bytesUploaded: row.bytes_uploaded,
    blobsUploaded: row.blobs_uploaded,
    error: row.error,
  };
}

function serializeTarget(row) {
  return {
    id: row.id,
    name: row.name,
    type: row.type,
    config: publicConfig(row.type, readConfig(row)),
    schedule: row.schedule,
    hour: row.hour,
    weekday: row.weekday,
    retention: row.retention,
    enabled: row.enabled === 1,
    lastRun: serializeRun(lastRun(row.id)),
    // Written by verify.js: { verifiedAt, ok, snapshot, database, blobsChecked, missingBlobs, error }.
    lastVerification: readState('backup-verify', {})[row.id] ?? null,
  };
}

function getTargetRow(id) {
  const row = db.prepare('SELECT * FROM backup_targets WHERE id = ?').get(id);
  if (!row) throw httpError(404, 'Backup target not found', 'NOT_FOUND');
  return row;
}

function listTargets() {
  return db.prepare('SELECT * FROM backup_targets ORDER BY id').all().map(serializeTarget);
}

function listRuns(limit = 50) {
  return db.prepare('SELECT * FROM backup_runs ORDER BY id DESC LIMIT ?').all(limit).map(serializeRun);
}

function cleanConfig(type, incoming, previous = {}) {
  if (incoming !== undefined && incoming !== null && (typeof incoming !== 'object' || Array.isArray(incoming))) {
    throw httpError(400, 'config must be an object.', 'BACKUP_CONFIG_INVALID');
  }
  incoming = incoming || {};
  const config = {};
  for (const field of FIELDS[type]) {
    let value = incoming[field];
    if (typeof value === 'string') value = value.trim();
    if (SECRET_FIELDS.has(field) && (value === undefined || value === '')) value = previous[field];
    if (value !== undefined && value !== '') config[field] = value;
  }
  if (config.port !== undefined) config.port = Number(config.port);
  if (config.forcePathStyle !== undefined) config.forcePathStyle = config.forcePathStyle === true || config.forcePathStyle === 'true';
  // Building the adapter validates the required fields.
  createAdapter(type, config);
  return config;
}

function cleanSchedule(body, current = {}) {
  const schedule = body.schedule ?? current.schedule ?? 'manual';
  if (!SCHEDULES.has(schedule)) throw httpError(400, 'schedule must be manual, daily or weekly.', 'INVALID_VALUE');
  const hour = Number(body.hour ?? current.hour ?? 3);
  const weekday = Number(body.weekday ?? current.weekday ?? 0);
  const retention = Number(body.retention ?? current.retention ?? 7);
  if (!Number.isInteger(hour) || hour < 0 || hour > 23) throw httpError(400, 'hour must be 0–23.', 'INVALID_VALUE');
  if (!Number.isInteger(weekday) || weekday < 0 || weekday > 6) throw httpError(400, 'weekday must be 0 (Sunday) to 6.', 'INVALID_VALUE');
  if (!Number.isInteger(retention) || retention < 1 || retention > 365) throw httpError(400, 'Keep between 1 and 365 snapshots.', 'INVALID_VALUE');
  return { schedule, hour, weekday, retention };
}

function cleanName(value, fallback) {
  const name = String(value ?? fallback ?? '').trim().slice(0, 80);
  if (!name) throw httpError(400, 'Give the target a name.', 'NAME_REQUIRED');
  return name;
}

function createTarget(body = {}) {
  if (!BACKUP_TYPES.includes(body.type)) throw httpError(400, 'type must be local, sftp, s3 or webdav.', 'INVALID_VALUE');
  const config = cleanConfig(body.type, body.config);
  const plan = cleanSchedule(body);
  const result = db.prepare(`
    INSERT INTO backup_targets (name, type, config_enc, schedule, hour, weekday, retention, enabled)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  `).run(cleanName(body.name), body.type, encryptValue(JSON.stringify(config)), plan.schedule, plan.hour, plan.weekday, plan.retention, body.enabled === false ? 0 : 1);
  return serializeTarget(getTargetRow(result.lastInsertRowid));
}

function updateTarget(id, body = {}) {
  const row = getTargetRow(id);
  const config = body.config ? cleanConfig(row.type, body.config, readConfig(row)) : readConfig(row);
  const plan = cleanSchedule(body, row);
  const enabled = body.enabled === undefined ? row.enabled : (body.enabled ? 1 : 0);
  db.prepare(`
    UPDATE backup_targets SET name = ?, config_enc = ?, schedule = ?, hour = ?, weekday = ?, retention = ?, enabled = ?
    WHERE id = ?
  `).run(cleanName(body.name, row.name), encryptValue(JSON.stringify(config)), plan.schedule, plan.hour, plan.weekday, plan.retention, enabled, id);
  return serializeTarget(getTargetRow(id));
}

function deleteTarget(id) {
  getTargetRow(id);
  db.prepare('DELETE FROM backup_targets WHERE id = ?').run(id);
  return { success: true };
}

/** A connected adapter for the target; the caller closes it. */
async function openTarget(id) {
  const row = getTargetRow(id);
  const adapter = createAdapter(row.type, readConfig(row));
  try {
    await adapter.open();
  } catch (err) {
    // A half-open connection (SFTP handshake done, folder refused) still
    // holds a socket.
    await adapter.close().catch(() => {});
    throw err;
  }
  return { row, adapter };
}

module.exports = {
  createTarget,
  deleteTarget,
  getTargetRow,
  listRuns,
  listTargets,
  openTarget,
  serializeRun,
  updateTarget,
};
