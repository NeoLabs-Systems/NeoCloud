'use strict';

// Live updates for the web app. Each signed-in socket joins its account's
// room and its session's room; services announce file changes there through
// services/events.js, and ending the session or the account disconnects it.

const { Server: SocketIO } = require('socket.io');
const { sessionHash } = require('../services/account/sessions');
const { sessionRoom, userRoom } = require('../services/events');

function createSocketServer(httpServer, { validateOrigin }) {
  return new SocketIO(httpServer, {
    pingInterval: 25000,
    pingTimeout: 20000,
    maxHttpBufferSize: 1024 * 1024,
    cors: {
      origin(origin, callback) {
        return validateOrigin(origin, callback, { allowMissingOrigin: true });
      },
      credentials: true,
    },
  });
}

function bindSocketSessions(io, sessionMiddleware) {
  io.use((socket, next) => {
    sessionMiddleware(socket.request, {}, (err) => {
      if (err) return next(err);
      const userId = socket.request.session?.userId;
      if (!userId) return next(new Error('Unauthorized'));
      socket.data.userId = userId;
      socket.data.sessionHash = sessionHash(socket.request.sessionID);
      socket.join([userRoom(userId), sessionRoom(socket.data.sessionHash)]);
      return next();
    });
  });
}

module.exports = { bindSocketSessions, createSocketServer };
