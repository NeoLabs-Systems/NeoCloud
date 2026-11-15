'use strict';

// How a device is addressed. A signed-in app is 'd:<device token id>'; a
// browser is 's:<prefix of its session hash>', one device per browser
// session however many tabs it has. The session hash is keyed (HMAC), so the
// key reveals nothing about the session cookie.

const { sessionHash } = require('../account/sessions');

const SESSION_KEY_LENGTH = 24;

function deviceKey(deviceId) {
  return `d:${Number(deviceId)}`;
}

function sessionKey(hash) {
  return `s:${String(hash).slice(0, SESSION_KEY_LENGTH)}`;
}

/** The key of whoever made this request: its device token, else its browser session. */
function requestKey(req) {
  if (req.deviceId) return deviceKey(req.deviceId);
  return sessionKey(sessionHash(req.sessionID));
}

/** The device token id of a 'd:' key, or null for a browser. */
function deviceIdOf(key) {
  const match = /^d:(\d+)$/.exec(String(key));
  return match ? Number(match[1]) : null;
}

module.exports = { deviceIdOf, deviceKey, requestKey, sessionKey };
