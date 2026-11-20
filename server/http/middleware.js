'use strict';

const { analyticsConfig } = require('../analytics-config');

const express = require('express');
const session = require('express-session');
const BaseSQLiteStore = require('better-sqlite3-session-store')(session);

// The store's expired-session sweep must not keep the process alive (tests
// and tools that load the app exit when their work is done).
class SQLiteStore extends BaseSQLiteStore {
  startInterval() {
    setInterval(() => this.clearExpiredSessions(), this.expired.intervalMs).unref();
  }
}
const compression = require('compression');
const helmet = require('helmet');
const cors = require('cors');
const db = require('../db/database');
const { redactErrorsForNonAdmins } = require('./errors');
const { logRequestSummary } = require('../utils/logger');
const { getSessionSecret } = require('../services/account/session_secret');
const { readEnvBool } = require('../../runtime/env');
const { editorOrigins } = require('../services/office/discovery');

const SESSION_COOKIE_NAME = 'neocloud.sid';
const SESSION_MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;

// Upload endpoints read the raw request body as a stream; the JSON and form
// parsers must never buffer it, whatever Content-Type the client sent.
const RAW_BODY_PATHS = [
  /^\/api\/upload$/,
  /^\/api\/uploads\/[^/]+\/parts\/\d+$/,
  /^\/api\/handoff\/offers\/[^/]+\/files\/\d+$/,
  /^\/api\/public\/shares\/[^/]+\/upload$/,
  /^\/api\/public\/sign\/[^/]+\/fields\/[^/]+\/upload$/,
  /^\/wopi\/files\/[^/]+\/contents$/,
];

// Requests that start writing a file. During shutdown these are turned away
// first, so in-flight transfers can finish while no new one begins.
function isUploadRequest(req) {
  if (!['PUT', 'POST'].includes(String(req.method).toUpperCase())) return false;
  const path = requestPath(req);
  return path === '/api/uploads' || RAW_BODY_PATHS.some((pattern) => pattern.test(path));
}

function requestPath(req) {
  return `${req.originalUrl || req.url || ''}`.split('?')[0];
}

// The document editor (docs/office-editing.md) is framed from, and its page
// is opened by a form post to, its own origin only, and only while it is on.
function editorFrameSources() {
  return editorOrigins().join(' ') || "'self'";
}

function buildHelmetOptions({ secureCookies }) {
  // Flutter web (CanvasKit) needs wasm eval and loads its engine from gstatic.
  const allowExternal = readEnvBool('NEOCLOUD_CSP_ALLOW_EXTERNAL', true);
  const scriptSrc = ["'self'", "'unsafe-inline'", 'blob:', "'wasm-unsafe-eval'"];
  // blob:/data: let the PDF viewer and image decoders read in-memory files.
  const connectSrc = ["'self'", 'blob:', 'data:', ...(secureCookies ? ['wss:'] : ['ws:', 'wss:'])];
  const analytics = analyticsConfig();
  if (analytics) connectSrc.push(analytics.origin);
  if (allowExternal) {
    scriptSrc.push('https://www.gstatic.com');
    connectSrc.push('https://fonts.googleapis.com', 'https://fonts.gstatic.com', 'https://www.gstatic.com');
  }
  return {
    strictTransportSecurity: secureCookies
      ? { maxAge: Number(process.env.NEOCLOUD_HSTS_MAX_AGE ?? 86400), includeSubDomains: false }
      : false,
    crossOriginOpenerPolicy: false,
    crossOriginResourcePolicy: { policy: 'same-site' },
    originAgentCluster: false,
    referrerPolicy: { policy: 'strict-origin-when-cross-origin' },
    contentSecurityPolicy: {
      directives: {
        defaultSrc: ["'self'"],
        scriptSrc,
        styleSrc: ["'self'", "'unsafe-inline'", 'https://fonts.googleapis.com'],
        imgSrc: ["'self'", 'data:', 'blob:'],
        mediaSrc: ["'self'", 'data:', 'blob:'],
        connectSrc,
        fontSrc: ["'self'", 'data:', 'https://fonts.gstatic.com'],
        workerSrc: ["'self'", 'blob:'],
        frameSrc: ["'self'", 'blob:', editorFrameSources],
        formAction: ["'self'", editorFrameSources],
        frameAncestors: ["'self'"],
        upgradeInsecureRequests: null,
      },
    },
  };
}

function createSessionMiddleware({ secureCookies, trustProxy }) {
  return session({
    store: new SQLiteStore({
      client: db,
      expired: { clear: true, intervalMs: 15 * 60 * 1000 },
    }),
    secret: getSessionSecret(),
    name: SESSION_COOKIE_NAME,
    // Follow Express's "trust proxy" (set from TRUST_PROXY) for req.secure.
    proxy: trustProxy === false ? false : undefined,
    resave: false,
    rolling: true,
    saveUninitialized: false,
    cookie: {
      maxAge: SESSION_MAX_AGE_MS,
      httpOnly: true,
      sameSite: 'lax',
      secure: secureCookies,
    },
  });
}

