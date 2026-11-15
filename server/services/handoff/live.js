'use strict';

// The two ways a device holds a live connection: a browser's socket.io
// socket (the web app), and a server-sent event stream for signed-in apps
// (phone and desktop clients, which authenticate with a bearer token and have
// no socket.io client). Both register in presence and receive the same events.

const { browserLabel } = require('./device_label');
const { sessionKey } = require('./keys');
const { inboxRows } = require('./offers');
const { offerArrived } = require('./notify');
const presence = require('./presence');

const HEARTBEAT_MS = 25 * 1000;

// Offers that arrived while the device was away reach it now.
function deliverWaiting(userId, key) {
  for (const row of inboxRows(userId, key)) {
    if (row.status === 'pending') offerArrived(userId, row);
  }
}

/** A web session's socket: one device per browser session. */
function attachSocket(socket) {
  const { userId } = socket.data;
  const key = sessionKey(socket.data.sessionHash);
  const userAgent = socket.request.headers['user-agent'] || '';
  const disconnect = presence.connect(userId, {
    key,
    kind: 'browser',
    label: browserLabel(userAgent),
    canReceive: true,
    push: (event, payload) => socket.emit(event, payload),
    close: () => socket.disconnect(true),
  });
  socket.once('disconnect', disconnect);
  deliverWaiting(userId, key);
}

/**
 * An event stream for the request's own device (`GET /api/handoff/events`).
 * `receive` false keeps the device out of other devices' send lists.
 */
function openEventStream(req, res, { userId, key, receive }) {
  res.set({
    'Content-Type': 'text/event-stream; charset=utf-8',
    'Cache-Control': 'no-store',
    Connection: 'keep-alive',
    'X-Accel-Buffering': 'no',
  });
  res.flushHeaders();
  res.write('retry: 3000\n: connected\n\n');
  const disconnect = presence.connect(userId, {
    key,
    kind: 'app',
    canReceive: receive,
    push: (event, payload) => res.write(`event: ${event}\ndata: ${JSON.stringify(payload)}\n\n`),
    close: () => res.end(),
  });
  const heartbeat = setInterval(() => res.write(': ping\n\n'), HEARTBEAT_MS);
  heartbeat.unref();
  req.on('close', () => {
    clearInterval(heartbeat);
    disconnect();
  });
  if (receive) deliverWaiting(userId, key);
}

module.exports = { attachSocket, openEventStream };
