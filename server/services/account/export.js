'use strict';

// "Export account data": everything stored about the account as one JSON
// document, as in NeoRouter. Metadata only: file contents are not included
// (they are downloaded as files or a ZIP), and neither is anything that
// works as a credential -- password and code hashes, 2FA secrets, link
// tokens and passwords, device token hashes, passkey keys, vault keys,
// stored import passwords.

const db = require('../../db/database');
const { readSettings } = require('../settings/store');
const { getUsage } = require('../storage/quota');
const { httpError } = require('../../utils/http_error');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('Export');
const FORMAT = 1;

const rows = (sql, ...params) => db.prepare(sql).all(...params);

function parseJson(text) {
  try {
    return JSON.parse(text || '{}');
  } catch {
    return {};
  }
}

// Files and folders, flat with parentId, each file with its earlier versions.
function exportTree(userId) {
  const versions = new Map();
  for (const row of rows(`
    SELECT v.node_id, v.version, v.size, v.created_at FROM file_versions v
    JOIN nodes n ON n.id = v.node_id WHERE n.user_id = ? ORDER BY v.node_id, v.version
  `, userId)) {
    if (!versions.has(row.node_id)) versions.set(row.node_id, []);
    versions.get(row.node_id).push({ version: row.version, size: row.size, createdAt: row.created_at });
  }
  return rows(`
    SELECT id, parent_id, type, name, size, mime, version, created_at, modified_at, trashed_at, vault_id
    FROM nodes WHERE user_id = ? ORDER BY parent_id IS NOT NULL, parent_id, name
  `, userId).map((row) => ({
    id: row.id,
    parentId: row.parent_id,
    type: row.type,
    name: row.name,
    size: row.size,
    mime: row.mime,
    version: row.version,
    createdAt: row.created_at,
    modifiedAt: row.modified_at,
    trashedAt: row.trashed_at,
    vaultId: row.vault_id,
    ...(row.type === 'file' ? { versions: versions.get(row.id) || [] } : {}),
  }));
}

function exportLinks(userId) {
  return rows(`
    SELECT s.*, n.name AS node_name FROM shares s JOIN nodes n ON n.id = s.node_id
    WHERE s.user_id = ? ORDER BY s.id
  `, userId).map((row) => ({
    id: row.id,
    nodeId: row.node_id,
    nodeName: row.node_name,
    mode: row.mode,
    hasPassword: Boolean(row.password_hash),
    note: row.note,
    expiresAt: row.expires_at,
    maxDownloads: row.max_downloads,
    downloadCount: row.download_count,
    maxUploadBytes: row.max_upload_bytes,
    uploadedBytes: row.uploaded_bytes,
    createdAt: row.created_at,
    revokedAt: row.revoked_at,
  }));
}

function exportMemberShares(userId) {
  const byMe = rows(`
    SELECT s.id, s.node_id, n.name AS node_name, s.permission, s.recipient, s.expires_at, s.created_at, u.username
    FROM user_shares s JOIN nodes n ON n.id = s.node_id JOIN users u ON u.id = s.grantee_id
    WHERE s.owner_id = ? ORDER BY s.id
  `, userId).map((row) => ({
    id: row.id,
    nodeId: row.node_id,
    nodeName: row.node_name,
    with: row.username,
    recipient: row.recipient,
    permission: row.permission,
    expiresAt: row.expires_at,
    createdAt: row.created_at,
  }));
  const withMe = rows(`
    SELECT s.id, s.node_id, n.name AS node_name, s.permission, s.expires_at, s.created_at, u.username
    FROM user_shares s JOIN nodes n ON n.id = s.node_id JOIN users u ON u.id = s.owner_id
    WHERE s.grantee_id = ? ORDER BY s.id
  `, userId).map((row) => ({
    id: row.id,
    nodeId: row.node_id,
    nodeName: row.node_name,
    owner: row.username,
    permission: row.permission,
    expiresAt: row.expires_at,
    createdAt: row.created_at,
  }));
  return { byMe, withMe };
}

