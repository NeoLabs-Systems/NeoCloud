'use strict';

// A signed-out app has no business holding a connection or a pending send:
// its live connections are closed and every open send to or from it is
// cancelled (the staged files go with them).

const db = require('../../db/database');
const { createServiceLogger } = require('../../utils/logger');
const { cancelForDevice } = require('./delivery');
const { deviceKey } = require('./keys');
const { dropDevice, onlineDevices } = require('./presence');

const logger = createServiceLogger('Handoff');

function endDevice(userId, key) {
  dropDevice(userId, key);
  cancelForDevice(userId, key).catch((err) => logger.warn(`Could not cancel the sends of ${key}: ${err.message}`));
}

/** One device token was revoked. */
function deviceRevoked(userId, deviceId) {
  endDevice(userId, deviceKey(deviceId));
}

/** Every device token of the account was revoked, except possibly one. */
function allDevicesRevoked(userId, { exceptDeviceId = null } = {}) {
  const keep = exceptDeviceId === null ? null : deviceKey(exceptDeviceId);
  const keys = new Set(onlineDevices(userId).map((device) => device.key).filter((key) => key.startsWith('d:')));
  const open = db.prepare(`
    SELECT sender_key, target_key FROM handoff_offers
    WHERE user_id = ? AND status IN ('uploading', 'pending', 'accepted')
  `).all(userId);
  for (const row of open) {
    for (const key of [row.sender_key, row.target_key]) if (key.startsWith('d:')) keys.add(key);
  }
  for (const key of keys) if (key !== keep) endDevice(userId, key);
}

module.exports = { allDevicesRevoked, deviceRevoked };
