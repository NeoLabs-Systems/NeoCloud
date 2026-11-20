'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { analyticsConfig } = require('./analytics-config');

const CLIENT_PATH = '/_neolabs/analytics.js';
const clientSource = fs.readFileSync(path.join(__dirname, 'analytics-client.js'), 'utf8');

function injectAnalytics(html) {
  return html.replace(/<\/head>/i, '<script defer src="' + CLIENT_PATH + '"></script>\n</head>');
}

function registerAnalyticsClient(app) {
  app.get(CLIENT_PATH, (_req, res, next) => {
    const config = analyticsConfig();
    if (!config) return next();
    const { endpoint, appKey } = config;
    const script = clientSource.replace('/* COUNTLY_CONFIG */ null', () => JSON.stringify({ endpoint, appKey }));
    res.set('Cache-Control', 'no-store');
    return res.type('application/javascript').send(script);
  });
}

module.exports = { registerAnalyticsClient, injectAnalytics };
