'use strict';

// One place that knows how to reach an account's open web sessions. The
// server wires in socket.io at startup; until then (CLI, tests) events are
// dropped. Each socket sits in its account's room and in a room for the
// browser session it signed in with (see http/socket.js), so ending a
// session or an account also ends its live connections.

let io = null;

function attachSocketServer(server) {
  io = server;
}

function userRoom(userId) {
  return `user:${userId}`;
}

function sessionRoom(sessionHash) {
  return `session:${sessionHash}`;
}

function emitToUser(userId, event, payload) {
  if (!io) return;
  io.to(userRoom(userId)).emit(event, payload);
}

/** Closes the sockets opened with these sessions (keyed session-id hashes). */
function disconnectSessions(sessionHashes) {
  if (!io) return;
  for (const hash of sessionHashes) io.in(sessionRoom(hash)).disconnectSockets(true);
}

/** Closes every socket of the account. */
function disconnectUser(userId) {
  if (!io) return;
  io.in(userRoom(userId)).disconnectSockets(true);
}

module.exports = { attachSocketServer, disconnectSessions, disconnectUser, emitToUser, sessionRoom, userRoom };
