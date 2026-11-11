'use strict';

// WebDAV signs in with HTTP Basic: the account's username plus an app
// password, which is a device token created with platform `webdav`.

const db = require('../../db/database');
const { resolveDeviceToken } = require('../devices/tokens');

/** { username, password } from a Basic Authorization header, or null. */
function basicCredentials(req) {
  const match = String(req.get('authorization') || '').match(/^Basic\s+([A-Za-z0-9+/]+={0,2})\s*$/i);
  if (!match) return null;
  const decoded = Buffer.from(match[1], 'base64').toString('utf8');
  const colon = decoded.indexOf(':');
  if (colon < 0) return null;
  return { username: decoded.slice(0, colon).trim(), password: decoded.slice(colon + 1) };
}

/** { userId, deviceId, username } when the pair is valid, else null. */
function resolveAppPassword({ username, password }) {
  const device = resolveDeviceToken(password);
  if (!device || device.platform !== 'webdav') return null;
  const user = db.prepare('SELECT id, username FROM users WHERE id = ?').get(device.userId);
  if (!user || user.username.toLowerCase() !== username.toLowerCase()) return null;
  return { userId: user.id, deviceId: device.deviceId, username: user.username };
}

module.exports = { basicCredentials, resolveAppPassword };
