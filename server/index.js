'use strict';

const fs = require('fs');
const {
  ENV_FILE,
  PID_FILE,
  applySecureUmask,
  ensureRuntimeDirs,
  ensureSecureRuntimeEnv,
} = require('../runtime/paths');
const dotenv = require('dotenv');

applySecureUmask();
ensureRuntimeDirs();
ensureSecureRuntimeEnv({ logger: console });
dotenv.config({ path: ENV_FILE, override: true, quiet: true });

// Opened first so a database this version can't run on (newer schema) stops
// the start with its own message instead of a stack trace.
let db;
try {
  db = require('./db/database');
} catch (err) {
  console.error(`[Startup] ${err.message}`);
  process.exit(1);
}

const { createServer } = require('http');
const { createApp } = require('./app');
const { readHttpRuntimeConfig } = require('./config/http');
const { createSocketServer, bindSocketSessions } = require('./http/socket');
const { isUploadRequest } = require('./http/middleware');
const { isDavPath } = require('./services/webdav/paths');
const { validateOrigin } = require('./config/origins');
const { attachSocketServer } = require('./services/events');
const { applyEnvAdminGrants } = require('./services/access/admin');
const { startMaintenance } = require('./services/maintenance');
const { startBackupScheduler } = require('./services/backup/scheduler');
const { startImports, stopImports } = require('./services/import/runner');
const { startSearchIndexer } = require('./services/search_index/indexer');
const { setupConsoleInterceptor } = require('./utils/logger');
const { DEFAULT_PORT } = require('../lib/setup/contract');

const PORT = Number(process.env.PORT) || DEFAULT_PORT;
// Long enough for the last uploads to finish; compose's stop_grace_period
// (40 s) and the service managers' stop timeouts leave room for it.
const SHUTDOWN_GRACE_MS = 30 * 1000;
const httpConfig = readHttpRuntimeConfig();

const logHistory = setupConsoleInterceptor();
console.log(`[Startup] Using env file: ${ENV_FILE}`);
console.log('[Startup] HTTP runtime:', httpConfig);

const { granted, missing } = applyEnvAdminGrants();
if (granted.length > 0) console.log(`[Startup] Granted admin from NEOCLOUD_ADMIN_USERS: ${granted.join(', ')}`);
if (missing.length > 0) console.warn(`[Startup] NEOCLOUD_ADMIN_USERS names missing accounts: ${missing.join(', ')}`);

const { app, sessionMiddleware } = createApp(httpConfig);
app.locals.logHistory = logHistory;

let draining = false;

// While shutting down, new uploads are refused and every response closes its
// connection; requests already running finish normally.
function handleRequest(req, res) {
  if (draining) {
    res.setHeader('Connection', 'close');
    if (isUploadRequest(req) || (req.method === 'PUT' && isDavPath(String(req.url || '').split('?')[0]))) {
      res.writeHead(503, { 'Content-Type': 'application/json; charset=utf-8', 'Retry-After': '30' });
      res.end(JSON.stringify({ error: 'The server is restarting. Try again in a moment.', code: 'SERVER_STOPPING' }));
      return;
    }
  }
  app(req, res);
}

const httpServer = createServer(handleRequest);
// Uploads of large files over slow links outlive Node's 5-minute default.
httpServer.requestTimeout = 60 * 60 * 1000;
// Longer than the 60 s idle timeout of common proxies and load balancers, so
// the proxy, not NeoCloud, closes idle connections (no random 502s).
httpServer.keepAliveTimeout = 65 * 1000;
httpServer.headersTimeout = 66 * 1000;
const io = createSocketServer(httpServer, { validateOrigin });
bindSocketSessions(io, sessionMiddleware);
attachSocketServer(io);
const stopMaintenance = startMaintenance();
const stopBackupScheduler = startBackupScheduler();
startImports();
const stopSearchIndexer = startSearchIndexer();

function removePidFile() {
  try {
    if (fs.readFileSync(PID_FILE, 'utf8').trim() === String(process.pid)) fs.rmSync(PID_FILE, { force: true });
  } catch {
    // Already gone.
  }
}

let shuttingDown = false;
function shutdown(signal) {
  if (shuttingDown) return;
  shuttingDown = true;
  draining = true;
  console.log(`[Shutdown] ${signal}: finishing in-flight requests (up to ${SHUTDOWN_GRACE_MS / 1000} s)...`);
  stopMaintenance();
  stopBackupScheduler();
  // Running imports stop here and resume from their last file on the next start.
  stopImports();
  // The indexer finishes the file it is on; the next start continues from the tables.
  stopSearchIndexer();

  // Connections that finish their last request go right away.
  const idleSweep = setInterval(() => httpServer.closeIdleConnections(), 1000);
  const force = setTimeout(() => {
    console.warn('[Shutdown] Grace period over; closing remaining connections.');
    httpServer.closeAllConnections();
  }, SHUTDOWN_GRACE_MS);
  idleSweep.unref();
  force.unref();

  httpServer.once('close', () => {
    clearInterval(idleSweep);
    clearTimeout(force);
    db.close();
    removePidFile();
    console.log('[Shutdown] Stopped.');
    process.exit(0);
  });
  // Disconnects realtime clients and stops accepting connections (socket.io
  // closes the HTTP server too).
  io.close();
  httpServer.closeIdleConnections();
}

httpServer.on('error', (error) => {
  if (error?.code === 'EADDRINUSE') {
    console.error(`[Startup] Port ${PORT} is already in use. Stop the running NeoCloud with \`neocloud stop\`, or set PORT in ${ENV_FILE}.`);
    process.exit(1);
  }
  throw error;
});

httpServer.listen(PORT, () => {
  // Lets `neocloud backup restore` and `neocloud rollback` see that the
  // server runs, whichever service manager started it.
  fs.writeFileSync(PID_FILE, String(process.pid), { mode: 0o600 });
  console.log(`NeoCloud running on ${httpConfig.publicUrl || `http://localhost:${PORT}`}`);
});

process.on('SIGINT', () => shutdown('SIGINT'));
process.on('SIGTERM', () => shutdown('SIGTERM'));

module.exports = { app, io, httpServer };
