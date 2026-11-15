'use strict';

// Who is online. A device is online while at least one live connection (a
// browser socket or a native client's event stream) is open for it. Kept in
// memory per account: presence is never visible to another account, and a
// restart empties it until the clients reconnect.

const crypto = require('crypto');

// userId -> connection id -> { key, kind, canReceive, label, push, close }
const accounts = new Map();

function accountConnections(userId) {
  return accounts.get(Number(userId)) || new Map();
}

/** Calls `push(event, payload)` on every live connection of the account. */
function pushToAccount(userId, event, payload) {
  for (const connection of accountConnections(userId).values()) connection.push(event, payload);
}

/** Calls `push` on the connections of one device; returns how many there were. */
function pushToDevice(userId, key, event, payload) {
  let delivered = 0;
  for (const connection of accountConnections(userId).values()) {
    if (connection.key !== key) continue;
    connection.push(event, payload);
    delivered += 1;
  }
  return delivered;
}

/** One entry per online device: { key, kind, label, canReceive }. */
function onlineDevices(userId) {
  const devices = new Map();
  for (const connection of accountConnections(userId).values()) {
    const known = devices.get(connection.key);
    if (known) {
      known.canReceive = known.canReceive || connection.canReceive;
      continue;
    }
    devices.set(connection.key, {
      key: connection.key,
      kind: connection.kind,
      label: connection.label,
      canReceive: connection.canReceive,
    });
  }
  return [...devices.values()];
}

function isOnline(userId, key) {
  return [...accountConnections(userId).values()].some((connection) => connection.key === key);
}

function announceDevices(userId) {
  pushToAccount(userId, 'handoff:devices', {});
}

/**
 * Registers a live connection. `push(event, payload)` writes to it, `close()`
 * ends it. Returns the function to call when the connection ends.
 */
function connect(userId, { key, kind, label = '', canReceive = true, push, close }) {
  const id = crypto.randomUUID();
  const numericId = Number(userId);
  if (!accounts.has(numericId)) accounts.set(numericId, new Map());
  const wasOnline = isOnline(numericId, key);
  accounts.get(numericId).set(id, { key, kind, label, canReceive, push, close });
  if (!wasOnline) announceDevices(numericId);
  return function disconnect() {
    const connections = accounts.get(numericId);
    if (!connections || !connections.delete(id)) return;
    if (connections.size === 0) accounts.delete(numericId);
    if (!isOnline(numericId, key)) announceDevices(numericId);
  };
}

/** Ends every connection of a device (its token was revoked). */
function dropDevice(userId, key) {
  for (const connection of [...accountConnections(userId).values()]) {
    if (connection.key === key) connection.close();
  }
}

/** Ends every connection of the account. */
function dropAccount(userId) {
  for (const connection of [...accountConnections(userId).values()]) connection.close();
}

module.exports = { connect, dropAccount, dropDevice, isOnline, onlineDevices, pushToAccount, pushToDevice };
