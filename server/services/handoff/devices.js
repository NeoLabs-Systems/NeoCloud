'use strict';

// The devices an account can send to: the ones online right now (browsers and
// apps) plus signed-in apps seen in the last week, which get an offer when
// they next connect. Names come from the device token (so a renamed device
// shows its new name) or, for a browser, from its User-Agent.

const db = require('../../db/database');
const { httpError } = require('../../utils/http_error');
const { browserLabel, browserPlatform } = require('./device_label');
const { deviceIdOf, deviceKey } = require('./keys');
const { onlineDevices } = require('./presence');

const RECENT_DAYS = 7;
const MOBILE_PLATFORMS = new Set(['android', 'ios']);

function tokenRow(userId, deviceId) {
  return db.prepare(`
    SELECT id, name, platform, last_seen_at FROM device_tokens
    WHERE id = ? AND user_id = ? AND revoked_at IS NULL AND platform != 'webdav'
  `).get(deviceId, userId);
}

function sessionUserAgent(userId, key) {
  const row = db.prepare(`
    SELECT user_agent FROM user_sessions
    WHERE user_id = ? AND revoked_at IS NULL AND substr(session_hash, 1, ?) = ?
  `).get(userId, key.length - 2, key.slice(2));
  return row?.user_agent || '';
}

function describeOnline(userId, entry) {
  const deviceId = deviceIdOf(entry.key);
  if (deviceId !== null) {
    const row = tokenRow(userId, deviceId);
    if (!row) return null;
    return { id: entry.key, name: row.name, platform: row.platform, kind: kindOf(row.platform), canReceive: entry.canReceive, online: true };
  }
  const userAgent = sessionUserAgent(userId, entry.key);
  return {
    id: entry.key,
    name: browserLabel(userAgent),
    platform: browserPlatform(userAgent),
    kind: 'browser',
    canReceive: entry.canReceive,
    online: true,
  };
}

function kindOf(platform) {
  return MOBILE_PLATFORMS.has(platform) ? 'phone' : 'desktop';
}

/** Online devices first, then recently seen apps (offline, `canReceive` unknown: true). */
function listDevices(userId) {
  const devices = onlineDevices(userId).map((entry) => describeOnline(userId, entry)).filter(Boolean);
  const onlineKeys = new Set(devices.map((device) => device.id));
  const recent = db.prepare(`
    SELECT id, name, platform FROM device_tokens
    WHERE user_id = ? AND revoked_at IS NULL AND platform != 'webdav'
      AND last_seen_at IS NOT NULL AND datetime(last_seen_at) > datetime('now', ?)
    ORDER BY id DESC
  `).all(userId, `-${RECENT_DAYS} days`);
  for (const row of recent) {
    const id = deviceKey(row.id);
    if (onlineKeys.has(id)) continue;
    devices.push({ id, name: row.name, platform: row.platform, kind: kindOf(row.platform), canReceive: true, online: false });
  }
  return devices;
}

/** The device to send to, or a 404: another account's keys never resolve. */
function resolveTarget(userId, key) {
  const wanted = String(key || '');
  const found = listDevices(userId).find((device) => device.id === wanted);
  if (!found) throw httpError(404, 'That device is not available.', 'DEVICE_NOT_FOUND');
  if (!found.canReceive) throw httpError(409, 'That device is not accepting files right now.', 'DEVICE_NOT_RECEIVING');
  return found;
}

/** Name and platform of the device that sends: for the offer the receiver sees. */
function describeSender(userId, key, userAgent) {
  const deviceId = deviceIdOf(key);
  if (deviceId !== null) {
    const row = tokenRow(userId, deviceId);
    return { name: row?.name || 'Device', platform: row?.platform || '' };
  }
  return { name: browserLabel(userAgent), platform: browserPlatform(userAgent) };
}

module.exports = { describeSender, listDevices, resolveTarget };
