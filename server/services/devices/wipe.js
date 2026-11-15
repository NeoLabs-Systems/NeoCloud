'use strict';

// Remote wipe: the owner of a lost phone or laptop signs it out from another
// client and asks it to erase its local NeoCloud data. The token dies at once.
// A wipe marker stays on the device row so that the next time that client
// shows up with the dead token it is told exactly once
// (401 DEVICE_WIPE_REQUESTED) and erases itself. A device that never comes
// back never wipes: the server cannot reach into a device.

const db = require('../../db/database');
const { emitToUser } = require('../events');
const { httpError } = require('../../utils/http_error');
const { recordAccessEvent } = require('../access/audit');
const { deviceRevoked } = require('../handoff/revoke');
const { hashToken, serialize } = require('./tokens');

// Marks the device to erase itself and revokes its token. Asking again keeps
// the first request (idempotent); only the erase-files choice can be widened.
function requestDeviceWipe(userId, id, { eraseFiles = false, currentDeviceId = null } = {}) {
  if (id === currentDeviceId) throw httpError(400, 'Use Disconnect to sign this phone out', 'CURRENT_DEVICE');
  const result = db.transaction(() => {
    const row = db.prepare('SELECT * FROM device_tokens WHERE id = ? AND user_id = ?').get(id, userId);
    if (!row || (row.revoked_at && !row.wipe_requested_at) || row.platform === 'webdav') {
      throw httpError(404, 'Device not found', 'NOT_FOUND');
    }
    if (row.wipe_delivered_at) return serialize(row);
    const erase = eraseFiles === true || row.wipe_erase_files === 1 ? 1 : 0;
    db.prepare(`
      UPDATE device_tokens SET revoked_at = COALESCE(revoked_at, datetime('now')),
        wipe_requested_at = COALESCE(wipe_requested_at, datetime('now')), wipe_erase_files = ?
      WHERE id = ?
    `).run(erase, id);
    if (!row.wipe_requested_at) {
      recordAccessEvent('device.wipe_requested', {
        actorUserId: userId,
        subjectUserId: userId,
        detail: { deviceId: id, deviceName: row.name, eraseFiles: erase === 1 },
      });
    }
    return serialize(db.prepare('SELECT * FROM device_tokens WHERE id = ?').get(id));
  })();
  deviceRevoked(userId, id);
  return result;
}

// Called for a request whose bearer token is dead. Returns { eraseFiles } the
// first time a wiped device asks, then null: the token stays dead for good.
function takePendingWipe(token) {
  if (!token) return null;
  const row = db.prepare(`
    SELECT id, user_id, name, wipe_erase_files FROM device_tokens
    WHERE token_hash = ? AND wipe_requested_at IS NOT NULL AND wipe_delivered_at IS NULL
  `).get(hashToken(token));
  if (!row) return null;
  const claimed = db.prepare(`
    UPDATE device_tokens SET wipe_delivered_at = datetime('now')
    WHERE id = ? AND wipe_delivered_at IS NULL
  `).run(row.id);
  if (claimed.changes === 0) return null;
  recordAccessEvent('device.wipe_delivered', {
    actorUserId: row.user_id,
    subjectUserId: row.user_id,
    detail: { deviceId: row.id, deviceName: row.name },
  });
  emitToUser(row.user_id, 'devices:wiped', { id: row.id, name: row.name });
  return { eraseFiles: row.wipe_erase_files === 1 };
}

module.exports = { requestDeviceWipe, takePendingWipe };
