'use strict';

const fs = require('fs');
const path = require('path');
const express = require('express');
const { registerAnalyticsClient, injectAnalytics } = require('../analytics');
const { analyticsConfig } = require('../analytics-config');

const FLUTTER_WEB_DIR = path.join(__dirname, '..', 'public');
const LANDING_DIR = path.join(__dirname, '..', '..', 'landing');

function setNoStoreHeaders(res) {
  res.set('Cache-Control', 'no-store, no-cache, must-revalidate, proxy-revalidate');
  res.set('Pragma', 'no-cache');
  res.set('Expires', '0');
}

function setFlutterStaticHeaders(res, filePath) {
  const relativePath = path.relative(FLUTTER_WEB_DIR, filePath).replaceAll('\\', '/');
  if (['index.html', 'flutter_bootstrap.js', 'flutter_service_worker.js', 'version.json'].includes(relativePath)) {
    setNoStoreHeaders(res);
  }
}

function serveFlutterApp(req, res) {
  const entry = path.join(FLUTTER_WEB_DIR, 'index.html');
  if (!fs.existsSync(entry)) {
    return res
      .status(503)
      .send('The NeoCloud web app is not built yet. Run "npm run flutter:build:web".');
  }
  setNoStoreHeaders(res);
  // Flutter resolves its assets against <base href>; the app lives under /app/.
  const html = fs.readFileSync(entry, 'utf8').replace('<base href="/">', '<base href="/app/">');
  res.set('Content-Type', 'text/html');
  const signingPage = /^\/app\/sign(?:\/|$)/i.test(req.path);
  if (signingPage) return res.send(html);
  const withNotice = html.replace(/<\/head>/i, '<script defer src="/js/cookie-notice.js"></script>\n</head>');
  return res.send(analyticsConfig() ? injectAnalytics(withNotice) : withNotice);
}

// A signing link is its own credential: never indexed, never sent on in a
// Referer, never framed, and the page may only talk to this server (no
// Google fonts or CanvasKit CDN; build with --no-web-resources-cdn).
const SIGNING_PAGE_CSP = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline' blob: 'wasm-unsafe-eval'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "media-src 'self' data: blob:",
  "connect-src 'self' data: blob:",
  "font-src 'self' data:",
  "worker-src 'self' blob:",
  "frame-src 'self' blob:",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
].join('; ');

function setSigningPageHeaders(req, res, next) {
  setNoStoreHeaders(res);
  res.set({
    'Referrer-Policy': 'no-referrer',
    'X-Robots-Tag': 'noindex, nofollow',
    'Content-Security-Policy': SIGNING_PAGE_CSP,
  });
  next();
}

function registerStaticRoutes(app) {
  registerAnalyticsClient(app);
  app.use('/app/sign', setSigningPageHeaders);
  app.get('/app/index.html', serveFlutterApp);
  app.use('/app', express.static(FLUTTER_WEB_DIR, { index: false, setHeaders: setFlutterStaticHeaders }));
  app.get(/^\/app(\/.*)?$/, serveFlutterApp);

  // The public page of a share link; it reads the token from its own URL.
  // The token is the secret, so it must not travel on in a Referer.
  app.get('/s/:token', (req, res, next) => {
    setNoStoreHeaders(res);
    res.set({ 'Referrer-Policy': 'no-referrer', 'X-Robots-Tag': 'noindex, nofollow' });
    res.sendFile(path.join(LANDING_DIR, 'share.html'), (err) => {
      if (err && !res.headersSent) next();
    });
  });

  // The landing page at /. A mistyped address in a browser gets the 404
  // page; API clients keep their JSON error.
  if (fs.existsSync(path.join(LANDING_DIR, 'index.html'))) {
    app.get('/status', (_req, res) => res.sendFile(path.join(LANDING_DIR, 'status.html')));
    app.get(['/', '/index.html'], (_req, res, next) => {
      if (!analyticsConfig()) return next();
      const entry = path.join(LANDING_DIR, 'index.html');
      fs.readFile(entry, 'utf8', (error, html) => {
        if (error) return next(error);
        setNoStoreHeaders(res);
        return res.type('html').send(injectAnalytics(html));
      });
    });
    app.use(express.static(LANDING_DIR));
    app.use((req, res, next) => {
      if (req.method !== 'GET' && req.method !== 'HEAD') return next();
      if (req.path.startsWith('/api/') || req.accepts(['json', 'html']) !== 'html') return next();
      return res.status(404).sendFile(path.join(LANDING_DIR, '404.html'), (err) => {
        if (err && !res.headersSent) next();
      });
    });
  } else {
    app.get('/', (req, res) => res.redirect('/app/'));
  }
}

module.exports = { FLUTTER_WEB_DIR, registerStaticRoutes };
