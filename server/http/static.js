'use strict';

const fs = require('fs');
const path = require('path');
const express = require('express');

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
  return res.send(html);
}

function registerStaticRoutes(app) {
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