// Only JSON is compressed: file bodies keep their exact length and byte
// ranges, and images and media are compressed already.
function isJsonResponse(req, res) {
  return /^application\/json\b/i.test(String(res.getHeader('Content-Type') || ''));
}

function isStateChangingMethod(method = '') {
  return ['POST', 'PUT', 'PATCH', 'DELETE'].includes(String(method).toUpperCase());
}

function originFromReferer(referer = '') {
  try {
    return new URL(String(referer || '').trim()).origin;
  } catch {
    return '';
  }
}

function applyHttpMiddleware(app, { trustProxy, secureCookies, sessionMiddleware, validateOrigin }) {
  // A hop count or a list of proxy addresses; never `true`, which would let
  // any client spoof its address past the rate limits.
  if (trustProxy !== false) app.set('trust proxy', trustProxy);

  app.use(helmet(buildHelmetOptions({ secureCookies })));
  app.use(compression({ filter: isJsonResponse }));
  // Routes match any letter case, but the origin check and auth look for a
  // lowercase /api/, so /API/ must not reach the handlers.
  app.use((req, res, next) => {
    const path = requestPath(req);
    if (/^\/api(\/|$)/i.test(path) && !/^\/api(\/|$)/.test(path)) {
      return res.status(404).json({ error: 'Not found', code: 'NOT_FOUND' });
    }
    return next();
  });
  // Requests without an Origin (native clients, same-origin GETs) pass;
  // a browser on another origin must be listed in ALLOWED_ORIGINS.
  app.use(cors((req, callback) => {
    callback(null, {
      origin(origin, originCallback) {
        return validateOrigin(origin, originCallback);
      },
      credentials: true,
      exposedHeaders: ['Content-Range', 'Accept-Ranges', 'Content-Length', 'ETag', 'Content-Disposition'],
    });
  }));
  app.use((req, res, next) => {
    if (requestPath(req).startsWith('/api/')) {
      res.setHeader('Cache-Control', 'no-store, max-age=0');
      // nginx would otherwise buffer whole downloads to disk before sending.
      if (req.method === 'GET' || req.method === 'HEAD') res.setHeader('X-Accel-Buffering', 'no');
    }
    next();
  });
  // Signing links: even a refused request body (bad JSON, too large) is answered
  // without a Referer policy that would let the token travel on.
  app.use('/api/public/sign', (req, res, next) => {
    res.set({ 'Referrer-Policy': 'no-referrer', 'X-Robots-Tag': 'noindex, nofollow' });
    next();
  });
  // CSRF: a state-changing API call from a browser must come from an allowed
  // origin. Native clients send no Origin and authenticate with a bearer token.
  app.use((req, res, next) => {
    if (!requestPath(req).startsWith('/api/') || !isStateChangingMethod(req.method)) return next();
    const origin = String(req.get('origin') || '').trim() || originFromReferer(req.get('referer'));
    if (!origin) return next();
    return validateOrigin(origin, (error) => {
      if (error) {
        logRequestSummary('warn', req, 'blocked state-changing request due to invalid origin', { origin });
        return res.status(403).json({ error: 'Origin not allowed', code: 'ORIGIN_NOT_ALLOWED' });
      }
      return next();
    }, { allowMissingOrigin: false });
  });
  app.use((req, res, next) => {
    const startedAt = Date.now();
    res.on('finish', () => {
      if (res.statusCode >= 400) {
        logRequestSummary(res.statusCode >= 500 ? 'error' : 'warn', req, `completed ${res.statusCode} in ${Date.now() - startedAt}ms`);
      }
    });
    next();
  });

  const jsonBody = express.json({ limit: '1mb' });
  // The agent API (routes/agent_api.js) takes whole files as JSON: up to
  // 5 MB of content, base64 encoded.
  const agentJsonBody = express.json({ limit: '8mb' });
  const formBody = express.urlencoded({ extended: false, limit: '64kb' });
  app.use((req, res, next) => {
    const path = requestPath(req);
    if (RAW_BODY_PATHS.some((pattern) => pattern.test(path))) return next();
    const json = path.startsWith('/api/v1/') ? agentJsonBody : jsonBody;
    return json(req, res, (err) => (err ? next(err) : formBody(req, res, next)));
  });
  app.use(sessionMiddleware);
  app.use(redactErrorsForNonAdmins);
}

module.exports = {
  SESSION_COOKIE_NAME,
  applyHttpMiddleware,
  createSessionMiddleware,
  isUploadRequest,
};
