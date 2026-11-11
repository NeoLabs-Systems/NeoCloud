'use strict';

// The Express app without a listening server, so tests can drive it directly.

const express = require('express');
const { validateOrigin } = require('./config/origins');
const { applyHttpMiddleware, createSessionMiddleware } = require('./http/middleware');
const { registerApiRoutes } = require('./http/routes');
const { registerStaticRoutes } = require('./http/static');
const { registerErrorHandler } = require('./http/errors');

function createApp(httpConfig) {
  const app = express();
  app.disable('x-powered-by');
  app.locals.httpRuntimeConfig = httpConfig;
  const sessionMiddleware = createSessionMiddleware(httpConfig);
  // Before the API middleware: WebDAV has its own auth, bodies and methods.
  app.use(require('./routes/webdav'));
  applyHttpMiddleware(app, { ...httpConfig, sessionMiddleware, validateOrigin });
  registerApiRoutes(app);
  registerStaticRoutes(app);
  registerErrorHandler(app);
  return { app, sessionMiddleware };
}

module.exports = { createApp };