/** The account's data as one document. */
function exportAccount(userId) {
  const user = db.prepare('SELECT * FROM users WHERE id = ?').get(userId);
  if (!user) throw httpError(404, 'User not found', 'NOT_FOUND');
  const twoFactor = db.prepare('SELECT enabled, enabled_at FROM user_two_factor WHERE user_id = ?').get(userId);
  const data = {
    format: FORMAT,
    kind: 'neocloud-account',
    exportedAt: new Date().toISOString(),
    note: 'File contents are not included. Download your files, or a folder as ZIP, to keep them.',
    profile: {
      id: user.id,
      username: user.username,
      displayName: user.display_name,
      email: user.email,
      isAdmin: Number(user.is_admin) === 1,
      createdAt: user.created_at,
      lastLogin: user.last_login,
    },
    storage: getUsage(userId),
    settings: readSettings(userId),
    security: {
      twoFactor: { enabled: twoFactor?.enabled === 1, enabledAt: twoFactor?.enabled_at || null },
      passkeys: rows(`
        SELECT id, label, device_type, backed_up, rp_id, created_at, last_used_at
        FROM user_webauthn_credentials WHERE user_id = ? ORDER BY id
      `, userId).map((row) => ({
        id: row.id,
        label: row.label,
        deviceType: row.device_type,
        backedUp: Boolean(row.backed_up),
        host: row.rp_id,
        createdAt: row.created_at,
        lastUsedAt: row.last_used_at,
      })),
    },
    sessions: rows(`
      SELECT id, ip_address, user_agent, created_at, last_seen_at, expires_at, revoked_at
      FROM user_sessions WHERE user_id = ? ORDER BY id
    `, userId).map((row) => ({
      id: row.id,
      ipAddress: row.ip_address,
      userAgent: row.user_agent,
      createdAt: row.created_at,
      lastSeenAt: row.last_seen_at,
      expiresAt: row.expires_at,
      revokedAt: row.revoked_at,
    })),
    devices: rows(`
      SELECT id, name, platform, created_at, last_seen_at, revoked_at FROM device_tokens WHERE user_id = ? ORDER BY id
    `, userId).map((row) => ({
      id: row.id,
      name: row.name,
      platform: row.platform,
      createdAt: row.created_at,
      lastSeenAt: row.last_seen_at,
      revokedAt: row.revoked_at,
    })),
    links: exportLinks(userId),
    sharing: exportMemberShares(userId),
    files: exportTree(userId),
    favorites: rows('SELECT node_id, created_at FROM favorites WHERE user_id = ? ORDER BY created_at', userId)
      .map((row) => ({ nodeId: row.node_id, createdAt: row.created_at })),
    vaults: rows('SELECT id, folder_id, created_at, updated_at FROM vaults WHERE user_id = ? ORDER BY created_at', userId)
      .map((row) => ({ id: row.id, folderId: row.folder_id, createdAt: row.created_at, updatedAt: row.updated_at })),
    imports: rows(`
      SELECT id, source, url, username, source_path, target_folder_id, status, created_at, started_at, finished_at
      FROM import_jobs WHERE user_id = ? ORDER BY created_at
    `, userId).map((row) => ({
      id: row.id,
      source: row.source,
      url: row.url,
      username: row.username,
      sourcePath: row.source_path,
      targetFolderId: row.target_folder_id,
      status: row.status,
      createdAt: row.created_at,
      startedAt: row.started_at,
      finishedAt: row.finished_at,
    })),
    audit: rows(`
      SELECT id, action, actor_user_id, subject_user_id, detail_json, created_at FROM access_audit_log
      WHERE actor_user_id = @id OR subject_user_id = @id ORDER BY id
    `, { id: userId }).map((row) => ({
      id: row.id,
      action: row.action,
      byMe: row.actor_user_id === userId,
      aboutMe: row.subject_user_id === userId,
      detail: parseJson(row.detail_json),
      createdAt: row.created_at,
    })),
  };
  logger.info(`Account ${userId} exported its data`);
  return data;
}

module.exports = { exportAccount };
