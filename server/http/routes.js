'use strict';

const { jsonHandler } = require('../routes/_helpers/http');
const { getServerVersion } = require('../utils/version');
const { checkHealth } = require('../services/health');

// Mount point → router module. Each router applies its own auth guard.
const ROUTE_REGISTRY = [
  { basePath: '/api/auth', modulePath: '../routes/auth' },
  { basePath: '/api/auth', modulePath: '../routes/auth_email' },
  { basePath: '/api/account', modulePath: '../routes/account' },
  { basePath: '/api/settings', modulePath: '../routes/settings' },
  { basePath: '/api/admin', modulePath: '../routes/admin' },
  { basePath: '/api', modulePath: '../routes/files' },
  { basePath: '/api', modulePath: '../routes/transfers' },
  { basePath: '/api', modulePath: '../routes/members' },
  { basePath: '/api', modulePath: '../routes/comments' },
  { basePath: '/api/activity', modulePath: '../routes/activity' },
  { basePath: '/api/handoff', modulePath: '../routes/handoff' },
  { basePath: '/api', modulePath: '../routes/office' },
  { basePath: '/wopi', modulePath: '../routes/wopi' },
  { basePath: '/api/sync', modulePath: '../routes/sync' },
  { basePath: '/api/devices', modulePath: '../routes/devices' },
  { basePath: '/api/storage', modulePath: '../routes/storage' },
  { basePath: '/api/shares', modulePath: '../routes/shares' },
  { basePath: '/api/public/shares', modulePath: '../routes/public_shares' },
  { basePath: '/api/vaults', modulePath: '../routes/vaults' },
  { basePath: '/api/photos', modulePath: '../routes/photos' },
  { basePath: '/api/imports', modulePath: '../routes/imports' },
  { basePath: '/api/external-storage', modulePath: '../routes/external_storage' },
  { basePath: '/api/ai', modulePath: '../routes/ai' },
  { basePath: '/api/neorouter', modulePath: '../routes/neorouter' },
  { basePath: '/api/notes', modulePath: '../routes/notes' },
  { basePath: '/api/signing', modulePath: '../routes/signing' },
  { basePath: '/api/public/sign', modulePath: '../routes/public_signing' },
  { basePath: '/api/account', modulePath: '../routes/connected_apps' },
  { basePath: '/api/oauth', modulePath: '../routes/oauth_companion' },
  { basePath: '/oauth', modulePath: '../routes/oauth' },
  { basePath: '/api/v1', modulePath: '../routes/agent_api' },
];

function registerApiRoutes(app) {
  app.get('/api/health', (req, res) => {
    const health = checkHealth();
    res.status(health.status === 'ok' ? 200 : 503).json(health);
  });
  app.get('/api/version', jsonHandler(() => getServerVersion()));
  for (const { basePath, modulePath } of ROUTE_REGISTRY) {
    app.use(basePath, require(modulePath));
  }
}

module.exports = { registerApiRoutes };
